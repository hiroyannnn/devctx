package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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
		server := roadmap.NewServer(s, scanner, roadmapServePort)
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

func init() {
	rootCmd.AddCommand(roadmapCmd)
	roadmapCmd.AddCommand(roadmapInitCmd)
	roadmapCmd.AddCommand(roadmapScanCmd)
	roadmapCmd.AddCommand(roadmapStatusCmd)
	roadmapCmd.AddCommand(roadmapServeCmd)

	roadmapInitCmd.Flags().StringVar(&roadmapInitPrompt, "prompt", "", "Initial prompt for the session")
	roadmapInitCmd.Flags().StringVar(&roadmapInitWorktree, "worktree", "", "Worktree path (defaults to current directory)")

	roadmapServeCmd.Flags().IntVar(&roadmapServePort, "port", 3333, "Port for the web dashboard")
}
