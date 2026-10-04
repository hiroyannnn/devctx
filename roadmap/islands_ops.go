package roadmap

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// IslandUpdater は islands.yaml をロック内で読み・更新・保存する。
type IslandUpdater interface {
	UpdateIslands(fn func(*model.IslandStore) error) error
}

// Compile-time check that storage.Storage satisfies IslandUpdater.
var _ IslandUpdater = (*storage.Storage)(nil)

const (
	maxOpsBodyBytes   = 64 << 10
	maxIslandNameRune = 80
)

// islandOp は /api/islands/ops の入力。op ごとに使うフィールドが違う。
type islandOp struct {
	Op       string   `json:"op"`
	Name     string   `json:"name"`
	Parent   string   `json:"parent"`
	Ref      string   `json:"ref"`
	Child    string   `json:"child"`
	Children []string `json:"children"`
}

// opError は HTTP 応答に対応づけた失敗。ロック内（fn）でも本文の検証でも、失敗はこれ 1 本で返す。
// Why: storage の I/O 失敗（500）と入力・木の不整合（4xx）を、返ってきた error の型だけで区別するため。
type opError struct {
	status int
	body   map[string]any
}

func (e *opError) Error() string { return fmt.Sprint(e.body["error"]) }

func newOpError(status int, body map[string]any) *opError {
	return &opError{status: status, body: body}
}

func badRequest(format string, args ...any) *opError {
	return newOpError(http.StatusBadRequest, map[string]any{"error": fmt.Sprintf(format, args...)})
}

func conflict(body map[string]any) *opError { return newOpError(http.StatusConflict, body) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("islands: failed to encode: %v", err)
	}
}

func writeOpError(w http.ResponseWriter, oe *opError) { writeJSON(w, oe.status, oe.body) }

func plainError(status int, msg string) *opError {
	return newOpError(status, map[string]any{"error": msg})
}

// decodeIslandOp は本文を islandOp にして、op の種別まで検査する。
func decodeIslandOp(w http.ResponseWriter, r *http.Request) (islandOp, *opError) {
	var req islandOp
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOpsBodyBytes))
	// 未知フィールドを黙って捨てると、UI と server のずれ（綴り違い等）に気づけない
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return req, plainError(http.StatusRequestEntityTooLarge, "request body too large")
		}
		return req, plainError(http.StatusBadRequest, "malformed JSON body")
	}
	switch req.Op {
	case "add", "rename", "remove", "attach", "detach":
		return req, nil
	}
	return req, plainError(http.StatusBadRequest, fmt.Sprintf("unknown op %q", req.Op))
}

// handleAPIIslandOps は Mind Map の island 編集（add / rename / remove / attach / detach）を受ける。
// 入力は境界でここだけが検証し、木の整合（存在・循環・子の変化）は model に任せる。
// 成功時は更新後の木（GET /api/islands と同じ形）も返す。
// Why: UI が操作直後に /api/islands を取り直さず、この応答の木をそのまま描画に使えるようにするため。
func (s *Server) handleAPIIslandOps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeOpError(w, plainError(http.StatusMethodNotAllowed, "method not allowed"))
		return
	}
	if s.IslandUpdater == nil {
		writeOpError(w, plainError(http.StatusServiceUnavailable, "island editing is not available on this server"))
		return
	}
	req, oe := decodeIslandOp(w, r)
	if oe != nil {
		writeOpError(w, oe)
		return
	}

	var ref string
	var tree model.IslandStore
	err := s.IslandUpdater.UpdateIslands(func(is *model.IslandStore) error {
		var err error
		if ref, err = applyIslandOp(is, s.knownRepos(is), req); err != nil {
			return err
		}
		tree = is.Resolved()
		return nil
	})
	if err != nil {
		var oe *opError
		if errors.As(err, &oe) {
			writeOpError(w, oe)
			return
		}
		log.Printf("islands: failed to update: %v", err)
		writeOpError(w, plainError(http.StatusInternalServerError, "failed to update islands"))
		return
	}
	resp := map[string]any{"ok": true, "islands": tree}
	if ref != "" {
		resp["ref"] = ref
	}
	writeJSON(w, http.StatusOK, resp)
}

