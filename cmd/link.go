package cmd

import (
	"fmt"
	"strings"

	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link <name> <url>",
	Short: "Link a GitHub Issue or PR to a context",
	Long: `Link a GitHub Issue or PR URL to a context.

Examples:
  devctx link auth https://github.com/user/repo/issues/123
  devctx link auth https://github.com/user/repo/pull/456
  devctx link auth --clear  # Clear links`,
	Args: cobra.MinimumNArgs(1),
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

		clearLinks, _ := cmd.Flags().GetBool("clear")

		if clearLinks {
			ctx.IssueURL = ""
			ctx.PRURL = ""
			if err := s.SaveStore(store); err != nil {
				return err
			}
			fmt.Printf("✓ Cleared links for [%s]\n", name)
			return nil
		}

		if len(args) < 2 {
			// Show current links
			if ctx.IssueURL == "" && ctx.PRURL == "" {
				fmt.Printf("[%s] has no linked Issue/PR\n", name)
			} else {
				if ctx.IssueURL != "" {
					fmt.Printf("[%s] Issue: %s\n", name, ctx.IssueURL)
				}
				if ctx.PRURL != "" {
					fmt.Printf("[%s] PR: %s\n", name, ctx.PRURL)
				}
			}
			return nil
		}

		url := args[1]

		// Detect if it's an issue or PR
		if strings.Contains(url, "/pull/") || strings.Contains(url, "/pulls/") {
			ctx.PRURL = url
			fmt.Printf("✓ Linked PR to [%s]: %s\n", name, url)
		} else if strings.Contains(url, "/issues/") || strings.Contains(url, "/issue/") {
			ctx.IssueURL = url
			fmt.Printf("✓ Linked Issue to [%s]: %s\n", name, url)
		} else {
			// Default to issue
			ctx.IssueURL = url
			fmt.Printf("✓ Linked to [%s]: %s\n", name, url)
		}

		if err := s.SaveStore(store); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(linkCmd)
	linkCmd.Flags().Bool("clear", false, "Clear all links")
}
