package model

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		in      string
		kind    RefKind
		value   string
		wantErr bool
	}{
		{"island:hr", RefIsland, "hr", false},
		{"repo:/a/b", RefRepo, "/a/b", false},
		{"repo:/a/b:c", RefRepo, "/a/b:c", false}, // パスに ':' を含んでも先頭の種別だけで分ける
		{"", "", "", true},
		{"hr", "", "", true},
		{"island:", "", "", true},
		{"repo:", "", "", true},
		{"team:x", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			kind, value, err := ParseRef(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if kind != tt.kind || value != tt.value {
				t.Errorf("got (%q, %q), want (%q, %q)", kind, value, tt.kind, tt.value)
			}
		})
	}
	if IslandRef("hr") != "island:hr" || RepoRef("/a") != "repo:/a" {
		t.Error("ref builders")
	}
}

func TestAddIsland(t *testing.T) {
	t.Run("id defaults to slug of ASCII name", func(t *testing.T) {
		s := &IslandStore{}
		got, err := s.AddIsland("M3 UI", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "m3-ui" || got.Name != "M3 UI" || got.Parent != "" {
			t.Errorf("got %+v", got)
		}
		if len(s.Islands) != 1 {
			t.Errorf("not stored")
		}
	})
	t.Run("non-ASCII name falls back to island-N", func(t *testing.T) {
		s := &IslandStore{}
		a, err := s.AddIsland("人事強化", "", "")
		if err != nil {
			t.Fatal(err)
		}
		b, err := s.AddIsland("採用フロー", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if a.ID != "island-1" || b.ID != "island-2" {
			t.Errorf("ids = %q, %q", a.ID, b.ID)
		}
	})
	t.Run("duplicate id is rejected and hints --id", func(t *testing.T) {
		s := &IslandStore{}
		if _, err := s.AddIsland("HR", "", ""); err != nil {
			t.Fatal(err)
		}
		_, err := s.AddIsland("hr", "", "")
		if err == nil || !strings.Contains(err.Error(), "--id") {
			t.Errorf("err = %v", err)
		}
		if len(s.Islands) != 1 {
			t.Error("store changed on error")
		}
	})
	t.Run("explicit id is validated", func(t *testing.T) {
		s := &IslandStore{}
		for _, id := range []string{"Has Space", "a:b", "UPPER", "-x"} {
			if _, err := s.AddIsland("n", id, ""); err == nil {
				t.Errorf("id %q accepted", id)
			}
		}
	})
	t.Run("empty name is rejected", func(t *testing.T) {
		if _, err := (&IslandStore{}).AddIsland("  ", "x", ""); err == nil {
			t.Error("accepted")
		}
	})
	t.Run("parent island must exist", func(t *testing.T) {
		s := &IslandStore{}
		if _, err := s.AddIsland("x", "x", "island:nope"); err == nil {
			t.Error("accepted dangling parent")
		}
	})
	t.Run("parent may be a repo ref and island under repo", func(t *testing.T) {
		s := &IslandStore{}
		got, err := s.AddIsland("M3 UI", "m3", "repo:/r/devctx")
		if err != nil {
			t.Fatal(err)
		}
		if got.Parent != "repo:/r/devctx" {
			t.Errorf("parent = %q", got.Parent)
		}
	})
	t.Run("malformed parent is rejected", func(t *testing.T) {
		if _, err := (&IslandStore{}).AddIsland("x", "x", "hr"); err == nil {
			t.Error("accepted untyped parent")
		}
	})
}

func TestRenameIsland(t *testing.T) {
	s := &IslandStore{Islands: []Island{{ID: "hr", Name: "HR"}}}
	if err := s.RenameIsland("hr", "人事強化"); err != nil {
		t.Fatal(err)
	}
	if s.Islands[0].ID != "hr" || s.Islands[0].Name != "人事強化" {
		t.Errorf("got %+v", s.Islands[0])
	}
	if err := s.RenameIsland("zzz", "x"); err == nil {
		t.Error("renamed missing island")
	}
	if err := s.RenameIsland("hr", " "); err == nil {
		t.Error("accepted empty name")
	}
}

func newTree() *IslandStore {
	// hr ─ hiring
	// repo:/r/a ─ m3 / repo:/r/b ─ (under island hr)
	return &IslandStore{
		Islands: []Island{
			{ID: "hr", Name: "HR"},
			{ID: "hiring", Name: "Hiring", Parent: "island:hr"},
			{ID: "m3", Name: "M3", Parent: "repo:/r/a"},
		},
		Repos: []RepoNode{
			{Root: "/r/a", Parent: "island:hr"},
		},
	}
}

func TestRemoveIsland(t *testing.T) {
	t.Run("leaf is removed", func(t *testing.T) {
		s := newTree()
		if err := s.RemoveIsland("m3", false); err != nil {
			t.Fatal(err)
		}
		if len(s.Islands) != 2 {
			t.Errorf("islands = %+v", s.Islands)
		}
	})
	t.Run("children without reparent is refused and store untouched", func(t *testing.T) {
		s := newTree()
		err := s.RemoveIsland("hr", false)
		if err == nil || !strings.Contains(err.Error(), "--reparent") {
			t.Errorf("err = %v", err)
		}
		if len(s.Islands) != 3 {
			t.Error("store changed on error")
		}
	})
	t.Run("reparent moves island and repo children to the removed island's parent", func(t *testing.T) {
		s := newTree()
		if err := s.RemoveIsland("hr", true); err != nil {
			t.Fatal(err)
		}
		if got := s.ParentOf("island:hiring"); got != "" {
			t.Errorf("hiring parent = %q", got)
		}
		if got := s.ParentOf("repo:/r/a"); got != "" {
			t.Errorf("repo parent = %q", got)
		}
		if len(s.Repos) != 0 {
			t.Errorf("empty-parent repo node should be dropped: %+v", s.Repos)
		}
	})
	t.Run("reparent onto grandparent", func(t *testing.T) {
		s := newTree()
		if err := s.RemoveIsland("hiring", true); err != nil { // no children
			t.Fatal(err)
		}
		s = &IslandStore{Islands: []Island{
			{ID: "top", Name: "T"},
			{ID: "mid", Name: "M", Parent: "island:top"},
			{ID: "leaf", Name: "L", Parent: "island:mid"},
		}}
		if err := s.RemoveIsland("mid", true); err != nil {
			t.Fatal(err)
		}
		if got := s.ParentOf("island:leaf"); got != "island:top" {
			t.Errorf("leaf parent = %q", got)
		}
	})
	t.Run("missing island", func(t *testing.T) {
		if err := newTree().RemoveIsland("zzz", true); err == nil {
			t.Error("removed missing island")
		}
	})
}

func TestSetParent(t *testing.T) {
	tests := []struct {
		name    string
		child   string
		parent  string
		wantErr string
	}{
		{"repo under island creates node", "repo:/r/new", "island:hr", ""},
		{"island under repo", "island:hiring", "repo:/r/a", ""},
		{"island under island", "island:m3", "island:hiring", ""},
		{"repo under repo", "repo:/r/new", "repo:/r/a", ""},
		{"self parent", "island:hr", "island:hr", "itself"},
		{"self parent repo", "repo:/r/a", "repo:/r/a", "itself"},
		{"direct cycle", "island:hr", "island:hiring", "cycle"},
		{"cycle via repo: island→repo→island", "island:hr", "island:m3", "cycle"}, // m3 → repo:/r/a → hr
		{"missing parent island", "island:hr", "island:nope", "not found"},
		{"missing child island", "island:nope", "island:hr", "not found"},
		{"empty parent is not allowed (use detach)", "island:hr", "", "detach"},
		{"malformed child", "hr", "island:hr", "ref"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTree()
			before := *newTree()
			err := s.SetParent(tt.child, tt.parent)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if got := s.ParentOf(tt.child); got != tt.parent {
					t.Errorf("ParentOf = %q, want %q", got, tt.parent)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
			if !reflect.DeepEqual(*s, before) {
				t.Error("store changed on error")
			}
		})
	}

	t.Run("re-parenting an existing repo node updates it in place", func(t *testing.T) {
		s := newTree()
		if err := s.SetParent("repo:/r/a", "island:hiring"); err != nil {
			t.Fatal(err)
		}
		if len(s.Repos) != 1 || s.Repos[0].Parent != "island:hiring" {
			t.Errorf("repos = %+v", s.Repos)
		}
	})
}

func TestDetach(t *testing.T) {
	t.Run("repo node is dropped to keep yaml small", func(t *testing.T) {
		s := newTree()
		if err := s.Detach("repo:/r/a"); err != nil {
			t.Fatal(err)
		}
		if len(s.Repos) != 0 {
			t.Errorf("repos = %+v", s.Repos)
		}
	})
	t.Run("island keeps its entry with empty parent", func(t *testing.T) {
		s := newTree()
		if err := s.Detach("island:hiring"); err != nil {
			t.Fatal(err)
		}
		if s.ParentOf("island:hiring") != "" || len(s.Islands) != 3 {
			t.Errorf("islands = %+v", s.Islands)
		}
	})
	t.Run("detaching an unattached repo is a no-op", func(t *testing.T) {
		if err := newTree().Detach("repo:/r/zzz"); err != nil {
			t.Error(err)
		}
	})
	t.Run("missing island", func(t *testing.T) {
		if err := newTree().Detach("island:nope"); err == nil {
			t.Error("accepted")
		}
	})
}

func TestChildrenAndParentOf(t *testing.T) {
	s := newTree()
	if got := s.Children("island:hr"); !reflect.DeepEqual(got, []string{"island:hiring", "repo:/r/a"}) {
		t.Errorf("children(hr) = %v", got)
	}
	if got := s.Children("repo:/r/a"); !reflect.DeepEqual(got, []string{"island:m3"}) {
		t.Errorf("children(repo a) = %v", got)
	}
	if got := s.ParentOf("repo:/r/unknown"); got != "" {
		t.Errorf("unknown repo parent = %q", got)
	}
}

func TestDanglingParentsResolveToTopLevel(t *testing.T) {
	s := &IslandStore{
		Islands: []Island{{ID: "orphan", Name: "O", Parent: "island:gone"}},
		Repos:   []RepoNode{{Root: "/r/x", Parent: "island:gone"}},
	}
	if got := s.ParentOf("island:orphan"); got != "" {
		t.Errorf("orphan parent = %q", got)
	}
	if got := s.ParentOf("repo:/r/x"); got != "" {
		t.Errorf("repo parent = %q", got)
	}
	r := s.Resolved()
	if r.Islands[0].Parent != "" || r.Repos[0].Parent != "" {
		t.Errorf("resolved = %+v", r)
	}
	if s.Islands[0].Parent == "" {
		t.Error("Resolved must not mutate the receiver")
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "island:gone") {
		t.Errorf("Validate = %v", err)
	}
	if err := newTree().Validate(); err != nil {
		t.Errorf("valid tree: %v", err)
	}
}

func TestParentWalkSurvivesHandEditedCycle(t *testing.T) {
	s := &IslandStore{Islands: []Island{
		{ID: "a", Name: "A", Parent: "island:b"},
		{ID: "b", Name: "B", Parent: "island:a"},
		{ID: "c", Name: "C"},
	}}
	// 手編集で壊れた yaml でも無限ループせず、無関係な付け替えは通る
	if err := s.SetParent("island:c", "island:a"); err != nil {
		t.Fatal(err)
	}
}

func TestNormalize(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "app")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "app-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	key := NormalizePath(real)

	s := &IslandStore{
		Islands: []Island{
			{ID: "m3", Name: "M3", Parent: "repo:" + link},
			{ID: "hr", Name: "HR", Parent: "island:x"}, // island ref はそのまま
		},
		Repos: []RepoNode{
			{Root: link, Parent: "island:hr"},
			{Root: real, Parent: "island:other"}, // 正規化後に同じ root: 先勝ちで落とす
			{Root: "/r/b", Parent: "repo:" + link + "/"},
		},
	}
	s.Normalize()

	if s.Islands[0].Parent != "repo:"+key || s.Islands[1].Parent != "island:x" {
		t.Errorf("islands = %+v", s.Islands)
	}
	want := []RepoNode{{Root: key, Parent: "island:hr"}, {Root: "/r/b", Parent: "repo:" + key}}
	if !reflect.DeepEqual(s.Repos, want) {
		t.Errorf("repos = %+v, want %+v", s.Repos, want)
	}
}

func TestResolvedEncodesAsJSONWithEmptyArrays(t *testing.T) {
	r := (&IslandStore{}).Resolved()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"islands":[],"repos":[]}` {
		t.Errorf("json = %s", b)
	}
	r = (&IslandStore{Islands: []Island{{ID: "a", Name: "A"}}, Repos: []RepoNode{{Root: "/r", Parent: "island:a"}}}).Resolved()
	b, _ = json.Marshal(r)
	if string(b) != `{"islands":[{"id":"a","name":"A","parent":""}],"repos":[{"root":"/r","parent":"island:a"}]}` {
		t.Errorf("json = %s", b)
	}
}

func TestNormalizeMergesDuplicateRepoParentsAndClearsSelfParent(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "app")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "app-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	key := NormalizePath(real)

	t.Run("later parent fills in when the first has none", func(t *testing.T) {
		s := &IslandStore{Repos: []RepoNode{{Root: link}, {Root: real, Parent: "island:a"}}}
		s.Normalize()
		if len(s.Repos) != 1 || s.Repos[0].Root != key || s.Repos[0].Parent != "island:a" {
			t.Errorf("repos = %+v", s.Repos)
		}
	})
	t.Run("first parent wins when both have one", func(t *testing.T) {
		s := &IslandStore{Repos: []RepoNode{{Root: link, Parent: "island:a"}, {Root: real, Parent: "island:b"}}}
		s.Normalize()
		if len(s.Repos) != 1 || s.Repos[0].Parent != "island:a" {
			t.Errorf("repos = %+v", s.Repos)
		}
	})
	t.Run("a repo under itself (via symlink) loses the parent", func(t *testing.T) {
		s := &IslandStore{Repos: []RepoNode{{Root: real, Parent: "repo:" + link}}}
		s.Normalize()
		if s.Repos[0].Parent != "" {
			t.Errorf("repos = %+v", s.Repos)
		}
	})
}

func TestResolvedBreaksCycles(t *testing.T) {
	// ファイル順に処理し、輪を閉じる辺を切る。どちらの木でも ParentOf を辿って必ず根に着く
	acyclic := func(t *testing.T, r IslandStore) {
		t.Helper()
		refs := []string{}
		for _, is := range r.Islands {
			refs = append(refs, IslandRef(is.ID))
		}
		for _, rn := range r.Repos {
			refs = append(refs, RepoRef(rn.Root))
		}
		for _, ref := range refs {
			cur := ref
			for i := 0; i <= len(refs); i++ {
				if cur = r.ParentOf(cur); cur == "" {
					break
				}
				if i == len(refs) {
					t.Fatalf("%s still loops", ref)
				}
			}
		}
	}

	t.Run("island to island", func(t *testing.T) {
		s := &IslandStore{Islands: []Island{
			{ID: "a", Name: "A", Parent: "island:b"},
			{ID: "b", Name: "B", Parent: "island:a"},
		}}
		r := s.Resolved()
		acyclic(t, r)
		if r.Islands[0].Parent != "island:b" || r.Islands[1].Parent != "" {
			t.Errorf("resolved = %+v (a keeps its parent, the edge closing the loop is cut)", r.Islands)
		}
		if s.Islands[1].Parent != "island:a" {
			t.Error("Resolved must not mutate the receiver")
		}
		if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "island:b -> island:a") {
			t.Errorf("Validate = %v", err)
		}
	})
	t.Run("island to repo to island", func(t *testing.T) {
		s := &IslandStore{
			Islands: []Island{{ID: "m3", Name: "M3", Parent: "repo:/r/a"}},
			Repos:   []RepoNode{{Root: "/r/a", Parent: "island:m3"}},
		}
		r := s.Resolved()
		acyclic(t, r)
		if r.Islands[0].Parent != "repo:/r/a" || r.Repos[0].Parent != "" {
			t.Errorf("resolved = %+v", r)
		}
	})
	t.Run("self parent", func(t *testing.T) {
		s := &IslandStore{Islands: []Island{{ID: "x", Name: "X", Parent: "island:x"}}}
		if r := s.Resolved(); r.Islands[0].Parent != "" {
			t.Errorf("resolved = %+v", r)
		}
	})
	t.Run("valid tree is untouched", func(t *testing.T) {
		if !reflect.DeepEqual(newTree().Resolved(), func() IslandStore { n := newTree(); return n.Resolved() }()) {
			t.Error("unstable")
		}
		if r := newTree().Resolved(); r.Islands[1].Parent != "island:hr" || r.Islands[2].Parent != "repo:/r/a" || r.Repos[0].Parent != "island:hr" {
			t.Errorf("resolved = %+v", r)
		}
	})
}

func TestRemoveIslandReparentDoesNotPropagateDanglingParent(t *testing.T) {
	s := &IslandStore{Islands: []Island{
		{ID: "mid", Name: "M", Parent: "island:gone"},
		{ID: "leaf", Name: "L", Parent: "island:mid"},
	}, Repos: []RepoNode{{Root: "/r/x", Parent: "island:mid"}}}
	if err := s.RemoveIsland("mid", true); err != nil {
		t.Fatal(err)
	}
	if s.Islands[0].Parent != "" {
		t.Errorf("leaf parent = %q, want top level (dangling parent must not be inherited)", s.Islands[0].Parent)
	}
	if len(s.Repos) != 0 {
		t.Errorf("repos = %+v", s.Repos)
	}
}

func TestAddIsland_DuplicateIDReturnsTypedError(t *testing.T) {
	s := &IslandStore{}
	if _, err := s.AddIsland("HR", "", ""); err != nil {
		t.Fatal(err)
	}
	_, err := s.AddIsland("hr", "", "")
	var dup *IDExistsError
	if !errors.As(err, &dup) || dup.ID != "hr" {
		t.Fatalf("err = %v, want *IDExistsError{hr}", err)
	}
	// CLI の案内文は従来のまま
	if !strings.Contains(err.Error(), "pass --id to choose another") {
		t.Errorf("message = %q", err.Error())
	}
}

func TestParseIslandRef(t *testing.T) {
	if id, err := ParseIslandRef("island:hr"); err != nil || id != "hr" {
		t.Errorf("island:hr -> %q, %v", id, err)
	}
	for _, bad := range []string{"repo:/r/app", "hr", "", "island:"} {
		if id, err := ParseIslandRef(bad); err == nil || id != "" {
			t.Errorf("%q must be rejected, got %q, %v", bad, id, err)
		}
	}
	// 種別違いは、何が違うかが分かるメッセージにする
	if _, err := ParseIslandRef("repo:/r/app"); err == nil || !strings.Contains(err.Error(), "not an island") {
		t.Errorf("err = %v", err)
	}
}

func TestRemoveIslandExpecting(t *testing.T) {
	newStore := func() *IslandStore {
		return &IslandStore{
			Islands: []Island{
				{ID: "top", Name: "Top"},
				{ID: "mid", Name: "Mid", Parent: "island:top"},
				{ID: "leaf", Name: "Leaf", Parent: "island:mid"},
				{ID: "lone", Name: "Lone"},
			},
			Repos: []RepoNode{{Root: "/r/app", Parent: "island:mid"}},
		}
	}

	t.Run("no children removes regardless of expect", func(t *testing.T) {
		s := newStore()
		if err := s.RemoveIslandExpecting("lone", nil); err != nil {
			t.Fatal(err)
		}
		if s.HasIsland("lone") {
			t.Error("lone must be removed")
		}
	})
	t.Run("matching set (order and duplicates ignored) reparents", func(t *testing.T) {
		s := newStore()
		if err := s.RemoveIslandExpecting("mid", []string{"repo:/r/app", "island:leaf", "repo:/r/app"}); err != nil {
			t.Fatal(err)
		}
		if s.HasIsland("mid") || s.ParentOf("island:leaf") != "island:top" || s.ParentOf("repo:/r/app") != "island:top" {
			t.Errorf("store = %+v", s)
		}
	})
	for name, expect := range map[string][]string{
		"subset":  {"island:leaf"},
		"missing": nil,
		"extra":   {"island:leaf", "repo:/r/app", "island:ghost"},
	} {
		t.Run("stale "+name, func(t *testing.T) {
			s := newStore()
			err := s.RemoveIslandExpecting("mid", expect)
			var cc *ChildrenChangedError
			if !errors.As(err, &cc) {
				t.Fatalf("err = %v, want *ChildrenChangedError", err)
			}
			got := append([]string(nil), cc.Current...)
			sort.Strings(got)
			if !reflect.DeepEqual(got, []string{"island:leaf", "repo:/r/app"}) {
				t.Errorf("Current = %v", got)
			}
			if !s.HasIsland("mid") {
				t.Error("mid must not be removed")
			}
		})
	}
	t.Run("missing island is a plain error", func(t *testing.T) {
		err := newStore().RemoveIslandExpecting("ghost", nil)
		var cc *ChildrenChangedError
		if err == nil || errors.As(err, &cc) {
			t.Errorf("err = %v", err)
		}
	})
}
