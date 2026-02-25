package model

import "time"

type Status string

const (
	StatusInProgress Status = "in-progress"
	StatusReview     Status = "review"
	StatusBlocked    Status = "blocked"
	StatusDone       Status = "done"
)

type Phase string

const (
	PhaseIdle           Phase = "idle"
	PhaseImplementation Phase = "implementation"
	PhaseCommitted      Phase = "committed"
	PhasePushed         Phase = "pushed"
	PhasePROpen         Phase = "pr_open"
	PhaseDone           Phase = "done"
)

func AllPhases() []Phase {
	return []Phase{PhaseIdle, PhaseImplementation, PhaseCommitted, PhasePushed, PhasePROpen, PhaseDone}
}

func (p Phase) Label() string {
	switch p {
	case PhaseIdle:
		return "Idle"
	case PhaseImplementation:
		return "Implementation"
	case PhaseCommitted:
		return "Committed"
	case PhasePushed:
		return "Pushed"
	case PhasePROpen:
		return "PR Open"
	case PhaseDone:
		return "Done"
	default:
		return string(p)
	}
}

type Context struct {
	Name           string            `yaml:"name"`
	Worktree       string            `yaml:"worktree"`
	Branch         string            `yaml:"branch"`
	SessionID      string            `yaml:"session_id"`
	SessionName    string            `yaml:"session_name,omitempty"` // Claude Code's auto-generated session name (slug)
	TranscriptPath string            `yaml:"transcript_path,omitempty"`
	Status         Status            `yaml:"status"`
	CreatedAt      time.Time         `yaml:"created_at"`
	LastSeen       time.Time         `yaml:"last_seen"`
	Checklist      map[string]bool   `yaml:"checklist,omitempty"`
	Note           string            `yaml:"note,omitempty"`
	TotalTime      time.Duration     `yaml:"total_time,omitempty"`
	IssueURL       string            `yaml:"issue_url,omitempty"`
	PRURL          string            `yaml:"pr_url,omitempty"`
	InitialPrompt  string            `yaml:"initial_prompt,omitempty"`
}

type Config struct {
	Statuses          []StatusConfig `yaml:"statuses"`
	DoneRetentionDays int            `yaml:"done_retention_days,omitempty"`
	AutoImport        *bool          `yaml:"auto_import,omitempty"` // nil = true (default enabled)
}

type StatusConfig struct {
	Name      Status   `yaml:"name"`
	Next      []Status `yaml:"next"`
	Checklist []string `yaml:"checklist,omitempty"`
	Archive   bool     `yaml:"archive,omitempty"`
}

type Store struct {
	Contexts []Context `yaml:"contexts"`
}

func (s *Store) FindByName(name string) *Context {
	for i := range s.Contexts {
		if s.Contexts[i].Name == name {
			return &s.Contexts[i]
		}
	}
	return nil
}

func (s *Store) FindBySessionID(sessionID string) *Context {
	for i := range s.Contexts {
		if s.Contexts[i].SessionID == sessionID {
			return &s.Contexts[i]
		}
	}
	return nil
}

func (s *Store) FindByWorktree(worktree string) *Context {
	for i := range s.Contexts {
		if s.Contexts[i].Worktree == worktree {
			return &s.Contexts[i]
		}
	}
	return nil
}

func (s *Store) Active() []Context {
	return s.ActiveWithRetention(0)
}

func (s *Store) ActiveWithRetention(doneRetentionDays int) []Context {
	var active []Context
	cutoff := time.Now().AddDate(0, 0, -doneRetentionDays)

	for _, c := range s.Contexts {
		if c.Status != StatusDone {
			active = append(active, c)
		} else if doneRetentionDays > 0 && c.LastSeen.After(cutoff) {
			// Include recently completed items
			active = append(active, c)
		}
	}
	return active
}

func (s *Store) ByStatus(status Status) []Context {
	var result []Context
	for _, c := range s.Contexts {
		if c.Status == status {
			result = append(result, c)
		}
	}
	return result
}

func (s *Store) Add(ctx Context) {
	s.Contexts = append(s.Contexts, ctx)
}

func (s *Store) Remove(name string) bool {
	for i, c := range s.Contexts {
		if c.Name == name {
			s.Contexts = append(s.Contexts[:i], s.Contexts[i+1:]...)
			return true
		}
	}
	return false
}
