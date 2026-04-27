package roadmap

import (
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func TestBuildAnalyzePrompt(t *testing.T) {
	ctx := &model.Context{
		Name:          "auth",
		Branch:        "feature/auth",
		InitialPrompt: "OAuth認証を実装して",
		Note:          "tokenの期限切れ対応も必要",
	}
	transcript := `{"role":"user","content":"OAuth認証を実装して"}
{"role":"assistant","content":"まずtokenの取得から始めます"}`

	prompt := BuildAnalyzePrompt(ctx, transcript)

	if !strings.Contains(prompt, "auth") {
		t.Error("prompt should contain context name")
	}
	if !strings.Contains(prompt, "feature/auth") {
		t.Error("prompt should contain branch name")
	}
	if !strings.Contains(prompt, "OAuth認証を実装して") {
		t.Error("prompt should contain initial prompt")
	}
	if !strings.Contains(prompt, "tokenの期限切れ対応も必要") {
		t.Error("prompt should contain note")
	}
	if !strings.Contains(prompt, transcript) {
		t.Error("prompt should contain transcript")
	}
	// Should ask for JSON output
	if !strings.Contains(prompt, "JSON") {
		t.Error("prompt should request JSON output")
	}
}

func TestParseAnalyzeResponse(t *testing.T) {
	response := `{
  "goal": "OAuth認証を実装する",
  "current_focus": "tokenリフレッシュのテスト",
  "next_step": "エラーハンドリング追加",
  "attention_state": "active"
}`

	insight, err := ParseAnalyzeResponse("auth", response)
	if err != nil {
		t.Fatalf("ParseAnalyzeResponse() error = %v", err)
	}
	if insight.Name != "auth" {
		t.Errorf("Name = %q, want %q", insight.Name, "auth")
	}
	if insight.Goal != "OAuth認証を実装する" {
		t.Errorf("Goal = %q, want %q", insight.Goal, "OAuth認証を実装する")
	}
	if insight.CurrentFocus != "tokenリフレッシュのテスト" {
		t.Errorf("CurrentFocus = %q", insight.CurrentFocus)
	}
	if insight.NextStep != "エラーハンドリング追加" {
		t.Errorf("NextStep = %q", insight.NextStep)
	}
	if insight.AttentionState != model.AttentionActive {
		t.Errorf("AttentionState = %q, want %q", insight.AttentionState, model.AttentionActive)
	}
	if insight.InferredAt.IsZero() {
		t.Error("InferredAt should be set")
	}
}

func TestParseAnalyzeResponse_ExtractsJSONFromMarkdown(t *testing.T) {
	// LLMs often wrap JSON in markdown code blocks
	response := "Here is the analysis:\n```json\n{\"goal\": \"テスト\", \"current_focus\": \"実装中\", \"next_step\": \"完了\", \"attention_state\": \"active\"}\n```\n"

	insight, err := ParseAnalyzeResponse("test", response)
	if err != nil {
		t.Fatalf("ParseAnalyzeResponse() error = %v", err)
	}
	if insight.Goal != "テスト" {
		t.Errorf("Goal = %q, want %q", insight.Goal, "テスト")
	}
}

func TestParseAnalyzeResponse_WithTopicsAndTasks(t *testing.T) {
	response := `{
  "goal": "セッション管理機能を追加",
  "current_focus": "マイルストーン追跡",
  "next_step": "ダッシュボード表示",
  "attention_state": "active",
  "topics": [
    {"name": "マイルストーン", "keywords": ["commit", "push", "PR"]},
    {"name": "ダッシュボード", "keywords": ["web", "UI"]}
  ],
  "tasks": [
    {"title": "イベントストア実装", "status": "done", "topic": "マイルストーン"},
    {"title": "タイムライン表示", "status": "in_progress", "topic": "ダッシュボード"},
    {"title": "トピック抽出", "status": "planned"}
  ]
}`

	insight, err := ParseAnalyzeResponse("roadmap", response)
	if err != nil {
		t.Fatalf("ParseAnalyzeResponse() error = %v", err)
	}

	if len(insight.Topics) != 2 {
		t.Fatalf("Topics len = %d, want 2", len(insight.Topics))
	}
	if insight.Topics[0].Name != "マイルストーン" {
		t.Errorf("Topics[0].Name = %q", insight.Topics[0].Name)
	}
	if insight.Topics[0].Source != "llm" {
		t.Errorf("Topics[0].Source = %q, want llm", insight.Topics[0].Source)
	}
	if len(insight.Topics[0].Keywords) != 3 {
		t.Errorf("Topics[0].Keywords len = %d, want 3", len(insight.Topics[0].Keywords))
	}

	if len(insight.Tasks) != 3 {
		t.Fatalf("Tasks len = %d, want 3", len(insight.Tasks))
	}
	if insight.Tasks[0].Status != model.TaskDone {
		t.Errorf("Tasks[0].Status = %q, want done", insight.Tasks[0].Status)
	}
	if insight.Tasks[1].Status != model.TaskInProgress {
		t.Errorf("Tasks[1].Status = %q, want in_progress", insight.Tasks[1].Status)
	}
	if insight.Tasks[2].Status != model.TaskPlanned {
		t.Errorf("Tasks[2].Status = %q, want planned", insight.Tasks[2].Status)
	}
	// Tasks with topic should have TopicID
	if insight.Tasks[0].TopicID == "" {
		t.Error("Tasks[0].TopicID should be set")
	}
	if insight.Tasks[2].TopicID != "" {
		t.Error("Tasks[2].TopicID should be empty (no topic specified)")
	}
}

func TestParseAnalyzeResponse_InvalidTaskStatus(t *testing.T) {
	response := `{
  "goal": "test",
  "current_focus": "test",
  "next_step": "test",
  "attention_state": "active",
  "tasks": [{"title": "unknown status", "status": "invalid"}]
}`
	insight, err := ParseAnalyzeResponse("test", response)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if insight.Tasks[0].Status != model.TaskPlanned {
		t.Errorf("invalid status should default to planned, got %q", insight.Tasks[0].Status)
	}
}

func TestParseAnalyzeResponseWithDAGFields(t *testing.T) {
	response := `{
  "goal": "認証機能を実装する",
  "current_focus": "OAuthフロー",
  "next_step": "トークンリフレッシュ",
  "attention_state": "active",
  "tasks": [
    {"id": "setup-oauth", "title": "OAuth設定", "status": "done", "depends_on": [], "flows_to": "impl-token", "topic": "認証"},
    {"id": "impl-token", "title": "トークン取得実装", "status": "in_progress", "depends_on": ["setup-oauth"], "flows_to": "pr-review", "topic": "認証"},
    {"id": "write-tests", "title": "テスト作成", "status": "planned", "depends_on": ["setup-oauth"], "flows_to": "pr-review", "topic": "テスト"},
    {"id": "pr-review", "title": "PRレビュー", "status": "planned", "depends_on": ["impl-token", "write-tests"], "flows_to": "", "topic": "レビュー"}
  ]
}`
	insight, err := ParseAnalyzeResponse("auth", response)
	if err != nil {
		t.Fatalf("ParseAnalyzeResponse() error = %v", err)
	}
	if len(insight.Tasks) != 4 {
		t.Fatalf("Tasks len = %d, want 4", len(insight.Tasks))
	}
	// Check ID mapping
	if insight.Tasks[0].ID != "setup-oauth" {
		t.Errorf("Tasks[0].ID = %q, want %q", insight.Tasks[0].ID, "setup-oauth")
	}
	if insight.Tasks[1].ID != "impl-token" {
		t.Errorf("Tasks[1].ID = %q, want %q", insight.Tasks[1].ID, "impl-token")
	}
	// Check DependsOn mapping
	if len(insight.Tasks[1].DependsOn) != 1 || insight.Tasks[1].DependsOn[0] != "setup-oauth" {
		t.Errorf("Tasks[1].DependsOn = %v, want [setup-oauth]", insight.Tasks[1].DependsOn)
	}
	if len(insight.Tasks[3].DependsOn) != 2 {
		t.Errorf("Tasks[3].DependsOn len = %d, want 2", len(insight.Tasks[3].DependsOn))
	}
	// Check FlowsTo mapping
	if insight.Tasks[0].FlowsTo != "impl-token" {
		t.Errorf("Tasks[0].FlowsTo = %q, want %q", insight.Tasks[0].FlowsTo, "impl-token")
	}
	if insight.Tasks[3].FlowsTo != "" {
		t.Errorf("Tasks[3].FlowsTo = %q, want empty", insight.Tasks[3].FlowsTo)
	}
}

func TestParseAnalyzeResponseWithRejectedStatus(t *testing.T) {
	response := `{
  "goal": "リファクタリング",
  "current_focus": "構造整理",
  "next_step": "テスト追加",
  "attention_state": "active",
  "tasks": [
    {"id": "remove-legacy", "title": "レガシーコード削除", "status": "rejected", "depends_on": [], "flows_to": ""},
    {"id": "add-tests", "title": "テスト追加", "status": "in_progress", "depends_on": [], "flows_to": ""}
  ]
}`
	insight, err := ParseAnalyzeResponse("refactor", response)
	if err != nil {
		t.Fatalf("ParseAnalyzeResponse() error = %v", err)
	}
	if len(insight.Tasks) != 2 {
		t.Fatalf("Tasks len = %d, want 2", len(insight.Tasks))
	}
	if insight.Tasks[0].Status != model.TaskRejected {
		t.Errorf("Tasks[0].Status = %q, want %q", insight.Tasks[0].Status, model.TaskRejected)
	}
	if insight.Tasks[1].Status != model.TaskInProgress {
		t.Errorf("Tasks[1].Status = %q, want %q", insight.Tasks[1].Status, model.TaskInProgress)
	}
}

func TestParseAnalyzeResponseBackwardCompatibility(t *testing.T) {
	// id/depends_on/flows_to がないレスポンスでも問題なくパースできること
	response := `{
  "goal": "後方互換テスト",
  "current_focus": "パース確認",
  "next_step": "完了",
  "attention_state": "active",
  "tasks": [
    {"title": "既存タスク", "status": "done", "topic": "テスト"},
    {"title": "新規タスク", "status": "planned"}
  ]
}`
	insight, err := ParseAnalyzeResponse("compat", response)
	if err != nil {
		t.Fatalf("ParseAnalyzeResponse() error = %v", err)
	}
	if len(insight.Tasks) != 2 {
		t.Fatalf("Tasks len = %d, want 2", len(insight.Tasks))
	}
	// ID, DependsOn, FlowsTo はゼロ値のまま
	if insight.Tasks[0].ID != "" {
		t.Errorf("Tasks[0].ID = %q, want empty", insight.Tasks[0].ID)
	}
	if insight.Tasks[0].DependsOn != nil {
		t.Errorf("Tasks[0].DependsOn = %v, want nil", insight.Tasks[0].DependsOn)
	}
	if insight.Tasks[0].FlowsTo != "" {
		t.Errorf("Tasks[0].FlowsTo = %q, want empty", insight.Tasks[0].FlowsTo)
	}
	// 既存フィールドは正常にパースされること
	if insight.Tasks[0].Title != "既存タスク" {
		t.Errorf("Tasks[0].Title = %q, want %q", insight.Tasks[0].Title, "既存タスク")
	}
	if insight.Tasks[0].Status != model.TaskDone {
		t.Errorf("Tasks[0].Status = %q, want %q", insight.Tasks[0].Status, model.TaskDone)
	}
}

func TestBuildAnalyzePromptContainsDAGFields(t *testing.T) {
	ctx := &model.Context{
		Name: "test",
	}
	prompt := BuildAnalyzePrompt(ctx, "test transcript")

	checks := []string{"id", "depends_on", "flows_to", "rejected"}
	for _, keyword := range checks {
		if !strings.Contains(prompt, keyword) {
			t.Errorf("prompt should contain %q", keyword)
		}
	}
}

func TestReadTranscriptTail(t *testing.T) {
	lines := []string{
		`{"role":"user","content":"line1"}`,
		`{"role":"assistant","content":"line2"}`,
		`{"role":"user","content":"line3"}`,
		`{"role":"assistant","content":"line4"}`,
		`{"role":"user","content":"line5"}`,
	}
	content := strings.Join(lines, "\n")

	got, offset := ReadTranscriptTail(content, 3, 0)
	gotLines := strings.Split(strings.TrimSpace(got), "\n")
	if len(gotLines) != 3 {
		t.Fatalf("ReadTranscriptTail(3) returned %d lines, want 3", len(gotLines))
	}
	if offset == 0 {
		t.Error("offset should be > 0")
	}
}

func TestReadTranscriptTail_WithOffset(t *testing.T) {
	lines := []string{
		`{"role":"user","content":"old1"}`,
		`{"role":"assistant","content":"old2"}`,
		`{"role":"user","content":"new1"}`,
		`{"role":"assistant","content":"new2"}`,
	}
	content := strings.Join(lines, "\n")

	// First read: get last 2 lines
	_, offset := ReadTranscriptTail(content, 2, 0)

	// Second read with offset: should get only new lines after offset
	got, _ := ReadTranscriptTail(content+"\n"+`{"role":"user","content":"newest"}`, 100, offset)
	if !strings.Contains(got, "newest") {
		t.Error("should contain lines after offset")
	}
}

func TestReadTranscriptTail_FiltersNonConversational(t *testing.T) {
	// Regression: queue-operation / file-history-snapshot / progress entries
	// must be skipped. Embedding their `content` back into the analyze prompt
	// is what caused the exponential JSON-escape blowup.
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"始めよう"}}`,
		`{"type":"queue-operation","operation":"enqueue","content":"\\\\\\\\\\\\\\\\ noisy escaped blob"}`,
		`{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":{}}}`,
		`{"type":"progress","status":"running"}`,
		`{"type":"last-prompt","content":"…"}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"了解"}]}}`,
	}
	content := strings.Join(lines, "\n")

	got, _ := ReadTranscriptTail(content, 50, 0)

	if strings.Contains(got, "queue-operation") || strings.Contains(got, "noisy escaped blob") {
		t.Errorf("queue-operation entry must not appear in output: %q", got)
	}
	if strings.Contains(got, "file-history-snapshot") || strings.Contains(got, "trackedFileBackups") {
		t.Errorf("file-history-snapshot entry must not appear: %q", got)
	}
	if strings.Contains(got, `\\`) {
		t.Errorf("output must not carry escape sequences: %q", got)
	}
	if !strings.Contains(got, "user: 始めよう") {
		t.Errorf("user message missing from output: %q", got)
	}
	if !strings.Contains(got, "assistant: 了解") {
		t.Errorf("assistant text part missing from output: %q", got)
	}
}

