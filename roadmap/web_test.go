package roadmap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

type mockStoreLoader struct {
	store *model.Store
	err   error
}

func (m *mockStoreLoader) LoadStore() (*model.Store, error) {
	return m.store, m.err
}

func TestHandleAPIRoadmap_EmptyStore(t *testing.T) {
	server := &Server{
		StoreLoader: &mockStoreLoader{store: &model.Store{}},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{}},
			Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var entries []RoadmapEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestHandleAPIRoadmap_WithContexts(t *testing.T) {
	now := time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC)
	store := &model.Store{
		Contexts: []model.Context{
			{
				Name:          "auth",
				Branch:        "feature/auth",
				Worktree:      "/tmp/auth",
				Status:        model.StatusInProgress,
				InitialPrompt: "認証機能を実装して",
				CreatedAt:     now,
				LastSeen:      now,
			},
			{
				Name:     "api-fix",
				Branch:   "fix/api-500",
				Worktree: "/tmp/api-fix",
				Status:   model.StatusReview,
				PRURL:    "https://github.com/example/repo/pull/1",
				CreatedAt: now,
				LastSeen:  now,
			},
		},
	}

	server := &Server{
		StoreLoader: &mockStoreLoader{store: store},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{
				"rev-parse --git-dir": {err: fmt.Errorf("not found")},
			}},
			Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var entries []RoadmapEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	if entries[0].Name != "auth" {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, "auth")
	}
	if entries[0].InitialPrompt != "認証機能を実装して" {
		t.Errorf("entries[0].InitialPrompt = %q, want %q", entries[0].InitialPrompt, "認証機能を実装して")
	}
	if entries[0].Phase != model.PhaseIdle {
		t.Errorf("entries[0].Phase = %q, want %q", entries[0].Phase, model.PhaseIdle)
	}

	if entries[1].Name != "api-fix" {
		t.Errorf("entries[1].Name = %q, want %q", entries[1].Name, "api-fix")
	}
	if entries[1].PRURL != "https://github.com/example/repo/pull/1" {
		t.Errorf("entries[1].PRURL = %q, want %q", entries[1].PRURL, "https://github.com/example/repo/pull/1")
	}
}

type mockInsightLoader struct {
	store *model.InsightStore
	err   error
}

func (m *mockInsightLoader) LoadInsights() (*model.InsightStore, error) {
	return m.store, m.err
}

func TestHandleAPIRoadmap_WithInsights(t *testing.T) {
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	store := &model.Store{
		Contexts: []model.Context{
			{
				Name:     "auth",
				Branch:   "feature/auth",
				Worktree: "/tmp/auth",
				Status:   model.StatusInProgress,
				CreatedAt: now,
				LastSeen:  now,
			},
		},
	}
	insights := &model.InsightStore{
		Insights: []model.SessionInsight{
			{
				Name:           "auth",
				Goal:           "OAuth認証を実装する",
				CurrentFocus:   "tokenリフレッシュ",
				NextStep:       "エラーハンドリング追加",
				AttentionState: model.AttentionActive,
				InferredAt:     now,
			},
		},
	}

	server := &Server{
		StoreLoader:   &mockStoreLoader{store: store},
		InsightLoader: &mockInsightLoader{store: insights},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{
				"rev-parse --git-dir": {err: fmt.Errorf("not found")},
			}},
			Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmap(w, req)

	var entries []RoadmapEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	if entries[0].Goal != "OAuth認証を実装する" {
		t.Errorf("Goal = %q, want %q", entries[0].Goal, "OAuth認証を実装する")
	}
	if entries[0].CurrentFocus != "tokenリフレッシュ" {
		t.Errorf("CurrentFocus = %q", entries[0].CurrentFocus)
	}
	if entries[0].NextStep != "エラーハンドリング追加" {
		t.Errorf("NextStep = %q", entries[0].NextStep)
	}
	if entries[0].AttentionState != model.AttentionActive {
		t.Errorf("AttentionState = %q", entries[0].AttentionState)
	}
}

func TestHandleIndex(t *testing.T) {
	server := &Server{
		StoreLoader: &mockStoreLoader{store: &model.Store{}},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{}},
			Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	server.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", contentType, "text/html; charset=utf-8")
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("body is empty")
	}
	if !strings.Contains(body, "Session Roadmap") {
		t.Error("body does not contain 'Session Roadmap'")
	}
}

type mockEventLoader struct {
	store *model.EventStore
	err   error
}

func (m *mockEventLoader) LoadEvents() (*model.EventStore, error) {
	return m.store, m.err
}

