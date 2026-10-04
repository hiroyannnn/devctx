package roadmap

import (
	"encoding/json"
	"errors"
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
		TaskSeq: f.store.TaskSeq,
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

type countingStoreLoader struct {
	calls int
	err   error
	store *model.Store
}

func (c *countingStoreLoader) LoadStore() (*model.Store, error) {
	c.calls++
	return c.store, c.err
}

func TestIslandOps_ContextsAreReadOnlyWhenARepoRefNeedsChecking(t *testing.T) {
	// contexts.yaml が壊れていても、island だけの操作は通る（repo ref の検査にだけ contexts が要る）
	bad := &countingStoreLoader{err: errors.New("corrupt contexts.yaml")}
	s, up := newOpsServer(&model.IslandStore{Islands: []model.Island{
		{ID: "a", Name: "A"}, {ID: "b", Name: "B"}, {ID: "c", Name: "C", Parent: "island:a"},
	}})
	s.StoreLoader = bad

	// 順序依存（attach の後に b を消す）なので、map ではなく順序つきで回す
	steps := []struct{ name, body string }{
		{"rename", `{"op":"rename","ref":"island:a","name":"AA"}`},
		{"add", `{"op":"add","name":"New","parent":"island:a"}`},
		{"add top", `{"op":"add","name":"Top2"}`},
		{"empty parent", `{"op":"add","name":"Top3","parent":""}`},
		{"attach", `{"op":"attach","child":"island:b","parent":"island:a"}`},
		{"detach", `{"op":"detach","child":"island:c"}`},
		{"remove", `{"op":"remove","ref":"island:b","children":[]}`},
	}
	for _, st := range steps {
		if w, resp := postOps(t, s, st.body); w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, body = %v", st.name, w.Code, resp)
		}
	}
	// 400 になる入力でも contexts は読まない
	if w, _ := postOps(t, s, `{"op":"add","name":"Bad","parent":"island:nope"}`); w.Code != http.StatusBadRequest {
		t.Errorf("stale parent: status = %d", w.Code)
	}
	if bad.calls != 0 {
		t.Errorf("contexts were read %d times for island-only ops", bad.calls)
	}
	if up.store.Islands[0].Name != "AA" {
		t.Errorf("rename not applied: %+v", up.store.Islands)
	}

	// repo ref を正規化するときだけ読む。読めなければ 500（入力の誤りではない）
	w, _ := postOps(t, s, `{"op":"attach","child":"repo:/r/app","parent":"island:a"}`)
	if w.Code != http.StatusInternalServerError || bad.calls != 1 {
		t.Errorf("repo ref: status = %d, calls = %d", w.Code, bad.calls)
	}
}

func TestIslandOps_ContextsAreReadOnceForSeveralRepoRefs(t *testing.T) {
	counting := &countingStoreLoader{store: &model.Store{Contexts: []model.Context{{Name: "a", RepoRoot: "/r/app"}, {Name: "b", RepoRoot: "/r/web"}}}}
	s, _ := newOpsServer(&model.IslandStore{Islands: []model.Island{{ID: "a", Name: "A"}}})
	s.StoreLoader = counting
	if w, resp := postOps(t, s, `{"op":"attach","child":"repo:/r/app","parent":"repo:/r/web"}`); w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", w.Code, resp)
	}
	if counting.calls != 1 {
		t.Errorf("contexts read %d times, want 1", counting.calls)
	}
}

