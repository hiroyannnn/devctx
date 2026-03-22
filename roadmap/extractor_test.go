package roadmap

import (
	"fmt"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func TestCollectEvidence(t *testing.T) {
	git := &mockGitRunner{results: map[string]mockResult{
		"rev-parse --verify origin/main":     {output: "abc123"},
		"log origin/main..HEAD --format=%s":  {output: "feat: add auth\nfix: token refresh\nchore: cleanup"},
		"diff --name-only origin/main..HEAD": {output: "cmd/auth.go\ncmd/auth_test.go\nmodel/user.go\nREADME.md"},
	}}

	extractor := &Extractor{Git: git}
	ctx := &model.Context{
		Name:          "auth",
		Branch:        "feature/auth",
		Worktree:      "/tmp/repo",
		InitialPrompt: "認証機能を実装して",
		Note:          "OAuth2フロー",
	}

	bundle := extractor.CollectEvidence(ctx)

	if bundle.Branch != "feature/auth" {
		t.Errorf("Branch = %q, want feature/auth", bundle.Branch)
	}
	if len(bundle.CommitSubjects) != 3 {
		t.Fatalf("CommitSubjects len = %d, want 3", len(bundle.CommitSubjects))
	}
	if bundle.CommitSubjects[0] != "feat: add auth" {
		t.Errorf("CommitSubjects[0] = %q, want 'feat: add auth'", bundle.CommitSubjects[0])
	}
	if len(bundle.ChangedDirs) == 0 {
		t.Fatal("ChangedDirs is empty")
	}
	if bundle.InitialPrompt != "認証機能を実装して" {
		t.Errorf("InitialPrompt = %q", bundle.InitialPrompt)
	}
}

func TestCollectEvidence_EmptyWorktree(t *testing.T) {
	extractor := &Extractor{Git: &mockGitRunner{results: map[string]mockResult{}}}
	ctx := &model.Context{Name: "test", Branch: "feature/x"}
	bundle := extractor.CollectEvidence(ctx)

	if bundle.Branch != "feature/x" {
		t.Errorf("Branch = %q, want feature/x", bundle.Branch)
	}
	if len(bundle.CommitSubjects) != 0 {
		t.Errorf("CommitSubjects len = %d, want 0", len(bundle.CommitSubjects))
	}
}

func TestCollectEvidence_NoBaseBranch(t *testing.T) {
	git := &mockGitRunner{results: map[string]mockResult{
		"rev-parse --verify origin/main":   {err: fmt.Errorf("not found")},
		"rev-parse --verify origin/master": {err: fmt.Errorf("not found")},
	}}
	extractor := &Extractor{Git: git}
	ctx := &model.Context{Name: "test", Worktree: "/tmp/repo", Branch: "feature/x"}
	bundle := extractor.CollectEvidence(ctx)

	if len(bundle.CommitSubjects) != 0 {
		t.Errorf("CommitSubjects len = %d, want 0", len(bundle.CommitSubjects))
	}
}

func TestExtractTopics(t *testing.T) {
	bundle := EvidenceBundle{
		Branch:        "feature/session-roadmap",
		ChangedDirs:   []string{"cmd", "model", "roadmap"},
		InitialPrompt: "セッション管理",
	}

	topics := ExtractTopics(bundle)

	if len(topics) < 3 {
		t.Fatalf("topics len = %d, want >= 3", len(topics))
	}

	// Branch topic
	hasBranch := false
	for _, topic := range topics {
		if topic.Name == "session-roadmap" {
			hasBranch = true
			if topic.Source != "git" {
				t.Errorf("branch topic Source = %q, want git", topic.Source)
			}
		}
	}
	if !hasBranch {
		t.Error("missing branch topic 'session-roadmap'")
	}

	// Prompt topic
	hasPrompt := false
	for _, topic := range topics {
		if topic.Name == "セッション管理" {
			hasPrompt = true
			if topic.Source != "manual" {
				t.Errorf("prompt topic Source = %q, want manual", topic.Source)
			}
		}
	}
	if !hasPrompt {
		t.Error("missing prompt topic")
	}
}

func TestExtractTopics_MainBranch(t *testing.T) {
	bundle := EvidenceBundle{Branch: "main"}
	topics := ExtractTopics(bundle)

	for _, topic := range topics {
		if topic.Name == "main" {
			t.Error("should not create topic for main branch")
		}
	}
}

func TestExtractTopics_LongPrompt(t *testing.T) {
	bundle := EvidenceBundle{
		InitialPrompt: "これは非常に長いプロンプトでトピック名としては不適切なので省略されるべきです。これは非常に長いプロンプトです。",
	}
	topics := ExtractTopics(bundle)

	for _, topic := range topics {
		if topic.Source == "manual" {
			t.Error("long prompt should not become a topic")
		}
	}
}

func TestExtractTasks(t *testing.T) {
	bundle := EvidenceBundle{
		CommitSubjects: []string{
			"feat: add auth handler",
			"fix: token refresh bug",
			"chore: cleanup imports",
			"feat: add auth handler", // duplicate
		},
	}

	tasks := ExtractTasks(bundle)

	if len(tasks) != 3 {
		t.Fatalf("tasks len = %d, want 3 (dedup)", len(tasks))
	}

	if tasks[0].Title != "add auth handler" {
		t.Errorf("tasks[0].Title = %q, want 'add auth handler'", tasks[0].Title)
	}
	if tasks[0].Status != model.TaskDone {
		t.Errorf("tasks[0].Status = %q, want done", tasks[0].Status)
	}
	if tasks[0].Source != "git" {
		t.Errorf("tasks[0].Source = %q, want git", tasks[0].Source)
	}
	if len(tasks[0].Evidence) != 1 {
		t.Fatalf("tasks[0].Evidence len = %d, want 1", len(tasks[0].Evidence))
	}
}

func TestExtractTasksNewFieldsEmpty(t *testing.T) {
	bundle := EvidenceBundle{
		CommitSubjects: []string{
			"feat: implement login",
			"fix: session timeout",
		},
	}

	tasks := ExtractTasks(bundle)

	if len(tasks) != 2 {
		t.Fatalf("tasks len = %d, want 2", len(tasks))
	}

	for i, task := range tasks {
		// Source must be "git"
		if task.Source != "git" {
			t.Errorf("tasks[%d].Source = %q, want \"git\"", i, task.Source)
		}
		// New fields must be empty (zero values)
		if task.ID != "" {
			t.Errorf("tasks[%d].ID = %q, want empty", i, task.ID)
		}
		if len(task.DependsOn) != 0 {
			t.Errorf("tasks[%d].DependsOn = %v, want empty", i, task.DependsOn)
		}
		if task.FlowsTo != "" {
			t.Errorf("tasks[%d].FlowsTo = %q, want empty", i, task.FlowsTo)
		}
	}
}

func TestExtractBranchTopic(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"feature/auth", "auth"},
		{"fix/api-error", "api-error"},
		{"hotfix/urgent", "urgent"},
		{"main", ""},
		{"master", ""},
		{"develop", ""},
		{"my-feature", "my-feature"},
	}

	for _, tt := range tests {
		got := extractBranchTopic(tt.branch)
		if got != tt.want {
			t.Errorf("extractBranchTopic(%q) = %q, want %q", tt.branch, got, tt.want)
		}
	}
}

func TestNormalizeCommitSubject(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"feat: add auth", "add auth"},
		{"fix: token bug", "token bug"},
		{"chore: cleanup", "cleanup"},
		{"plain message", "plain message"},
	}

	for _, tt := range tests {
		got := normalizeCommitSubject(tt.subject)
		if got != tt.want {
			t.Errorf("normalizeCommitSubject(%q) = %q, want %q", tt.subject, got, tt.want)
		}
	}
}

func TestTopicID(t *testing.T) {
	id1 := topicID("auth")
	id2 := topicID("auth")
	id3 := topicID("api-fix")

	if id1 != id2 {
		t.Error("same name should produce same ID")
	}
	if id1 == id3 {
		t.Error("different names should produce different IDs")
	}
	if len(id1) == 0 {
		t.Error("ID should not be empty")
	}
}
