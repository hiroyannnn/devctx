package cmd

import (
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

var registerNow = time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)

func TestUpsertRegistration_CreatesNewContext(t *testing.T) {
	store := &model.Store{}
	ctx, created := upsertRegistration(store, registration{
		Name: "feat-x", Worktree: "/w/feat-x", Branch: "feat/x", Provider: model.ProviderClaude, SessionID: "c1",
	}, registerNow)

	if !created {
		t.Fatalf("created = false, want true")
	}
	if ctx.Name != "feat-x" || ctx.SessionID != "c1" || ctx.Status != model.StatusInProgress {
		t.Fatalf("unexpected context: %+v", ctx)
	}
	if ctx.Provider != "" {
		t.Fatalf("claude context should keep provider empty for legacy format, got %q", ctx.Provider)
	}
	if len(store.Contexts) != 1 {
		t.Fatalf("store has %d contexts, want 1", len(store.Contexts))
	}
}

func TestUpsertRegistration_ClaudeReusesSameWorktreeContext(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "feat-x", Worktree: "/w/feat-x", SessionID: "c1", Status: model.StatusInProgress},
	}}
	ctx, created := upsertRegistration(store, registration{
		Name: "feat-x", Worktree: "/w/feat-x", Provider: model.ProviderClaude, SessionID: "c2", TranscriptPath: "/t/c2.jsonl",
	}, registerNow)

	if created {
		t.Fatalf("created = true, want false (same worktree + provider should be reused)")
	}
	if ctx.SessionID != "c2" || ctx.TranscriptPath != "/t/c2.jsonl" || !ctx.LastSeen.Equal(registerNow) {
		t.Fatalf("existing context was not updated: %+v", ctx)
	}
	if len(store.Contexts) != 1 {
		t.Fatalf("store has %d contexts, want 1", len(store.Contexts))
	}
}

func TestUpsertRegistration_CodexDoesNotOverwriteClaude(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "feat-x", Worktree: "/w/feat-x", SessionID: "c1"},
	}}
	ctx, created := upsertRegistration(store, registration{
		Name: "feat-x", Worktree: "/w/feat-x", Provider: model.ProviderCodex, SessionID: "x1",
	}, registerNow)

	if !created {
		t.Fatalf("created = false, want true")
	}
	if ctx.Name != "feat-x-codex" || ctx.Provider != model.ProviderCodex || ctx.SessionID != "x1" {
		t.Fatalf("unexpected codex context: %+v", ctx)
	}
	claude := store.FindByName("feat-x")
	if claude.SessionID != "c1" {
		t.Fatalf("claude context was overwritten: %+v", claude)
	}
}

func TestUpsertRegistration_CodexReusesCodexContext(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "feat-x", Worktree: "/w/feat-x", SessionID: "c1"},
		{Name: "feat-x-codex", Worktree: "/w/feat-x", SessionID: "x1", Provider: model.ProviderCodex},
	}}
	ctx, created := upsertRegistration(store, registration{
		Name: "feat-x", Worktree: "/w/feat-x", Provider: model.ProviderCodex, SessionID: "x2",
	}, registerNow)

	if created || ctx.Name != "feat-x-codex" || ctx.SessionID != "x2" {
		t.Fatalf("created=%v ctx=%+v, want codex context updated", created, ctx)
	}
}

func TestUpsertRegistration_EmptySessionKeepsExisting(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "feat-x", Worktree: "/w/feat-x", SessionID: "c1", TranscriptPath: "/t/c1.jsonl"},
	}}
	ctx, _ := upsertRegistration(store, registration{
		Name: "feat-x", Worktree: "/w/feat-x", Provider: model.ProviderClaude,
	}, registerNow)

	if ctx.SessionID != "c1" || ctx.TranscriptPath != "/t/c1.jsonl" {
		t.Fatalf("manual register should not clear session: %+v", ctx)
	}
}

func TestUniqueContextName(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		base     string
		provider model.Provider
		want     string
	}{
		{name: "free", existing: nil, base: "feat-x", provider: model.ProviderClaude, want: "feat-x"},
		{name: "claude collision uses date", existing: []string{"feat-x"}, base: "feat-x", provider: model.ProviderClaude, want: "feat-x-0926"},
		{name: "codex collision uses provider", existing: []string{"feat-x"}, base: "feat-x", provider: model.ProviderCodex, want: "feat-x-codex"},
		{name: "repeated collision adds counter", existing: []string{"feat-x", "feat-x-0926"}, base: "feat-x", provider: model.ProviderClaude, want: "feat-x-0926-2"},
		{name: "counter keeps increasing", existing: []string{"feat-x", "feat-x-codex", "feat-x-codex-2"}, base: "feat-x", provider: model.ProviderCodex, want: "feat-x-codex-3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &model.Store{}
			for _, n := range tt.existing {
				store.Contexts = append(store.Contexts, model.Context{Name: n})
			}
			if got := uniqueContextName(store, tt.base, tt.provider, registerNow); got != tt.want {
				t.Fatalf("uniqueContextName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpsertRegistration_NewSessionResetsAgentState(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "feat-x", Worktree: "/w/feat-x", SessionID: "c1", AgentState: model.AgentEnded, AgentStateAt: registerNow.Add(-time.Hour)},
	}}
	ctx, _ := upsertRegistration(store, registration{
		Name: "feat-x", Worktree: "/w/feat-x", Provider: model.ProviderClaude, SessionID: "c2",
	}, registerNow)

	if ctx.AgentState != "" || !ctx.AgentStateAt.IsZero() {
		t.Fatalf("session start should clear the previous session's state, got %q at %v", ctx.AgentState, ctx.AgentStateAt)
	}
}

func TestUpsertRegistration_ManualRegisterKeepsAgentState(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "feat-x", Worktree: "/w/feat-x", SessionID: "c1", AgentState: model.AgentTurnDone},
	}}
	ctx, _ := upsertRegistration(store, registration{
		Name: "feat-x", Worktree: "/w/feat-x", Provider: model.ProviderClaude,
	}, registerNow)

	if ctx.AgentState != model.AgentTurnDone {
		t.Fatalf("manual register without a session should keep state, got %q", ctx.AgentState)
	}
}
