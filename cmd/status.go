package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

type SessionStatus string

const (
	SessionStatusActive  SessionStatus = "active"   // Recently updated (< 30s)
	SessionStatusIdle    SessionStatus = "idle"     // Not recently updated (> 30s)
	SessionStatusWaiting SessionStatus = "waiting"  // Last message was from assistant
	SessionStatusOffline SessionStatus = "offline"  // No transcript or very old
)

type LiveStatus struct {
	Context       *model.Context
	SessionStatus SessionStatus
	LastActivity  time.Time
	LastRole      string
}

var watchMode bool

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"ps"},
	Short:   "Show live status of all contexts",
	Long: `Show real-time status of all registered contexts.

Status indicators:
  🟢 active  - Claude is currently working (updated < 30s ago)
  🟡 waiting - Waiting for user input
  ⚪ idle    - Session is idle (updated > 30s ago)
  ⚫ offline - No active session`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		if watchMode {
			return watchStatus(store)
		}

		return showStatus(store)
	},
}

func showStatus(store *model.Store) error {
	statuses := getLiveStatuses(store)

	if len(statuses) == 0 {
		fmt.Println("No contexts registered.")
		return nil
	}

	// Styles
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	waitingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	idleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	offlineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	nameStyle := lipgloss.NewStyle().Bold(true)

	fmt.Println("Live Session Status:")
	fmt.Println()

	for _, ls := range statuses {
		var liveIcon, statusText string
		var style lipgloss.Style

		switch ls.SessionStatus {
		case SessionStatusActive:
			liveIcon = "🟢"
			statusText = "active"
			style = activeStyle
		case SessionStatusWaiting:
			liveIcon = "🟡"
			statusText = "waiting"
			style = waitingStyle
		case SessionStatusIdle:
			liveIcon = "⚪"
			statusText = "idle"
			style = idleStyle
		case SessionStatusOffline:
			liveIcon = "⚫"
			statusText = "offline"
			style = offlineStyle
		}

		name := nameStyle.Render(fmt.Sprintf("[%s]", ls.Context.Name))
		status := style.Render(fmt.Sprintf("%s %s", liveIcon, statusText))

		lastActivity := ""
		if !ls.LastActivity.IsZero() {
			lastActivity = fmt.Sprintf(" (%s)", formatRelativeTime(ls.LastActivity))
		}

		fmt.Printf("%s %s%s\n", name, status, idleStyle.Render(lastActivity))
		fmt.Printf("    %s %s\n", statusIcon(ls.Context.Status), ls.Context.Status)
		if ls.Context.Branch != "" {
			fmt.Printf("    ⎇ %s\n", ls.Context.Branch)
		}
		fmt.Println()
	}

	return nil
}

func watchStatus(store *model.Store) error {
	fmt.Println("Watching session status... (Ctrl+C to exit)")
	fmt.Println()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Initial display
	showStatus(store)

	for range ticker.C {
		// Clear and redraw
		fmt.Print("\033[H\033[2J")
		fmt.Println("Watching session status... (Ctrl+C to exit)")
		fmt.Printf("Updated: %s\n\n", time.Now().Format("15:04:05"))
		showStatus(store)
	}

	return nil
}

func getLiveStatuses(store *model.Store) []LiveStatus {
	var statuses []LiveStatus

	for _, ctx := range store.Active() {
		ls := LiveStatus{
			Context:       &ctx,
			SessionStatus: SessionStatusOffline,
		}

		if ctx.TranscriptPath != "" {
			status, lastActivity, lastRole := getTranscriptStatus(ctx.TranscriptPath)
			ls.SessionStatus = status
			ls.LastActivity = lastActivity
			ls.LastRole = lastRole
		}

		statuses = append(statuses, ls)
	}

	return statuses
}

func getTranscriptStatus(transcriptPath string) (SessionStatus, time.Time, string) {
	// Expand ~ in path
	if strings.HasPrefix(transcriptPath, "~") {
		home, _ := os.UserHomeDir()
		transcriptPath = filepath.Join(home, transcriptPath[1:])
	}

	info, err := os.Stat(transcriptPath)
	if err != nil {
		return SessionStatusOffline, time.Time{}, ""
	}

	lastModified := info.ModTime()
	timeSinceUpdate := time.Since(lastModified)

	// Get last message role
	lastRole := getLastMessageRole(transcriptPath)

	// Determine status based on time since last update
	if timeSinceUpdate < 30*time.Second {
		if lastRole == "assistant" {
			return SessionStatusWaiting, lastModified, lastRole
		}
		return SessionStatusActive, lastModified, lastRole
	}

	if timeSinceUpdate < 5*time.Minute {
		if lastRole == "assistant" {
			return SessionStatusWaiting, lastModified, lastRole
		}
		return SessionStatusIdle, lastModified, lastRole
	}

	return SessionStatusOffline, lastModified, lastRole
}

func getLastMessageRole(transcriptPath string) string {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	var lastRole string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var msg struct {
			Role string `json:"role"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil {
			if msg.Role != "" {
				lastRole = msg.Role
			}
		}
	}

	return lastRole
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().BoolVarP(&watchMode, "watch", "w", false, "Watch mode - continuously update status")
}
