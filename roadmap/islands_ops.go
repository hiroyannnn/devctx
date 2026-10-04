package roadmap

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
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

// opError は UpdateIslands の fn から返す、HTTP 応答に対応づけた失敗。
// Why: storage の I/O 失敗（500）と入力・木の不整合（4xx）を、fn から返る error だけで区別するため。
type opError struct {
	status int
	body   map[string]any
}

func (e *opError) Error() string { return fmt.Sprint(e.body["error"]) }

func badRequest(format string, args ...any) *opError {
	return &opError{status: http.StatusBadRequest, body: map[string]any{"error": fmt.Sprintf(format, args...)}}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("islands: failed to encode: %v", err)
	}
}

func writeOpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// handleAPIIslandOps は Mind Map の island 編集（add / rename / remove / attach / detach）を受ける。
// 入力は境界でここだけが検証し、木の整合（存在・循環）は model に任せる。
func (s *Server) handleAPIIslandOps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeOpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.IslandUpdater == nil {
		writeOpError(w, http.StatusServiceUnavailable, "island editing is not available on this server")
		return
	}

	var req islandOp
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxOpsBodyBytes))
	// 未知フィールドを黙って捨てると、UI と server のずれ（綴り違い等）に気づけない
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeOpError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeOpError(w, http.StatusBadRequest, "malformed JSON body")
		return
	}
	switch req.Op {
	case "add", "rename", "remove", "attach", "detach":
	default:
		writeOpError(w, http.StatusBadRequest, fmt.Sprintf("unknown op %q", req.Op))
		return
	}

	// 既知 repo の判定に contexts が要る。ロックの外で読む（contexts.yaml は別ファイル・別ロック）
	store := &model.Store{}
	if s.StoreLoader != nil {
		loaded, err := s.StoreLoader.LoadStore()
		if err != nil {
			log.Printf("islands: failed to load store: %v", err)
			writeOpError(w, http.StatusInternalServerError, "failed to load session data")
			return
		}
		store = loaded
	}

	result := map[string]any{"ok": true}
	err := s.IslandUpdater.UpdateIslands(func(is *model.IslandStore) error {
		return applyIslandOp(is, store, req, result)
	})
	if err != nil {
		var oe *opError
		if errors.As(err, &oe) {
			writeJSON(w, oe.status, oe.body)
			return
		}
		log.Printf("islands: failed to update: %v", err)
		writeOpError(w, http.StatusInternalServerError, "failed to update islands")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// applyIslandOp は 1 つの op を is に適用する。失敗（opError）なら呼び出し側が保存しない。
func applyIslandOp(is *model.IslandStore, store *model.Store, req islandOp, result map[string]any) error {
	known := map[string]bool{}
	for _, root := range model.KnownRepos(store, is) {
		known[root] = true
	}
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
		root := model.NormalizePath(v)
		if !known[root] {
			return "", badRequest("unknown repo %q", root)
		}
		return model.RepoRef(root), nil
	}
	islandID := func(ref string) (string, error) {
		kind, v, err := model.ParseRef(ref)
		if err != nil {
			return "", badRequest("%v", err)
		}
		if kind != model.RefIsland {
			return "", badRequest("%s is not an island", ref)
		}
		return v, nil
	}

	switch req.Op {
	case "add":
		name, err := validateIslandName(req.Name)
		if err != nil {
			return err
		}
		parent := ""
		if req.Parent != "" {
			if parent, err = canon(req.Parent); err != nil {
				return err
			}
		}
		added, err := is.AddIsland(name, "", parent)
		var dup *model.IDExistsError
		switch {
		case errors.As(err, &dup):
			return &opError{status: http.StatusConflict, body: map[string]any{
				"error": fmt.Sprintf("island id %q already exists; use a different name", dup.ID),
			}}
		case err != nil:
			return badRequest("%v", err)
		}
		result["ref"] = model.IslandRef(added.ID)

	case "rename":
		id, err := islandID(req.Ref)
		if err != nil {
			return err
		}
		name, err := validateIslandName(req.Name)
		if err != nil {
			return err
		}
		if err := is.RenameIsland(id, name); err != nil {
			return badRequest("%v", err)
		}

	case "remove":
		id, err := islandID(req.Ref)
		if err != nil {
			return err
		}
		current := is.Children(model.IslandRef(id))
		if len(current) == 0 {
			if err := is.RemoveIsland(id, false); err != nil {
				return badRequest("%v", err)
			}
			return nil
		}
		// UI が見せた子と今の子が違えば、見ていない子を黙って付け替えない（別タブ・CLI の同時編集対策）
		if !sameRefSet(current, req.Children) {
			sort.Strings(current)
			return &opError{status: http.StatusConflict, body: map[string]any{"error": "children changed", "children": current}}
		}
		if err := is.RemoveIsland(id, true); err != nil {
			return badRequest("%v", err)
		}

	case "attach":
		child, err := canon(req.Child)
		if err != nil {
			return err
		}
		parent, err := canon(req.Parent)
		if err != nil {
			return err
		}
		if err := is.SetParent(child, parent); err != nil {
			return badRequest("%v", err)
		}

	case "detach":
		child, err := canon(req.Child)
		if err != nil {
			return err
		}
		if err := is.Detach(child); err != nil {
			return badRequest("%v", err)
		}
	}
	return nil
}

func sameRefSet(a, b []string) bool {
	set := map[string]bool{}
	for _, r := range a {
		set[r] = true
	}
	other := map[string]bool{}
	for _, r := range b {
		other[r] = true
	}
	if len(set) != len(other) {
		return false
	}
	for r := range set {
		if !other[r] {
			return false
		}
	}
	return true
}

func validateIslandName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", badRequest("island name is empty")
	}
	if utf8.RuneCountInString(name) > maxIslandNameRune {
		return "", badRequest("island name is too long (max %d characters)", maxIslandNameRune)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", badRequest("island name must not contain control characters")
		}
	}
	return name, nil
}
