package model

import (
	"testing"
	"time"
)

func TestStoreFinders(t *testing.T) {
	store := &Store{
		Contexts: []Context{
			{Name: "auth", SessionID: "session-1", Worktree: "/tmp/auth"},
			{Name: "api-fix", SessionID: "session-2", Worktree: "/tmp/api-fix"},
		},
	}

	if got := store.FindByName("auth"); got == nil || got.SessionID != "session-1" {
		t.Fatalf("FindByName(auth) = %#v, want session-1", got)
	}

	if got := store.FindBySessionID("session-2"); got == nil || got.Name != "api-fix" {
		t.Fatalf("FindBySessionID(session-2) = %#v, want api-fix", got)
	}

	if got := store.FindByWorktree("/tmp/auth"); got == nil || got.Name != "auth" {
		t.Fatalf("FindByWorktree(/tmp/auth) = %#v, want auth", got)
	}

	if got := store.FindByName("missing"); got != nil {
		t.Fatalf("FindByName(missing) = %#v, want nil", got)
	}
}

func TestStoreActiveWithRetention(t *testing.T) {
	now := time.Now()
	store := &Store{
		Contexts: []Context{
			{Name: "in-progress", Status: StatusInProgress, LastSeen: now.Add(-72 * time.Hour)},
			{Name: "done-recent", Status: StatusDone, LastSeen: now.Add(-12 * time.Hour)},
			{Name: "done-old", Status: StatusDone, LastSeen: now.Add(-48 * time.Hour)},
		},
	}

	active := store.Active()
	if hasContext(active, "in-progress") != true {
		t.Fatalf("Active() should include in-progress context")
	}
	if hasContext(active, "done-recent") {
		t.Fatalf("Active() should not include done context without retention")
	}

	retained := store.ActiveWithRetention(1)
	if !hasContext(retained, "in-progress") {
		t.Fatalf("ActiveWithRetention(1) should include in-progress context")
	}
	if !hasContext(retained, "done-recent") {
		t.Fatalf("ActiveWithRetention(1) should include recently done context")
	}
	if hasContext(retained, "done-old") {
		t.Fatalf("ActiveWithRetention(1) should exclude old done context")
	}
}

func TestStoreByStatusAddRemove(t *testing.T) {
	store := &Store{}
	store.Add(Context{Name: "auth", Status: StatusInProgress})
	store.Add(Context{Name: "api-fix", Status: StatusReview})

	inProgress := store.ByStatus(StatusInProgress)
	if len(inProgress) != 1 || inProgress[0].Name != "auth" {
		t.Fatalf("ByStatus(in-progress) = %#v, want one context named auth", inProgress)
	}

	if ok := store.Remove("auth"); !ok {
		t.Fatalf("Remove(auth) = false, want true")
	}
	if got := store.FindByName("auth"); got != nil {
		t.Fatalf("FindByName(auth) after remove = %#v, want nil", got)
	}

	if ok := store.Remove("missing"); ok {
		t.Fatalf("Remove(missing) = true, want false")
	}
}

func TestAllPhases(t *testing.T) {
	phases := AllPhases()
	if len(phases) != 6 {
		t.Fatalf("AllPhases() returned %d phases, want 6", len(phases))
	}

	expected := []Phase{PhaseIdle, PhaseImplementation, PhaseCommitted, PhasePushed, PhasePROpen, PhaseDone}
	for i, p := range phases {
		if p != expected[i] {
			t.Errorf("AllPhases()[%d] = %q, want %q", i, p, expected[i])
		}
	}
}

