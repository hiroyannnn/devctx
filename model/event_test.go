package model

import (
	"testing"
	"time"
)

func TestEventStoreForSession(t *testing.T) {
	now := time.Now()
	store := &EventStore{
		Events: []SessionEvent{
			{SessionName: "auth", Type: MilestoneCommit, OccurredAt: now},
			{SessionName: "api-fix", Type: MilestoneCommit, OccurredAt: now},
			{SessionName: "auth", Type: MilestoneFirstPush, OccurredAt: now},
		},
	}

	events := store.ForSession("auth")
	if len(events) != 2 {
		t.Fatalf("ForSession(auth) len = %d, want 2", len(events))
	}

	events = store.ForSession("missing")
	if len(events) != 0 {
		t.Fatalf("ForSession(missing) len = %d, want 0", len(events))
	}
}

func TestEventStoreAppend(t *testing.T) {
	store := &EventStore{}
	now := time.Now()

	store.Append(SessionEvent{
		SessionName: "auth",
		Type:        MilestoneSessionStart,
		OccurredAt:  now,
		ObservedAt:  now,
	})

	if len(store.Events) != 1 {
		t.Fatalf("Events len = %d, want 1", len(store.Events))
	}
	if store.Events[0].SessionName != "auth" {
		t.Errorf("SessionName = %q, want auth", store.Events[0].SessionName)
	}
}

func TestEventStoreHasMilestone(t *testing.T) {
	now := time.Now()
	store := &EventStore{
		Events: []SessionEvent{
			{SessionName: "auth", Type: MilestoneFirstCommit, OccurredAt: now},
		},
	}

	if !store.HasMilestone("auth", MilestoneFirstCommit) {
		t.Error("HasMilestone(auth, first_commit) = false, want true")
	}
	if store.HasMilestone("auth", MilestoneFirstPush) {
		t.Error("HasMilestone(auth, first_push) = true, want false")
	}
	if store.HasMilestone("missing", MilestoneFirstCommit) {
		t.Error("HasMilestone(missing, first_commit) = true, want false")
	}
}

func TestEventStoreSummarize(t *testing.T) {
	t1 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	t4 := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	t5 := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)

	store := &EventStore{
		Events: []SessionEvent{
			{SessionName: "auth", Type: MilestoneSessionStart, OccurredAt: t1, ObservedAt: t1},
			{SessionName: "auth", Type: MilestoneFirstCommit, OccurredAt: t2, ObservedAt: t2},
			{SessionName: "auth", Type: MilestoneCommit, OccurredAt: t2, ObservedAt: t2},
			{SessionName: "auth", Type: MilestoneCommit, OccurredAt: t3, ObservedAt: t3},
			{SessionName: "auth", Type: MilestoneFirstPush, OccurredAt: t4, ObservedAt: t4},
			{SessionName: "auth", Type: MilestonePRCreated, OccurredAt: t5, ObservedAt: t5},
			{SessionName: "auth", Type: MilestoneCommand, Detail: "move review", OccurredAt: t3, ObservedAt: t3},
			// Different session - should not be counted
			{SessionName: "api-fix", Type: MilestoneCommit, OccurredAt: t1, ObservedAt: t1},
		},
	}

	summary := store.Summarize("auth")

	if summary.SessionName != "auth" {
		t.Errorf("SessionName = %q, want auth", summary.SessionName)
	}
	if !summary.FirstCommitAt.Equal(t2) {
		t.Errorf("FirstCommitAt = %v, want %v", summary.FirstCommitAt, t2)
	}
	if summary.CommitCount != 2 {
		t.Errorf("CommitCount = %d, want 2", summary.CommitCount)
	}
	if !summary.LatestCommitAt.Equal(t3) {
		t.Errorf("LatestCommitAt = %v, want %v", summary.LatestCommitAt, t3)
	}
	if !summary.FirstPushAt.Equal(t4) {
		t.Errorf("FirstPushAt = %v, want %v", summary.FirstPushAt, t4)
	}
	if !summary.PRCreatedAt.Equal(t5) {
		t.Errorf("PRCreatedAt = %v, want %v", summary.PRCreatedAt, t5)
	}
	if summary.SessionCount != 1 {
		t.Errorf("SessionCount = %d, want 1", summary.SessionCount)
	}
	if summary.CommandCount != 1 {
		t.Errorf("CommandCount = %d, want 1", summary.CommandCount)
	}
}

func TestEventStoreSummarizeEmpty(t *testing.T) {
	store := &EventStore{}
	summary := store.Summarize("missing")

	if summary.SessionName != "missing" {
		t.Errorf("SessionName = %q, want missing", summary.SessionName)
	}
	if summary.CommitCount != 0 {
		t.Errorf("CommitCount = %d, want 0", summary.CommitCount)
	}
}

func TestMilestoneTypeConstants(t *testing.T) {
	types := []MilestoneType{
		MilestoneFirstCommit, MilestoneCommit, MilestoneFirstPush,
		MilestonePRCreated, MilestonePRMerged, MilestoneSessionStart,
		MilestoneSessionEnd, MilestoneCommand, MilestoneStatusChange,
	}
	expected := []string{
		"first_commit", "commit", "first_push",
		"pr_created", "pr_merged", "session_start",
		"session_end", "command", "status_change",
	}
	for i, mt := range types {
		if string(mt) != expected[i] {
			t.Errorf("MilestoneType[%d] = %q, want %q", i, mt, expected[i])
		}
	}
}
