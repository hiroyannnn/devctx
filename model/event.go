package model

import "time"

type MilestoneType string

const (
	MilestoneFirstCommit  MilestoneType = "first_commit"
	MilestoneCommit       MilestoneType = "commit"
	MilestoneFirstPush    MilestoneType = "first_push"
	MilestonePRCreated    MilestoneType = "pr_created"
	MilestonePRMerged     MilestoneType = "pr_merged"
	MilestoneSessionStart MilestoneType = "session_start"
	MilestoneSessionEnd   MilestoneType = "session_end"
	MilestoneCommand      MilestoneType = "command"
	MilestoneStatusChange MilestoneType = "status_change"
)

type SessionEvent struct {
	SessionName string        `yaml:"session_name"`
	Type        MilestoneType `yaml:"type"`
	Detail      string        `yaml:"detail,omitempty"`
	OccurredAt  time.Time     `yaml:"occurred_at"`
	ObservedAt  time.Time     `yaml:"observed_at"`
}

type EventStore struct {
	Events []SessionEvent `yaml:"events"`
}

// ForSession returns all events for a given session name.
func (s *EventStore) ForSession(name string) []SessionEvent {
	var result []SessionEvent
	for _, e := range s.Events {
		if e.SessionName == name {
			result = append(result, e)
		}
	}
	return result
}

// Append adds an event to the store.
func (s *EventStore) Append(event SessionEvent) {
	s.Events = append(s.Events, event)
}

// HasMilestone checks if a milestone of a given type already exists for a session.
func (s *EventStore) HasMilestone(sessionName string, mtype MilestoneType) bool {
	for _, e := range s.Events {
		if e.SessionName == sessionName && e.Type == mtype {
			return true
		}
	}
	return false
}

// MilestoneSummary provides a summary view of milestones for a session.
type MilestoneSummary struct {
	SessionName    string    `json:"session_name"`
	FirstCommitAt  time.Time `json:"first_commit_at,omitempty"`
	LatestCommitAt time.Time `json:"latest_commit_at,omitempty"`
	CommitCount    int       `json:"commit_count"`
	FirstPushAt    time.Time `json:"first_push_at,omitempty"`
	PRCreatedAt    time.Time `json:"pr_created_at,omitempty"`
	PRMergedAt     time.Time `json:"pr_merged_at,omitempty"`
	SessionCount   int       `json:"session_count"`
	CommandCount   int       `json:"command_count"`
}

// Summarize builds a MilestoneSummary from events for a session.
func (s *EventStore) Summarize(sessionName string) MilestoneSummary {
	summary := MilestoneSummary{SessionName: sessionName}
	for _, e := range s.Events {
		if e.SessionName != sessionName {
			continue
		}
		switch e.Type {
		case MilestoneFirstCommit:
			summary.FirstCommitAt = e.OccurredAt
		case MilestoneCommit:
			summary.CommitCount++
			if summary.LatestCommitAt.Before(e.OccurredAt) {
				summary.LatestCommitAt = e.OccurredAt
			}
		case MilestoneFirstPush:
			summary.FirstPushAt = e.OccurredAt
		case MilestonePRCreated:
			summary.PRCreatedAt = e.OccurredAt
		case MilestonePRMerged:
			summary.PRMergedAt = e.OccurredAt
		case MilestoneSessionStart:
			summary.SessionCount++
		case MilestoneCommand:
			summary.CommandCount++
		}
	}
	return summary
}
