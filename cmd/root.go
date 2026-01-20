package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "devctx",
	Short: "Manage Claude Code sessions and git worktrees",
	Long: `devctx helps you manage the relationship between Claude Code sessions,
git worktrees, and development context with a kanban-style interface.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(registerCmd)
	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(moveCmd)
	rootCmd.AddCommand(touchCmd)
	rootCmd.AddCommand(archiveCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(hooksCmd)
}
