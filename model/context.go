package model

import "time"

type Status string

const (
	StatusInProgress Status = "in-progress"
	StatusReview     Status = "review"
	StatusBlocked    Status = "blocked"
	StatusDone       Status = "done"
)

type Context struct {
	Name           string            `yaml:"name"`
	Worktree       string            `yaml:"worktree"`
	Branch         string            `yaml:"branch"`
	SessionID      string            `yaml:"session_id"`
	TranscriptPath string            `yaml:"transcript_path,omitempty"`
	Status         Status            `yaml:"status"`
	CreatedAt      time.Time         `yaml:"created_at"`
	LastSeen       time.Time         `yaml:"last_seen"`
	Checklist      map[string]bool   `yaml:"checklist,omitempty"`
}

type Config struct {
	Statuses []StatusConfig `yaml:"statuses"`
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
	var active []Context
	for _, c := range s.Contexts {
		if c.Status != StatusDone {
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