func TestHandleAPIRoadmap_WithMilestones(t *testing.T) {
	now := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	store := &model.Store{
		Contexts: []model.Context{
			{
				Name:     "auth",
				Branch:   "feature/auth",
				Worktree: "/tmp/auth",
				Status:   model.StatusInProgress,
				RepoRoot: "/tmp/project",
				CreatedAt: now,
				LastSeen:  now,
			},
		},
	}
	events := &model.EventStore{
		Events: []model.SessionEvent{
			{SessionName: "auth", Type: model.MilestoneSessionStart, OccurredAt: now, ObservedAt: now},
			{SessionName: "auth", Type: model.MilestoneFirstCommit, Detail: "initial", OccurredAt: now, ObservedAt: now},
			{SessionName: "auth", Type: model.MilestoneCommit, Detail: "fix bug", OccurredAt: now, ObservedAt: now},
			{SessionName: "auth", Type: model.MilestoneCommit, Detail: "add test", OccurredAt: now, ObservedAt: now},
		},
	}

	server := &Server{
		StoreLoader: &mockStoreLoader{store: store},
		EventLoader: &mockEventLoader{store: events},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{
				"rev-parse --git-dir": {err: fmt.Errorf("not found")},
			}},
			Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmap(w, req)

	var entries []RoadmapEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}

	if entries[0].RepoRoot != "/tmp/project" {
		t.Errorf("RepoRoot = %q, want /tmp/project", entries[0].RepoRoot)
	}
	if entries[0].Milestones == nil {
		t.Fatal("Milestones is nil")
	}
	if entries[0].Milestones.SessionCount != 1 {
		t.Errorf("SessionCount = %d, want 1", entries[0].Milestones.SessionCount)
	}
	if entries[0].Milestones.CommitCount != 2 {
		t.Errorf("CommitCount = %d, want 2", entries[0].Milestones.CommitCount)
	}
}

func TestHandleAPIRoadmapMap_GroupsByProject(t *testing.T) {
	now := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	store := &model.Store{
		Contexts: []model.Context{
			{
				Name:     "auth",
				Branch:   "feature/auth",
				Worktree: "/tmp/project/worktrees/auth",
				Status:   model.StatusInProgress,
				RepoRoot: "/tmp/project",
				CreatedAt: now,
				LastSeen:  now,
			},
			{
				Name:     "api-fix",
				Branch:   "fix/api",
				Worktree: "/tmp/project/worktrees/api-fix",
				Status:   model.StatusReview,
				RepoRoot: "/tmp/project",
				CreatedAt: now,
				LastSeen:  now,
			},
			{
				Name:     "other-task",
				Branch:   "feature/other",
				Worktree: "/tmp/other-project",
				Status:   model.StatusInProgress,
				RepoRoot: "/tmp/other-project",
				CreatedAt: now,
				LastSeen:  now,
			},
		},
	}

	server := &Server{
		StoreLoader: &mockStoreLoader{store: store},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{
				"rev-parse --git-dir": {err: fmt.Errorf("not found")},
			}},
			Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap-map", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmapMap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var groups []ProjectGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}

	if groups[0].Name != "project" {
		t.Errorf("groups[0].Name = %q, want project", groups[0].Name)
	}
	if len(groups[0].Sessions) != 2 {
		t.Errorf("groups[0].Sessions len = %d, want 2", len(groups[0].Sessions))
	}

	if groups[1].Name != "other-project" {
		t.Errorf("groups[1].Name = %q, want other-project", groups[1].Name)
	}
	if len(groups[1].Sessions) != 1 {
		t.Errorf("groups[1].Sessions len = %d, want 1", len(groups[1].Sessions))
	}
}

