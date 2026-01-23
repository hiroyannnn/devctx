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
	"github.com/spf13/cobra"
)

type TranscriptMessage struct {
	Type      string    `json:"type"`
	Role      string    `json:"role,omitempty"`
	Content   string    `json:"content,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
}

type SearchResult struct {
	SessionID      string
	TranscriptPath string
	ProjectPath    string
	Matches        []MatchedLine
	LastModified   time.Time
}

type MatchedLine struct {
	LineNum int
	Role    string
	Content string
}

var (
	searchLimit   int
	searchContext int
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search through session history",
	Long: `Search through all Claude Code session transcripts.

Examples:
  devctx search "authentication"
  devctx search "OAuth" --limit 5
  devctx search "error" --context 2`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.ToLower(args[0])

		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		claudeDir := filepath.Join(home, ".claude", "projects")
		if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
			return fmt.Errorf("Claude projects directory not found: %s", claudeDir)
		}

		results, err := searchTranscripts(claudeDir, query)
		if err != nil {
			return err
		}

		if len(results) == 0 {
			fmt.Println("No matches found.")
			return nil
		}

		// Sort by last modified (most recent first)
		sort.Slice(results, func(i, j int) bool {
			return results[i].LastModified.After(results[j].LastModified)
		})

		// Apply limit
		if searchLimit > 0 && len(results) > searchLimit {
			results = results[:searchLimit]
		}

		// Display results
		displaySearchResults(results, query)

		return nil
	},
}

func searchTranscripts(claudeDir, query string) ([]SearchResult, error) {
	var results []SearchResult

	err := filepath.Walk(claudeDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		matches, err := searchFile(path, query)
		if err != nil {
			return nil // Skip files with errors
		}

		if len(matches) > 0 {
			// Extract session ID from filename
			sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")

			// Get project path from directory
			projectPath := filepath.Dir(path)
			projectPath = strings.TrimPrefix(projectPath, claudeDir+"/")

			results = append(results, SearchResult{
				SessionID:      sessionID,
				TranscriptPath: path,
				ProjectPath:    projectPath,
				Matches:        matches,
				LastModified:   info.ModTime(),
			})
		}

		return nil
	})

	return results, err
}

func searchFile(path, query string) ([]MatchedLine, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var matches []MatchedLine
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for long lines

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		var msg TranscriptMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		// Search in content
		content := strings.ToLower(msg.Content)
		if strings.Contains(content, query) {
			matches = append(matches, MatchedLine{
				LineNum: lineNum,
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	return matches, scanner.Err()
}

func displaySearchResults(results []SearchResult, query string) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	matchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	roleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))

	fmt.Printf("Found %d session(s) with matches:\n\n", len(results))

	for _, result := range results {
		// Session header
		fmt.Println(titleStyle.Render(fmt.Sprintf("Session: %s", result.SessionID[:min(12, len(result.SessionID))])))
		fmt.Println(pathStyle.Render(fmt.Sprintf("  Project: %s", result.ProjectPath)))
		fmt.Println(pathStyle.Render(fmt.Sprintf("  Modified: %s", formatRelativeTime(result.LastModified))))
		fmt.Printf("  Matches: %d\n", len(result.Matches))

		// Show first few matches
		maxMatches := 3
		for i, match := range result.Matches {
			if i >= maxMatches {
				fmt.Printf("  ... and %d more matches\n", len(result.Matches)-maxMatches)
				break
			}

			content := match.Content
			if len(content) > 200 {
				// Find the query position and show context around it
				idx := strings.Index(strings.ToLower(content), query)
				start := max(0, idx-50)
				end := min(len(content), idx+len(query)+100)
				content = "..." + content[start:end] + "..."
			}

			// Highlight the query
			highlighted := highlightQuery(content, query)

			fmt.Println()
			fmt.Printf("  %s: %s\n", roleStyle.Render(match.Role), matchStyle.Render(highlighted))
		}

		fmt.Println()
		fmt.Println("  " + pathStyle.Render(fmt.Sprintf("Resume: dx %s (if registered)", result.SessionID[:8])))
		fmt.Println(strings.Repeat("─", 60))
		fmt.Println()
	}
}

func highlightQuery(content, query string) string {
	// Simple case-insensitive highlight
	lower := strings.ToLower(content)
	idx := strings.Index(lower, query)
	if idx == -1 {
		return content
	}

	return content[:idx] + "**" + content[idx:idx+len(query)] + "**" + content[idx+len(query):]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().IntVar(&searchLimit, "limit", 10, "Maximum number of sessions to show")
	searchCmd.Flags().IntVar(&searchContext, "context", 0, "Lines of context around matches")
}
