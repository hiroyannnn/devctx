package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func testResolver(repos []string, is *model.IslandStore) refResolver {
	return refResolver{
		islands: is,
		repos:   func() ([]string, error) { return repos, nil },
		repoFromCwd: func() (string, error) {
			return "/cwd/repo", nil
		},
	}
}

func TestScanRepos(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "app")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "app-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	store := &model.Store{Contexts: []model.Context{
		{Name: "a", RepoRoot: real},
		{Name: "b", RepoRoot: link},                                    // symlink は同じ repo
		{Name: "c", Worktree: "/w/solo"},                               // RepoRoot なしは Worktree
		{Name: "d", RepoRoot: "/r/archived", Status: model.StatusDone}, // done でも repo は既知
		{Name: "e"}, // キー無しは除外
	}}
	is := &model.IslandStore{Repos: []model.RepoNode{{Root: "/r/only-in-yaml", Parent: "island:x"}, {Root: real, Parent: "island:x"}}}

	is.Normalize() // LoadIslands が済ませる前提。knownRepos 自身は再正規化しない
	got, active := scanRepos(store, is)
	if active[model.NormalizePath(real)] != 2 || active["/r/archived"] != 0 || active["/w/solo"] != 1 {
		t.Errorf("active = %v", active) // done は数えない
	}
	want := []string{model.NormalizePath(real), "/r/archived", "/r/only-in-yaml", "/w/solo"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveRef(t *testing.T) {
	existing := t.TempDir()
	is := &model.IslandStore{Islands: []model.Island{
		{ID: "hr", Name: "HR"},
		{ID: "devctx", Name: "devctx island"}, // repo basename と衝突させる
	}}
	r := testResolver([]string{"/r/devctx", "/r/web", "/r1/app", "/r2/app", "/r/hr"}, is)

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr []string // 全部含むこと
	}{
		{"typed island", "island:hr", "island:hr", nil},
		{"typed island missing", "island:nope", "", []string{"island", "nope"}},
		{"typed known repo", "repo:/r/web", "repo:/r/web", nil},
		{"typed repo unknown but existing dir", "repo:" + existing, "repo:" + model.NormalizePath(existing), nil},
		{"typed repo unknown and missing", "repo:/no/such/repo", "", []string{"/no/such/repo"}},
		{"repo:. uses the repo of the current directory", "repo:.", "repo:/cwd/repo", nil},
		{"bare island id", "hr2", "", []string{"hr2"}}, // 無い id は未解決
		{"bare repo basename", "web", "repo:/r/web", nil},
		{"bare token matching island and repo is ambiguous", "devctx", "", []string{"island:devctx", "repo:/r/devctx"}},
		{"bare token matching island and repo (hr)", "hr", "", []string{"island:hr", "repo:/r/hr"}},
		{"same basename in two repos is ambiguous", "app", "", []string{"repo:/r1/app", "repo:/r2/app"}},
		{"empty", "", "", []string{"empty"}},
		{"unknown bare", "zzz", "", []string{"zzz"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.resolve(tt.in)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatal(err)
				}
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("got %q, want error", got)
			}
			for _, sub := range tt.wantErr {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("err %q does not contain %q", err, sub)
				}
			}
		})
	}
}

func TestResolveRef_BareIslandWhenNoRepoCollides(t *testing.T) {
	r := testResolver([]string{"/r/web"}, &model.IslandStore{Islands: []model.Island{{ID: "hr", Name: "HR"}}})
	got, err := r.resolve("hr")
	if err != nil || got != "island:hr" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestResolveRef_CwdFailurePropagates(t *testing.T) {
	r := refResolver{islands: &model.IslandStore{}, repos: func() ([]string, error) { return nil, nil }, repoFromCwd: func() (string, error) { return "", errors.New("not a git repository") }}
	if _, err := r.resolve("repo:."); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveRef_ReposAreLoadedLazily(t *testing.T) {
	calls := 0
	r := refResolver{
		islands: &model.IslandStore{Islands: []model.Island{{ID: "hr", Name: "HR"}}},
		repos:   func() ([]string, error) { calls++; return []string{"/r/web"}, nil },
	}
	if got, err := r.resolve("island:hr"); err != nil || got != "island:hr" {
		t.Fatalf("got %q, %v", got, err)
	}
	if calls != 0 {
		t.Errorf("typed island ref must not need the repo list (calls = %d)", calls)
	}
	if _, err := r.resolve("web"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}
