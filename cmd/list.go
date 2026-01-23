package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const columnWidth = 32

var (
	// Lane header styles
	inProgressHeader = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("10")).
				Width(columnWidth).
				Align(lipgloss.Center)

	reviewHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("11")).
			Width(columnWidth).
			Align(lipgloss.Center)

	blockedHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("9")).
			Width(columnWidth).
			Align(lipgloss.Center)

	doneHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("8")).
			Width(columnWidth).
			Align(lipgloss.Center)

	// Card styles
	inProgressStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("10")).
			Padding(0, 1).
			Width(columnWidth - 2)

	reviewStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("11")).
			Padding(0, 1).
			Width(columnWidth - 2)

	blockedStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")).
			Padding(0, 1).
			Width(columnWidth - 2)

	doneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1).
			Width(columnWidth - 2)

	// Column container style
	columnStyle = lipgloss.NewStyle().
			Width(columnWidth).
			MarginRight(2)

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

		// Names-only mode for shell completion
		if listNamesOnly {
			store, err := s.LoadStore()
			if err != nil {
				return err
			}
			for _, ctx := range store.Active() {
				fmt.Println(ctx.Name)
			}
			return nil
		}

		// fzf mode for interactive selection
		if listFzf {
			store, err := s.LoadStore()
			if err != nil {
				return err
			}
			for _, ctx := range store.Active() {
				status := statusIcon(ctx.Status)
				lastSeen := formatRelativeTime(ctx.LastSeen)
				fmt.Printf("%s\t%s\t%s\t%s\n", ctx.Name, status, ctx.Branch, lastSeen)
			}
			return nil
		}

		// Watch mode or single display
		for {
			store, err := s.LoadStore()
			if err != nil {
				return err
			}

			if len(store.Contexts) == 0 {
				// Auto-discover existing sessions
				sessions, err := discoverSessions(store)
				if err != nil || len(sessions) == 0 {
					if listWatch {
						fmt.Print("\033[H\033[2J") // Clear screen
						fmt.Println(dimStyle.Render("No Claude Code sessions found."))
						fmt.Println(dimStyle.Render("Waiting for sessions... (Ctrl+C to exit)"))
						time.Sleep(2 * time.Second)
						continue
					}
					fmt.Println(dimStyle.Render("No Claude Code sessions found."))
					fmt.Println(dimStyle.Render("Start a Claude Code session and it will appear here."))
					return nil
				}

				// Auto-import recent sessions (last 30 days)
				if !listWatch {
					fmt.Println(dimStyle.Render("Auto-importing discovered sessions..."))
					fmt.Println()
				}

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

			if listWatch {
				fmt.Print("\033[H\033[2J") // Clear screen
				fmt.Printf("devctx - %s (Ctrl+C to exit)\n", time.Now().Format("15:04:05"))
			}

			printKanban(store)

			if !listWatch {
				return nil
			}

			time.Sleep(2 * time.Second)
		}
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
	lanes := []struct {
		status      model.Status
		title       string
		headerStyle lipgloss.Style
		cardStyle   lipgloss.Style
	}{
		{model.StatusInProgress, "🚀 In Progress", inProgressHeader, inProgressStyle},
		{model.StatusReview, "👀 Review", reviewHeader, reviewStyle},
		{model.StatusBlocked, "🚧 Blocked", blockedHeader, blockedStyle},
		{model.StatusDone, "✅ Done", doneHeader, doneStyle},
	}

	// Get terminal height and calculate max cards
	maxCards := 5 // default
	if _, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && h > 0 {
		// Each card is ~6 lines, header is 3 lines, leave 3 for margins
		availableLines := h - 6
		if availableLines > 0 {
			maxCards = availableLines / 6
			if maxCards < 1 {
				maxCards = 1
			}
		}
	}

	// Build each lane column
	var columns []string
	for _, lane := range lanes {
		contexts := store.ByStatus(lane.status)

		// Skip Done column if empty
		if len(contexts) == 0 && lane.status == model.StatusDone {
			continue
		}

		var col strings.Builder

		// Header
		col.WriteString(lane.headerStyle.Render(lane.title))
		col.WriteString("\n")
		col.WriteString(strings.Repeat("─", columnWidth))
		col.WriteString("\n")

		// Cards
		if len(contexts) == 0 {
			emptyStyle := lipgloss.NewStyle().
				Width(columnWidth - 2).
				Foreground(lipgloss.Color("8")).
				Align(lipgloss.Center)
			col.WriteString(emptyStyle.Render("(empty)"))
			col.WriteString("\n")
		} else {
			displayed := 0
			for _, ctx := range contexts {
				if displayed >= maxCards {
					remaining := len(contexts) - displayed
					moreStyle := lipgloss.NewStyle().
						Foreground(lipgloss.Color("8")).
						Italic(true)
					col.WriteString(moreStyle.Render(fmt.Sprintf("  +%d more...", remaining)))
					col.WriteString("\n")
					break
				}
				card := formatCard(ctx)
				col.WriteString(lane.cardStyle.Render(card))
				col.WriteString("\n")
				displayed++
			}
		}

		columns = append(columns, columnStyle.Render(col.String()))
	}

	// Join columns horizontally
	fmt.Println()
	fmt.Println(lipgloss.JoinHorizontal(lipgloss.Top, columns...))
}

func formatCard(ctx model.Context) string {
	var b strings.Builder

	// Name (bold)
	b.WriteString(nameStyle.Render(ctx.Name))
	b.WriteString("\n")

	// Branch (truncate if too long)
	branch := ctx.Branch
	if len(branch) > 24 {
		branch = branch[:21] + "..."
	}
	b.WriteString(branchStyle.Render("⎇ " + branch))

	// Time info
	lastSeen := formatRelativeTime(ctx.LastSeen)
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("⏱ " + lastSeen))
	if ctx.TotalTime > 0 {
		b.WriteString(dimStyle.Render(" ⌛" + formatDuration(ctx.TotalTime)))
	}

	// Note (truncate)
	if ctx.Note != "" {
		note := ctx.Note
		if len(note) > 22 {
			note = note[:19] + "..."
		}
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("📝 " + note))
	}

	// PR/Issue indicator (just icon, no full URL)
	if ctx.IssueURL != "" || ctx.PRURL != "" {
		b.WriteString("\n")
		if ctx.PRURL != "" {
			b.WriteString(dimStyle.Render("🔀 PR linked"))
		} else if ctx.IssueURL != "" {
			b.WriteString(dimStyle.Render("🔗 Issue linked"))
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
