package roadmap

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// maxAnalyzeTranscriptRunes caps the transcript portion of the prompt as a
// final safety net. Beyond per-message truncation in ReadTranscriptTail, this
// guarantees the transcript block can never dominate the prompt or LLM context.
const maxAnalyzeTranscriptRunes = 32000

// BuildAnalyzePrompt creates a prompt for Claude to analyze a session.
func BuildAnalyzePrompt(ctx *model.Context, transcript string) string {
	transcript = truncateRunes(transcript, maxAnalyzeTranscriptRunes)
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
  "attention_state": "active|waiting|idle|blocked のいずれか",
  "topics": [
    {"name": "トピック名", "keywords": ["関連キーワード"]}
  ],
  "tasks": [
    {
      "id": "slug形式のタスクID",
      "title": "タスク名",
      "status": "planned|in_progress|done|blocked|rejected",
      "depends_on": ["依存先タスクID"],
      "flows_to": "合流先タスクID",
      "topic": "関連トピック名"
    }
  ]
}

attention_stateの判定基準:
- active: 作業が進行中
- waiting: ユーザーの入力やレビューを待っている
- idle: 作業が一段落して次の指示待ち
- blocked: エラーや問題で詰まっている

topicsはこのセッションで扱っている意味的なテーマ（2-5個程度）。

tasksは具体的な作業項目とその状態。以下のルールに従ってください:
- 各タスクに一意のid（英数字とハイフンのslug形式）を付与すること
- タスク間の依存関係をdepends_onで明示すること
- 複数タスクの結果が合流するノード（PRレビュー等）はdepends_onに合流元を列挙すること
- 不要と判断されたタスクはstatus: "rejected"とし、flows_toを空にすること
- rejectedはタスクが不要になった/実施しないことを意味する

JSONのみを出力してください。`)

	return b.String()
}

// analyzeResponse is the expected JSON structure from the LLM.
type analyzeResponse struct {
	Goal           string              `json:"goal"`
	CurrentFocus   string              `json:"current_focus"`
	NextStep       string              `json:"next_step"`
	AttentionState string              `json:"attention_state"`
	Topics         []analyzeTopicResp  `json:"topics,omitempty"`
	Tasks          []analyzeTaskResp   `json:"tasks,omitempty"`
}

type analyzeTopicResp struct {
	Name     string   `json:"name"`
	Keywords []string `json:"keywords,omitempty"`
}

type analyzeTaskResp struct {
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Topic     string   `json:"topic,omitempty"`
	ID        string   `json:"id,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	FlowsTo   string   `json:"flows_to,omitempty"`
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

	insight := &model.SessionInsight{
		Name:           name,
		Goal:           resp.Goal,
		CurrentFocus:   resp.CurrentFocus,
		NextStep:       resp.NextStep,
		AttentionState: state,
		InferredAt:     time.Now(),
	}

	// Parse LLM-inferred topics
	for _, t := range resp.Topics {
		if t.Name == "" {
			continue
		}
		insight.Topics = append(insight.Topics, model.SemanticTopic{
			ID:       topicID(t.Name),
			Name:     t.Name,
			Keywords: t.Keywords,
			Source:   "llm",
		})
	}

	// Parse LLM-inferred tasks
	for _, t := range resp.Tasks {
		if t.Title == "" {
			continue
		}
		status := model.TaskStatus(t.Status)
		switch status {
		case model.TaskPlanned, model.TaskInProgress, model.TaskDone, model.TaskBlocked, model.TaskRejected:
			// valid
		default:
			status = model.TaskPlanned
		}
		task := model.TaskItem{
			Title:     t.Title,
			Status:    status,
			Source:    "llm",
			ID:        t.ID,
			DependsOn: t.DependsOn,
			FlowsTo:   t.FlowsTo,
		}
		// Link to topic by name
		if t.Topic != "" {
			task.TopicID = topicID(t.Topic)
		}
		insight.Tasks = append(insight.Tasks, task)
	}

	return insight, nil
}

// maxTranscriptMessageRunes caps each extracted message so a single huge entry
// (pasted log, file dump) cannot dominate the analyze prompt.
const maxTranscriptMessageRunes = 2000

// ReadTranscriptTail parses Claude Code JSONL transcript content and returns a
// compact human-readable excerpt of the last maxMessages user/assistant turns.
//
// Non-conversational events (queue-operation, file-history-snapshot, progress,
// last-prompt, sub-agent sidechains) and tool result payloads are filtered out.
// Without this, a previous bug embedded raw JSONL — including queue-operation
// entries that themselves contained a prior prompt — back into the next prompt,
// doubling JSON-escape characters every analyze cycle and producing tens of MB
// of `\\\\…` blowup in the session log.
//
// If fromOffset > 0 and inside the content, parsing starts at that byte. The
// returned offset is the end-of-content position to remember for next time.
func ReadTranscriptTail(content string, maxMessages int, fromOffset int64) (string, int64) {
	newOffset := int64(len(content))

	if fromOffset > 0 && fromOffset < int64(len(content)) {
		content = content[fromOffset:]
	}

	var messages []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		msg, ok := extractTranscriptMessage(line)
		if !ok {
			continue
		}
		messages = append(messages, msg)
	}

	if maxMessages > 0 && len(messages) > maxMessages {
		messages = messages[len(messages)-maxMessages:]
	}

	return strings.Join(messages, "\n"), newOffset
}

