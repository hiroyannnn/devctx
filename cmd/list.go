package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62")).
			Padding(0, 1)

	inProgressStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("10")).
			Padding(0, 1).
			Width(50)

	reviewStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("11")).
			Padding(0, 1).
			Width(50)

	blockedStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")).
			Padding(0, 1).
			Width(50)

	doneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1).
			Width(50)

	nameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	branchStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14"))
)

var listFzf bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Display contexts in kanban view",
	Aliases: []string{"ls", "l"},
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		// Names-only mode for shell completion
		if listNamesOnly {
			for _, ctx := range store.Active() {
				fmt.Println(ctx.Name)
			}
			return nil
		}

		// fzf mode for interactive selection
		if listFzf {
			for _, ctx := range store.Active() {
				status := statusIcon(ctx.Status)
				lastSeen := formatRelativeTime(ctx.LastSeen)
				fmt.Printf("%s\t%s\t%s\t%s\n", ctx.Name, status, ctx.Branch, lastSeen)
			}
			return nil
		}

		if len(store.Contexts) == 0 {
			// Auto-discover existing sessions
			sessions, err := discoverSessions(store)
			if err != nil || len(sessions) == 0 {
				fmt.Println(dimStyle.Render("No Claude Code sessions found."))
				fmt.Println(dimStyle.Render("Start a Claude Code session and it will appear here."))
				return nil
			}

			// Auto-import recent sessions (last 30 days)
			fmt.Println(dimStyle.Render("Auto-importing discovered sessions..."))
			fmt.Println()

			imported := 0
			cutoff := time.Now().AddDate(0, 0, -30)
			for _, sess := range sessions {
				if sess.LastModified.Before(cutoff) {
					continue
				}
				if sess.IsRegistered {
					continue
				}

				name := generateNameFromPath(sess.ProjectPath, sess.SessionID)
				if store.FindByName(name) != nil {
					name = name + "-" + sess.SessionID[:6]
				}

				branch := ""
				if sess.ProjectPath != "" {
					branch = getGitBranch(sess.ProjectPath)
				}

				ctx := model.Context{
					Name:           name,
					Worktree:       sess.ProjectPath,
					Branch:         branch,
					SessionID:      sess.SessionID,
					TranscriptPath: sess.TranscriptPath,
					Status:         model.StatusInProgress,
					CreatedAt:      sess.LastModified,
					LastSeen:       sess.LastModified,
					Checklist:      make(map[string]bool),
				}

				store.Add(ctx)
				imported++
			}

			if imported > 0 {
				if err := s.SaveStore(store); err != nil {
					return err
				}
			}
		}

		printKanban(store)
		return nil
	},
}

func statusIcon(status model.Status) string {
	switch status {
	case model.StatusInProgress:
		return "🚀"
	case model.StatusReview:
		return "👀"
	case model.StatusBlocked:
		return "🚧"
	case model.StatusDone:
		return "✅"
	default:
		return "📋"
	}
}

func printKanban(store *model.Store) {
	statuses := []struct {
		status model.Status
		title  string
		style  lipgloss.Style
	}{
		{model.StatusInProgress, "🚀 In Progress", inProgressStyle},
		{model.StatusReview, "👀 Review", reviewStyle},
		{model.StatusBlocked, "🚧 Blocked", blockedStyle},
		{model.StatusDone, "✅ Done (Recent)", doneStyle},
	}

	for _, s := range statuses {
		contexts := store.ByStatus(s.status)
		if len(contexts) == 0 && s.status == model.StatusDone {
			continue // Skip empty done section
		}

		fmt.Println(headerStyle.Render(s.title))
		fmt.Println()

		if len(contexts) == 0 {
			fmt.Println(dimStyle.Render("  (empty)"))
		} else {
			for _, ctx := range contexts {
				card := formatCard(ctx)
				fmt.Println(s.style.Render(card))
			}
		}
		fmt.Println()
	}
}

func formatCard(ctx model.Context) string {
	var b strings.Builder

	// Name
	b.WriteString(nameStyle.Render(fmt.Sprintf("[%s]", ctx.Name)))
	b.WriteString("\n")

	// Branch
	b.WriteString(branchStyle.Render(fmt.Sprintf("  ⎇ %s", ctx.Branch)))
	b.WriteString("\n")

	// Worktree (shortened)
	worktree := shortenPath(ctx.Worktree)
	b.WriteString(dimStyle.Render(fmt.Sprintf("  📁 %s", worktree)))
	b.WriteString("\n")

	// Session info
	sessionShort := ctx.SessionID
	if len(sessionShort) > 8 {
		sessionShort = sessionShort[:8] + "..."
	}
	lastSeen := formatRelativeTime(ctx.LastSeen)
	timeInfo := fmt.Sprintf("  🤖 %s  ⏱ %s", sessionShort, lastSeen)
	if ctx.TotalTime > 0 {
		timeInfo += fmt.Sprintf("  ⌛ %s", formatDuration(ctx.TotalTime))
	}
	b.WriteString(dimStyle.Render(timeInfo))

	// Note if any
	if ctx.Note != "" {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf("  📝 %s", ctx.Note)))
	}

	// Issue/PR links if any
	if ctx.IssueURL != "" || ctx.PRURL != "" {
		b.WriteString("\n")
		if ctx.IssueURL != "" {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  🔗 %s", ctx.IssueURL)))
		}
		if ctx.PRURL != "" {
			if ctx.IssueURL != "" {
				b.WriteString("\n")
			}
			b.WriteString(dimStyle.Render(fmt.Sprintf("  🔀 %s", ctx.PRURL)))
		}
	}

	// Checklist if any
	if len(ctx.Checklist) > 0 {
		b.WriteString("\n")
		for item, done := range ctx.Checklist {
			mark := "☐"
			if done {
				mark = "☑"
			}
			b.WriteString(dimStyle.Render(fmt.Sprintf("  %s %s", mark, item)))
		}
	}

	return b.String()
}

func shortenPath(path string) string {
	home, _ := filepath.Abs(filepath.Join("~"))
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	// Keep last 3 components
	parts := strings.Split(path, string(filepath.Separator))
	if len(parts) > 3 {
		return ".../" + strings.Join(parts[len(parts)-3:], "/")
	}
	return path
}

func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
