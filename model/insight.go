package model

import "time"

type AttentionState string

const (
	AttentionActive  AttentionState = "active"
	AttentionWaiting AttentionState = "waiting"
	AttentionIdle    AttentionState = "idle"
	AttentionBlocked AttentionState = "blocked"
)

type TaskStatus string

const (
	TaskPlanned    TaskStatus = "planned"
	TaskInProgress TaskStatus = "in_progress"
	TaskDone       TaskStatus = "done"
	TaskBlocked    TaskStatus = "blocked"
)

type SemanticTopic struct {
	ID       string   `yaml:"id" json:"id"`
	Name     string   `yaml:"name" json:"name"`
	Keywords []string `yaml:"keywords,omitempty" json:"keywords,omitempty"`
	Source   string   `yaml:"source" json:"source"` // "git", "transcript", "llm", "manual"
}

type TaskItem struct {
	Title    string     `yaml:"title" json:"title"`
	Status   TaskStatus `yaml:"status" json:"status"`
	TopicID  string     `yaml:"topic_id,omitempty" json:"topic_id,omitempty"`
	Evidence []string   `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	Source   string     `yaml:"source" json:"source"` // "git", "transcript", "llm", "manual"
}

type SessionInsight struct {
	Name             string         `yaml:"name"`
	Goal             string         `yaml:"goal,omitempty"`
	CurrentFocus     string         `yaml:"current_focus,omitempty"`
	NextStep         string         `yaml:"next_step,omitempty"`
	AttentionState   AttentionState `yaml:"attention_state,omitempty"`
	InferredAt       time.Time      `yaml:"inferred_at,omitempty"`
	TranscriptOffset int64          `yaml:"transcript_offset,omitempty"`
	Topics           []SemanticTopic `yaml:"topics,omitempty"`
	Tasks            []TaskItem      `yaml:"tasks,omitempty"`
}

type InsightStore struct {
	Insights []SessionInsight `yaml:"insights"`
}

func (s *InsightStore) Get(name string) *SessionInsight {
	for i := range s.Insights {
		if s.Insights[i].Name == name {
			return &s.Insights[i]
		}
	}
	return nil
}

func (s *InsightStore) Set(insight SessionInsight) {
	for i := range s.Insights {
		if s.Insights[i].Name == insight.Name {
			s.Insights[i] = insight
			return
		}
	}
	s.Insights = append(s.Insights, insight)
}
