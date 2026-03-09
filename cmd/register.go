package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/roadmap"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

type SessionStartInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Source         string `json:"source"` // "startup", "resume", "clear"
}

var registerCmd = &cobra.Command{
	Use:   "register [name]",
	Short: "Register current worktree with a Claude session",
	Long: `Register the current directory as a development context.
If called from a Claude Code hook, reads session info from stdin.
If called manually, uses current directory and prompts for name.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		var input SessionStartInput
		var name string

		// Check if stdin has data (called from hook)
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			// Reading from pipe (hook mode)
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
					return fmt.Errorf("failed to parse hook input: %w", err)
				}
			}
		}

		// Get working directory
		cwd := input.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		// Detect git info
		branch := getGitBranch(cwd)
		worktreeRoot := getWorktreeRoot(cwd)
		if worktreeRoot != "" {
			cwd = worktreeRoot
		}

		// Detect repo root for project grouping
		repoRoot := detectRepoRoot(cwd)

		// Check if already registered by worktree
		existing := store.FindByWorktree(cwd)
		if existing != nil {
			// Update existing context
			if input.SessionID != "" {
				existing.SessionID = input.SessionID
				existing.TranscriptPath = input.TranscriptPath
				// Try to extract session name from transcript
				if input.TranscriptPath != "" {
					if sessionName := extractSessionName(input.TranscriptPath); sessionName != "" {
						existing.SessionName = sessionName
					}
				}
			}
			existing.LastSeen = time.Now()
			if branch != "" {
				existing.Branch = branch
			}
			if repoRoot != "" {
				existing.RepoRoot = repoRoot
			}
			// Auto-detect phase (fast mode for hook performance)
			phaseScanner := roadmap.NewScanner()
			phaseScanner.RefreshPhase(existing, roadmap.ScanModeFast)

			// Collect git milestones
			collectAndSaveMilestones(s, existing)

			// Record session_start event
			recordEvent(s, existing.Name, model.MilestoneSessionStart, "")

			if err := s.SaveStore(store); err != nil {
				return err
			}
			fmt.Printf("Updated context [%s]\n", existing.Name)
			return nil
		}

		// Determine name
		if len(args) > 0 {
			name = args[0]
		} else {
			// Generate name from branch or directory
			name = generateName(branch, cwd)
		}

		// Check for name collision
		if store.FindByName(name) != nil {
			name = name + "-" + time.Now().Format("0102")
		}

		// Extract session name if transcript is available
		sessionName := ""
		if input.TranscriptPath != "" {
			sessionName = extractSessionName(input.TranscriptPath)
		}

		// Create new context
		ctx := model.Context{
			Name:           name,
			Worktree:       cwd,
			Branch:         branch,
			SessionID:      input.SessionID,
			SessionName:    sessionName,
			TranscriptPath: input.TranscriptPath,
			Status:         model.StatusInProgress,
			CreatedAt:      time.Now(),
			LastSeen:       time.Now(),
			Checklist:      make(map[string]bool),
			RepoRoot:       repoRoot,
		}

		// Auto-detect phase (fast mode for hook performance)
		phaseScanner := roadmap.NewScanner()
		phaseScanner.RefreshPhase(&ctx, roadmap.ScanModeFast)

		store.Add(ctx)

		// Record session_start event
		recordEvent(s, name, model.MilestoneSessionStart, "")
		if err := s.SaveStore(store); err != nil {
			return err
		}

		fmt.Printf("Registered new context [%s]\n", name)
		fmt.Printf("  Worktree: %s\n", cwd)
		fmt.Printf("  Branch: %s\n", branch)
		if input.SessionID != "" {
			fmt.Printf("  Session: %s\n", input.SessionID[:min(8, len(input.SessionID))])
		}

		return nil
	},
}

func getGitBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getWorktreeRoot(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func generateName(branch, dir string) string {
	if branch != "" && branch != "main" && branch != "master" {
		// Use last part of branch name
		parts := strings.Split(branch, "/")
		name := parts[len(parts)-1]
		// Shorten if too long
		if len(name) > 20 {
			name = name[:20]
		}
		return name
	}
	// Use directory name
	return filepath.Base(dir)
}

func detectRepoRoot(dir string) string {
	// For worktrees, find the main repository root
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	commonDir := strings.TrimSpace(string(out))
	// commonDir is like /path/to/repo/.git - get parent
	if strings.HasSuffix(commonDir, "/.git") {
		return strings.TrimSuffix(commonDir, "/.git")
	}
	// Fallback: use toplevel
	return getWorktreeRoot(dir)
}

func collectAndSaveMilestones(s *storage.Storage, ctx *model.Context) {
	events, err := s.LoadEvents()
	if err != nil {
		return
	}
	collector := roadmap.NewMilestoneCollector()
	newEvents := collector.CollectGitMilestones(ctx, events)
	for _, e := range newEvents {
		events.Append(e)
	}
	if len(newEvents) > 0 {
		_ = s.SaveEvents(events)
	}
}

func recordEvent(s *storage.Storage, sessionName string, mtype model.MilestoneType, detail string) {
	now := time.Now()
	_ = s.AppendEvent(model.SessionEvent{
		SessionName: sessionName,
		Type:        mtype,
		Detail:      detail,
		OccurredAt:  now,
		ObservedAt:  now,
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
