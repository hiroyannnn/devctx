package roadmap

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// ContextUpdater は contexts.yaml をロック内で読み・更新・保存する。
type ContextUpdater interface {
	UpdateStore(fn func(*model.Store) error) error
}

// Compile-time check that storage.Storage satisfies ContextUpdater.
var _ ContextUpdater = (*storage.Storage)(nil)

// sessionOp は /api/sessions/ops の入力。
type sessionOp struct {
	Op   string `json:"op"`
	Name string `json:"name"` // context 名
	Task string `json:"task"` // link のみ。"island:t<n>"
}

// handleAPISessionOps は Mind Map からのセッション操作（link / unlink）を受ける。
// 紐付けは context 側の項目だけを書き換え、タスクの完了状態や islands.yaml には触れない。
func (s *Server) handleAPISessionOps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeOpError(w, plainError(http.StatusMethodNotAllowed, "method not allowed"))
		return
	}
	if s.ContextUpdater == nil {
		writeOpError(w, plainError(http.StatusServiceUnavailable, "session editing is not available on this server"))
		return
	}

	var req sessionOp
	if oe := decodeOpBody(w, r, &req); oe != nil {
		writeOpError(w, oe)
		return
	}
	if oe := s.checkSessionOp(req); oe != nil {
		writeOpError(w, oe)
		return
	}

	now := time.Now()
	err := s.ContextUpdater.UpdateStore(func(store *model.Store) error {
		ctx := store.FindByName(req.Name)
		if ctx == nil {
			return newOpError(http.StatusNotFound, map[string]any{"error": "context not found"})
		}
		if req.Op == "link" {
			model.LinkTask(ctx, req.Task, now)
		} else {
			model.UnlinkTask(ctx)
		}
		return nil
	})
	if err != nil {
		var oe *opError
		if errors.As(err, &oe) {
			writeOpError(w, oe)
			return
		}
		log.Printf("sessions: failed to update: %v", err)
		writeOpError(w, plainError(http.StatusInternalServerError, "failed to update sessions"))
		return
	}
	// task_ref を載せた /api/roadmap の 5 秒キャッシュを捨てる。残すと直後の再描画で操作が戻って見える
	s.invalidateRoadmapCache()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// checkSessionOp は境界で見るべきこと（op・名前・タスクの実在）を検査する。
// Why islands をロックの外で読む: UpdateStore の中で islands のロックを取る入れ子を作らない。
// 検査後にタスクが消えても、描画側が「削除済み」として扱うので整合は崩れない。
func (s *Server) checkSessionOp(req sessionOp) *opError {
	if strings.TrimSpace(req.Name) == "" {
		return plainError(http.StatusBadRequest, "name is required")
	}
	switch req.Op {
	case "unlink":
		return nil
	case "link":
	default:
		return badRequest("unknown op %q", req.Op)
	}
	id, err := model.ParseIslandRef(req.Task)
	if err != nil {
		return badRequest("task must be island:t<n>: %v", err)
	}
	if s.IslandLoader == nil {
		return plainError(http.StatusServiceUnavailable, "task linking is not available on this server")
	}
	is, err := s.IslandLoader.LoadIslands()
	if err != nil {
		log.Printf("sessions: failed to load islands: %v", err)
		return plainError(http.StatusInternalServerError, "failed to load islands")
	}
	if is.FindTask(id) == nil {
		return badRequest("task %q not found", req.Task)
	}
	return nil
}

// taskLabel は task_ref の表示名を返す。紐付けが無ければ ""、タスクが消えていれば「削除済み tN」。
// islands を読めなかった（nil）ときは ""。Why: 読めないだけで「削除済み」と出すと、実在するタスクを消えたと誤認させる。
func taskLabel(is *model.IslandStore, ref string) string {
	if ref == "" || is == nil {
		return ""
	}
	id, err := model.ParseIslandRef(ref)
	if err != nil {
		return "削除済み " + ref
	}
	if i := is.FindTask(id); i != nil {
		return i.Name
	}
	return "削除済み " + id
}
