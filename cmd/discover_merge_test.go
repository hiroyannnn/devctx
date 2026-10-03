package cmd

import (
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

func TestMergeDiscoveredSessions_ImportsNewCodexSession(t *testing.T) {
	store := &model.Store{}
	mtime := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

	names, changed := mergeDiscoveredSessions(store, []DiscoveredSession{{
		Provider:       model.ProviderCodex,
		SessionID:      "cx-1",
		SessionName:    "Fix the bug",
		TranscriptPath: "/c/rollout-cx-1.jsonl",
		ProjectPath:    "/w/proj/sub",
		Worktree:       "/w/proj",
		RepoRoot:       "/w/proj",
		Branch:         "feat/x",
		LastModified:   mtime,
	}}, now)

	if !changed || len(names) != 1 || len(store.Contexts) != 1 {
		t.Fatalf("names=%v changed=%v contexts=%d", names, changed, len(store.Contexts))
	}
	c := store.Contexts[0]
	if c.Name != "x" || names[0] != "x" {
		t.Fatalf("name = %q (names %v)", c.Name, names)
	}
	if c.Provider != model.ProviderCodex || c.SessionID != "cx-1" || c.SessionName != "Fix the bug" ||
		c.Worktree != "/w/proj" || c.RepoRoot != "/w/proj" || c.Branch != "feat/x" ||
		c.TranscriptPath != "/c/rollout-cx-1.jsonl" || c.Status != model.StatusInProgress ||
		!c.CreatedAt.Equal(mtime) || !c.LastSeen.Equal(mtime) {
		t.Fatalf("unexpected context: %+v", c)
	}
}

func TestMergeDiscoveredSessions_CodexFallsBackToCwdOutsideGit(t *testing.T) {
	store := &model.Store{}
	mergeDiscoveredSessions(store, []DiscoveredSession{{
		Provider: model.ProviderCodex, SessionID: "cx-1", ProjectPath: "/tmp/scratch",
	}}, time.Now())
	if c := store.Contexts[0]; c.Worktree != "/tmp/scratch" || c.Name != "scratch" {
		t.Fatalf("unexpected context: %+v", c)
	}
}

func TestMergeDiscoveredSessions_CodexNameCollisionWithClaudeContext(t *testing.T) {
	store := &model.Store{}
	store.Add(model.Context{Name: "proj", Worktree: "/w/proj", SessionID: "cl-1"})
	names, _ := mergeDiscoveredSessions(store, []DiscoveredSession{
		{Provider: model.ProviderCodex, SessionID: "cx-1", Worktree: "/w/proj", ProjectPath: "/w/proj"},
		{Provider: model.ProviderCodex, SessionID: "cx-2", Worktree: "/w/proj", ProjectPath: "/w/proj"},
	}, time.Now())
	if len(names) != 2 || names[0] != "proj-codex" || names[1] != "proj-codex-2" {
		t.Fatalf("names = %v", names)
	}
}

func TestMergeDiscoveredSessions_RefreshesRegisteredOnlyWhenNewer(t *testing.T) {
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	older := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	store := &model.Store{}
	c := model.Context{Name: "a", Provider: model.ProviderCodex, SessionID: "cx-1", LastSeen: old, Status: model.StatusReview, Branch: "keep"}
	c.SetAgentState(model.AgentNeedsInput, old)
	store.Add(c)
	wantState := store.Contexts[0].AgentState

	names, changed := mergeDiscoveredSessions(store, []DiscoveredSession{
		{Provider: model.ProviderCodex, SessionID: "cx-1", LastModified: newer, Branch: "other", IsRegistered: true},
	}, time.Now())
	if len(names) != 0 || !changed || len(store.Contexts) != 1 {
		t.Fatalf("names=%v changed=%v contexts=%d", names, changed, len(store.Contexts))
	}
	got := store.Contexts[0]
	if !got.LastSeen.Equal(newer) {
		t.Fatalf("LastSeen = %v, want %v", got.LastSeen, newer)
	}
	if got.AgentState != wantState || got.Status != model.StatusReview || got.Branch != "keep" {
		t.Fatalf("other fields were touched: %+v", got)
	}

	_, changed = mergeDiscoveredSessions(store, []DiscoveredSession{
		{Provider: model.ProviderCodex, SessionID: "cx-1", LastModified: older},
	}, time.Now())
	if changed || !store.Contexts[0].LastSeen.Equal(newer) {
		t.Fatalf("LastSeen must not go backwards: changed=%v %v", changed, store.Contexts[0].LastSeen)
	}
}

func TestMergeDiscoveredSessions_ProviderIsPartOfIdentity(t *testing.T) {
	store := &model.Store{}
	store.Add(model.Context{Name: "a", SessionID: "same-id"}) // claude
	names, _ := mergeDiscoveredSessions(store, []DiscoveredSession{
		{Provider: model.ProviderCodex, SessionID: "same-id", ProjectPath: "/w/a"},
	}, time.Now())
	if len(names) != 1 {
		t.Fatalf("codex session with the same id as a claude context must be imported: %v", names)
	}
}

func TestMergeDiscoveredSessions_ClaudeImportUnchanged(t *testing.T) {
	store := &model.Store{}
	store.Add(model.Context{Name: "proj", SessionID: "other"})
	mtime := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)

	names, changed := mergeDiscoveredSessions(store, []DiscoveredSession{{
		Provider: model.ProviderClaude, SessionID: "abcdef123", SessionName: "slug",
		TranscriptPath: "/t/abcdef123.jsonl", ProjectPath: "/w/proj", Branch: "main", LastModified: mtime,
	}}, time.Now())
	if !changed || len(names) != 1 || names[0] != "proj-abcdef" {
		t.Fatalf("names=%v changed=%v", names, changed)
	}
	c := store.Contexts[1]
	if c.Provider != "" || c.Worktree != "/w/proj" || c.Branch != "main" || c.SessionName != "slug" ||
		c.RepoRoot != "" || !c.CreatedAt.Equal(mtime) {
		t.Fatalf("unexpected context: %+v", c)
	}
}

func TestMergeDiscoveredSessions_ClaudeCollisionLoopsUntilUnique(t *testing.T) {
	store := &model.Store{}
	store.Add(model.Context{Name: "proj", SessionID: "x"})
	store.Add(model.Context{Name: "proj-abcdef", SessionID: "y"})
	names, _ := mergeDiscoveredSessions(store, []DiscoveredSession{
		{Provider: model.ProviderClaude, SessionID: "abcdef123", ProjectPath: "/w/proj"},
	}, time.Now())
	if len(names) != 1 || names[0] != "proj-abcdef-2" {
		t.Fatalf("names = %v", names)
	}
}

func TestSelectAdapters(t *testing.T) {
	all, err := selectAdapters("")
	if err != nil || len(all) != 2 {
		t.Fatalf("all = %v, %v", all, err)
	}
	one, err := selectAdapters("codex")
	if err != nil || len(one) != 1 || one[0].Provider() != model.ProviderCodex {
		t.Fatalf("codex = %v, %v", one, err)
	}
	if _, err := selectAdapters("manual"); err == nil {
		t.Fatal("manual has no discovery")
	}
	if _, err := selectAdapters("bogus"); err == nil {
		t.Fatal("unknown provider must error")
	}
}
