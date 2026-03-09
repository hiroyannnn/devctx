package roadmap

import (
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// MilestoneCollector extracts milestone events from git state.
type MilestoneCollector struct {
	Git GitRunner
	Gh  GhRunner
}

// NewMilestoneCollector creates a collector with real command runners.
func NewMilestoneCollector() *MilestoneCollector {
	return &MilestoneCollector{
		Git: &ExecGitRunner{},
		Gh:  &ExecGhRunner{},
	}
}

// CollectGitMilestones detects git-based milestones that haven't been recorded yet.
// It checks for commits ahead of the base branch and returns new events.
func (c *MilestoneCollector) CollectGitMilestones(ctx *model.Context, existing *model.EventStore) []model.SessionEvent {
	if ctx.Worktree == "" || ctx.Branch == "" {
		return nil
	}

	now := time.Now()
	var events []model.SessionEvent

	base := c.detectBaseBranch(ctx.Worktree)
	if base == "" {
		return nil
	}

	// Check for commits ahead of base
	ahead, err := c.Git.Run(ctx.Worktree, "log", "origin/"+base+"..HEAD", "--oneline", "--reverse")
	if err != nil || ahead == "" {
		return nil
	}

	lines := strings.Split(ahead, "\n")

	// Record first_commit if not already recorded
	if !existing.HasMilestone(ctx.Name, model.MilestoneFirstCommit) && len(lines) > 0 {
		parts := strings.SplitN(lines[0], " ", 2)
		detail := ""
		if len(parts) >= 2 {
			detail = parts[1]
		}
		events = append(events, model.SessionEvent{
			SessionName: ctx.Name,
			Type:        model.MilestoneFirstCommit,
			Detail:      detail,
			OccurredAt:  now,
			ObservedAt:  now,
		})
	}

	// Record latest commits (up to 3)
	maxCommits := 3
	if len(lines) < maxCommits {
		maxCommits = len(lines)
	}
	existingCommitCount := countEvents(existing, ctx.Name, model.MilestoneCommit)
	if len(lines) > existingCommitCount {
		// Record new commits since last check
		for i := existingCommitCount; i < len(lines) && i < existingCommitCount+maxCommits; i++ {
			parts := strings.SplitN(lines[i], " ", 2)
			detail := ""
			if len(parts) >= 2 {
				detail = parts[1]
			}
			events = append(events, model.SessionEvent{
				SessionName: ctx.Name,
				Type:        model.MilestoneCommit,
				Detail:      detail,
				OccurredAt:  now,
				ObservedAt:  now,
			})
		}
	}

	// Check for push (remote branch exists and is up-to-date)
	if !existing.HasMilestone(ctx.Name, model.MilestoneFirstPush) {
		remoteRef := "origin/" + ctx.Branch
		if _, err := c.Git.Run(ctx.Worktree, "rev-parse", "--verify", remoteRef); err == nil {
			events = append(events, model.SessionEvent{
				SessionName: ctx.Name,
				Type:        model.MilestoneFirstPush,
				OccurredAt:  now,
				ObservedAt:  now,
			})
		}
	}

	return events
}

// CollectPRMilestones detects PR-related milestones using gh CLI.
func (c *MilestoneCollector) CollectPRMilestones(ctx *model.Context, existing *model.EventStore) []model.SessionEvent {
	if ctx.Worktree == "" || ctx.Branch == "" {
		return nil
	}
	if c.Gh == nil || !c.Gh.Available() {
		return nil
	}

	now := time.Now()
	var events []model.SessionEvent

	// Check for open PR
	if !existing.HasMilestone(ctx.Name, model.MilestonePRCreated) {
		out, err := c.Gh.Run(ctx.Worktree, "pr", "list",
			"--head", ctx.Branch, "--json", "state", "--limit", "1")
		if err == nil && out != "" && out != "[]" {
			events = append(events, model.SessionEvent{
				SessionName: ctx.Name,
				Type:        model.MilestonePRCreated,
				OccurredAt:  now,
				ObservedAt:  now,
			})
		}
	}

	// Check for merged PR
	if !existing.HasMilestone(ctx.Name, model.MilestonePRMerged) {
		out, err := c.Gh.Run(ctx.Worktree, "pr", "list",
			"--head", ctx.Branch, "--state", "merged", "--json", "state", "--limit", "1")
		if err == nil && out != "" && out != "[]" {
			events = append(events, model.SessionEvent{
				SessionName: ctx.Name,
				Type:        model.MilestonePRMerged,
				OccurredAt:  now,
				ObservedAt:  now,
			})
		}
	}

	return events
}

func (c *MilestoneCollector) detectBaseBranch(dir string) string {
	for _, branch := range []string{"main", "master"} {
		if _, err := c.Git.Run(dir, "rev-parse", "--verify", "origin/"+branch); err == nil {
			return branch
		}
	}
	return ""
}

func countEvents(store *model.EventStore, sessionName string, mtype model.MilestoneType) int {
	count := 0
	for _, e := range store.Events {
		if e.SessionName == sessionName && e.Type == mtype {
			count++
		}
	}
	return count
}
