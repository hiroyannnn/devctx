package roadmap

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// BuildAnalyzePrompt creates a prompt for Claude to analyze a session.
func BuildAnalyzePrompt(ctx *model.Context, transcript string) string {
	var b strings.Builder

	b.WriteString("以下の開発セッションの状態を分析して、JSON形式で回答してください。\n\n")

	b.WriteString("## セッション情報\n")
	b.WriteString(fmt.Sprintf("- 名前: %s\n", ctx.Name))
	if ctx.Branch != "" {
		b.WriteString(fmt.Sprintf("- ブランチ: %s\n", ctx.Branch))
	}
	if ctx.InitialPrompt != "" {
		b.WriteString(fmt.Sprintf("- 初期プロンプト: %s\n", ctx.InitialPrompt))
	}
	if ctx.Note != "" {
		b.WriteString(fmt.Sprintf("- メモ: %s\n", ctx.Note))
	}

	b.WriteString("\n## 会話ログ（直近）\n```\n")
	b.WriteString(transcript)
	b.WriteString("\n```\n\n")

	b.WriteString(`以下のJSON形式で回答してください。各フィールドは日本語で簡潔に記述してください。

{
  "goal": "このセッションが達成しようとしていること（1行）",
  "current_focus": "今取り組んでいるサブタスク（1行）",
  "next_step": "次にやるべきこと（1行）",
  "attention_state": "active|waiting|idle|blocked のいずれか"
}

attention_stateの判定基準:
- active: 作業が進行中
- waiting: ユーザーの入力やレビューを待っている
- idle: 作業が一段落して次の指示待ち
- blocked: エラーや問題で詰まっている

JSONのみを出力してください。`)

	return b.String()
}

// analyzeResponse is the expected JSON structure from the LLM.
type analyzeResponse struct {
	Goal           string `json:"goal"`
	CurrentFocus   string `json:"current_focus"`
	NextStep       string `json:"next_step"`
	AttentionState string `json:"attention_state"`
}

var jsonBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*\\n?(.*?)\\n?```")

// ParseAnalyzeResponse parses the LLM response into a SessionInsight.
func ParseAnalyzeResponse(name string, response string) (*model.SessionInsight, error) {
	// Try to extract JSON from markdown code block
	if matches := jsonBlockRe.FindStringSubmatch(response); len(matches) > 1 {
		response = strings.TrimSpace(matches[1])
	}

	var resp analyzeResponse
	if err := json.Unmarshal([]byte(response), &resp); err != nil {
		// Try to find bare JSON object
		start := strings.Index(response, "{")
		end := strings.LastIndex(response, "}")
		if start >= 0 && end > start {
			if err2 := json.Unmarshal([]byte(response[start:end+1]), &resp); err2 != nil {
				return nil, fmt.Errorf("failed to parse LLM response as JSON: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to parse LLM response as JSON: %w", err)
		}
	}

	state := model.AttentionState(resp.AttentionState)
	switch state {
	case model.AttentionActive, model.AttentionWaiting, model.AttentionIdle, model.AttentionBlocked:
		// valid
	default:
		state = model.AttentionIdle
	}

	return &model.SessionInsight{
		Name:           name,
		Goal:           resp.Goal,
		CurrentFocus:   resp.CurrentFocus,
		NextStep:       resp.NextStep,
		AttentionState: state,
		InferredAt:     time.Now(),
	}, nil
}

// ReadTranscriptTail reads the last maxLines lines from the transcript content.
// If fromOffset > 0, only reads content after that byte offset.
// Returns the lines and the new offset (end of content).
func ReadTranscriptTail(content string, maxLines int, fromOffset int64) (string, int64) {
	newOffset := int64(len(content))

	if fromOffset > 0 && fromOffset < int64(len(content)) {
		content = content[fromOffset:]
	}

	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	return strings.Join(lines, "\n"), newOffset
}