func TestIslandOps_SuccessReturnsResolvedTree(t *testing.T) {
	s, _ := newOpsServer(&model.IslandStore{
		Islands: []model.Island{{ID: "hr", Name: "HR"}, {ID: "orphan", Name: "O", Parent: "island:gone"}},
	})
	w, resp := postOps(t, s, `{"op":"add","name":"Child","parent":"island:hr"}`)
	if w.Code != http.StatusOK || resp["ref"] != "island:child" {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	tree, ok := resp["islands"].(map[string]any)
	if !ok {
		t.Fatalf("islands missing: %s", w.Body.String())
	}
	islands, _ := tree["islands"].([]any)
	parents := map[string]string{}
	for _, i := range islands {
		m := i.(map[string]any)
		parents[m["id"].(string)] = m["parent"].(string)
	}
	// GET /api/islands と同じ形・同じ解決（dangling な親は ""）
	want := map[string]string{"hr": "", "orphan": "", "child": "island:hr"}
	if !reflect.DeepEqual(parents, want) {
		t.Errorf("parents = %v, want %v", parents, want)
	}
	if repos, ok := tree["repos"].([]any); !ok || len(repos) != 0 {
		t.Errorf("repos must be an empty array: %v", tree["repos"])
	}
	// rename / detach 等の ref を返さない op でも木は返る
	_, resp = postOps(t, s, `{"op":"rename","ref":"island:hr","name":"HR2"}`)
	if _, has := resp["ref"]; has {
		t.Errorf("rename must not return ref: %v", resp)
	}
	if resp["islands"] == nil || resp["ok"] != true {
		t.Errorf("rename response = %v", resp)
	}
}

func TestIslandOps_ErrorsStayJSON(t *testing.T) {
	// pre-validation（本文・op）の失敗も、ロック内の失敗と同じ JSON 形式で返す
	s, _ := newOpsServer(&model.IslandStore{})
	for _, body := range []string{`{"op":"zzz"}`, `{`, `{"op":"add","name":"x","nope":1}`} {
		w, resp := postOps(t, s, body)
		if w.Code < 400 || errorOf(resp) == "" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: status = %d, ct = %q", body, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

func TestIslandOps_AddWithCollidingSlugGetsASuffix(t *testing.T) {
	s, _ := newOpsServer(&model.IslandStore{Islands: []model.Island{{ID: "hr", Name: "改名済み"}}})
	w, resp := postOps(t, s, `{"op":"add","name":"HR"}`)
	if w.Code != http.StatusOK || resp["ref"] != "island:hr-2" {
		t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestIslandOps_AddTask(t *testing.T) {
	s, up := newOpsServer(&model.IslandStore{Islands: []model.Island{{ID: "hr", Name: "HR"}}})

	w, resp := postOps(t, s, `{"op":"add","kind":"task","name":"求人票を直す","parent":"island:hr"}`)
	if w.Code != http.StatusOK || resp["ref"] != "island:t1" {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := up.store.Islands[1]; got.Kind != "task" || got.Parent != "island:hr" || got.ID != "t1" {
		t.Errorf("stored = %+v", got)
	}
	// 応答の木は kind / done を JSON で返す
	if !strings.Contains(w.Body.String(), `"kind":"task"`) {
		t.Errorf("tree lacks kind: %s", w.Body.String())
	}

	// repo の下にも置ける（既知 repo のみ）
	if w, _ := postOps(t, s, `{"op":"add","kind":"task","name":"x","parent":"repo:/r/app"}`); w.Code != http.StatusOK {
		t.Errorf("repo parent: status = %d, body = %s", w.Code, w.Body.String())
	}

	bad := []struct{ name, body string }{
		{"parent is required", `{"op":"add","kind":"task","name":"x"}`},
		{"unknown kind", `{"op":"add","kind":"epic","name":"x","parent":"island:hr"}`},
		{"unknown repo parent", `{"op":"add","kind":"task","name":"x","parent":"repo:/nowhere"}`},
		{"task parent", `{"op":"add","kind":"task","name":"x","parent":"island:t1"}`},
		{"name too long", `{"op":"add","kind":"task","name":"` + strings.Repeat("あ", 81) + `","parent":"island:hr"}`},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			before := len(up.store.Islands)
			w, resp := postOps(t, s, tt.body)
			if w.Code != http.StatusBadRequest || errorOf(resp) == "" {
				t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if len(up.store.Islands) != before {
				t.Error("store changed")
			}
		})
	}
}

func TestIslandOps_TaskDone(t *testing.T) {
	s, up := newOpsServer(&model.IslandStore{
		Islands: []model.Island{{ID: "hr", Name: "HR"}, {ID: "t1", Name: "T", Kind: "task", Parent: "island:hr"}},
		TaskSeq: 1,
	})
	if w, _ := postOps(t, s, `{"op":"done","ref":"island:t1","done":true}`); w.Code != http.StatusOK || !up.store.Islands[1].Done {
		t.Fatalf("done: status = %d", w.Code)
	}
	if w, _ := postOps(t, s, `{"op":"done","ref":"island:t1","done":false}`); w.Code != http.StatusOK || up.store.Islands[1].Done {
		t.Fatalf("undo: status = %d", w.Code)
	}
	for name, body := range map[string]string{
		"theme island": `{"op":"done","ref":"island:hr","done":true}`,
		"missing":      `{"op":"done","ref":"island:nope","done":true}`,
		"repo ref":     `{"op":"done","ref":"repo:/r/app","done":true}`,
		"done omitted": `{"op":"done","ref":"island:t1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if w, resp := postOps(t, s, body); w.Code != http.StatusBadRequest || errorOf(resp) == "" {
				t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestIslandOps_NothingUnderATask(t *testing.T) {
	s, up := newOpsServer(&model.IslandStore{
		Islands: []model.Island{{ID: "hr", Name: "HR"}, {ID: "t1", Name: "T", Kind: "task", Parent: "island:hr"}},
		TaskSeq: 1,
	})
	for name, body := range map[string]string{
		"add island under task":    `{"op":"add","name":"x","parent":"island:t1"}`,
		"attach repo under task":   `{"op":"attach","child":"repo:/r/app","parent":"island:t1"}`,
		"attach island under task": `{"op":"attach","child":"island:hr","parent":"island:t1"}`,
		"detach a task":            `{"op":"detach","child":"island:t1"}`,
	} {
		t.Run(name, func(t *testing.T) {
			before := *up.store
			if w, resp := postOps(t, s, body); w.Code != http.StatusBadRequest || errorOf(resp) == "" {
				t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if !reflect.DeepEqual(*up.store, before) {
				t.Error("store changed")
			}
		})
	}
	// タスクは island の下へ付け替えられる
	if w, _ := postOps(t, s, `{"op":"attach","child":"island:t1","parent":"repo:/r/web"}`); w.Code != http.StatusOK {
		t.Errorf("move task: status = %d", w.Code)
	}
}

func TestIslandOps_RemoveRefusesToOrphanTasks(t *testing.T) {
	s, up := newOpsServer(&model.IslandStore{
		Islands: []model.Island{{ID: "top", Name: "Top"}, {ID: "t1", Name: "T", Kind: "task", Parent: "island:top"}},
		TaskSeq: 1,
	})
	before := *up.store
	w, resp := postOps(t, s, `{"op":"remove","ref":"island:top","children":["island:t1"]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(errorOf(resp), "tasks need a parent") {
		t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if !reflect.DeepEqual(*up.store, before) {
		t.Error("store changed")
	}
}
