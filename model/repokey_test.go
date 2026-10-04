package model

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolvedReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty stays empty", "", ""},
		{"symlink is resolved", link, resolvedReal},
		{"trailing slash and dots are cleaned", real + "/./", resolvedReal},
		{"missing path falls back to Clean", "/no/such/dir/../path/", "/no/such/path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizePath(tt.in); got != tt.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRepoKey(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "repo")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "repo-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	t.Run("symlinked and plain RepoRoot share a key", func(t *testing.T) {
		a := RepoKey(Context{RepoRoot: real})
		b := RepoKey(Context{RepoRoot: link})
		if a == "" || a != b {
			t.Errorf("keys differ: %q vs %q", a, b)
		}
	})
	t.Run("empty RepoRoot falls back to Worktree", func(t *testing.T) {
		got := RepoKey(Context{Worktree: link})
		if got != RepoKey(Context{RepoRoot: real}) {
			t.Errorf("RepoKey(worktree only) = %q", got)
		}
	})
	t.Run("RepoRoot wins over Worktree", func(t *testing.T) {
		got := RepoKey(Context{RepoRoot: real, Worktree: "/elsewhere"})
		if got != NormalizePath(real) {
			t.Errorf("got %q", got)
		}
	})
	t.Run("both empty is empty", func(t *testing.T) {
		if got := RepoKey(Context{}); got != "" {
			t.Errorf("got %q", got)
		}
	})
}