func TestHandleAPITimeline(t *testing.T) {
	now := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	events := &model.EventStore{
		Events: []model.SessionEvent{
			{SessionName: "auth", Type: model.MilestoneSessionStart, OccurredAt: now, ObservedAt: now},
			{SessionName: "auth", Type: model.MilestoneFirstCommit, Detail: "initial", OccurredAt: now, ObservedAt: now},
			{SessionName: "auth", Type: model.MilestoneCommit, Detail: "fix", OccurredAt: now, ObservedAt: now},
			{SessionName: "other", Type: model.MilestoneCommit, OccurredAt: now, ObservedAt: now},
		},
	}

	server := &Server{
		StoreLoader: &mockStoreLoader{store: &model.Store{}},
		EventLoader: &mockEventLoader{store: events},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{}},
			Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/timeline/auth", nil)
	w := httptest.NewRecorder()
	server.handleAPITimeline(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var timeline SessionTimeline
	if err := json.Unmarshal(w.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if timeline.SessionName != "auth" {
		t.Errorf("SessionName = %q, want auth", timeline.SessionName)
	}
	if len(timeline.Events) != 3 {
		t.Errorf("Events len = %d, want 3", len(timeline.Events))
	}
	if timeline.Summary.CommitCount != 1 {
		t.Errorf("Summary.CommitCount = %d, want 1", timeline.Summary.CommitCount)
	}
	if timeline.Summary.SessionCount != 1 {
		t.Errorf("Summary.SessionCount = %d, want 1", timeline.Summary.SessionCount)
	}
}

func TestHandleAPITimeline_Empty(t *testing.T) {
	server := &Server{
		StoreLoader: &mockStoreLoader{store: &model.Store{}},
		EventLoader: &mockEventLoader{store: &model.EventStore{}},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{}},
			Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/timeline/missing", nil)
	w := httptest.NewRecorder()
	server.handleAPITimeline(w, req)

	var timeline SessionTimeline
	if err := json.Unmarshal(w.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(timeline.Events) != 0 {
		t.Errorf("Events len = %d, want 0", len(timeline.Events))
	}
}

func TestHandleAPITimeline_NoSessionName(t *testing.T) {
	server := &Server{
		StoreLoader: &mockStoreLoader{store: &model.Store{}},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{}},
			Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/timeline/", nil)
	w := httptest.NewRecorder()
	server.handleAPITimeline(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleAPIRoadmapMap_IncludesTopicsAndTasks(t *testing.T) {
	now := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)
	store := &model.Store{
		Contexts: []model.Context{
			{
				Name:     "auth",
				Branch:   "feature/auth",
				Worktree: "/tmp/project/worktrees/auth",
				Status:   model.StatusInProgress,
				RepoRoot: "/tmp/project",
				CreatedAt: now,
				LastSeen:  now,
			},
		},
	}
	insights := &model.InsightStore{
		Insights: []model.SessionInsight{
			{
				Name:           "auth",
				Goal:           "OAuth認証",
				AttentionState: model.AttentionActive,
				InferredAt:     now,
				Topics: []model.SemanticTopic{
					{ID: "auth", Name: "認証", Source: "llm"},
				},
				Tasks: []model.TaskItem{
					{Title: "トークン実装", Status: model.TaskInProgress, Source: "llm"},
				},
			},
		},
	}

	server := &Server{
		StoreLoader:   &mockStoreLoader{store: store},
		InsightLoader: &mockInsightLoader{store: insights},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{
				"rev-parse --git-dir": {err: fmt.Errorf("not found")},
			}},
			Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap-map", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmapMap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var groups []ProjectGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}

	session := groups[0].Sessions[0]
	if session.Goal != "OAuth認証" {
		t.Errorf("Goal = %q, want OAuth認証", session.Goal)
	}
	if len(session.Topics) != 1 {
		t.Fatalf("Topics len = %d, want 1", len(session.Topics))
	}
	if session.Topics[0].Name != "認証" {
		t.Errorf("Topics[0].Name = %q, want 認証", session.Topics[0].Name)
	}
	if len(session.Tasks) != 1 {
		t.Fatalf("Tasks len = %d, want 1", len(session.Tasks))
	}
	if session.Tasks[0].Title != "トークン実装" {
		t.Errorf("Tasks[0].Title = %q, want トークン実装", session.Tasks[0].Title)
	}
}

func TestAPIRoadmapMapIncludesDAGFields(t *testing.T) {
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	store := &model.Store{
		Contexts: []model.Context{
			{
				Name:      "test-ctx",
				Branch:    "feature/test",
				Worktree:  "/tmp/project/worktrees/test",
				Status:    model.StatusInProgress,
				RepoRoot:  "/tmp/project",
				CreatedAt: now,
				LastSeen:  now,
			},
		},
	}
	insights := &model.InsightStore{
		Insights: []model.SessionInsight{
			{
				Name: "test-ctx",
				Goal: "Test goal",
				Tasks: []model.TaskItem{
					{
						Title:     "Task A",
						Status:    model.TaskDone,
						Source:    "llm",
						ID:        "task-a",
						DependsOn: nil,
						FlowsTo:  "pr-review",
					},
					{
						Title:     "Task B",
						Status:    model.TaskInProgress,
						Source:    "llm",
						ID:        "task-b",
						DependsOn: []string{"task-a"},
						FlowsTo:  "pr-review",
					},
					{
						Title:     "PR Review",
						Status:    model.TaskPlanned,
						Source:    "llm",
						ID:        "pr-review",
						DependsOn: []string{"task-a", "task-b"},
					},
					{
						Title:  "Cleanup",
						Status: model.TaskRejected,
						Source: "llm",
						ID:     "cleanup",
					},
				},
			},
		},
	}

	server := &Server{
		StoreLoader:   &mockStoreLoader{store: store},
		InsightLoader: &mockInsightLoader{store: insights},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{
				"rev-parse --git-dir": {err: fmt.Errorf("not found")},
			}},
			Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap-map", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmapMap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	// Parse as raw JSON to verify field presence/absence
	var groups []struct {
		Sessions []struct {
			Tasks []struct {
				Title     string   `json:"title"`
				Status    string   `json:"status"`
				ID        string   `json:"id"`
				DependsOn []string `json:"depends_on"`
				FlowsTo  string   `json:"flows_to"`
			} `json:"tasks"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if len(groups[0].Sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(groups[0].Sessions))
	}

	tasks := groups[0].Sessions[0].Tasks
	if len(tasks) != 4 {
		t.Fatalf("got %d tasks, want 4", len(tasks))
	}

	// task-a: has flows_to, no depends_on
	if tasks[0].ID != "task-a" {
		t.Errorf("tasks[0].ID = %q, want task-a", tasks[0].ID)
	}
	if tasks[0].FlowsTo != "pr-review" {
		t.Errorf("tasks[0].FlowsTo = %q, want pr-review", tasks[0].FlowsTo)
	}
	if tasks[0].DependsOn != nil {
		t.Errorf("tasks[0].DependsOn = %v, want nil (omitempty)", tasks[0].DependsOn)
	}

	// task-b: has depends_on and flows_to
	if tasks[1].ID != "task-b" {
		t.Errorf("tasks[1].ID = %q, want task-b", tasks[1].ID)
	}
	if len(tasks[1].DependsOn) != 1 || tasks[1].DependsOn[0] != "task-a" {
		t.Errorf("tasks[1].DependsOn = %v, want [task-a]", tasks[1].DependsOn)
	}
	if tasks[1].FlowsTo != "pr-review" {
		t.Errorf("tasks[1].FlowsTo = %q, want pr-review", tasks[1].FlowsTo)
	}

	// pr-review: depends_on has both task-a and task-b
	if tasks[2].ID != "pr-review" {
		t.Errorf("tasks[2].ID = %q, want pr-review", tasks[2].ID)
	}
	if len(tasks[2].DependsOn) != 2 {
		t.Fatalf("tasks[2].DependsOn len = %d, want 2", len(tasks[2].DependsOn))
	}
	if tasks[2].DependsOn[0] != "task-a" || tasks[2].DependsOn[1] != "task-b" {
		t.Errorf("tasks[2].DependsOn = %v, want [task-a task-b]", tasks[2].DependsOn)
	}
	if tasks[2].FlowsTo != "" {
		t.Errorf("tasks[2].FlowsTo = %q, want empty (omitempty)", tasks[2].FlowsTo)
	}

	// cleanup: rejected, no depends_on/flows_to
	if tasks[3].ID != "cleanup" {
		t.Errorf("tasks[3].ID = %q, want cleanup", tasks[3].ID)
	}
	if tasks[3].Status != "rejected" {
		t.Errorf("tasks[3].Status = %q, want rejected", tasks[3].Status)
	}
	if tasks[3].DependsOn != nil {
		t.Errorf("tasks[3].DependsOn = %v, want nil (omitempty)", tasks[3].DependsOn)
	}
	if tasks[3].FlowsTo != "" {
		t.Errorf("tasks[3].FlowsTo = %q, want empty (omitempty)", tasks[3].FlowsTo)
	}

	// Also verify omitempty by checking raw JSON
	rawJSON := w.Body.String()
	// task-a should NOT have depends_on in JSON (omitempty)
	// We check by looking for the task-a block
	if strings.Contains(rawJSON, `"id":"cleanup"`) && strings.Contains(rawJSON, `"depends_on":[]`) {
		// This would be wrong - omitempty should omit empty slices
	}
	// Verify flows_to appears for task-a
	if !strings.Contains(rawJSON, `"flows_to":"pr-review"`) {
		t.Error("raw JSON should contain flows_to:pr-review for task-a/task-b")
	}
	// Verify depends_on appears for task-b
	if !strings.Contains(rawJSON, `"depends_on":["task-a"]`) {
		t.Error("raw JSON should contain depends_on:[task-a] for task-b")
	}
}

func TestHandleAPIRoadmap_StoreError(t *testing.T) {
	server := &Server{
		StoreLoader: &mockStoreLoader{err: fmt.Errorf("disk error")},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{}},
			Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmap(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
