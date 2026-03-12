package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/roadmap"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var roadmapCmd = &cobra.Command{
	Use:   "roadmap",
	Short: "Session roadmap - track development lifecycle phases",
	Long: `View and manage the development lifecycle of your sessions.
Automatically detects git-based phases: idle, implementation, committed, pushed, pr_open, done.

Use 'devctx roadmap scan' to see all sessions with their current phase.
Use 'devctx roadmap serve' to start a web dashboard.`,
}

// --- roadmap init ---

var roadmapInitPrompt string
var roadmapInitWorktree string

var roadmapInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Set the initial prompt for a session",
	Long: `Store the initial prompt/goal for a session's development context.

The prompt is displayed in the roadmap dashboard to help you remember
what each session is working on.

Examples:
  devctx roadmap init --prompt "認証機能を実装して"
  devctx roadmap init --prompt "APIの500エラーを修正" --worktree ./wt-fix-api`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if roadmapInitPrompt == "" {
			return fmt.Errorf("--prompt is required")
		}

		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		worktree := roadmapInitWorktree
		if worktree == "" {
			worktree, _ = os.Getwd()
		}

		// Resolve to absolute path
		worktree, err = filepath.Abs(worktree)
		if err != nil {
			return fmt.Errorf("failed to resolve worktree path: %w", err)
		}

		// Try to get git worktree root
		root := getWorktreeRoot(worktree)
		if root != "" {
			worktree = root
		}

		ctx := store.FindByWorktree(worktree)
		if ctx == nil {
			// Try finding by name from args
			if len(args) > 0 {
				ctx = store.FindByName(args[0])
			}
		}

		if ctx == nil {
			return fmt.Errorf("no context found for worktree %s\nRegister a context first with 'devctx register'", worktree)
		}

		ctx.InitialPrompt = roadmapInitPrompt
		if err := s.SaveStore(store); err != nil {
			return err
		}

		fmt.Printf("Set initial prompt for [%s]\n", ctx.Name)
		fmt.Printf("  %s\n", roadmapInitPrompt)
		return nil
	},
}

// --- roadmap scan ---

var roadmapScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan all sessions and show their git-based phases",
	Long: `Scan all registered contexts and determine their current development phase
based on git state (uncommitted changes, commits, pushes, PRs).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		contexts := store.Active()
		if len(contexts) == 0 {
			fmt.Println("No active contexts.")
			return nil
		}

		scanner := roadmap.NewScanner()
		phases := scanner.ScanAll(contexts)

		nameStyle := lipgloss.NewStyle().Bold(true)
		phaseColors := map[model.Phase]lipgloss.Style{
			model.PhaseIdle:           lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
			model.PhaseImplementation: lipgloss.NewStyle().Foreground(lipgloss.Color("11")),
			model.PhaseCommitted:      lipgloss.NewStyle().Foreground(lipgloss.Color("13")),
			model.PhasePushed:         lipgloss.NewStyle().Foreground(lipgloss.Color("14")),
			model.PhasePROpen:         lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
			model.PhaseDone:           lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		}

		for _, ctx := range contexts {
			phase := phases[ctx.Name]
			style, ok := phaseColors[phase]
			if !ok {
				style = lipgloss.NewStyle()
			}

			name := nameStyle.Render(fmt.Sprintf("[%s]", ctx.Name))
			phaseStr := style.Render(phase.Label())
			fmt.Printf("%s %s %s\n", name, ctx.Branch, phaseStr)
		}

		return nil
	},
}

// --- roadmap status ---

var roadmapStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show roadmap status with phase progress",
	Long:  `Show all sessions with a visual progress indicator through development phases.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		contexts := store.Active()
		if len(contexts) == 0 {
			fmt.Println("No active contexts.")
			return nil
		}

		scanner := roadmap.NewScanner()
		phases := scanner.ScanAll(contexts)
		allPhases := model.AllPhases()

		nameStyle := lipgloss.NewStyle().Bold(true)
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
		doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		branchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

		for _, ctx := range contexts {
			phase := phases[ctx.Name]
			currentIdx := phaseIndex(phase)

			fmt.Println(nameStyle.Render(fmt.Sprintf("[%s]", ctx.Name)))
			fmt.Printf("  %s %s\n", branchStyle.Render("branch:"), ctx.Branch)
			if ctx.InitialPrompt != "" {
				prompt := ctx.InitialPrompt
				if len(prompt) > 60 {
					prompt = prompt[:57] + "..."
				}
				fmt.Printf("  %s %s\n", branchStyle.Render("prompt:"), prompt)
			}
			fmt.Print("  ")

			for i, p := range allPhases {
				var icon, label string
				if i < currentIdx {
					icon = doneStyle.Render("●")
					label = doneStyle.Render(p.Label())
				} else if i == currentIdx {
					icon = activeStyle.Render("●")
					label = activeStyle.Render(p.Label())
				} else {
					icon = dimStyle.Render("○")
					label = dimStyle.Render(p.Label())
				}

				indicator := ""
				if i == currentIdx {
					indicator = " ←"
				}
				fmt.Printf("\n    %s %s%s", icon, label, activeStyle.Render(indicator))
			}
			fmt.Println()
			fmt.Println()
		}

		return nil
	},
}

