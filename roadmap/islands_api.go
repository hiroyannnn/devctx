package roadmap

import (
	"encoding/json"
	"log"
	"net/http"

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
