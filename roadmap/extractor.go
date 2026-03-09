package roadmap

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hiroyannnn/devctx/model"
)

// Extractor extracts topics and tasks from git state mechanically (no LLM).
type Extractor struct {
	Git GitRunner
}

// NewExtractor creates an Extractor with real git runner.
func NewExtractor() *Extractor {
	return &Extractor{Git: &ExecGitRunner{}}
}

// EvidenceBundle holds raw data collected from git for extraction.
type EvidenceBundle struct {
	Branch        string
	CommitSubjects []string
	ChangedDirs   []string
	InitialPrompt string
	Note          string
}

// CollectEvidence gathers raw evidence from git and context metadata.
func (e *Extractor) CollectEvidence(ctx *model.Context) EvidenceBundle {
	bundle := EvidenceBundle{
		Branch:        ctx.Branch,
		InitialPrompt: ctx.InitialPrompt,
		Note:          ctx.Note,
	}

	if ctx.Worktree == "" {
		return bundle
	}

	base := e.detectBaseBranch(ctx.Worktree)
	if base == "" {
		return bundle
	}

	// Collect commit subjects
	out, err := e.Git.Run(ctx.Worktree, "log", "origin/"+base+"..HEAD", "--format=%s")
	if err == nil && out != "" {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				bundle.CommitSubjects = append(bundle.CommitSubjects, line)
			}
		}
	}

	// Collect changed directories
	out, err = e.Git.Run(ctx.Worktree, "diff", "--name-only", "origin/"+base+"..HEAD")
	if err == nil && out != "" {
		dirSet := make(map[string]bool)
		for _, file := range strings.Split(out, "\n") {
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			dir := filepath.Dir(file)
			if dir == "." {
				dir = "root"
			}
			dirSet[dir] = true
		}
		for dir := range dirSet {
			bundle.ChangedDirs = append(bundle.ChangedDirs, dir)
		}
	}

	return bundle
}

// ExtractTopics derives SemanticTopics from an evidence bundle.
func ExtractTopics(bundle EvidenceBundle) []model.SemanticTopic {
	seen := make(map[string]bool)
	var topics []model.SemanticTopic

	addTopic := func(name, source string, keywords []string) {
		key := strings.ToLower(name)
		if seen[key] || name == "" {
			return
		}
		seen[key] = true
		topics = append(topics, model.SemanticTopic{
			ID:       topicID(name),
			Name:     name,
			Keywords: keywords,
			Source:   source,
		})
	}

	// Branch name → topic
	if bundle.Branch != "" {
		branchTopic := extractBranchTopic(bundle.Branch)
		if branchTopic != "" {
			addTopic(branchTopic, "git", nil)
		}
	}

	// Changed directories → topics
	for _, dir := range bundle.ChangedDirs {
		addTopic(dir, "git", nil)
	}

	// Initial prompt → topic (if short enough to be a topic name)
	if bundle.InitialPrompt != "" && len(bundle.InitialPrompt) <= 40 {
		addTopic(bundle.InitialPrompt, "manual", nil)
	}

	return topics
}

// ExtractTasks derives TaskItems from an evidence bundle.
func ExtractTasks(bundle EvidenceBundle) []model.TaskItem {
	var tasks []model.TaskItem

	// Commit subjects → done tasks
	seen := make(map[string]bool)
	for _, subject := range bundle.CommitSubjects {
		// Normalize: remove common prefixes
		normalized := normalizeCommitSubject(subject)
		key := strings.ToLower(normalized)
		if seen[key] || normalized == "" {
			continue
		}
		seen[key] = true
		tasks = append(tasks, model.TaskItem{
			Title:    normalized,
			Status:   model.TaskDone,
			Evidence: []string{"commit: " + subject},
			Source:   "git",
		})
	}

	return tasks
}

func extractBranchTopic(branch string) string {
	// Remove common prefixes: feature/, fix/, hotfix/, etc.
	prefixes := []string{"feature/", "fix/", "hotfix/", "bugfix/", "chore/", "refactor/", "docs/"}
	for _, p := range prefixes {
		if strings.HasPrefix(branch, p) {
			return strings.TrimPrefix(branch, p)
		}
	}
	if branch == "main" || branch == "master" || branch == "develop" {
		return ""
	}
	return branch
}

func normalizeCommitSubject(subject string) string {
	// Remove conventional commit prefixes
	prefixes := []string{"feat: ", "fix: ", "chore: ", "refactor: ", "docs: ", "test: ", "style: ", "perf: ", "ci: "}
	for _, p := range prefixes {
		if strings.HasPrefix(subject, p) {
			return strings.TrimPrefix(subject, p)
		}
	}
	return subject
}

func topicID(name string) string {
	h := sha256.Sum256([]byte(name))
	return fmt.Sprintf("t-%x", h[:4])
}

func (e *Extractor) detectBaseBranch(dir string) string {
	for _, branch := range []string{"main", "master"} {
		if _, err := e.Git.Run(dir, "rev-parse", "--verify", "origin/"+branch); err == nil {
			return branch
		}
	}
	return ""
}
