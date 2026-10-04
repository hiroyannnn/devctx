package roadmap

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// IslandLoader abstracts islands loading for testing.
type IslandLoader interface {
	LoadIslands() (*model.IslandStore, error)
}

// Compile-time check that storage.Storage satisfies IslandLoader.
var _ IslandLoader = (*storage.Storage)(nil)

func (s *Server) handleAPIIslands(w http.ResponseWriter, r *http.Request) {
	// 他の API は手を付けず、新設のこの endpoint だけメソッドを絞る（将来の編集系 API と取り違えないため）
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 空でも null ではなく [] で返す（フロントが forEach できるように）。dangling な親は Resolved が "" にする
	resp := (&model.IslandStore{}).Resolved()
	if s.IslandLoader != nil {
		loaded, err := s.IslandLoader.LoadIslands()
		if err != nil {
			log.Printf("islands: failed to load: %v", err)
			http.Error(w, "failed to load islands", http.StatusInternalServerError)
			return
		}
		resp = loaded.Resolved()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("islands: failed to encode: %v", err)
	}
}

// handleAPIKnownRepos は island に付けられる repo（contexts / islands.yaml に現れる repo）の一覧を返す。
// UI がメニューを開いたときだけ取りに来る（5 秒のポーリングには含めない）。
// Why: 親を外した repo は、アクティブな context が無いと Mind Map から消える。この一覧から付け直せるようにする。
// 定義は編集 API の repo 検査と同じ model.KnownRepos（CLI とも同じ）。
func (s *Server) handleAPIKnownRepos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	store := &model.Store{}
	if s.StoreLoader != nil {
		loaded, err := s.StoreLoader.LoadStore()
		if err != nil {
			log.Printf("known-repos: failed to load store: %v", err)
			http.Error(w, "failed to load session data", http.StatusInternalServerError)
			return
		}
		store = loaded
	}
	islands := &model.IslandStore{}
	if s.IslandLoader != nil {
		loaded, err := s.IslandLoader.LoadIslands()
		if err != nil {
			log.Printf("known-repos: failed to load islands: %v", err)
			http.Error(w, "failed to load islands", http.StatusInternalServerError)
			return
		}
		islands = loaded
	}

	type repoEntry struct {
		Root  string `json:"root"`
		Label string `json:"label"`
	}
	repos := []repoEntry{} // 空でも null ではなく []
	for _, root := range model.KnownRepos(store, islands) {
		repos = append(repos, repoEntry{Root: root, Label: filepath.Base(root)})
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"repos": repos}); err != nil {
		log.Printf("known-repos: failed to encode: %v", err)
	}
}
