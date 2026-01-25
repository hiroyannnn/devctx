package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var (
	cleanDays    int
	cleanDone    bool
	cleanDryRun  bool
	cleanForce   bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean up old or completed contexts",
	Long: `Remove old contexts that haven't been accessed recently.

By default, removes contexts older than 30 days that are marked as done.
Use --days to change the age threshold.
Use --all to include non-done contexts.
Use --dry-run to preview what would be deleted.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		cutoff := time.Now().AddDate(0, 0, -cleanDays)
		var toRemove []model.Context

		for _, ctx := range store.Contexts {
			shouldRemove := false

			if cleanDone {
				// Only remove done contexts older than cutoff
				if ctx.Status == model.StatusDone && ctx.LastSeen.Before(cutoff) {
					shouldRemove = true
				}
			} else {
				// Remove any context older than cutoff
				if ctx.LastSeen.Before(cutoff) {
					shouldRemove = true
				}
			}

			if shouldRemove {
				toRemove = append(toRemove, ctx)
			}
		}

		if len(toRemove) == 0 {
			fmt.Println("No contexts to clean up.")
			return nil
		}

		// Display what will be removed
		fmt.Printf("Found %d context(s) to remove:\n\n", len(toRemove))
		for _, ctx := range toRemove {
			status := string(ctx.Status)
			age := formatRelativeTime(ctx.LastSeen)
			fmt.Printf("  [%s] %s (%s, %s)\n", ctx.Name, ctx.Branch, status, age)
		}
		fmt.Println()

		if cleanDryRun {
			fmt.Println("(dry-run mode - no changes made)")
			return nil
		}

		// Confirm unless --force
		if !cleanForce {
			fmt.Print("Remove these contexts? [y/N]: ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		// Remove contexts
		removed := 0
		for _, ctx := range toRemove {
			if store.Remove(ctx.Name) {
				removed++
			}
		}

		if err := s.SaveStore(store); err != nil {
			return err
		}

		fmt.Printf("✓ Removed %d context(s)\n", removed)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)
	cleanCmd.Flags().IntVar(&cleanDays, "days", 30, "Remove contexts older than N days")
	cleanCmd.Flags().BoolVar(&cleanDone, "done", true, "Only remove done contexts")
	cleanCmd.Flags().BoolVar(&cleanDryRun, "dry-run", false, "Preview what would be removed")
	cleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false, "Skip confirmation")
}
