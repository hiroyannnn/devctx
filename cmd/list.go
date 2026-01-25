package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
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

		// Load config for retention days
		config, _ := s.LoadConfig()
		retentionDays := 1
		if config != nil && config.DoneRetentionDays > 0 {
			retentionDays = config.DoneRetentionDays
		}

		// Names-only mode for shell completion
		if listNamesOnly {
			store, err := s.LoadStore()
			if err != nil {
				return err
			}
			for _, ctx := range store.ActiveWithRetention(retentionDays) {
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
			for _, ctx := range store.ActiveWithRetention(retentionDays) {
				status := statusIcon(ctx.Status)
				lastSeen := formatRelativeTime(ctx.LastSeen)
				fmt.Printf("%s\t%s\t%s\t%s\n", ctx.Name, status, ctx.Branch, lastSeen)
			}
			return nil
		}

		// Auto-discover and import on first run
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		if len(store.Contexts) == 0 {
			sessions, err := discoverSessions(store)
			if err == nil && len(sessions) > 0 {
				fmt.Println(dimStyle.Render("Auto-importing discovered sessions..."))
				fmt.Println()

				imported := 0
				cutoff := time.Now().AddDate(0, 0, -1)
				for _, sess := range sessions {
					if sess.LastModified.Before(cutoff) || sess.IsRegistered {
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
						SessionName:    sess.SessionName,
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
		}

		// Watch mode with interactive scrolling
		if listWatch {
			p := tea.NewProgram(newKanbanModel(s), tea.WithAltScreen())
			_, err := p.Run()
			return err
		}

		// Single display
		printKanban(store, 0)
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

func printKanban(store *model.Store, offset int) {
	printKanbanWithSize(store, offset, "", 0, 0)
}

func printKanbanWithSize(store *model.Store, offset int, selectedName string, width int, height int) string {
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
	h := height
	if h == 0 {
		if _, termH, err := term.GetSize(int(os.Stdout.Fd())); err == nil && termH > 0 {
			h = termH
		}
	}
	if h > 0 {
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

		// Header with count
		title := fmt.Sprintf("%s (%d)", lane.title, len(contexts))
		col.WriteString(lane.headerStyle.Render(title))
		col.WriteString("\n")
		col.WriteString(strings.Repeat("─", columnWidth))
		col.WriteString("\n")

		// Cards with offset
		if len(contexts) == 0 {
			emptyStyle := lipgloss.NewStyle().
				Width(columnWidth - 2).
				Foreground(lipgloss.Color("8")).
				Align(lipgloss.Center)
			col.WriteString(emptyStyle.Render("(empty)"))
			col.WriteString("\n")
		} else {
			// Apply offset
			startIdx := offset
			if startIdx >= len(contexts) {
				startIdx = len(contexts) - 1
			}
			if startIdx < 0 {
				startIdx = 0
			}

			// Show "N above" indicator
			if startIdx > 0 {
				moreStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("8")).
					Italic(true)
				col.WriteString(moreStyle.Render(fmt.Sprintf("  ↑ %d above...", startIdx)))
				col.WriteString("\n")
			}

			displayed := 0
			for i := startIdx; i < len(contexts); i++ {
				if displayed >= maxCards {
					remaining := len(contexts) - i
					moreStyle := lipgloss.NewStyle().
						Foreground(lipgloss.Color("8")).
						Italic(true)
					col.WriteString(moreStyle.Render(fmt.Sprintf("  ↓ %d more...", remaining)))
					col.WriteString("\n")
					break
				}
				card := formatCard(contexts[i])
				// Highlight selected card
				cardStyle := lane.cardStyle
				if contexts[i].Name == selectedName {
					cardStyle = cardStyle.Copy().
						BorderForeground(lipgloss.Color("14")).
						BorderStyle(lipgloss.ThickBorder())
				}
				col.WriteString(cardStyle.Render(card))
				col.WriteString("\n")
				displayed++
			}
		}

		columns = append(columns, columnStyle.Render(col.String()))
	}

	// Join columns horizontally
	result := "\n" + lipgloss.JoinHorizontal(lipgloss.Top, columns...)
	if width == 0 && height == 0 {
		fmt.Println(result)
	}
	return result
}

func formatCard(ctx model.Context) string {
	var b strings.Builder

	// Name (bold)
	b.WriteString(nameStyle.Render(ctx.Name))
	b.WriteString("\n")

	// Session name (Claude's auto-generated slug)
	if ctx.SessionName != "" {
		sessionName := ctx.SessionName
		if len(sessionName) > 26 {
			sessionName = sessionName[:23] + "..."
		}
		sessionStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("13")).
			Italic(true)
		b.WriteString(sessionStyle.Render("💬 " + sessionName))
		b.WriteString("\n")
	}

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

// Bubble Tea model for interactive kanban view
type kanbanModel struct {
	storage           *storage.Storage
	store             *model.Store
	config            *model.Config
	cursor            int    // Selected item index
	offset            int    // Scroll offset for display
	width             int
	height            int
	maxItem           int
	message           string // Status message
	contexts          []model.Context
	doneRetentionDays int
}

type tickMsg time.Time

func newKanbanModel(s *storage.Storage) kanbanModel {
	store, _ := s.LoadStore()
	config, _ := s.LoadConfig()

	retentionDays := 1 // default
	if config != nil && config.DoneRetentionDays > 0 {
		retentionDays = config.DoneRetentionDays
	}

	contexts := store.ActiveWithRetention(retentionDays)
	return kanbanModel{
		storage:           s,
		store:             store,
		config:            config,
		cursor:            0,
		offset:            0,
		maxItem:           len(contexts),
		contexts:          contexts,
		doneRetentionDays: retentionDays,
	}
}

func (m kanbanModel) Init() tea.Cmd {
	return tea.Batch(tickCmd(), tea.EnterAltScreen)
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m kanbanModel) selectedContext() *model.Context {
	if m.cursor >= 0 && m.cursor < len(m.contexts) {
		return &m.contexts[m.cursor]
	}
	return nil
}

func (m kanbanModel) getResumeCommand() string {
	ctx := m.selectedContext()
	if ctx == nil {
		return ""
	}
	cmd := fmt.Sprintf("cd '%s'", ctx.Worktree)
	if ctx.SessionID != "" {
		cmd += fmt.Sprintf(" && claude --resume '%s'", ctx.SessionID)
	} else {
		cmd += " && claude"
	}
	return cmd
}

func (m kanbanModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		// Reload store
		store, err := m.storage.LoadStore()
		if err == nil {
			m.store = store
			m.contexts = store.ActiveWithRetention(m.doneRetentionDays)
			m.maxItem = len(m.contexts)
			if m.cursor >= m.maxItem {
				m.cursor = m.maxItem - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
		}
		m.message = ""
		return m, tickCmd()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit

		case "j", "down":
			if m.cursor < m.maxItem-1 {
				m.cursor++
				// Adjust offset to keep cursor visible
				maxVisible := m.calcMaxVisible()
				if m.cursor >= m.offset+maxVisible {
					m.offset = m.cursor - maxVisible + 1
				}
			}
			return m, nil

		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
				// Adjust offset to keep cursor visible
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}
			return m, nil

		case "g", "home":
			m.cursor = 0
			m.offset = 0
			return m, nil

		case "G", "end":
			m.cursor = m.maxItem - 1
			if m.cursor < 0 {
				m.cursor = 0
			}
			maxVisible := m.calcMaxVisible()
			if m.cursor >= maxVisible {
				m.offset = m.cursor - maxVisible + 1
			}
			return m, nil

		case "enter", "c":
			// Copy resume command to clipboard
			cmd := m.getResumeCommand()
			if cmd != "" {
				if err := clipboard.WriteAll(cmd); err == nil {
					m.message = "📋 Copied to clipboard!"
				} else {
					m.message = "❌ Failed to copy"
				}
			}
			return m, nil

		case "o":
			// Open in new terminal
			ctx := m.selectedContext()
			if ctx != nil {
				if err := openInNewTerminal(ctx.Worktree, ctx.SessionID); err == nil {
					m.message = "🚀 Opened in new terminal!"
				} else {
					m.message = "❌ Failed to open: " + err.Error()
				}
			}
			return m, nil

		case "r":
			return m.moveSelectedTo(model.StatusReview, "👀 Moved to Review")

		case "p":
			return m.moveSelectedTo(model.StatusInProgress, "🚀 Moved to In Progress")

		case "b":
			return m.moveSelectedTo(model.StatusBlocked, "🚧 Moved to Blocked")

		case "D":
			return m.moveSelectedTo(model.StatusDone, "✅ Moved to Done")

		case "x":
			// Delete/remove context
			ctx := m.selectedContext()
			if ctx != nil {
				m.store.Remove(ctx.Name)
				if err := m.storage.SaveStore(m.store); err == nil {
					m.contexts = m.store.ActiveWithRetention(m.doneRetentionDays)
					m.maxItem = len(m.contexts)
					if m.cursor >= m.maxItem {
						m.cursor = m.maxItem - 1
					}
					if m.cursor < 0 {
						m.cursor = 0
					}
					m.message = "🗑 Removed: " + ctx.Name
				}
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *kanbanModel) moveSelectedTo(status model.Status, msg string) (tea.Model, tea.Cmd) {
	ctx := m.selectedContext()
	if ctx == nil {
		return m, nil
	}

	// Find and update in store
	storeCtx := m.store.FindByName(ctx.Name)
	if storeCtx != nil {
		storeCtx.Status = status
		storeCtx.LastSeen = time.Now() // Update LastSeen when status changes
		if err := m.storage.SaveStore(m.store); err == nil {
			m.contexts = m.store.ActiveWithRetention(m.doneRetentionDays)
			m.maxItem = len(m.contexts)
			if m.cursor >= m.maxItem {
				m.cursor = m.maxItem - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.message = msg
		}
	}
	return m, nil
}

func (m kanbanModel) calcMaxVisible() int {
	if m.height > 0 {
		return (m.height - 6) / 6
	}
	return 5
}

func (m kanbanModel) View() string {
	// Header with help
	help := "↑↓:move c:copy o:open r:review p:progress b:blocked D:done x:delete q:quit"
	header := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		Render(fmt.Sprintf("devctx - %s | %s", time.Now().Format("15:04:05"), help))

	// Selected context info
	var selectedInfo string
	if ctx := m.selectedContext(); ctx != nil {
		selectedStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true)
		selectedInfo = "\n" + selectedStyle.Render(fmt.Sprintf("▶ %s", ctx.Name))
		if ctx.Branch != "" {
			selectedInfo += dimStyle.Render(fmt.Sprintf(" (%s)", ctx.Branch))
		}
	}

	// Status message
	var msgLine string
	if m.message != "" {
		msgStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("10"))
		msgLine = "\n" + msgStyle.Render(m.message)
	}

	selectedName := ""
	if ctx := m.selectedContext(); ctx != nil {
		selectedName = ctx.Name
	}
	kanban := printKanbanWithSize(m.store, m.offset, selectedName, m.width, m.height-4)

	return header + selectedInfo + msgLine + kanban
}

func openInNewTerminal(worktree, sessionID string) error {
	var cmd string
	if sessionID != "" {
		cmd = fmt.Sprintf("cd '%s' && claude --resume '%s'", worktree, sessionID)
	} else {
		cmd = fmt.Sprintf("cd '%s' && claude", worktree)
	}

	switch runtime.GOOS {
	case "darwin":
		// macOS - try iTerm2 first, then Terminal.app
		script := fmt.Sprintf(`
			tell application "System Events"
				if exists (processes where name is "iTerm2") then
					tell application "iTerm"
						create window with default profile command "/bin/zsh -c '%s'"
					end tell
				else
					tell application "Terminal"
						do script "%s"
						activate
					end tell
				end if
			end tell
		`, strings.ReplaceAll(cmd, "'", "'\"'\"'"), strings.ReplaceAll(cmd, "\"", "\\\""))
		return exec.Command("osascript", "-e", script).Start()

	case "linux":
		// Try common terminal emulators
		terminals := []struct {
			name string
			args []string
		}{
			{"gnome-terminal", []string{"--", "bash", "-c", cmd + "; exec bash"}},
			{"konsole", []string{"-e", "bash", "-c", cmd + "; exec bash"}},
			{"xterm", []string{"-e", "bash", "-c", cmd + "; exec bash"}},
		}
		for _, t := range terminals {
			if _, err := exec.LookPath(t.name); err == nil {
				return exec.Command(t.name, t.args...).Start()
			}
		}
		return fmt.Errorf("no terminal emulator found")

	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}
