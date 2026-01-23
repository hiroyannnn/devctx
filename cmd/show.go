package cmd

import (
	"fmt"

	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show details of a context",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		ctx := store.FindByName(name)
		if ctx == nil {
			return fmt.Errorf("context [%s] not found", name)
		}

		fmt.Printf("Context: %s\n", ctx.Name)
		fmt.Printf("Status:  %s %s\n", statusIcon(ctx.Status), ctx.Status)
		fmt.Printf("Branch:  %s\n", ctx.Branch)
		fmt.Printf("Worktree: %s\n", ctx.Worktree)

		if ctx.SessionID != "" {
			fmt.Printf("Session: %s\n", ctx.SessionID)
		}

		fmt.Printf("Created: %s\n", ctx.CreatedAt.Format("2006-01-02 15:04"))
		fmt.Printf("Last seen: %s (%s)\n", ctx.LastSeen.Format("2006-01-02 15:04"), formatRelativeTime(ctx.LastSeen))

		if ctx.TotalTime > 0 {
			fmt.Printf("Total time: %s\n", ctx.TotalTime)
		}

		if ctx.Note != "" {
			fmt.Printf("\nNote: %s\n", ctx.Note)
		}

		if ctx.IssueURL != "" {
			fmt.Printf("Issue: %s\n", ctx.IssueURL)
		}

		if ctx.PRURL != "" {
			fmt.Printf("PR: %s\n", ctx.PRURL)
		}

		if len(ctx.Checklist) > 0 {
			fmt.Println("\nChecklist:")
			for item, done := range ctx.Checklist {
				mark := "☐"
				if done {
					mark = "☑"
				}
				fmt.Printf("  %s %s\n", mark, item)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(showCmd)
}
