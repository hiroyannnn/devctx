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
	// Same session re-registered (e.g. resume) reuses its context; a different session
	// gets its own context (see TestUpsertRegistration_NewCodexSessionInSameWorktreeIsSeparate)
	ctx, created := upsertRegistration(store, registration{
		Name: "feat-x", Worktree: "/w/feat-x", Provider: model.ProviderCodex, SessionID: "x1", TranscriptPath: "/t/x1.jsonl",
	}, registerNow)

	if created || ctx.Name != "feat-x-codex" || ctx.TranscriptPath != "/t/x1.jsonl" {
		t.Fatalf("created=%v ctx=%+v, want codex context updated", created, ctx)
	}
	if manual, created := upsertRegistration(store, registration{
		Name: "feat-x", Worktree: "/w/feat-x", Provider: model.ProviderCodex,
	}, registerNow); created || manual.Name != "feat-x-codex" {
		t.Fatalf("manual register without a session should reuse the worktree's codex context, got created=%v %q", created, manual.Name)
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

func TestUpsertRegistration_SessionIDMatchesBeforeWorktree(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "proj-codex", Worktree: "/w/proj", SessionID: "a", Provider: model.ProviderCodex},
		{Name: "proj-codex-2", Worktree: "/w/proj", SessionID: "b", Provider: model.ProviderCodex},
	}}
	ctx, created := upsertRegistration(store, registration{
		Name: "proj", Worktree: "/w/proj", Provider: model.ProviderCodex, SessionID: "b",
	}, registerNow)

	if created || ctx.Name != "proj-codex-2" {
		t.Fatalf("created=%v ctx=%q, want session b's own context updated", created, ctx.Name)
	}
	if store.FindByName("proj-codex").SessionID != "a" {
		t.Fatalf("session a's context must not be overwritten")
	}
}

func TestUpsertRegistration_NewCodexSessionInSameWorktreeIsSeparate(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "proj-codex", Worktree: "/w/proj", SessionID: "a", Provider: model.ProviderCodex},
	}}
	ctx, created := upsertRegistration(store, registration{
		Name: "proj", Worktree: "/w/proj", Provider: model.ProviderCodex, SessionID: "c",
	}, registerNow)

	if !created || ctx.SessionID != "c" || store.FindByName("proj-codex").SessionID != "a" {
		t.Fatalf("created=%v ctx=%+v; a new codex session should get its own context (codex is tracked per session)", created, ctx)
	}
}

func TestUniqueNameWithSuffix(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{{Name: "a"}, {Name: "a-x"}, {Name: "a-x-2"}}}
	if got := uniqueNameWithSuffix(store, "free", "x"); got != "free" {
		t.Fatalf("got %q", got)
	}
	if got := uniqueNameWithSuffix(store, "a", "x"); got != "a-x-3" {
		t.Fatalf("got %q", got)
	}
}

func TestTracksPerSession(t *testing.T) {
	if !tracksPerSession(model.ProviderCodex) || tracksPerSession(model.ProviderClaude) || tracksPerSession(model.ProviderManual) {
		t.Fatal("only codex is tracked per session")
	}
}
