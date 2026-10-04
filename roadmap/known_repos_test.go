package roadmap

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

type knownReposPayload struct {
	Repos []struct {
		Root  string `json:"root"`
		Label string `json:"label"`
	} `json:"repos"`
}

func getKnownRepos(t *testing.T, s *Server) (*httptest.ResponseRecorder, knownReposPayload) {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleAPIKnownRepos(w, httptest.NewRequest("GET", "/api/islands/known-repos", nil))
	var resp knownReposPayload
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse: %v\n%s", err, w.Body.String())
		}
	}
	return w, resp
}

func TestHandleAPIKnownRepos_SortedDedupedWithLabels(t *testing.T) {
	s := &Server{
		StoreLoader: &mockStoreLoader{store: &model.Store{Contexts: []model.Context{
			{Name: "a", RepoRoot: "/r/web"},
			{Name: "b", RepoRoot: "/r/app"},
			{Name: "c", RepoRoot: "/r/web/"}, // 正規化で /r/web に畳まれる
			{Name: "d", Worktree: "/w/solo"},
		}}},
		IslandLoader: &mockIslandLoader{store: &model.IslandStore{
			Islands: []model.Island{{ID: "x", Name: "X", Parent: "repo:/r/only-parent"}},
			Repos:   []model.RepoNode{{Root: "/r/app", Parent: "island:x"}, {Root: "/r/only-yaml", Parent: "island:x"}},
		}},
	}
	w, resp := getKnownRepos(t, s)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status = %d, ct = %q", w.Code, w.Header().Get("Content-Type"))
	}
	var got [][2]string
	for _, r := range resp.Repos {
		got = append(got, [2]string{r.Root, r.Label})
	}
	want := [][2]string{
		{"/r/app", "app"}, {"/r/only-parent", "only-parent"}, {"/r/only-yaml", "only-yaml"}, {"/r/web", "web"}, {"/w/solo", "solo"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("repos = %v, want %v", got, want)
	}
}

func TestHandleAPIKnownRepos_EmptyIsArray(t *testing.T) {
	w, _ := getKnownRepos(t, &Server{})
	if got := w.Body.String(); got != "{\"repos\":[]}\n" {
		t.Errorf("body = %q", got)
	}
}

func TestHandleAPIKnownRepos_Errors(t *testing.T) {
	storeErr := &Server{StoreLoader: &mockStoreLoader{err: errors.New("corrupt")}}
	if w, _ := getKnownRepos(t, storeErr); w.Code != http.StatusInternalServerError {
		t.Errorf("store error: status = %d", w.Code)
	}
	islandErr := &Server{StoreLoader: &mockStoreLoader{store: &model.Store{}}, IslandLoader: &mockIslandLoader{err: errors.New("corrupt")}}
	if w, _ := getKnownRepos(t, islandErr); w.Code != http.StatusInternalServerError {
		t.Errorf("islands error: status = %d", w.Code)
	}
	w := httptest.NewRecorder()
	(&Server{}).handleAPIKnownRepos(w, httptest.NewRequest("POST", "/api/islands/known-repos", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET" {
		t.Errorf("POST: status = %d, allow = %q", w.Code, w.Header().Get("Allow"))
	}
}

func TestHandler_RoutesKnownRepos(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/islands/known-repos", nil)
	req.Host = "127.0.0.1:3333"
	w := httptest.NewRecorder()
	(&Server{Port: 3333}).Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
}
