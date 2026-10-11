package roadmap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// fakeContextUpdater は複製した store に fn を適用し、成功時だけ反映する（storage と同じ「失敗なら保存しない」）。
type fakeContextUpdater struct {
	store *model.Store
	calls int
}

func (f *fakeContextUpdater) UpdateStore(fn func(*model.Store) error) error {
	f.calls++
	cp := &model.Store{Contexts: append([]model.Context(nil), f.store.Contexts...)}
	if err := fn(cp); err != nil {
		if err == storage.ErrSkipSave {
			return nil
		}
		return err
	}
	f.store = cp
	return nil
}

func (f *fakeContextUpdater) LoadStore() (*model.Store, error) { return f.store, nil }

func linkTestIslands() *model.IslandStore {
	return &model.IslandStore{
		Islands: []model.Island{
			{ID: "hr", Name: "HR"},
			{ID: "t1", Name: "面接の質問を作る", Parent: "island:hr", Kind: "task"},
		},
		TaskSeq: 1,
	}
}

func newSessionOpsServer() (*Server, *fakeContextUpdater, *model.IslandStore) {
	is := linkTestIslands()
	up := &fakeContextUpdater{store: &model.Store{Contexts: []model.Context{
		{Name: "a", SessionID: "s1", RepoRoot: "/r/app"},
		{Name: "b", SessionID: "s2", RepoRoot: "/r/app"},
	}}}
	return &Server{
		ContextUpdater: up,
		IslandLoader:   &mockIslandLoader{store: is},
		StoreLoader:    up,
	}, up, is
}

func postSessionOps(t *testing.T, s *Server, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/sessions/ops", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.handleAPISessionOps(w, req)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, w.Body.String())
	}
	return w, resp
}

func TestSessionOps_NilUpdaterIs503(t *testing.T) {
	w, _ := postSessionOps(t, &Server{}, `{"op":"unlink","name":"a"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d", w.Code)
	}
}

func TestSessionOps_MethodNotAllowed(t *testing.T) {
	s, _, _ := newSessionOpsServer()
	w := httptest.NewRecorder()
	s.handleAPISessionOps(w, httptest.NewRequest("GET", "/api/sessions/ops", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "POST" {
		t.Errorf("status = %d allow = %q", w.Code, w.Header().Get("Allow"))
	}
}

func TestSessionOps_LinkAndUnlink(t *testing.T) {
	s, up, _ := newSessionOpsServer()
	w, resp := postSessionOps(t, s, `{"op":"link","name":"a","task":"island:t1"}`)
	if w.Code != http.StatusOK || resp["ok"] != true {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	c := up.store.FindByName("a")
	if c.TaskRef != "island:t1" || c.TaskLinkSource != "manual" || c.TaskLinkSession != "s1" || c.TaskLinkAt.IsZero() {
		t.Fatalf("link = %+v", c)
	}
	if up.store.FindByName("b").TaskRef != "" {
		t.Error("other contexts must not change")
	}

	w, resp = postSessionOps(t, s, `{"op":"unlink","name":"a"}`)
	if w.Code != http.StatusOK || resp["ok"] != true {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
	if c := up.store.FindByName("a"); c.TaskRef != "" || c.TaskLinkSource != "" {
		t.Fatalf("unlink = %+v", c)
	}
}

func TestSessionOps_Errors(t *testing.T) {
	tests := []struct {
		name, body string
		want       int
	}{
		{"malformed", `{`, http.StatusBadRequest},
		{"unknown field", `{"op":"unlink","name":"a","x":1}`, http.StatusBadRequest},
		{"unknown op", `{"op":"nope","name":"a"}`, http.StatusBadRequest},
		{"link without task", `{"op":"link","name":"a"}`, http.StatusBadRequest},
		{"task is a theme island", `{"op":"link","name":"a","task":"island:hr"}`, http.StatusBadRequest},
		{"task missing", `{"op":"link","name":"a","task":"island:t9"}`, http.StatusBadRequest},
		{"repo ref", `{"op":"link","name":"a","task":"repo:/r/app"}`, http.StatusBadRequest},
		{"unknown context", `{"op":"link","name":"zzz","task":"island:t1"}`, http.StatusNotFound},
		{"unlink unknown context", `{"op":"unlink","name":"zzz"}`, http.StatusNotFound},
		{"empty name", `{"op":"unlink","name":""}`, http.StatusBadRequest},
		{"too large", `{"op":"unlink","name":"` + strings.Repeat("a", 70<<10) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, up, _ := newSessionOpsServer()
			w, resp := postSessionOps(t, s, tt.body)
			if w.Code != tt.want || errorOf(resp) == "" {
				t.Errorf("status = %d body = %s, want %d", w.Code, w.Body.String(), tt.want)
			}
			if up.store.FindByName("a").TaskRef != "" {
				t.Error("nothing must be saved on error")
			}
		})
	}
}

