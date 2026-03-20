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
