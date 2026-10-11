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
		if p == "" {
			return "(unknown)"
		}
		return string(p)
	}
}

type Context struct {
	Name           string          `yaml:"name"`
	Worktree       string          `yaml:"worktree"`
	Branch         string          `yaml:"branch"`
	SessionID      string          `yaml:"session_id"`
	SessionName    string          `yaml:"session_name,omitempty"` // Claude Code's auto-generated session name (slug)
	TranscriptPath string          `yaml:"transcript_path,omitempty"`
	Status         Status          `yaml:"status"`
	CreatedAt      time.Time       `yaml:"created_at"`
	LastSeen       time.Time       `yaml:"last_seen"`
	Checklist      map[string]bool `yaml:"checklist,omitempty"`
	Note           string          `yaml:"note,omitempty"`
	TotalTime      time.Duration   `yaml:"total_time,omitempty"`
	IssueURL       string          `yaml:"issue_url,omitempty"`
	PRURL          string          `yaml:"pr_url,omitempty"`
	InitialPrompt  string          `yaml:"initial_prompt,omitempty"`
	Phase          Phase           `yaml:"phase,omitempty"`
	PhaseCheckedAt time.Time       `yaml:"phase_checked_at,omitempty"`
	RepoRoot       string          `yaml:"repo_root,omitempty"` // Git repo root path for project grouping
	Provider       Provider        `yaml:"provider,omitempty"`  // 空は claude（EffectiveProvider を使う）
	AgentState     AgentState      `yaml:"agent_state,omitempty"`
	AgentStateAt   time.Time       `yaml:"agent_state_at,omitempty"`
	// PendingRequest は needs_input の間だけ、何を待っているか（hook 由来の単一スロット）
	PendingRequest *PendingRequest `yaml:"pending_request,omitempty"`
	// TaskRef は手で作ったタスク（"island:t<n>"）への紐付け。register は触らない。
	// Why: Claude は worktree 単位で context を使い回すので、新しいセッションでも前のリンクを残して見失わないようにする
	TaskRef string `yaml:"task_ref,omitempty"`
	// TaskLinkSession / Source / At は、どのセッションがいつ・どう付けたか。
	// 同じセッションの 2 つ目以降の marker を無視し、新セッションの最初の marker だけが付け替えられるようにするための記録
	TaskLinkSession string    `yaml:"task_link_session,omitempty"`
	TaskLinkSource  string    `yaml:"task_link_source,omitempty"` // "marker" | "manual"
	TaskLinkAt      time.Time `yaml:"task_link_at,omitempty"`
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