func TestSessionOps_LinkWithoutIslandLoaderIs503(t *testing.T) {
	s, _, _ := newSessionOpsServer()
	s.IslandLoader = nil
	w, _ := postSessionOps(t, s, `{"op":"link","name":"a","task":"island:t1"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d", w.Code)
	}
}

func TestSessionOps_RegisteredUnderGuard(t *testing.T) {
	s, _, _ := newSessionOpsServer()
	s.Port = 3333
	h := s.Handler()
	body := `{"op":"unlink","name":"a"}`

	req := httptest.NewRequest("POST", "http://evil.example/api/sessions/ops", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("foreign host status = %d, want 403", w.Code)
	}

	req = httptest.NewRequest("POST", "http://127.0.0.1:3333/api/sessions/ops", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden { // Origin が無い更新系は guard が落とす
		t.Errorf("no-origin status = %d, want 403", w.Code)
	}

	req = httptest.NewRequest("POST", "http://127.0.0.1:3333/api/sessions/ops", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:3333")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("local status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestTaskLabel(t *testing.T) {
	is := linkTestIslands()
	for _, tt := range []struct{ ref, want string }{
		{"", ""},
		{"island:t1", "面接の質問を作る"},
		{"island:t9", "削除済み t9"},
		{"island:hr", "削除済み hr"}, // テーマ island はタスクではない
	} {
		if got := taskLabel(is, tt.ref); got != tt.want {
			t.Errorf("taskLabel(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
	if got := taskLabel(nil, "island:t1"); got != "" {
		t.Errorf("islands unavailable should give no label, got %q", got)
	}
}

func linkedStore() *model.Store {
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	return &model.Store{Contexts: []model.Context{
		{Name: "a", RepoRoot: "/r/app", Worktree: "/r/app", Status: model.StatusInProgress, CreatedAt: now, LastSeen: now, TaskRef: "island:t1"},
		{Name: "b", RepoRoot: "/r/app", Worktree: "/r/app-b", Status: model.StatusInProgress, CreatedAt: now, LastSeen: now, TaskRef: "island:t9"},
		{Name: "c", RepoRoot: "/r/app", Worktree: "/r/app-c", Status: model.StatusInProgress, CreatedAt: now, LastSeen: now},
	}}
}

func linkedServer() *Server {
	return &Server{
		StoreLoader:  &mockStoreLoader{store: linkedStore()},
		IslandLoader: &mockIslandLoader{store: linkTestIslands()},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{}},
			Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}
}

func TestAPIRoadmap_IncludesTaskRefAndLabel(t *testing.T) {
	s := linkedServer()
	w := httptest.NewRecorder()
	s.handleAPIRoadmap(w, httptest.NewRequest("GET", "/api/roadmap", nil))
	var entries []RoadmapEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	got := map[string][2]string{}
	for _, e := range entries {
		got[e.Name] = [2]string{e.TaskRef, e.TaskLabel}
	}
	want := map[string][2]string{"a": {"island:t1", "面接の質問を作る"}, "b": {"island:t9", "削除済み t9"}, "c": {"", ""}}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if strings.Contains(w.Body.String(), `"name":"c"`) && strings.Count(w.Body.String(), "task_ref") != 2 {
		t.Errorf("unlinked sessions must omit task_ref: %s", w.Body.String())
	}
}

func TestAPIRoadmapMap_IncludesTaskRefAndLabel(t *testing.T) {
	s := linkedServer()
	w := httptest.NewRecorder()
	s.handleAPIRoadmapMap(w, httptest.NewRequest("GET", "/api/roadmap-map", nil))
	var groups []ProjectGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Sessions[0].TaskRef != "island:t1" || groups[0].Sessions[0].TaskLabel != "面接の質問を作る" {
		t.Fatalf("groups = %s", w.Body.String())
	}
}

func TestAPIRoadmapGraph_IncludesTaskRefAndLabel(t *testing.T) {
	s := linkedServer()
	w := httptest.NewRecorder()
	s.handleAPIRoadmapGraph(w, httptest.NewRequest("GET", "/api/roadmap-graph", nil))
	var groups []ProjectGraphGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	got := map[string][2]string{}
	for _, sg := range groups[0].Sessions {
		got[sg.Name] = [2]string{sg.TaskRef, sg.TaskLabel}
	}
	if got["a"] != [2]string{"island:t1", "面接の質問を作る"} || got["b"] != [2]string{"island:t9", "削除済み t9"} || got["c"] != [2]string{"", ""} {
		t.Fatalf("got %v", got)
	}
}

func TestSessionOps_InvalidatesRoadmapCache(t *testing.T) {
	s, _, _ := newSessionOpsServer()
	s.Scanner = &Scanner{Git: &mockGitRunner{results: map[string]mockResult{}}, Gh: &mockGhRunner{results: map[string]mockResult{}}}
	w := httptest.NewRecorder()
	s.handleAPIRoadmap(w, httptest.NewRequest("GET", "/api/roadmap", nil))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	if postW, _ := postSessionOps(t, s, `{"op":"link","name":"a","task":"island:t1"}`); postW.Code != http.StatusOK {
		t.Fatal(postW.Body.String())
	}
	w = httptest.NewRecorder()
	s.handleAPIRoadmap(w, httptest.NewRequest("GET", "/api/roadmap", nil))
	if !strings.Contains(w.Body.String(), `"task_ref":"island:t1"`) {
		t.Errorf("stale cache served after link: %s", w.Body.String())
	}
}

// タスクの改名・削除も task_label に出るので、islands の編集でも応答キャッシュを捨てる。
func TestIslandOps_InvalidatesRoadmapCache(t *testing.T) {
	s, _, _ := newSessionOpsServer()
	s.Scanner = &Scanner{Git: &mockGitRunner{results: map[string]mockResult{}}, Gh: &mockGhRunner{results: map[string]mockResult{}}}
	iu := &fakeIslandUpdater{store: linkTestIslands()}
	s.IslandUpdater = iu
	s.IslandLoader = &mockIslandLoader{store: iu.store}
	w := httptest.NewRecorder()
	s.handleAPIRoadmap(w, httptest.NewRequest("GET", "/api/roadmap", nil))
	s.cacheMu.RLock()
	cached := s.cachedResult != nil
	s.cacheMu.RUnlock()
	if !cached {
		t.Fatal("precondition: response should be cached")
	}
	if rec, _ := postOps(t, s, `{"op":"rename","ref":"island:t1","name":"新しい名前"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	if s.cachedResult != nil {
		t.Error("island edit must drop the cached /api/roadmap response")
	}
}
