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
