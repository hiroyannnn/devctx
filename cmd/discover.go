package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

type DiscoveredSession struct {
	SessionID      string
	TranscriptPath string
	ProjectPath    string // The actual project directory
	ProjectHash    string // The hash used in ~/.claude/projects/
	LastModified   time.Time
	MessageCount   int
	IsRegistered   bool
}

var (
	discoverImport bool
	discoverAll    bool
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover existing Claude Code sessions",
	Long: `Scan ~/.claude/projects/ to find existing Claude Code sessions.

This helps you see sessions that weren't registered with devctx hooks.
Use --import to automatically register discovered sessions.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		sessions, err := discoverSessions(store)
		if err != nil {
			return err
		}

		if len(sessions) == 0 {
			fmt.Println("No Claude Code sessions found.")
			return nil
		}

		// Filter to unregistered only unless --all
		var displaySessions []DiscoveredSession
		for _, sess := range sessions {
			if discoverAll || !sess.IsRegistered {
				displaySessions = append(displaySessions, sess)
			}
		}

		if len(displaySessions) == 0 {
			fmt.Println("All sessions are already registered.")
			return nil
		}

		// Display
		displayDiscoveredSessions(displaySessions)

		// Import if requested
		if discoverImport {
			return importSessions(s, store, displaySessions)
		}

		fmt.Println()
		fmt.Println("Run 'devctx discover --import' to register these sessions.")

		return nil
	},
}

func discoverSessions(store *model.Store) ([]DiscoveredSession, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	claudeDir := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		return nil, nil
	}

	var sessions []DiscoveredSession

	// Walk through project directories
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectHash := entry.Name()
		projectDir := filepath.Join(claudeDir, projectHash)

		// Find transcript files
		transcripts, err := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
		if err != nil {
			continue
		}

		for _, transcriptPath := range transcripts {
			info, err := os.Stat(transcriptPath)
			if err != nil {
				continue
			}

			sessionID := strings.TrimSuffix(filepath.Base(transcriptPath), ".jsonl")

			// Try to get project path from transcript
			projectPath := extractProjectPath(transcriptPath)

			// Check if already registered
			isRegistered := store.FindBySessionID(sessionID) != nil

			// Count messages
			msgCount := countMessages(transcriptPath)

			sessions = append(sessions, DiscoveredSession{
				SessionID:      sessionID,
				TranscriptPath: transcriptPath,
				ProjectPath:    projectPath,
				ProjectHash:    projectHash,
				LastModified:   info.ModTime(),
				MessageCount:   msgCount,
				IsRegistered:   isRegistered,
			})
		}
	}

	// Sort by last modified (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastModified.After(sessions[j].LastModified)
	})

	return sessions, nil
}

func extractProjectPath(transcriptPath string) string {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Look for cwd in the first few lines
	lineCount := 0
	for scanner.Scan() && lineCount < 50 {
		lineCount++
		var msg struct {
			Cwd string `json:"cwd"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil {
			if msg.Cwd != "" {
				return msg.Cwd
			}
		}
	}

	return ""
}

func countMessages(transcriptPath string) int {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return 0
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		count++
	}

	return count
}

func displayDiscoveredSessions(sessions []DiscoveredSession) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	registeredStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	newStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	fmt.Printf("Found %d session(s):\n\n", len(sessions))

	for i, sess := range sessions {
		// Limit display
		if i >= 20 {
			fmt.Printf("... and %d more sessions\n", len(sessions)-20)
			break
		}

		statusTag := newStyle.Render("[NEW]")
		if sess.IsRegistered {
			statusTag = registeredStyle.Render("[registered]")
		}

		sessionShort := sess.SessionID
		if len(sessionShort) > 12 {
			sessionShort = sessionShort[:12] + "..."
		}

		fmt.Printf("%s %s\n", titleStyle.Render(sessionShort), statusTag)

		if sess.ProjectPath != "" {
			fmt.Printf("  📁 %s\n", pathStyle.Render(shortenPath(sess.ProjectPath)))
		}

		fmt.Printf("  📝 %d messages  ⏱ %s\n",
			sess.MessageCount,
			pathStyle.Render(formatRelativeTime(sess.LastModified)))
		fmt.Println()
	}
}

func importSessions(s *storage.Storage, store *model.Store, sessions []DiscoveredSession) error {
	imported := 0

	for _, sess := range sessions {
		if sess.IsRegistered {
			continue
		}

		// Generate name from project path or session ID
		name := generateNameFromPath(sess.ProjectPath, sess.SessionID)

		// Check for name collision
		if store.FindByName(name) != nil {
			name = name + "-" + sess.SessionID[:6]
		}

		// Detect branch if possible
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
		fmt.Printf("✓ Imported [%s] from %s\n", name, shortenPath(sess.ProjectPath))
	}

	if imported > 0 {
		if err := s.SaveStore(store); err != nil {
			return err
		}
		fmt.Printf("\n✓ Imported %d session(s)\n", imported)
	}

	return nil
}

func generateNameFromPath(projectPath, sessionID string) string {
	if projectPath != "" {
		// Use directory name
		name := filepath.Base(projectPath)
		if name != "" && name != "." && name != "/" {
			return name
		}
	}

	// Fallback to session ID prefix
	if len(sessionID) > 8 {
		return "session-" + sessionID[:8]
	}
	return "session-" + sessionID
}

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().BoolVar(&discoverImport, "import", false, "Import discovered sessions")
	discoverCmd.Flags().BoolVar(&discoverAll, "all", false, "Show all sessions including registered ones")
}