// transcriptEntry covers both Claude Code's nested format
// ({"type":"user","message":{"role":..,"content":..}}) and the flatter
// shape used by older snapshots and test fixtures
// ({"role":"user","content":".."}). Fields not in the line are zero values.
type transcriptEntry struct {
	Type        string             `json:"type"`
	IsSidechain bool               `json:"isSidechain"`
	Message     *transcriptMessage `json:"message,omitempty"`
	Role        string             `json:"role,omitempty"`
	Content     json.RawMessage    `json:"content,omitempty"`
}

type transcriptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// extractTranscriptMessage returns "<role>: <text>" for one JSONL line, or
// false if the line should be skipped (sidechain, non-conversational type,
// missing/empty content).
func extractTranscriptMessage(line string) (string, bool) {
	var entry transcriptEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return "", false
	}
	if entry.IsSidechain {
		return "", false
	}
	if entry.Type != "" && entry.Type != "user" && entry.Type != "assistant" {
		return "", false
	}

	var role string
	var content json.RawMessage
	if entry.Message != nil {
		role = entry.Message.Role
		content = entry.Message.Content
	} else {
		role = entry.Role
		content = entry.Content
	}
	if role != "user" && role != "assistant" {
		return "", false
	}

	text := extractTranscriptContentText(content)
	if text == "" {
		return "", false
	}
	return role + ": " + truncateRunes(text, maxTranscriptMessageRunes), true
}

// extractTranscriptContentText flattens Claude Code's content field into plain
// text. Content is either a JSON string or an array of typed parts; tool_use
// becomes a short marker, tool_result is dropped to keep the excerpt compact.
func extractTranscriptContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			if t := strings.TrimSpace(p.Text); t != "" {
				out = append(out, t)
			}
		case "tool_use":
			if p.Name != "" {
				out = append(out, "[tool: "+p.Name+"]")
			}
		}
	}
	return strings.Join(out, "\n")
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// AnalyzeMaxReadBytes is the file-size threshold above which the transcript
// is tail-read instead of fully loaded. Anything bigger is either a very
// long-running session (where the recent tail is what we care about anyway)
// or a corrupted file from a past escape blowup.
const AnalyzeMaxReadBytes = 10 * 1024 * 1024 // 10MB

// AnalyzeTailReadBytes is how many bytes from the end of an oversized
// transcript we read. Generous enough to capture the last several dozen
// turns of a normal session.
const AnalyzeTailReadBytes = 1 * 1024 * 1024 // 1MB

// ReadTranscriptForAnalyze loads a Claude Code JSONL transcript from disk and
// returns a compact human-readable excerpt suitable for the analyze prompt.
//
// Files at or below AnalyzeMaxReadBytes are loaded fully and incrementally
// processed via prevOffset. Larger files are tail-read (last
// AnalyzeTailReadBytes only); in that case prevOffset is ignored and the
// returned offset is the full file size. tailed=true tells the caller to
// surface a warning. fullSize is always the on-disk file size for diagnostics.
func ReadTranscriptForAnalyze(path string, maxMessages int, prevOffset int64) (excerpt string, newOffset int64, tailed bool, fullSize int64, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		return "", 0, false, 0, statErr
	}
	fullSize = info.Size()

	if fullSize <= AnalyzeMaxReadBytes {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", 0, false, fullSize, readErr
		}
		excerpt, newOffset = ReadTranscriptTail(string(data), maxMessages, prevOffset)
		return excerpt, newOffset, false, fullSize, nil
	}

	f, openErr := os.Open(path)
	if openErr != nil {
		return "", 0, false, fullSize, openErr
	}
	defer f.Close()

	if _, seekErr := f.Seek(fullSize-AnalyzeTailReadBytes, io.SeekStart); seekErr != nil {
		return "", 0, false, fullSize, seekErr
	}
	data, readErr := io.ReadAll(f)
	if readErr != nil {
		return "", 0, false, fullSize, readErr
	}
	// Tail-read invalidates absolute byte offsets; ignore prevOffset and
	// hand back fullSize so the next call starts fresh from end-of-file.
	excerpt, _ = ReadTranscriptTail(string(data), maxMessages, 0)
	return excerpt, fullSize, true, fullSize, nil
}
