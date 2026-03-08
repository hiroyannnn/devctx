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
