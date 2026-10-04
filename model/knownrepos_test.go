package model

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

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
	store := &Store{Contexts: []Context{
		{Name: "a", RepoRoot: real},
		{Name: "b", RepoRoot: link},                              // symlink は同じ repo
		{Name: "c", Worktree: "/w/solo"},                         // RepoRoot なしは Worktree
		{Name: "d", RepoRoot: "/r/archived", Status: StatusDone}, // done でも repo は既知
		{Name: "e"}, // キー無しは除外
	}}
	is := &IslandStore{
		Repos:   []RepoNode{{Root: "/r/only-in-yaml", Parent: "island:x"}, {Root: real, Parent: "island:x"}},
		Islands: []Island{{ID: "x", Name: "X", Parent: "repo:/r/parent-only"}},
	}

	is.Normalize() // LoadIslands が済ませる前提。ScanRepos 自身は再正規化しない
	got, active := ScanRepos(store, is)
	if active[NormalizePath(real)] != 2 || active["/r/archived"] != 0 || active["/w/solo"] != 1 {
		t.Errorf("active = %v", active) // done は数えない
	}
	// 一時ディレクトリの位置は OS で違う（macOS: /var/folders、Linux: /tmp）ので、期待値も同じ規則でソートする
	want := []string{NormalizePath(real), "/r/archived", "/r/only-in-yaml", "/r/parent-only", "/w/solo"}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %v, want %v", got, want)
	}
	if kr := KnownRepos(store, is); strings.Join(kr, "\n") != strings.Join(want, "\n") {
		t.Errorf("KnownRepos = %v, want %v", kr, want)
	}
}