func TestPhaseLabel(t *testing.T) {
	tests := []struct {
		phase Phase
		want  string
	}{
		{PhaseIdle, "Idle"},
		{PhaseImplementation, "Implementation"},
		{PhaseCommitted, "Committed"},
		{PhasePushed, "Pushed"},
		{PhasePROpen, "PR Open"},
		{PhaseDone, "Done"},
		{Phase("custom"), "custom"},
		{Phase(""), "(unknown)"},
	}

	for _, tt := range tests {
		got := tt.phase.Label()
		if got != tt.want {
			t.Errorf("Phase(%q).Label() = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestContextInitialPrompt(t *testing.T) {
	store := &Store{}
	store.Add(Context{
		Name:          "test",
		InitialPrompt: "認証機能を実装して",
	})

	ctx := store.FindByName("test")
	if ctx == nil {
		t.Fatal("FindByName(test) returned nil")
	}
	if ctx.InitialPrompt != "認証機能を実装して" {
		t.Errorf("InitialPrompt = %q, want %q", ctx.InitialPrompt, "認証機能を実装して")
	}
}

func TestContextPhaseFields(t *testing.T) {
	now := time.Now()
	store := &Store{}
	store.Add(Context{
		Name:           "test",
		Phase:          PhaseCommitted,
		PhaseCheckedAt: now,
	})

	ctx := store.FindByName("test")
	if ctx == nil {
		t.Fatal("FindByName(test) returned nil")
	}
	if ctx.Phase != PhaseCommitted {
		t.Errorf("Phase = %q, want %q", ctx.Phase, PhaseCommitted)
	}
	if !ctx.PhaseCheckedAt.Equal(now) {
		t.Errorf("PhaseCheckedAt = %v, want %v", ctx.PhaseCheckedAt, now)
	}
}

func TestInsightStore(t *testing.T) {
	store := &InsightStore{}

	// Initially empty
	if got := store.Get("auth"); got != nil {
		t.Fatalf("Get(auth) = %#v, want nil", got)
	}

	now := time.Now()
	insight := SessionInsight{
		Name:             "auth",
		Goal:             "OAuth認証を実装する",
		CurrentFocus:     "tokenリフレッシュのテスト",
		NextStep:         "エラーハンドリング追加",
		AttentionState:   AttentionActive,
		InferredAt:       now,
		TranscriptOffset: 4096,
	}

	store.Set(insight)

	got := store.Get("auth")
	if got == nil {
		t.Fatal("Get(auth) returned nil after Set")
	}
	if got.Goal != "OAuth認証を実装する" {
		t.Errorf("Goal = %q, want %q", got.Goal, "OAuth認証を実装する")
	}
	if got.AttentionState != AttentionActive {
		t.Errorf("AttentionState = %q, want %q", got.AttentionState, AttentionActive)
	}
	if got.TranscriptOffset != 4096 {
		t.Errorf("TranscriptOffset = %d, want 4096", got.TranscriptOffset)
	}

	// Update existing
	insight.Goal = "OAuth2認証を実装する"
	store.Set(insight)
	got = store.Get("auth")
	if got.Goal != "OAuth2認証を実装する" {
		t.Errorf("Goal after update = %q, want %q", got.Goal, "OAuth2認証を実装する")
	}
	if len(store.Insights) != 1 {
		t.Errorf("Insights len = %d, want 1 (no duplicates)", len(store.Insights))
	}
}

func TestAttentionStateConstants(t *testing.T) {
	states := []AttentionState{AttentionActive, AttentionWaiting, AttentionIdle, AttentionBlocked}
	expected := []string{"active", "waiting", "idle", "blocked"}
	for i, s := range states {
		if string(s) != expected[i] {
			t.Errorf("state[%d] = %q, want %q", i, s, expected[i])
		}
	}
}

func TestContextRepoRoot(t *testing.T) {
	store := &Store{}
	store.Add(Context{
		Name:     "auth",
		Worktree: "/tmp/project/worktrees/auth",
		RepoRoot: "/tmp/project",
	})
	store.Add(Context{
		Name:     "api-fix",
		Worktree: "/tmp/project/worktrees/api-fix",
		RepoRoot: "/tmp/project",
	})
	store.Add(Context{
		Name:     "other",
		Worktree: "/tmp/other-project",
		RepoRoot: "/tmp/other-project",
	})

	ctx := store.FindByName("auth")
	if ctx.RepoRoot != "/tmp/project" {
		t.Errorf("RepoRoot = %q, want /tmp/project", ctx.RepoRoot)
	}
}

func hasContext(contexts []Context, name string) bool {
	for _, c := range contexts {
		if c.Name == name {
			return true
		}
	}
	return false
}
