package roadmap

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

type mockIslandLoader struct {
	store *model.IslandStore
	err   error
}

func (m *mockIslandLoader) LoadIslands() (*model.IslandStore, error) { return m.store, m.err }

type islandsPayload struct {
	Islands []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Parent string `json:"parent"`
	} `json:"islands"`
	Repos []struct {
		Root   string `json:"root"`
		Parent string `json:"parent"`
	} `json:"repos"`
}

func getIslands(t *testing.T, s *Server) (*httptest.ResponseRecorder, islandsPayload) {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleAPIIslands(w, httptest.NewRequest("GET", "/api/islands", nil))
	var resp islandsPayload
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse: %v\n%s", err, w.Body.String())
		}
	}
	return w, resp
}

func TestHandleAPIIslands_NilLoaderReturnsEmptyArrays(t *testing.T) {
	w, _ := getIslands(t, &Server{})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	// フロントが null を気にせず forEach できるよう、空でも [] で返す
	if got := strings.TrimSpace(w.Body.String()); got != `{"islands":[],"repos":[]}` {
		t.Errorf("body = %s", got)
	}
}

func TestHandleAPIIslands_ReturnsTreeWithDanglingParentsResolved(t *testing.T) {
	loader := &mockIslandLoader{store: &model.IslandStore{
		Islands: []model.Island{
			{ID: "hr", Name: "人事強化"},
			{ID: "m3", Name: "M3", Parent: "repo:/r/devctx"},
			{ID: "orphan", Name: "Orphan", Parent: "island:gone"},
		},
		Repos: []model.RepoNode{
			{Root: "/r/devctx", Parent: "island:hr"},
			{Root: "/r/stray", Parent: "island:gone"},
		},
	}}
	_, resp := getIslands(t, &Server{IslandLoader: loader})

	parents := map[string]string{}
	for _, i := range resp.Islands {
		parents["island:"+i.ID] = i.Parent
	}
	for _, r := range resp.Repos {
		parents["repo:"+r.Root] = r.Parent
	}
	want := map[string]string{
		"island:hr":      "",
		"island:m3":      "repo:/r/devctx",
		"island:orphan":  "",
		"repo:/r/devctx": "island:hr",
		"repo:/r/stray":  "",
	}
	for k, v := range want {
		if got, ok := parents[k]; !ok || got != v {
			t.Errorf("parent of %s = %q (present %v), want %q", k, got, ok, v)
		}
	}
	if resp.Islands[0].Name != "人事強化" {
		t.Errorf("name = %q", resp.Islands[0].Name)
	}
}

func TestHandleAPIIslands_NormalizesRepoRoots(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "app")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "app-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	key := model.NormalizePath(real)

	loader := &mockIslandLoader{store: &model.IslandStore{
		Islands: []model.Island{
			{ID: "hr", Name: "HR"},
			{ID: "m3", Name: "M3", Parent: "repo:" + link},
		},
		Repos: []model.RepoNode{{Root: link, Parent: "island:hr"}},
	}}
	_, resp := getIslands(t, &Server{IslandLoader: loader})

	if len(resp.Repos) != 1 || resp.Repos[0].Root != key || resp.Repos[0].Parent != "island:hr" {
		t.Errorf("repos = %+v, want root %q", resp.Repos, key)
	}
	for _, i := range resp.Islands {
		if i.ID == "m3" && i.Parent != "repo:"+key {
			t.Errorf("m3 parent = %q, want repo:%s", i.Parent, key)
		}
	}
}

func TestHandleAPIIslands_RejectsNonGET(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).handleAPIIslands(w, httptest.NewRequest("POST", "/api/islands", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET" {
		t.Errorf("Allow = %q", got)
	}
}

func TestHandleAPIIslands_LoaderError(t *testing.T) {
	w, _ := getIslands(t, &Server{IslandLoader: &mockIslandLoader{err: errors.New("boom")}})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// index.html は埋め込み静的ファイルでブラウザ無しでは動作を試せないため、
// island 描画に必要な取得・共通 helper・安定ノード ID が残っていることだけを守る。
func TestHandleIndex_WiresIslandsIntoMindMap(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).handleIndex(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()

	for _, want := range []string{
		"/api/islands",                               // refresh と同じ周期で取得する
		"function islandOverlay(",                    // tree / semantic の両 builder が共有する
		"islandOverlay(nodes, edges, cachedIslands)", // 両 builder から呼ばれる
		"JSON.stringify(islandsData)",                // island の編集が再描画の変化検知に入る
		"'session:' + session.name",                  // 連番ではなく安定した ID
		"repoNodeId(",                                // repo ノードの安定 ID
		"'more:' + ",                                 // more ノードも repo 単位の安定 ID
		"var MINDMAP_THEME = {",                      // 見た目の数値・色は 1 か所
		"function balancedLayout(",                   // 左右バランス配置の純関数
		"applyMindmapTheme(nodes);",                  // 両 builder で共通のテーマ適用
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index.html does not contain %q", want)
		}
	}
	for _, gone := range []string{"'proj:' + (++nid)", "'sess:' + (++nid)"} {
		if strings.Contains(body, gone) {
			t.Errorf("index.html still uses sequential id %q", gone)
		}
	}
}
