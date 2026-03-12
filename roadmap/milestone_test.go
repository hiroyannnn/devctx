package roadmap

import (
	"fmt"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func TestCollectGitMilestones_EmptyContext(t *testing.T) {
	collector := &MilestoneCollector{
		Git: &mockGitRunner{results: map[string]mockResult{}},
	}
	ctx := &model.Context{Name: "test"}
	events := collector.CollectGitMilestones(ctx, &model.EventStore{})
	if len(events) != 0 {
		t.Fatalf("CollectGitMilestones(empty) len = %d, want 0", len(events))
	}
}

func TestCollectGitMilestones_NoBaseBranch(t *testing.T) {
	collector := &MilestoneCollector{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --verify origin/main":   {err: fmt.Errorf("not found")},
			"rev-parse --verify origin/master": {err: fmt.Errorf("not found")},
		}},
	}
	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	events := collector.CollectGitMilestones(ctx, &model.EventStore{})
	if len(events) != 0 {
		t.Fatalf("CollectGitMilestones(no base) len = %d, want 0", len(events))
	}
}

func TestCollectGitMilestones_FirstCommit(t *testing.T) {
	collector := &MilestoneCollector{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --verify origin/main":                        {output: "abc123"},
			"log origin/main..HEAD --oneline --reverse":             {output: "abc123 initial commit"},
			"rev-parse --verify origin/feature":                     {err: fmt.Errorf("not found")},
		}},
	}
	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	events := collector.CollectGitMilestones(ctx, &model.EventStore{})

	hasFirstCommit := false
	hasCommit := false
	for _, e := range events {
		if e.Type == model.MilestoneFirstCommit {
			hasFirstCommit = true
			if e.Detail != "initial commit" {
				t.Errorf("FirstCommit Detail = %q, want 'initial commit'", e.Detail)
			}
		}
		if e.Type == model.MilestoneCommit {
			hasCommit = true
		}
	}
	if !hasFirstCommit {
		t.Error("missing first_commit event")
	}
	if !hasCommit {
		t.Error("missing commit event")
	}
}

func TestCollectGitMilestones_SkipExistingFirstCommit(t *testing.T) {
	collector := &MilestoneCollector{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --verify origin/main":                        {output: "abc123"},
			"log origin/main..HEAD --oneline --reverse":             {output: "abc123 initial commit"},
			"rev-parse --verify origin/feature":                     {err: fmt.Errorf("not found")},
		}},
	}
	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}

	existing := &model.EventStore{}
	existing.Append(model.SessionEvent{SessionName: "test", Type: model.MilestoneFirstCommit})
	existing.Append(model.SessionEvent{SessionName: "test", Type: model.MilestoneCommit})

	events := collector.CollectGitMilestones(ctx, existing)

	for _, e := range events {
		if e.Type == model.MilestoneFirstCommit {
			t.Error("should not emit first_commit when already recorded")
		}
	}
}

func TestCollectGitMilestones_FirstPush(t *testing.T) {
	collector := &MilestoneCollector{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --verify origin/main":            {output: "abc123"},
			"log origin/main..HEAD --oneline --reverse": {output: "abc123 commit"},
			"rev-parse --verify origin/feature":         {output: "def456"},
		}},
	}
	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	events := collector.CollectGitMilestones(ctx, &model.EventStore{})

	hasPush := false
	for _, e := range events {
		if e.Type == model.MilestoneFirstPush {
			hasPush = true
		}
	}
	if !hasPush {
		t.Error("missing first_push event")
	}
}

func TestCollectGitMilestones_SkipExistingPush(t *testing.T) {
	collector := &MilestoneCollector{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --verify origin/main":            {output: "abc123"},
			"log origin/main..HEAD --oneline --reverse": {output: "abc123 commit"},
			"rev-parse --verify origin/feature":         {output: "def456"},
		}},
	}
	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	existing := &model.EventStore{}
	existing.Append(model.SessionEvent{SessionName: "test", Type: model.MilestoneFirstPush})
	existing.Append(model.SessionEvent{SessionName: "test", Type: model.MilestoneFirstCommit})
	existing.Append(model.SessionEvent{SessionName: "test", Type: model.MilestoneCommit})

	events := collector.CollectGitMilestones(ctx, existing)

	for _, e := range events {
		if e.Type == model.MilestoneFirstPush {
			t.Error("should not emit first_push when already recorded")
		}
	}
}

func TestCollectPRMilestones_NoGh(t *testing.T) {
	collector := &MilestoneCollector{
		Git: &mockGitRunner{results: map[string]mockResult{}},
		Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
	}
	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	events := collector.CollectPRMilestones(ctx, &model.EventStore{})
	if len(events) != 0 {
		t.Fatalf("CollectPRMilestones(no gh) len = %d, want 0", len(events))
	}
}

func TestCollectPRMilestones_PRCreated(t *testing.T) {
	collector := &MilestoneCollector{
		Git: &mockGitRunner{results: map[string]mockResult{}},
		Gh: &mockGhRunner{
			available: true,
			results: map[string]mockResult{
				"pr list --head feature --json state --limit 1":                {output: `[{"state":"OPEN"}]`},
				"pr list --head feature --state merged --json state --limit 1": {output: "[]"},
			},
		},
	}
	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	events := collector.CollectPRMilestones(ctx, &model.EventStore{})

	hasPR := false
	for _, e := range events {
		if e.Type == model.MilestonePRCreated {
			hasPR = true
		}
	}
	if !hasPR {
		t.Error("missing pr_created event")
	}
}

func TestCollectPRMilestones_PRMerged(t *testing.T) {
	collector := &MilestoneCollector{
		Git: &mockGitRunner{results: map[string]mockResult{}},
		Gh: &mockGhRunner{
			available: true,
			results: map[string]mockResult{
				"pr list --head feature --json state --limit 1":                {output: `[{"state":"MERGED"}]`},
				"pr list --head feature --state merged --json state --limit 1": {output: `[{"state":"MERGED"}]`},
			},
		},
	}
	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	events := collector.CollectPRMilestones(ctx, &model.EventStore{})

	hasPR := false
	hasMerged := false
	for _, e := range events {
		if e.Type == model.MilestonePRCreated {
			hasPR = true
		}
		if e.Type == model.MilestonePRMerged {
			hasMerged = true
		}
	}
	if !hasPR {
		t.Error("missing pr_created event")
	}
	if !hasMerged {
		t.Error("missing pr_merged event")
	}
}

func TestCollectPRMilestones_SkipExisting(t *testing.T) {
	collector := &MilestoneCollector{
		Git: &mockGitRunner{results: map[string]mockResult{}},
		Gh: &mockGhRunner{
			available: true,
			results: map[string]mockResult{
				"pr list --head feature --json state --limit 1":                {output: `[{"state":"OPEN"}]`},
				"pr list --head feature --state merged --json state --limit 1": {output: "[]"},
			},
		},
	}
	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	existing := &model.EventStore{}
	existing.Append(model.SessionEvent{SessionName: "test", Type: model.MilestonePRCreated})

	events := collector.CollectPRMilestones(ctx, existing)
	for _, e := range events {
		if e.Type == model.MilestonePRCreated {
			t.Error("should not emit pr_created when already recorded")
		}
	}
}
