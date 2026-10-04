package roadmap

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// symlink 経由の RepoRoot と実パスの RepoRoot、RepoRoot 空で Worktree だけの context が同じ repo に束ねられること。
func repoKeyFixture(t *testing.T) (*model.Store, string) {
	t.Helper()
	dir := t.TempDir()
	real := filepath.Join(dir, "app")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "app-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	store := &model.Store{Contexts: []model.Context{
		{Name: "plain", Status: model.StatusInProgress, RepoRoot: real, Worktree: real, CreatedAt: now, LastSeen: now},
		{Name: "linked", Status: model.StatusInProgress, RepoRoot: link, Worktree: link, CreatedAt: now, LastSeen: now},
		{Name: "wt-only", Status: model.StatusInProgress, Worktree: link, CreatedAt: now, LastSeen: now},
	}}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	return store, resolved
}

func TestHandleAPIRoadmapMap_GroupsSymlinkedRepoRoots(t *testing.T) {
	store, key := repoKeyFixture(t)
	server := &Server{StoreLoader: &mockStoreLoader{store: store}}

	w := httptest.NewRecorder()
	server.handleAPIRoadmapMap(w, httptest.NewRequest("GET", "/api/roadmap-map", nil))

	var groups []ProjectGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1: %+v", len(groups), groups)
	}
	if groups[0].RepoRoot != key || groups[0].Name != "app" || len(groups[0].Sessions) != 3 {
		t.Errorf("group = %+v, want root %q with 3 sessions", groups[0], key)
	}
}

func TestHandleAPIRoadmapGraph_GroupsSymlinkedRepoRoots(t *testing.T) {
	store, key := repoKeyFixture(t)
	server := &Server{StoreLoader: &mockStoreLoader{store: store}}

	w := httptest.NewRecorder()
	server.handleAPIRoadmapGraph(w, httptest.NewRequest("GET", "/api/roadmap-graph", nil))

	var groups []ProjectGraphGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1: %+v", len(groups), groups)
	}
	if groups[0].RepoRoot != key || len(groups[0].Sessions) != 3 {
		t.Errorf("group = %+v, want root %q with 3 sessions", groups[0], key)
	}
}

func TestHandleAPIRoadmapGraph_EmptyKeyStaysUngrouped(t *testing.T) {
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	store := &model.Store{Contexts: []model.Context{
		{Name: "orphan", Status: model.StatusInProgress, CreatedAt: now, LastSeen: now},
	}}
	server := &Server{StoreLoader: &mockStoreLoader{store: store}}

	w := httptest.NewRecorder()
	server.handleAPIRoadmapGraph(w, httptest.NewRequest("GET", "/api/roadmap-graph", nil))

	var groups []ProjectGraphGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].RepoRoot != "__ungrouped__" || groups[0].Name != "Other" {
		t.Errorf("groups = %+v", groups)
	}
}