func TestReadTranscriptTail_SidechainSkipped(t *testing.T) {
	// Sub-agent sidechain turns are noise for the parent session's analysis.
	lines := []string{
		`{"type":"user","isSidechain":true,"message":{"role":"user","content":"sub-agent prompt"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"main reply"}]}}`,
	}
	got, _ := ReadTranscriptTail(strings.Join(lines, "\n"), 50, 0)

	if strings.Contains(got, "sub-agent prompt") {
		t.Errorf("sidechain content must be filtered: %q", got)
	}
	if !strings.Contains(got, "main reply") {
		t.Errorf("main reply missing: %q", got)
	}
}

func TestReadTranscriptTail_ToolUseAndResult(t *testing.T) {
	// tool_use → short marker, tool_result → dropped (large/noisy).
	line := `{"type":"assistant","message":{"role":"assistant","content":[` +
		`{"type":"text","text":"調べます"},` +
		`{"type":"tool_use","name":"Bash","input":{"command":"git status"}},` +
		`{"type":"tool_result","content":"on branch main\nnothing to commit"}` +
		`]}}`

	got, _ := ReadTranscriptTail(line, 50, 0)

	if !strings.Contains(got, "調べます") {
		t.Errorf("text part missing: %q", got)
	}
	if !strings.Contains(got, "[tool: Bash]") {
		t.Errorf("tool_use marker missing: %q", got)
	}
	if strings.Contains(got, "nothing to commit") {
		t.Errorf("tool_result body must not leak into output: %q", got)
	}
}

func TestReadTranscriptTail_TruncatesLongMessage(t *testing.T) {
	// A single huge user paste must not dominate the prompt.
	huge := strings.Repeat("あ", 5000)
	line := `{"type":"user","message":{"role":"user","content":` +
		jsonString(huge) + `}}`

	got, _ := ReadTranscriptTail(line, 50, 0)

	runes := []rune(got)
	if len(runes) > maxTranscriptMessageRunes+200 {
		t.Errorf("message not truncated: %d runes", len(runes))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncation marker missing: %q", got[len(got)-30:])
	}
}

// jsonString quotes s as a JSON string literal for embedding in raw JSONL
// fixtures. Equivalent to encoding/json's string encoding for ASCII/Unicode
// content (no need for HTML-safe escaping in tests).
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
