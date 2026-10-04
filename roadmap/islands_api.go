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

// islandsResponse は /api/islands の応答。Resolved + 正規化済みで、フロントは親をそのまま辿れる。
type islandsResponse struct {
	Islands []islandJSON `json:"islands"`
	Repos   []repoJSON   `json:"repos"`
}

type islandJSON struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"parent"`
}

type repoJSON struct {
	Root   string `json:"root"`
	Parent string `json:"parent"`
}

// normalizeRepoRef は repo ref のパスを NormalizePath にそろえる。island ref や "" はそのまま。
func normalizeRepoRef(ref string) string {
	if kind, v, err := model.ParseRef(ref); err == nil && kind == model.RefRepo {
		return model.RepoRef(model.NormalizePath(v))
	}
	return ref
}

func (s *Server) handleAPIIslands(w http.ResponseWriter, r *http.Request) {
	// 他の API は手を付けず、新設のこの endpoint だけメソッドを絞る（将来の編集系 API と取り違えないため）
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := islandsResponse{Islands: []islandJSON{}, Repos: []repoJSON{}}
	if s.IslandLoader != nil {
		loaded, err := s.IslandLoader.LoadIslands()
		if err != nil {
			log.Printf("islands: failed to load: %v", err)
			http.Error(w, "failed to load islands", http.StatusInternalServerError)
			return
		}
		// contexts のグルーピング（RepoKey）と同じ正規化をかけてから dangling を解決する。
		// 手編集で symlink 経由のパスが書かれていても、repo ノードと突き合うようにするため。
		norm := model.IslandStore{}
		seen := map[string]bool{}
		for _, is := range loaded.Islands {
			is.Parent = normalizeRepoRef(is.Parent)
			norm.Islands = append(norm.Islands, is)
		}
		for _, rn := range loaded.Repos {
			rn.Root = model.NormalizePath(rn.Root)
			rn.Parent = normalizeRepoRef(rn.Parent)
			if seen[rn.Root] {
				continue
			}
			seen[rn.Root] = true
			norm.Repos = append(norm.Repos, rn)
		}
		resolved := norm.Resolved()
		for _, is := range resolved.Islands {
			resp.Islands = append(resp.Islands, islandJSON{ID: is.ID, Name: is.Name, Parent: is.Parent})
		}
		for _, rn := range resolved.Repos {
			resp.Repos = append(resp.Repos, repoJSON{Root: rn.Root, Parent: rn.Parent})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("islands: failed to encode: %v", err)
	}
}