// --- roadmap serve ---

var roadmapServePort int

var roadmapServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start web dashboard for session roadmap",
	Long: `Start a local web server that displays all sessions with their
development phases in a visual dashboard.

The dashboard auto-refreshes every 5 seconds.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}

		scanner := roadmap.NewScanner()
		server := roadmap.NewServer(s, s, s, scanner, roadmapServePort)
		return server.ListenAndServe()
	},
}

func phaseIndex(phase model.Phase) int {
	for i, p := range model.AllPhases() {
		if p == phase {
			return i
		}
	}
	return 0
}

// --- roadmap analyze ---

var (
	roadmapAnalyzeAll        bool
	roadmapAnalyzeIfStale    bool
	roadmapAnalyzeBackground bool
	roadmapAnalyzeCooldown   int
)

const defaultAnalyzeCooldownMin = 5

var roadmapAnalyzeCmd = &cobra.Command{
	Use:   "analyze [name]",
	Short: "Analyze sessions using Claude to infer goal, focus, and next steps",
	Long: `Read the session transcript and use Claude CLI to generate insights:
goal, current focus, next step, and attention state.

Without arguments, analyzes the context matching the current directory.
With --all, analyzes all active contexts.
With --if-stale, skips analysis if last insight is fresh (default: 5 min cooldown).
With --background, forks to background and returns immediately.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Background mode: re-exec self as detached process
		if roadmapAnalyzeBackground {
			return execAnalyzeBackground(args)
		}

		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}
		insights, err := s.LoadInsights()
		if err != nil {
			return err
		}

		var targets []*model.Context

		if roadmapAnalyzeAll {
			for i := range store.Contexts {
				if store.Contexts[i].Status != model.StatusDone {
					targets = append(targets, &store.Contexts[i])
				}
			}
		} else if len(args) > 0 {
			ctx := store.FindByName(args[0])
			if ctx == nil {
				return fmt.Errorf("context [%s] not found", args[0])
			}
			targets = append(targets, ctx)
		} else {
			cwd, _ := os.Getwd()
			worktreeRoot := getWorktreeRoot(cwd)
			if worktreeRoot != "" {
				cwd = worktreeRoot
			}
			ctx := store.FindByWorktree(cwd)
			if ctx == nil {
				return fmt.Errorf("no context found for current directory\nSpecify a name or use --all")
			}
			targets = append(targets, ctx)
		}

		if len(targets) == 0 {
			fmt.Println("No active contexts to analyze.")
			return nil
		}

		cooldown := time.Duration(roadmapAnalyzeCooldown) * time.Minute

		for _, ctx := range targets {
			fmt.Printf("Analyzing [%s]...\n", ctx.Name)

			if ctx.TranscriptPath == "" {
				fmt.Printf("  Skipped: no transcript path\n")
				continue
			}

			// Staleness check
			if roadmapAnalyzeIfStale {
				if existing := insights.Get(ctx.Name); existing != nil {
					if time.Since(existing.InferredAt) < cooldown {
						fmt.Printf("  Skipped: fresh (analyzed %s ago)\n", time.Since(existing.InferredAt).Truncate(time.Second))
						continue
					}
				}
			}

			// Read transcript
			data, err := os.ReadFile(ctx.TranscriptPath)
			if err != nil {
				fmt.Printf("  Skipped: cannot read transcript: %v\n", err)
				continue
			}

			// Get previous offset for incremental processing
			existing := insights.Get(ctx.Name)
			var prevOffset int64
			if existing != nil {
				prevOffset = existing.TranscriptOffset
			}

			transcript, newOffset := roadmap.ReadTranscriptTail(string(data), 50, prevOffset)
			if transcript == "" {
				fmt.Printf("  Skipped: no new transcript content\n")
				continue
			}

			// Build prompt and call Claude CLI
			prompt := roadmap.BuildAnalyzePrompt(ctx, transcript)
			response, err := runClaude(prompt)
			if err != nil {
				fmt.Printf("  Error: %v\n", err)
				continue
			}

			// Parse response
			insight, err := roadmap.ParseAnalyzeResponse(ctx.Name, response)
			if err != nil {
				fmt.Printf("  Error parsing response: %v\n", err)
				continue
			}
			insight.TranscriptOffset = newOffset

			// Merge mechanical extraction with LLM results
			extractor := roadmap.NewExtractor()
			bundle := extractor.CollectEvidence(ctx)
			gitTopics := roadmap.ExtractTopics(bundle)
			gitTasks := roadmap.ExtractTasks(bundle)
			insight.Topics = mergeTopics(insight.Topics, gitTopics)
			insight.Tasks = mergeTasks(insight.Tasks, gitTasks)

			insights.Set(*insight)

			fmt.Printf("  Goal: %s\n", insight.Goal)
			fmt.Printf("  Focus: %s\n", insight.CurrentFocus)
			fmt.Printf("  Next: %s\n", insight.NextStep)
			fmt.Printf("  State: %s\n", insight.AttentionState)
			fmt.Printf("  Topics: %d, Tasks: %d\n", len(insight.Topics), len(insight.Tasks))
		}

		if err := s.SaveInsights(insights); err != nil {
			return err
		}

		return nil
	},
}

