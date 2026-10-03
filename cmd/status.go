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
	"github.com/hiroyannnn/devctx/agentview"
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
	// Reason は待ち要求のラベル、なければ live の待ち理由（permission prompt 等）
	Reason string
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

		return showStatus(store, newLiveViews())
	},
}

func showStatus(store *model.Store, live *liveViews) error {
	statuses := getLiveStatuses(store, live.views(store.Contexts))

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

		if ls.Reason != "" {
			status += idleStyle.Render(" · " + ls.Reason)
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

	// watch は 2 秒 tick。tick では Refresher の直近 snapshot を読むだけで、claude は待たない
	live := newWatchLiveViews()

	// Initial display
	showStatus(store, live)

	for range ticker.C {
		// Clear and redraw
		fmt.Print("\033[H\033[2J")
		fmt.Println("Watching session status... (Ctrl+C to exit)")
		fmt.Printf("Updated: %s\n\n", time.Now().Format("15:04:05"))
		showStatus(store, live)
	}

	return nil
}

// getLiveStatuses は context ごとの稼働状態を返す。優先順位は live（agent view）→ transcript 推論。
// views は agentview.Overlay の結果（nil なら transcript 推論のみ）。
func getLiveStatuses(store *model.Store, views map[string]agentview.View) []LiveStatus {
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

		// agent view の live 状態だけを transcript の mtime 推論より優先する。
		// Why not hook state: hook の状態は SessionEnd が欠けると古いまま残る（昨日の turn_done 等）ため、
		// 従来 hook を見ていなかった status の推論を上書きすると後退になる
		if view := views[ctx.Name]; view.Source == agentview.SourceLive {
			if status, ok := sessionStatusFromAgentState(view.State); ok {
				ls.SessionStatus = status
				ls.Reason = view.Detail()
			}
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

// sessionStatusFromAgentState は live の状態を status 表示用の区分に写す。
// turn_done もユーザーの番なので waiting 扱い。live は ended を返さないので offline への写像は持たない。
func sessionStatusFromAgentState(state model.AgentState) (SessionStatus, bool) {
	switch {
	case state == model.AgentRunning:
		return SessionStatusActive, true
	case state.WaitsForUser():
		return SessionStatusWaiting, true
	}
	return "", false
}
