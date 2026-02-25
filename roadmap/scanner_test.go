package roadmap

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

type mockResult struct {
	output string
	err    error
}

type mockGitRunner struct {
	results map[string]mockResult
}

func (m *mockGitRunner) Run(dir string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	if r, ok := m.results[key]; ok {
		return r.output, r.err
	}
	return "", fmt.Errorf("command not mocked: git %s", key)
}

type mockGhRunner struct {
	available bool
	results   map[string]mockResult
}

func (m *mockGhRunner) Run(dir string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	if r, ok := m.results[key]; ok {
		return r.output, r.err
	}
	return "", fmt.Errorf("command not mocked: gh %s", key)
}

func (m *mockGhRunner) Available() bool {
	return m.available
}

func TestScanContext_EmptyWorktree(t *testing.T) {
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{}},
		Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
	}

	ctx := &model.Context{Name: "test"}
	got := scanner.ScanContext(ctx)
	if got != model.PhaseIdle {
		t.Errorf("ScanContext(empty worktree) = %q, want %q", got, model.PhaseIdle)
	}
}

func TestScanContext_NotGitDir(t *testing.T) {
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --git-dir": {err: fmt.Errorf("not a git repo")},
		}},
		Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
	}

	ctx := &model.Context{Name: "test", Worktree: "/tmp/notgit", Branch: "main"}
	got := scanner.ScanContext(ctx)
	if got != model.PhaseIdle {
		t.Errorf("ScanContext(not git dir) = %q, want %q", got, model.PhaseIdle)
	}
}

func TestScanContext_UncommittedChanges(t *testing.T) {
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --git-dir":                      {output: ".git"},
			"rev-parse --verify origin/main":           {output: "abc123"},
			"rev-parse --verify origin/feature":        {err: fmt.Errorf("not found")},
			"log origin/main..HEAD --oneline":          {output: ""},
			"status --porcelain":                       {output: " M file.go"},
		}},
		Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
	}

	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	got := scanner.ScanContext(ctx)
	if got != model.PhaseImplementation {
		t.Errorf("ScanContext(uncommitted changes) = %q, want %q", got, model.PhaseImplementation)
	}
}

func TestScanContext_CommittedNotPushed(t *testing.T) {
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --git-dir":                      {output: ".git"},
			"rev-parse --verify origin/main":           {output: "abc123"},
			"rev-parse --verify origin/feature":        {err: fmt.Errorf("not found")},
			"log origin/main..HEAD --oneline":          {output: "abc123 some commit"},
			"status --porcelain":                       {output: ""},
		}},
		Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
	}

	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	got := scanner.ScanContext(ctx)
	if got != model.PhaseCommitted {
		t.Errorf("ScanContext(committed) = %q, want %q", got, model.PhaseCommitted)
	}
}

func TestScanContext_Pushed(t *testing.T) {
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --git-dir":                      {output: ".git"},
			"rev-parse --verify origin/main":           {output: "abc123"},
			"rev-parse --verify origin/feature":        {output: "def456"},
			"log origin/feature..HEAD --oneline":       {output: ""},
		}},
		Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
	}

	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	got := scanner.ScanContext(ctx)
	if got != model.PhasePushed {
		t.Errorf("ScanContext(pushed) = %q, want %q", got, model.PhasePushed)
	}
}

func TestScanContext_PROpen(t *testing.T) {
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --git-dir": {output: ".git"},
		}},
		Gh: &mockGhRunner{
			available: true,
			results: map[string]mockResult{
				"pr list --head feature --state merged --json state --limit 1": {output: "[]"},
				"pr list --head feature --state open --json state --limit 1":   {output: `[{"state":"OPEN"}]`},
			},
		},
	}

	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	got := scanner.ScanContext(ctx)
	if got != model.PhasePROpen {
		t.Errorf("ScanContext(pr_open) = %q, want %q", got, model.PhasePROpen)
	}
}

func TestScanContext_PRMerged(t *testing.T) {
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --git-dir": {output: ".git"},
		}},
		Gh: &mockGhRunner{
			available: true,
			results: map[string]mockResult{
				"pr list --head feature --state merged --json state --limit 1": {output: `[{"state":"MERGED"}]`},
			},
		},
	}

	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	got := scanner.ScanContext(ctx)
	if got != model.PhaseDone {
		t.Errorf("ScanContext(done) = %q, want %q", got, model.PhaseDone)
	}
}

func TestScanContext_NoGhAvailable(t *testing.T) {
	// When gh is not available, PR-based phases should be skipped
	// Even if the branch is pushed, max phase should be "pushed"
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --git-dir":                      {output: ".git"},
			"rev-parse --verify origin/main":           {output: "abc123"},
			"rev-parse --verify origin/feature":        {output: "def456"},
			"log origin/feature..HEAD --oneline":       {output: ""},
		}},
		Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
	}

	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	got := scanner.ScanContext(ctx)
	if got != model.PhasePushed {
		t.Errorf("ScanContext(no gh, pushed) = %q, want %q", got, model.PhasePushed)
	}
}

func TestScanContext_NoChanges(t *testing.T) {
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --git-dir":                      {output: ".git"},
			"rev-parse --verify origin/main":           {output: "abc123"},
			"rev-parse --verify origin/feature":        {err: fmt.Errorf("not found")},
			"log origin/main..HEAD --oneline":          {output: ""},
			"status --porcelain":                       {output: ""},
		}},
		Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
	}

	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	got := scanner.ScanContext(ctx)
	if got != model.PhaseIdle {
		t.Errorf("ScanContext(no changes) = %q, want %q", got, model.PhaseIdle)
	}
}

func TestScanContext_DetectBaseBranchMaster(t *testing.T) {
	// When origin/main doesn't exist but origin/master does
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --git-dir":                      {output: ".git"},
			"rev-parse --verify origin/main":           {err: fmt.Errorf("not found")},
			"rev-parse --verify origin/master":         {output: "abc123"},
			"rev-parse --verify origin/feature":        {err: fmt.Errorf("not found")},
			"log origin/master..HEAD --oneline":        {output: "abc123 commit"},
			"status --porcelain":                       {output: ""},
		}},
		Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
	}

	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature"}
	got := scanner.ScanContext(ctx)
	if got != model.PhaseCommitted {
		t.Errorf("ScanContext(master base) = %q, want %q", got, model.PhaseCommitted)
	}
}

func TestScanAll(t *testing.T) {
	scanner := &Scanner{
		Git: &mockGitRunner{results: map[string]mockResult{
			"rev-parse --git-dir": {err: fmt.Errorf("not a git repo")},
		}},
		Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
	}

	contexts := []model.Context{
		{Name: "a", Worktree: "/tmp/a", Branch: "feature-a"},
		{Name: "b"},
	}

	phases := scanner.ScanAll(contexts)
	if len(phases) != 2 {
		t.Fatalf("ScanAll returned %d entries, want 2", len(phases))
	}
	if phases["a"] != model.PhaseIdle {
		t.Errorf("phases[a] = %q, want %q", phases["a"], model.PhaseIdle)
	}
	if phases["b"] != model.PhaseIdle {
		t.Errorf("phases[b] = %q, want %q", phases["b"], model.PhaseIdle)
	}
}