func execAnalyzeBackground(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find executable: %w", err)
	}

	cmdArgs := []string{"roadmap", "analyze", "--if-stale"}
	if roadmapAnalyzeAll {
		cmdArgs = append(cmdArgs, "--all")
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(exe, cmdArgs...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	// Detach from parent process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start background analyze: %w", err)
	}
	// Don't wait - let it run in background
	go cmd.Wait()
	return nil
}

func runClaude(prompt string) (string, error) {
	cmd := exec.Command("claude", "--print", "--model", "claude-sonnet-4-20250514", "-p", "-")
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude CLI failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// --- roadmap refresh ---

var roadmapRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refresh phases for all active contexts (full scan with gh)",
	Long: `Re-scan all active contexts using both git and gh CLI to update
their development phase. This is useful when you want accurate PR-based
phase detection for all sessions at once.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		contexts := store.Active()
		if len(contexts) == 0 {
			fmt.Println("No active contexts.")
			return nil
		}

		scanner := roadmap.NewScanner()
		updated := 0
		for i := range store.Contexts {
			ctx := &store.Contexts[i]
			if ctx.Status == model.StatusDone {
				continue
			}
			oldPhase := ctx.Phase
			scanner.RefreshPhase(ctx, roadmap.ScanModeFull)
			if ctx.Phase != oldPhase {
				updated++
				fmt.Printf("  [%s] %s → %s\n", ctx.Name, oldPhase.Label(), ctx.Phase.Label())
			} else {
				fmt.Printf("  [%s] %s (unchanged)\n", ctx.Name, ctx.Phase.Label())
			}
		}

		if err := s.SaveStore(store); err != nil {
			return err
		}

		fmt.Printf("\nRefreshed %d contexts (%d changed)\n", len(contexts), updated)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(roadmapCmd)
	roadmapCmd.AddCommand(roadmapInitCmd)
	roadmapCmd.AddCommand(roadmapScanCmd)
	roadmapCmd.AddCommand(roadmapStatusCmd)
	roadmapCmd.AddCommand(roadmapServeCmd)
	roadmapCmd.AddCommand(roadmapRefreshCmd)
	roadmapCmd.AddCommand(roadmapAnalyzeCmd)

	roadmapAnalyzeCmd.Flags().BoolVar(&roadmapAnalyzeAll, "all", false, "Analyze all active contexts")
	roadmapAnalyzeCmd.Flags().BoolVar(&roadmapAnalyzeIfStale, "if-stale", false, "Skip if insight is fresh")
	roadmapAnalyzeCmd.Flags().BoolVar(&roadmapAnalyzeBackground, "background", false, "Fork to background and return immediately")
	roadmapAnalyzeCmd.Flags().IntVar(&roadmapAnalyzeCooldown, "cooldown", defaultAnalyzeCooldownMin, "Cooldown minutes for --if-stale")

	roadmapInitCmd.Flags().StringVar(&roadmapInitPrompt, "prompt", "", "Initial prompt for the session")
	roadmapInitCmd.Flags().StringVar(&roadmapInitWorktree, "worktree", "", "Worktree path (defaults to current directory)")

	roadmapServeCmd.Flags().IntVar(&roadmapServePort, "port", 3333, "Port for the web dashboard")
}

// mergeTopics combines LLM-inferred topics with git-extracted topics, avoiding duplicates.
func mergeTopics(llmTopics, gitTopics []model.SemanticTopic) []model.SemanticTopic {
	seen := make(map[string]bool)
	var result []model.SemanticTopic

	// LLM topics take priority
	for _, t := range llmTopics {
		key := strings.ToLower(t.Name)
		if !seen[key] {
			seen[key] = true
			result = append(result, t)
		}
	}
	// Add git topics if not already present
	for _, t := range gitTopics {
		key := strings.ToLower(t.Name)
		if !seen[key] {
			seen[key] = true
			result = append(result, t)
		}
	}
	return result
}

// mergeTasks combines LLM-inferred tasks with git-extracted tasks, avoiding duplicates.
func mergeTasks(llmTasks, gitTasks []model.TaskItem) []model.TaskItem {
	seen := make(map[string]bool)
	var result []model.TaskItem

	// LLM tasks take priority
	for _, t := range llmTasks {
		key := strings.ToLower(t.Title)
		if !seen[key] {
			seen[key] = true
			result = append(result, t)
		}
	}
	// Add git tasks if not already present
	for _, t := range gitTasks {
		key := strings.ToLower(t.Title)
		if !seen[key] {
			seen[key] = true
			result = append(result, t)
		}
	}
	return result
}
