package cmd

import (
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func TestMergeTasks_AIFieldsPreserved(t *testing.T) {
	llmTasks := []model.TaskItem{
		{
			Title:     "implement auth",
			Status:    model.TaskPlanned,
			Source:    "llm",
			ID:        "task-001",
			DependsOn: []string{"task-000"},
			FlowsTo:  "task-002",
		},
		{
			Title:     "add tests",
			Status:    model.TaskPlanned,
			Source:    "llm",
			ID:        "task-002",
			DependsOn: []string{"task-001"},
			FlowsTo:  "",
		},
	}

	gitTasks := []model.TaskItem{
		{
			Title:    "implement auth", // duplicate with LLM
			Status:   model.TaskDone,
			Source:   "git",
			Evidence: []string{"commit: feat: implement auth"},
			// ID, DependsOn, FlowsTo are empty (git source)
		},
		{
			Title:    "fix typo",
			Status:   model.TaskDone,
			Source:   "git",
			Evidence: []string{"commit: fix: fix typo"},
		},
	}

	merged := mergeTasks(llmTasks, gitTasks)

	// Expect 3 tasks: 2 from LLM + 1 unique from git
	if len(merged) != 3 {
		t.Fatalf("merged len = %d, want 3", len(merged))
	}

	// LLM "implement auth" should win over git duplicate, preserving AI fields
	authTask := merged[0]
	if authTask.Title != "implement auth" {
		t.Errorf("merged[0].Title = %q, want \"implement auth\"", authTask.Title)
	}
	if authTask.ID != "task-001" {
		t.Errorf("merged[0].ID = %q, want \"task-001\" (AI field must be preserved)", authTask.ID)
	}
	if len(authTask.DependsOn) != 1 || authTask.DependsOn[0] != "task-000" {
		t.Errorf("merged[0].DependsOn = %v, want [\"task-000\"] (AI field must be preserved)", authTask.DependsOn)
	}
	if authTask.FlowsTo != "task-002" {
		t.Errorf("merged[0].FlowsTo = %q, want \"task-002\" (AI field must be preserved)", authTask.FlowsTo)
	}
	// LLM version is kept, so Source should remain "llm"
	if authTask.Source != "llm" {
		t.Errorf("merged[0].Source = %q, want \"llm\"", authTask.Source)
	}

	// Second LLM task preserved
	addTestsTask := merged[1]
	if addTestsTask.ID != "task-002" {
		t.Errorf("merged[1].ID = %q, want \"task-002\"", addTestsTask.ID)
	}

	// Git-only task added
	fixTypoTask := merged[2]
	if fixTypoTask.Title != "fix typo" {
		t.Errorf("merged[2].Title = %q, want \"fix typo\"", fixTypoTask.Title)
	}
	if fixTypoTask.Source != "git" {
		t.Errorf("merged[2].Source = %q, want \"git\"", fixTypoTask.Source)
	}
	if fixTypoTask.ID != "" {
		t.Errorf("merged[2].ID = %q, want empty (git source)", fixTypoTask.ID)
	}
}

func TestMergeTasks_EmptyInputs(t *testing.T) {
	// LLM only
	llmOnly := mergeTasks([]model.TaskItem{
		{Title: "task1", Source: "llm", ID: "t-1"},
	}, nil)
	if len(llmOnly) != 1 || llmOnly[0].ID != "t-1" {
		t.Errorf("LLM-only merge failed: %v", llmOnly)
	}

	// Git only
	gitOnly := mergeTasks(nil, []model.TaskItem{
		{Title: "task2", Source: "git"},
	})
	if len(gitOnly) != 1 || gitOnly[0].Source != "git" {
		t.Errorf("Git-only merge failed: %v", gitOnly)
	}

	// Both empty
	empty := mergeTasks(nil, nil)
	if len(empty) != 0 {
		t.Errorf("Empty merge should return empty, got %v", empty)
	}
}
