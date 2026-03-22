package model

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTaskItemNewFields(t *testing.T) {
	item := TaskItem{
		Title:     "依存関係の解決",
		Status:    TaskInProgress,
		Source:    "llm",
		ID:        "task-1",
		DependsOn: []string{"task-0a", "task-0b"},
		FlowsTo:  "task-2",
	}

	if item.ID != "task-1" {
		t.Errorf("ID = %q, want %q", item.ID, "task-1")
	}
	if len(item.DependsOn) != 2 || item.DependsOn[0] != "task-0a" || item.DependsOn[1] != "task-0b" {
		t.Errorf("DependsOn = %v, want [task-0a task-0b]", item.DependsOn)
	}
	if item.FlowsTo != "task-2" {
		t.Errorf("FlowsTo = %q, want %q", item.FlowsTo, "task-2")
	}
}

func TestTaskRejectedStatus(t *testing.T) {
	if string(TaskRejected) != "rejected" {
		t.Errorf("TaskRejected = %q, want %q", TaskRejected, "rejected")
	}
}

func TestTaskItemJSONRoundTrip(t *testing.T) {
	original := TaskItem{
		Title:     "API設計",
		Status:    TaskPlanned,
		TopicID:   "t1",
		Evidence:  []string{"commit abc123"},
		Source:    "git",
		ID:        "task-42",
		DependsOn: []string{"task-41"},
		FlowsTo:  "task-43",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var restored TaskItem
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if restored.ID != original.ID {
		t.Errorf("ID = %q, want %q", restored.ID, original.ID)
	}
	if len(restored.DependsOn) != 1 || restored.DependsOn[0] != "task-41" {
		t.Errorf("DependsOn = %v, want [task-41]", restored.DependsOn)
	}
	if restored.FlowsTo != "task-43" {
		t.Errorf("FlowsTo = %q, want %q", restored.FlowsTo, "task-43")
	}

	// omitempty: フィールドが空の場合は JSON に含まれないことを確認
	empty := TaskItem{Title: "minimal", Status: TaskPlanned, Source: "manual"}
	emptyData, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("json.Marshal(empty) failed: %v", err)
	}
	emptyJSON := string(emptyData)
	for _, field := range []string{"id", "depends_on", "flows_to"} {
		if contains(emptyJSON, field) {
			t.Errorf("empty JSON should not contain %q, got: %s", field, emptyJSON)
		}
	}
}

func TestTaskItemYAMLRoundTrip(t *testing.T) {
	original := TaskItem{
		Title:     "テスト追加",
		Status:    TaskDone,
		TopicID:   "t2",
		Evidence:  []string{"PR #10"},
		Source:    "git",
		ID:        "task-99",
		DependsOn: []string{"task-98", "task-97"},
		FlowsTo:  "task-100",
	}

	data, err := yaml.Marshal(original)
	if err != nil {
		t.Fatalf("yaml.Marshal failed: %v", err)
	}

	var restored TaskItem
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	if restored.ID != original.ID {
		t.Errorf("ID = %q, want %q", restored.ID, original.ID)
	}
	if len(restored.DependsOn) != 2 || restored.DependsOn[0] != "task-98" {
		t.Errorf("DependsOn = %v, want [task-98 task-97]", restored.DependsOn)
	}
	if restored.FlowsTo != "task-100" {
		t.Errorf("FlowsTo = %q, want %q", restored.FlowsTo, "task-100")
	}

	// omitempty: フィールドが空の場合は YAML に含まれないことを確認
	empty := TaskItem{Title: "minimal", Status: TaskPlanned, Source: "manual"}
	emptyData, err := yaml.Marshal(empty)
	if err != nil {
		t.Fatalf("yaml.Marshal(empty) failed: %v", err)
	}
	emptyYAML := string(emptyData)
	for _, field := range []string{"id:", "depends_on:", "flows_to:"} {
		if contains(emptyYAML, field) {
			t.Errorf("empty YAML should not contain %q, got: %s", field, emptyYAML)
		}
	}
}

func TestTaskItemBackwardCompatibility(t *testing.T) {
	// 新フィールドがない既存の JSON をデシリアライズしても問題ないこと
	legacyJSON := `{"title":"レガシータスク","status":"in_progress","source":"manual"}`
	var fromJSON TaskItem
	if err := json.Unmarshal([]byte(legacyJSON), &fromJSON); err != nil {
		t.Fatalf("json.Unmarshal(legacy) failed: %v", err)
	}
	if fromJSON.Title != "レガシータスク" {
		t.Errorf("Title = %q, want %q", fromJSON.Title, "レガシータスク")
	}
	if fromJSON.ID != "" {
		t.Errorf("ID = %q, want empty", fromJSON.ID)
	}
	if fromJSON.DependsOn != nil {
		t.Errorf("DependsOn = %v, want nil", fromJSON.DependsOn)
	}
	if fromJSON.FlowsTo != "" {
		t.Errorf("FlowsTo = %q, want empty", fromJSON.FlowsTo)
	}

	// 新フィールドがない既存の YAML をデシリアライズしても問題ないこと
	legacyYAML := "title: レガシータスク\nstatus: blocked\nsource: git\n"
	var fromYAML TaskItem
	if err := yaml.Unmarshal([]byte(legacyYAML), &fromYAML); err != nil {
		t.Fatalf("yaml.Unmarshal(legacy) failed: %v", err)
	}
	if fromYAML.Title != "レガシータスク" {
		t.Errorf("Title = %q, want %q", fromYAML.Title, "レガシータスク")
	}
	if fromYAML.Status != TaskBlocked {
		t.Errorf("Status = %q, want %q", fromYAML.Status, TaskBlocked)
	}
	if fromYAML.ID != "" {
		t.Errorf("ID = %q, want empty", fromYAML.ID)
	}
	if fromYAML.DependsOn != nil {
		t.Errorf("DependsOn = %v, want nil", fromYAML.DependsOn)
	}
	if fromYAML.FlowsTo != "" {
		t.Errorf("FlowsTo = %q, want empty", fromYAML.FlowsTo)
	}

	// 新フィールド付きを再シリアライズ→既存フィールドが壊れないこと
	reData, err := json.Marshal(fromJSON)
	if err != nil {
		t.Fatalf("json.Marshal(fromJSON) failed: %v", err)
	}
	var reRestored TaskItem
	if err := json.Unmarshal(reData, &reRestored); err != nil {
		t.Fatalf("json.Unmarshal(re-serialized) failed: %v", err)
	}
	if reRestored.Title != "レガシータスク" {
		t.Errorf("re-restored Title = %q, want %q", reRestored.Title, "レガシータスク")
	}
}

// contains はシンプルな文字列包含チェック
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
