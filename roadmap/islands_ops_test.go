package roadmap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// fakeIslandUpdater はメモリ上の IslandStore に対して、複製に fn を適用し成功時だけ反映する（storage と同じ「失敗なら保存しない」）。
type fakeIslandUpdater struct {
	store *model.IslandStore
	calls int
}

func (f *fakeIslandUpdater) UpdateIslands(fn func(*model.IslandStore) error) error {
	f.calls++
	cp := &model.IslandStore{
		Islands: append([]model.Island(nil), f.store.Islands...),
		Repos:   append([]model.RepoNode(nil), f.store.Repos...),
	}
	if err := fn(cp); err != nil {
		if err == storage.ErrSkipSave {
			return nil
		}
		return err
	}
	f.store = cp
	return nil
}

func newOpsServer(is *model.IslandStore) (*Server, *fakeIslandUpdater) {
	up := &fakeIslandUpdater{store: is}
	return &Server{
		Port:          3333,
		IslandUpdater: up,
		StoreLoader: &mockStoreLoader{store: &model.Store{Contexts: []model.Context{
			{Name: "a", RepoRoot: "/r/app"},
			{Name: "b", RepoRoot: "/r/web"},
		}}},
	}, up
}

func postOps(t *testing.T, s *Server, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/islands/ops", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleAPIIslandOps(w, req)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, w.Body.String())
	}
	return w, resp
}

func errorOf(resp map[string]any) string {
	s, _ := resp["error"].(string)
	return s
}

