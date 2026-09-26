package cmd

import (
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func resolveTestStore() *model.Store {
	return &model.Store{Contexts: []model.Context{
		{Name: "solo", Worktree: "/w/solo"},
		{Name: "feat-x", Worktree: "/w/feat-x"},
		{Name: "feat-x-codex", Worktree: "/w/feat-x", Provider: model.ProviderCodex},
	}}
}

func TestResolveContext_ExplicitNameWins(t *testing.T) {
	ctx, err := resolveContext(resolveTestStore(), []string{"feat-x-codex"}, "/w/solo")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name != "feat-x-codex" {
		t.Fatalf("got %q, want explicit name feat-x-codex even when worktree matches another context", ctx.Name)
	}
}

func TestResolveContext_UnknownName(t *testing.T) {
	_, err := resolveContext(resolveTestStore(), []string{"missing"}, "/w/solo")
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err = %v, want not found error naming the context", err)
	}
}

func TestResolveContext_SingleWorktreeMatch(t *testing.T) {
	ctx, err := resolveContext(resolveTestStore(), nil, "/w/solo")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name != "solo" {
		t.Fatalf("got %q, want solo", ctx.Name)
	}
}

func TestResolveContext_NoWorktreeMatch(t *testing.T) {
	_, err := resolveContext(resolveTestStore(), nil, "/w/none")
	if err == nil {
		t.Fatalf("err = nil, want error")
	}
}

func TestResolveContext_AmbiguousWorktree(t *testing.T) {
	_, err := resolveContext(resolveTestStore(), nil, "/w/feat-x")
	if err == nil {
		t.Fatalf("err = nil, want ambiguity error")
	}
	for _, want := range []string{"feat-x (claude)", "feat-x-codex (codex)"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %q, want it to list %q", err, want)
		}
	}
}

func TestResolveHookContext_PrefersClaudeSession(t *testing.T) {
	store := resolveTestStore()
	store.Contexts[1].SessionID = "c1"
	ctx, err := resolveHookContext(store, nil, "c1", "/w/feat-x")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name != "feat-x" {
		t.Fatalf("got %q, want claude context resolved by session id despite ambiguous worktree", ctx.Name)
	}
}

func TestResolveHookContext_FallsBackToWorktree(t *testing.T) {
	ctx, err := resolveHookContext(resolveTestStore(), nil, "unknown", "/w/solo")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name != "solo" {
		t.Fatalf("got %q, want solo", ctx.Name)
	}
}

func TestResolveHookContext_ExplicitNameWins(t *testing.T) {
	store := resolveTestStore()
	store.Contexts[1].SessionID = "c1"
	ctx, err := resolveHookContext(store, []string{"solo"}, "c1", "/w/feat-x")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Name != "solo" {
		t.Fatalf("got %q, want explicit name solo", ctx.Name)
	}
}

func TestReadHookSessionID(t *testing.T) {
	if got := readHookSessionID(strings.NewReader(`{"session_id":"c1","cwd":"/w"}` + "\n")); got != "c1" {
		t.Fatalf("readHookSessionID = %q, want c1", got)
	}
	if got := readHookSessionID(strings.NewReader("not json")); got != "" {
		t.Fatalf("readHookSessionID(invalid) = %q, want empty", got)
	}
	if got := readHookSessionID(strings.NewReader("")); got != "" {
		t.Fatalf("readHookSessionID(empty) = %q, want empty", got)
	}
}
