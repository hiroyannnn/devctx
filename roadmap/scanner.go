package roadmap

import (
	"os/exec"
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// GitRunner abstracts git command execution for testing.
type GitRunner interface {
	Run(dir string, args ...string) (string, error)
}

// GhRunner abstracts gh CLI command execution for testing.
type GhRunner interface {
	Run(dir string, args ...string) (string, error)
	Available() bool
}

// ExecGitRunner executes real git commands.
type ExecGitRunner struct{}

func (r *ExecGitRunner) Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// ExecGhRunner executes real gh CLI commands.
type ExecGhRunner struct{}

func (r *ExecGhRunner) Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func (r *ExecGhRunner) Available() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// ScanMode controls how thorough the scan should be.
type ScanMode int

const (
	// ScanModeFast uses only local git commands (no gh CLI).
	ScanModeFast ScanMode = iota
	// ScanModeFull uses both git and gh CLI for PR detection.
	ScanModeFull
)

// Scanner detects the Phase for contexts.
type Scanner struct {
	Git GitRunner
	Gh  GhRunner
}

// NewScanner creates a Scanner with real command runners.
func NewScanner() *Scanner {
	return &Scanner{
		Git: &ExecGitRunner{},
		Gh:  &ExecGhRunner{},
	}
}

// RefreshPhase scans and updates the Phase and PhaseCheckedAt fields on the context.
// If the scan returns PhaseIdle (e.g. worktree not found), the existing phase is preserved.
func (s *Scanner) RefreshPhase(ctx *model.Context, mode ScanMode) {
	phase := s.scanWithMode(ctx, mode)
	if phase != model.PhaseIdle || ctx.Phase == "" {
		ctx.Phase = phase
	}
	ctx.PhaseCheckedAt = time.Now()
}

// ScanContext determines the Phase for a single context (full mode).
func (s *Scanner) ScanContext(ctx *model.Context) model.Phase {
	return s.scanWithMode(ctx, ScanModeFull)
}

func (s *Scanner) scanWithMode(ctx *model.Context, mode ScanMode) model.Phase {
	if ctx.Worktree == "" || ctx.Branch == "" {
		return model.PhaseIdle
	}

	if _, err := s.Git.Run(ctx.Worktree, "rev-parse", "--git-dir"); err != nil {
		return model.PhaseIdle
	}

	if mode == ScanModeFull && s.Gh.Available() {
		if phase := s.checkPRPhase(ctx); phase != "" {
			return phase
		}
	}

	return s.checkGitPhase(ctx)
}

func (s *Scanner) checkPRPhase(ctx *model.Context) model.Phase {
	out, err := s.Gh.Run(ctx.Worktree, "pr", "list",
		"--head", ctx.Branch, "--state", "merged", "--json", "state", "--limit", "1")
	if err == nil && out != "" && out != "[]" {
		return model.PhaseDone
	}

	out, err = s.Gh.Run(ctx.Worktree, "pr", "list",
		"--head", ctx.Branch, "--state", "open", "--json", "state", "--limit", "1")
	if err == nil && out != "" && out != "[]" {
		return model.PhasePROpen
	}

	return ""
}

func (s *Scanner) checkGitPhase(ctx *model.Context) model.Phase {
	base := s.detectBaseBranch(ctx.Worktree)

	remoteRef := "origin/" + ctx.Branch
	_, remoteErr := s.Git.Run(ctx.Worktree, "rev-parse", "--verify", remoteRef)
	remoteExists := remoteErr == nil

	if remoteExists {
		localOnly, err := s.Git.Run(ctx.Worktree, "log", remoteRef+"..HEAD", "--oneline")
		if err == nil && localOnly == "" {
			return model.PhasePushed
		}
	}

	if base != "" {
		ahead, err := s.Git.Run(ctx.Worktree, "log", "origin/"+base+"..HEAD", "--oneline")
		if err == nil && ahead != "" {
			return model.PhaseCommitted
		}
	}

	status, err := s.Git.Run(ctx.Worktree, "status", "--porcelain")
	if err == nil && status != "" {
		return model.PhaseImplementation
	}

	return model.PhaseIdle
}

func (s *Scanner) detectBaseBranch(dir string) string {
	for _, branch := range []string{"main", "master"} {
		if _, err := s.Git.Run(dir, "rev-parse", "--verify", "origin/"+branch); err == nil {
			return branch
		}
	}
	return ""
}

// ScanAll scans all contexts and returns a map of context name to phase.
func (s *Scanner) ScanAll(contexts []model.Context) map[string]model.Phase {
	results := make(map[string]model.Phase, len(contexts))
	for i := range contexts {
		results[contexts[i].Name] = s.ScanContext(&contexts[i])
	}
	return results
}