// knownRepoList は contexts と is から既知 repo（昇順・重複なし）を求める。contexts の読み込みはここで行う。
// Why: 編集 API の repo 検査と、UI へ返す known-repos 一覧が同じ定義（model.KnownRepos）を通るようにする。
func (s *Server) knownRepoList(is *model.IslandStore) ([]string, error) {
	store := &model.Store{}
	if s.StoreLoader != nil {
		loaded, err := s.StoreLoader.LoadStore()
		if err != nil {
			return nil, err
		}
		store = loaded
	}
	return model.KnownRepos(store, is), nil
}

// knownRepos は既知 repo の集合を返す関数を作る。contexts の読み込みは repo ref を検査するときに 1 回だけ行う。
// Why: island だけの操作（rename / remove 等）が contexts.yaml の不調や読み込みコストに巻き込まれないため。
func (s *Server) knownRepos(is *model.IslandStore) func() (map[string]bool, error) {
	return sync.OnceValues(func() (map[string]bool, error) {
		roots, err := s.knownRepoList(is)
		if err != nil {
			return nil, err
		}
		known := map[string]bool{}
		for _, root := range roots {
			known[root] = true
		}
		return known, nil
	})
}

// applyIslandOp は 1 つの op を is に適用し、作った island の ref（add のみ）を返す。失敗なら呼び出し側が保存しない。
func applyIslandOp(is *model.IslandStore, known func() (map[string]bool, error), req islandOp) (string, error) {
	// canon は ref を保存形にする。repo は既知のものだけ受け付ける（CLI の resolver が未登録 repo を
	// ディスク上の存在で通すのと違い、Web は任意パスを木に書き込める口にしない）。
	canon := func(ref string) (string, error) {
		kind, v, err := model.ParseRef(ref)
		if err != nil {
			return "", badRequest("%v", err)
		}
		if kind == model.RefIsland {
			return ref, nil
		}
		repos, err := known()
		if err != nil {
			return "", err // 入力の誤りではないので opError にしない（500）
		}
		root := model.NormalizePath(v)
		if !repos[root] {
			return "", badRequest("unknown repo %q", root)
		}
		return model.RepoRef(root), nil
	}

	switch req.Op {
	case "add":
		if err := validateIslandName(req.Name); err != nil {
			return "", err
		}
		parent := ""
		if req.Parent != "" {
			var err error
			if parent, err = canon(req.Parent); err != nil {
				return "", err
			}
		}
		// id は名前から作る（衝突時は model が連番を付ける）ので、id 衝突の応答は無い
		added, err := is.AddIsland(req.Name, "", parent)
		if err != nil {
			return "", badRequest("%v", err)
		}
		return model.IslandRef(added.ID), nil

	case "rename":
		id, err := model.ParseIslandRef(req.Ref)
		if err != nil {
			return "", badRequest("%v", err)
		}
		if err := validateIslandName(req.Name); err != nil {
			return "", err
		}
		return "", badRequestIf(is.RenameIsland(id, req.Name))

	case "remove":
		id, err := model.ParseIslandRef(req.Ref)
		if err != nil {
			return "", badRequest("%v", err)
		}
		err = is.RemoveIslandExpecting(id, req.Children)
		var changed *model.ChildrenChangedError
		if errors.As(err, &changed) {
			current := append([]string(nil), changed.Current...)
			sort.Strings(current)
			return "", conflict(map[string]any{"error": "children changed", "children": current})
		}
		return "", badRequestIf(err)

	case "attach":
		child, err := canon(req.Child)
		if err != nil {
			return "", err
		}
		parent, err := canon(req.Parent)
		if err != nil {
			return "", err
		}
		return "", badRequestIf(is.SetParent(child, parent))

	case "detach":
		child, err := canon(req.Child)
		if err != nil {
			return "", err
		}
		return "", badRequestIf(is.Detach(child))
	}
	return "", nil
}

// badRequestIf は model の検証エラー（存在・循環など）を 400 にする。nil はそのまま nil。
func badRequestIf(err error) error {
	if err == nil {
		return nil
	}
	return badRequest("%v", err)
}

// validateIslandName は境界で見るべきもの（長さ・制御文字）だけを検査する。
// trim と空チェックは model（AddIsland / RenameIsland）が持つので、ここでは重ねない。
func validateIslandName(name string) error {
	if utf8.RuneCountInString(name) > maxIslandNameRune {
		return badRequest("island name is too long (max %d characters)", maxIslandNameRune)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return badRequest("island name must not contain control characters")
		}
	}
	return nil
}
