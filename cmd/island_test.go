package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// islandFixture は隔離した HOME に contexts を置き、実データに触れずに island コマンドを試す。
func islandFixture(t *testing.T, contexts ...model.Context) (*storage.Storage, refResolver) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, err := storage.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveStore(&model.Store{Contexts: contexts}); err != nil {
		t.Fatal(err)
	}
	return s, refResolver{repoFromCwd: func() (string, error) { return "/r/devctx", nil }}
}

func fixtureContexts() []model.Context {
	return []model.Context{
		{Name: "a1", RepoRoot: "/r/devctx", Status: model.StatusInProgress},
		{Name: "a2", RepoRoot: "/r/devctx", Status: model.StatusReview},
		{Name: "a3", RepoRoot: "/r/devctx", Status: model.StatusDone},
		{Name: "b1", RepoRoot: "/r/web", Status: model.StatusInProgress},
	}
}

func TestIslandAddAttachList(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer

	mustRun := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mustRun(islandAdd(s, &out, base, "人事強化", "hr", ""))
	mustRun(islandAttach(s, &out, base, "devctx", "hr"))               // bare basename → repo:/r/devctx
	mustRun(islandAdd(s, &out, base, "M3 UI", "m3", "repo:/r/devctx")) // island under repo
	mustRun(islandAdd(s, &out, base, "採用フロー", "hiring", "island:hr"))

	is, err := s.LoadIslands()
	if err != nil {
		t.Fatal(err)
	}
	if got := is.ParentOf("repo:/r/devctx"); got != "island:hr" {
		t.Errorf("repo parent = %q", got)
	}
	if got := is.ParentOf("island:m3"); got != "repo:/r/devctx" {
		t.Errorf("m3 parent = %q", got)
	}

	out.Reset()
	mustRun(islandList(s, &out))
	want := `人事強化 [island:hr]
├── 採用フロー [island:hiring]
└── devctx (/r/devctx, 2 active)
    └── M3 UI [island:m3]
web (/r/web, 1 active)
`
	if out.String() != want {
		t.Errorf("list output:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestIslandAddRejectsDuplicateAndStoresNothing(t *testing.T) {
	s, base := islandFixture(t)
	var out bytes.Buffer
	if err := islandAdd(s, &out, base, "HR", "", ""); err != nil {
		t.Fatal(err)
	}
	err := islandAdd(s, &out, base, "hr", "", "")
	if err == nil || !strings.Contains(err.Error(), "--id") {
		t.Fatalf("err = %v", err)
	}
	is, _ := s.LoadIslands()
	if len(is.Islands) != 1 {
		t.Errorf("islands = %+v", is.Islands)
	}
}

func TestIslandAttachAmbiguousBareTokenFailsWithoutWriting(t *testing.T) {
	s, base := islandFixture(t,
		model.Context{Name: "x", RepoRoot: "/r1/app"},
		model.Context{Name: "y", RepoRoot: "/r2/app"},
	)
	var out bytes.Buffer
	if err := islandAdd(s, &out, base, "Platform", "plat", ""); err != nil {
		t.Fatal(err)
	}
	err := islandAttach(s, &out, base, "app", "plat")
	if err == nil || !strings.Contains(err.Error(), "repo:/r1/app") || !strings.Contains(err.Error(), "repo:/r2/app") {
		t.Fatalf("err = %v", err)
	}
	if err := islandAttach(s, &out, base, "repo:/r2/app", "island:plat"); err != nil {
		t.Fatal(err)
	}
	is, _ := s.LoadIslands()
	if len(is.Repos) != 1 || is.Repos[0].Root != "/r2/app" {
		t.Errorf("repos = %+v", is.Repos)
	}
}

func TestIslandAttachRejectsCycle(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer
	_ = islandAdd(s, &out, base, "HR", "hr", "repo:/r/devctx")
	err := islandAttach(s, &out, base, "repo:/r/devctx", "island:hr")
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("err = %v", err)
	}
}

func TestIslandRmAndReparent(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer
	_ = islandAdd(s, &out, base, "HR", "hr", "")
	_ = islandAdd(s, &out, base, "Hiring", "hiring", "island:hr")

	if err := islandRm(s, &out, "hr", false); err == nil || !strings.Contains(err.Error(), "--reparent") {
		t.Fatalf("err = %v", err)
	}
	if err := islandRm(s, &out, "island:hr", true); err != nil {
		t.Fatal(err)
	}
	is, _ := s.LoadIslands()
	if len(is.Islands) != 1 || is.Islands[0].ID != "hiring" || is.Islands[0].Parent != "" {
		t.Errorf("islands = %+v", is.Islands)
	}
}

func TestIslandRenameAndDetach(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer
	_ = islandAdd(s, &out, base, "HR", "hr", "")
	_ = islandAttach(s, &out, base, "web", "hr")
	if err := islandRename(s, &out, "hr", "人事"); err != nil {
		t.Fatal(err)
	}
	if err := islandDetach(s, &out, base, "web"); err != nil {
		t.Fatal(err)
	}
	is, _ := s.LoadIslands()
	if is.Islands[0].Name != "人事" || is.Islands[0].ID != "hr" || len(is.Repos) != 0 {
		t.Errorf("store = %+v", is)
	}
}

func TestIslandListWarnsOnDanglingParent(t *testing.T) {
	s, _ := islandFixture(t)
	if err := s.UpdateIslands(func(is *model.IslandStore) error {
		is.Islands = append(is.Islands, model.Island{ID: "orphan", Name: "Orphan", Parent: "island:gone"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := islandList(s, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Orphan [island:orphan]") || !strings.Contains(out.String(), "warning: invalid parent") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestIslandRenameAndRmRejectNonIslandRef(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer
	_ = islandAdd(s, &out, base, "HR", "hr", "")

	if err := islandRename(s, &out, "repo:/r/devctx", "x"); err == nil || !strings.Contains(err.Error(), "island") {
		t.Errorf("rename err = %v", err)
	}
	if err := islandRm(s, &out, "repo:/r/devctx", false); err == nil || !strings.Contains(err.Error(), "island") {
		t.Errorf("rm err = %v", err)
	}
	if err := islandRename(s, &out, "island:hr", "人事"); err != nil {
		t.Errorf("typed island ref: %v", err)
	}
	if err := islandRename(s, &out, "hr", "人事2"); err != nil {
		t.Errorf("bare id: %v", err)
	}
	if err := islandRm(s, &out, "hr", false); err != nil {
		t.Errorf("rm: %v", err)
	}
}

// contexts.yaml が壊れていても、repo の一覧を必要としない操作（親なしの add）は動く。必要な操作だけが失敗する。
func TestIslandOpsLoadContextsOnlyWhenNeeded(t *testing.T) {
	s, base := islandFixture(t)
	home, _ := os.UserHomeDir()
	if err := os.WriteFile(filepath.Join(home, ".config", "devctx", "contexts.yaml"), []byte("contexts: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := islandAdd(s, &out, base, "HR", "hr", ""); err != nil {
		t.Fatalf("add without parent should not read contexts: %v", err)
	}
	if err := islandAdd(s, &out, base, "Sub", "sub", "island:hr"); err != nil {
		t.Fatalf("typed island parent should not read contexts: %v", err)
	}
	if err := islandAttach(s, &out, base, "web", "hr"); err == nil {
		t.Error("bare repo name needs contexts; want error")
	}
}

// 親としてだけ参照される repo（contexts も RepoNode も無い）の下の island も list に出る。Web は同じ木を描く。
func TestIslandListShowsRepoReferencedOnlyAsParent(t *testing.T) {
	s, base := islandFixture(t)
	dir := t.TempDir()
	var out bytes.Buffer
	if err := islandAdd(s, &out, base, "Theme", "hr", ""); err != nil {
		t.Fatal(err)
	}
	if err := islandAttach(s, &out, base, "island:hr", "repo:"+dir); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := islandList(s, &out); err != nil {
		t.Fatal(err)
	}
	root := model.NormalizePath(dir)
	want := filepath.Base(root) + " (" + root + ", 0 active)\n└── Theme [island:hr]\n"
	if out.String() != want {
		t.Errorf("list output:\n%q\nwant:\n%q", out.String(), want)
	}
}
