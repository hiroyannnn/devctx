package cmd

import (
	"fmt"
	"strings"

	"github.com/hiroyannnn/devctx/model"
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
			_, err := updateContext(s, name, func(ctx *model.Context) error {
				ctx.IssueURL = ""
				ctx.PRURL = ""
				return nil
			})
			if err != nil {
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
		isPR := strings.Contains(url, "/pull/") || strings.Contains(url, "/pulls/")
		label := "Linked" // Default to issue
		if isPR {
			label = "Linked PR"
		} else if strings.Contains(url, "/issues/") || strings.Contains(url, "/issue/") {
			label = "Linked Issue"
		}

		_, err = updateContext(s, name, func(ctx *model.Context) error {
			if isPR {
				ctx.PRURL = url
			} else {
				ctx.IssueURL = url
			}
			return nil
		})
		if err != nil {
			return err
		}
		fmt.Printf("✓ %s to [%s]: %s\n", label, name, url)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(linkCmd)
	linkCmd.Flags().Bool("clear", false, "Clear all links")
}