func TestIslandOps_NilUpdaterIs503(t *testing.T) {
	w, resp := postOps(t, &Server{}, `{"op":"add","name":"x"}`)
	if w.Code != http.StatusServiceUnavailable || errorOf(resp) == "" {
		t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestIslandOps_MethodNotAllowed(t *testing.T) {
	s, _ := newOpsServer(&model.IslandStore{})
	w := httptest.NewRecorder()
	s.handleAPIIslandOps(w, httptest.NewRequest("GET", "/api/islands/ops", nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "POST" {
		t.Errorf("status = %d, allow = %q", w.Code, w.Header().Get("Allow"))
	}
}

func TestIslandOps_Add(t *testing.T) {
	s, up := newOpsServer(&model.IslandStore{Islands: []model.Island{{ID: "hr", Name: "HR"}}})

	w, resp := postOps(t, s, `{"op":"add","name":"採用 flow"}`)
	if w.Code != http.StatusOK || resp["ref"] != "island:flow" {
		t.Fatalf("top-level: status = %d, body = %s", w.Code, w.Body.String())
	}
	w, resp = postOps(t, s, `{"op":"add","name":"Hiring","parent":"island:hr"}`)
	if w.Code != http.StatusOK || resp["ref"] != "island:hiring" {
		t.Fatalf("under island: status = %d, body = %s", w.Code, w.Body.String())
	}
	w, resp = postOps(t, s, `{"op":"add","name":"On repo","parent":"repo:/r/app"}`)
	if w.Code != http.StatusOK || resp["ref"] != "island:on-repo" {
		t.Fatalf("under known repo: status = %d, body = %s", w.Code, w.Body.String())
	}
	var parents []string
	for _, i := range up.store.Islands {
		parents = append(parents, i.ID+"<"+i.Parent)
	}
	want := []string{"hr<", "flow<", "hiring<island:hr", "on-repo<repo:/r/app"}
	if !reflect.DeepEqual(parents, want) {
		t.Errorf("islands = %v, want %v", parents, want)
	}
}

func TestIslandOps_AddErrors(t *testing.T) {
	long := strings.Repeat("あ", 81)
	tests := []struct {
		name   string
		body   string
		status int
		substr string
	}{
		{"duplicate id", `{"op":"add","name":"hr"}`, http.StatusConflict, "different name"},
		{"empty name", `{"op":"add","name":"  "}`, http.StatusBadRequest, "empty"},
		{"name too long", `{"op":"add","name":"` + long + `"}`, http.StatusBadRequest, "too long"},
		{"control char in name", `{"op":"add","name":"a\nb"}`, http.StatusBadRequest, "control"},
		{"missing island parent", `{"op":"add","name":"x","parent":"island:nope"}`, http.StatusBadRequest, "not found"},
		{"unknown repo parent", `{"op":"add","name":"x","parent":"repo:/r/unknown"}`, http.StatusBadRequest, "unknown repo"},
		{"bare parent", `{"op":"add","name":"x","parent":"hr"}`, http.StatusBadRequest, "invalid ref"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, up := newOpsServer(&model.IslandStore{Islands: []model.Island{{ID: "hr", Name: "HR"}}})
			w, resp := postOps(t, s, tt.body)
			if w.Code != tt.status || !strings.Contains(errorOf(resp), tt.substr) {
				t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if len(up.store.Islands) != 1 {
				t.Errorf("store must be unchanged: %+v", up.store.Islands)
			}
		})
	}
}

func TestIslandOps_Rename(t *testing.T) {
	s, up := newOpsServer(&model.IslandStore{Islands: []model.Island{{ID: "hr", Name: "HR"}}})
	if w, _ := postOps(t, s, `{"op":"rename","ref":"island:hr","name":" 人事 "}`); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if up.store.Islands[0].Name != "人事" {
		t.Errorf("name = %q", up.store.Islands[0].Name)
	}
	for body, status := range map[string]int{
		`{"op":"rename","ref":"island:nope","name":"x"}`: http.StatusBadRequest,
		`{"op":"rename","ref":"island:hr","name":""}`:    http.StatusBadRequest,
		`{"op":"rename","ref":"repo:/r/app","name":"x"}`: http.StatusBadRequest, // repo は改名できない
		`{"op":"rename","ref":"bogus","name":"x"}`:       http.StatusBadRequest,
	} {
		if w, resp := postOps(t, s, body); w.Code != status || errorOf(resp) == "" {
			t.Errorf("%s: status = %d, body = %s", body, w.Code, w.Body.String())
		}
	}
}

func TestIslandOps_Remove(t *testing.T) {
	newStore := func() *model.IslandStore {
		return &model.IslandStore{
			Islands: []model.Island{
				{ID: "top", Name: "Top"},
				{ID: "mid", Name: "Mid", Parent: "island:top"},
				{ID: "leaf", Name: "Leaf", Parent: "island:mid"},
				{ID: "lone", Name: "Lone"},
			},
			Repos: []model.RepoNode{{Root: "/r/app", Parent: "island:mid"}},
		}
	}

	t.Run("no children removes without the children field", func(t *testing.T) {
		s, up := newOpsServer(newStore())
		if w, _ := postOps(t, s, `{"op":"remove","ref":"island:lone"}`); w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if up.store.HasIsland("lone") {
			t.Error("lone must be removed")
		}
	})

	t.Run("matching children reparents them", func(t *testing.T) {
		s, up := newOpsServer(newStore())
		// 順序違いでも集合として一致すれば通す
		w, _ := postOps(t, s, `{"op":"remove","ref":"island:mid","children":["repo:/r/app","island:leaf"]}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		if up.store.HasIsland("mid") || up.store.ParentOf("island:leaf") != "island:top" || up.store.ParentOf("repo:/r/app") != "island:top" {
			t.Errorf("store = %+v", up.store)
		}
	})

	t.Run("stale children is a conflict and removes nothing", func(t *testing.T) {
		for name, body := range map[string]string{
			"subset":  `{"op":"remove","ref":"island:mid","children":["island:leaf"]}`,
			"missing": `{"op":"remove","ref":"island:mid"}`,
			"extra":   `{"op":"remove","ref":"island:mid","children":["island:leaf","repo:/r/app","island:ghost"]}`,
		} {
			t.Run(name, func(t *testing.T) {
				s, up := newOpsServer(newStore())
				w, resp := postOps(t, s, body)
				if w.Code != http.StatusConflict || errorOf(resp) != "children changed" {
					t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
				}
				var got []string
				for _, c := range resp["children"].([]any) {
					got = append(got, c.(string))
				}
				sort.Strings(got)
				if !reflect.DeepEqual(got, []string{"island:leaf", "repo:/r/app"}) {
					t.Errorf("children = %v", got)
				}
				if !up.store.HasIsland("mid") {
					t.Error("mid must not be removed")
				}
			})
		}
	})

	t.Run("missing island is 400", func(t *testing.T) {
		s, _ := newOpsServer(newStore())
		if w, _ := postOps(t, s, `{"op":"remove","ref":"island:ghost"}`); w.Code != http.StatusBadRequest {
			t.Errorf("status = %d", w.Code)
		}
	})
	t.Run("repo ref is 400", func(t *testing.T) {
		s, _ := newOpsServer(newStore())
		if w, _ := postOps(t, s, `{"op":"remove","ref":"repo:/r/app"}`); w.Code != http.StatusBadRequest {
			t.Errorf("status = %d", w.Code)
		}
	})
}

func TestIslandOps_AttachDetach(t *testing.T) {
	s, up := newOpsServer(&model.IslandStore{Islands: []model.Island{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B", Parent: "island:a"},
	}})

	if w, _ := postOps(t, s, `{"op":"attach","child":"repo:/r/web","parent":"island:b"}`); w.Code != http.StatusOK {
		t.Fatalf("attach repo: status = %d", w.Code)
	}
	if up.store.ParentOf("repo:/r/web") != "island:b" {
		t.Errorf("repos = %+v", up.store.Repos)
	}
	if w, _ := postOps(t, s, `{"op":"attach","child":"island:b","parent":"repo:/r/app"}`); w.Code != http.StatusOK {
		t.Fatalf("attach island under repo: status = %d", w.Code)
	}
	if w, _ := postOps(t, s, `{"op":"detach","child":"repo:/r/web"}`); w.Code != http.StatusOK {
		t.Fatalf("detach: status = %d", w.Code)
	}
	if len(up.store.Repos) != 0 {
		t.Errorf("repos = %+v", up.store.Repos)
	}

	// a <- b <- repo:/r/app の鎖に対する不正な操作
	chain := func() *Server {
		s, _ := newOpsServer(&model.IslandStore{
			Islands: []model.Island{{ID: "a", Name: "A"}, {ID: "b", Name: "B", Parent: "island:a"}},
			Repos:   []model.RepoNode{{Root: "/r/app", Parent: "island:b"}},
		})
		return s
	}
	for name, body := range map[string]string{
		"self":                `{"op":"attach","child":"island:a","parent":"island:a"}`,
		"cycle direct":        `{"op":"attach","child":"island:a","parent":"island:b"}`,
		"cycle via repo":      `{"op":"attach","child":"island:a","parent":"repo:/r/app"}`,
		"unknown child repo":  `{"op":"attach","child":"repo:/r/unknown","parent":"island:a"}`,
		"unknown parent repo": `{"op":"attach","child":"island:a","parent":"repo:/r/unknown"}`,
		"missing child":       `{"op":"attach","child":"island:ghost","parent":"island:a"}`,
		"empty parent":        `{"op":"attach","child":"island:a","parent":""}`,
		"detach unknown repo": `{"op":"detach","child":"repo:/r/unknown"}`,
		"detach missing":      `{"op":"detach","child":"island:ghost"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if w, resp := postOps(t, chain(), body); w.Code != http.StatusBadRequest || errorOf(resp) == "" {
				t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestIslandOps_RepoPathIsNormalizedAndKnownViaIslandsYAML(t *testing.T) {
	// contexts に無くても islands.yaml の repo ノード / 親としてだけ現れる repo は既知（CLI と同じ定義）
	s, up := newOpsServer(&model.IslandStore{
		Islands: []model.Island{{ID: "a", Name: "A", Parent: "repo:/r/only-parent"}, {ID: "b", Name: "B"}},
		Repos:   []model.RepoNode{{Root: "/r/only-yaml", Parent: "island:a"}},
	})
	for _, p := range []string{"/r/only-parent", "/r/only-yaml", "/r/app/../app/"} {
		body, _ := json.Marshal(map[string]string{"op": "attach", "child": "repo:" + p, "parent": "island:b"})
		if w, resp := postOps(t, s, string(body)); w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, body = %v", p, w.Code, resp)
		}
	}
	if up.store.ParentOf("repo:/r/app") != "island:b" {
		t.Errorf("repo path must be stored normalized: %+v", up.store.Repos)
	}
}

func TestIslandOps_BadRequests(t *testing.T) {
	s, up := newOpsServer(&model.IslandStore{})
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"unknown op", `{"op":"explode"}`, http.StatusBadRequest},
		{"missing op", `{}`, http.StatusBadRequest},
		{"malformed json", `{"op":`, http.StatusBadRequest},
		{"unknown field", `{"op":"add","name":"x","bogus":1}`, http.StatusBadRequest},
		{"too large", `{"op":"add","name":"` + strings.Repeat("x", 64*1024) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, resp := postOps(t, s, tt.body)
			if w.Code != tt.status || errorOf(resp) == "" {
				t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}
	if up.calls != 0 {
		t.Errorf("updater must not be called for rejected bodies (calls = %d)", up.calls)
	}
}

func TestIslandOps_ThroughHandlerRequiresGuardHeaders(t *testing.T) {
	s, up := newOpsServer(&model.IslandStore{})
	h := s.Handler()
	do := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/islands/ops", strings.NewReader(`{"op":"add","name":"x"}`))
		req.Host = "127.0.0.1:3333"
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := do(""); w.Code != http.StatusForbidden {
		t.Errorf("no origin: status = %d", w.Code)
	}
	if len(up.store.Islands) != 0 {
		t.Fatal("must not write without Origin")
	}
	if w := do("http://127.0.0.1:3333"); w.Code != http.StatusOK {
		t.Errorf("ok: status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestIslandOps_WithRealStorageLock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	st, err := storage.New()
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Port: 3333, StoreLoader: st, IslandLoader: st, IslandUpdater: st}

	w, resp := postOps(t, s, `{"op":"add","name":"HR"}`)
	if w.Code != http.StatusOK || resp["ref"] != "island:hr" {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if w, _ := postOps(t, s, `{"op":"add","name":"Child","parent":"island:hr"}`); w.Code != http.StatusOK {
		t.Fatalf("child: status = %d", w.Code)
	}
	// 失敗した操作（循環）は保存されない
	if w, _ := postOps(t, s, `{"op":"attach","child":"island:hr","parent":"island:child"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("cycle: status = %d", w.Code)
	}
	if w, _ := postOps(t, s, `{"op":"remove","ref":"island:hr","children":["island:child"]}`); w.Code != http.StatusOK {
		t.Fatalf("remove: status = %d", w.Code)
	}
	got, err := st.LoadIslands()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Islands) != 1 || got.Islands[0].ID != "child" || got.Islands[0].Parent != "" {
		t.Errorf("islands = %+v", got.Islands)
	}
}
