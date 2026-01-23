package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

type ghPR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

type ghIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

var syncCmd = &cobra.Command{
	Use:   "sync [name]",
	Short: "Sync GitHub Issue/PR information for a context",
	Long: `Automatically detect and link GitHub PR for the current branch.

If no name is provided, uses the current directory to find the context.
Uses 'gh' CLI to fetch PR information.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		var name string
		if len(args) > 0 {
			name = args[0]
		} else {
			// Find by current directory
			cwd := getWorktreeRoot(".")
			if cwd == "" {
				return fmt.Errorf("not in a git repository")
			}
			ctx := store.FindByWorktree(cwd)
			if ctx == nil {
				return fmt.Errorf("no context found for current directory")
			}
			name = ctx.Name
		}

		ctx := store.FindByName(name)
		if ctx == nil {
			return fmt.Errorf("context [%s] not found", name)
		}

		// Check if gh is available
		if _, err := exec.LookPath("gh"); err != nil {
			return fmt.Errorf("'gh' CLI not found. Install from https://cli.github.com/")
		}

		// Get PR for current branch
		fmt.Printf("Syncing GitHub info for [%s] (branch: %s)...\n", name, ctx.Branch)

		pr, err := getPRForBranch(ctx.Branch, ctx.Worktree)
		if err == nil && pr != nil {
			ctx.PRURL = pr.URL
			fmt.Printf("✓ Found PR #%d: %s\n", pr.Number, pr.Title)
			fmt.Printf("  %s\n", pr.URL)
		} else {
			fmt.Println("  No PR found for this branch")
		}

		// Try to extract issue number from branch name
		issueNum := extractIssueNumber(ctx.Branch)
		if issueNum != "" {
			issue, err := getIssue(issueNum, ctx.Worktree)
			if err == nil && issue != nil {
				ctx.IssueURL = issue.URL
				fmt.Printf("✓ Linked Issue #%s: %s\n", issueNum, issue.Title)
				fmt.Printf("  %s\n", issue.URL)
			}
		}

		if err := s.SaveStore(store); err != nil {
			return err
		}

		return nil
	},
}

func getPRForBranch(branch, worktree string) (*ghPR, error) {
	cmd := exec.Command("gh", "pr", "view", branch, "--json", "number,title,url,state")
	cmd.Dir = worktree
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var pr ghPR
	if err := json.Unmarshal(out, &pr); err != nil {
		return nil, err
	}

	return &pr, nil
}

func getIssue(number, worktree string) (*ghIssue, error) {
	cmd := exec.Command("gh", "issue", "view", number, "--json", "number,title,url,state")
	cmd.Dir = worktree
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var issue ghIssue
	if err := json.Unmarshal(out, &issue); err != nil {
		return nil, err
	}

	return &issue, nil
}

// extractIssueNumber tries to find an issue number in the branch name
// Patterns: feature/123-description, fix/issue-123, #123, etc.
func extractIssueNumber(branch string) string {
	patterns := []string{
		`#(\d+)`,           // #123
		`issue[/-](\d+)`,   // issue-123, issue/123
		`(\d+)[/-]`,        // 123-description
		`[/-](\d+)$`,       // feature/description-123
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(branch)
		if len(matches) > 1 {
			return matches[1]
		}
	}

	return ""
}

var prCmd = &cobra.Command{
	Use:   "pr <name>",
	Short: "Create a PR for the context's branch",
	Long: `Create a GitHub Pull Request for the context's branch.

Uses 'gh pr create' with information from the context.`,
	Args: cobra.ExactArgs(1),
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

		// Check if gh is available
		if _, err := exec.LookPath("gh"); err != nil {
			return fmt.Errorf("'gh' CLI not found")
		}

		// Build title from branch name
		title := buildPRTitle(ctx.Branch)
		if ctx.Note != "" {
			title = ctx.Note
		}

		// Create PR
		ghArgs := []string{"pr", "create", "--title", title, "--body", ""}
		if ctx.IssueURL != "" {
			// Extract issue number and add to body
			if num := extractIssueFromURL(ctx.IssueURL); num != "" {
				ghArgs = append(ghArgs, "--body", fmt.Sprintf("Closes #%s", num))
			}
		}

		fmt.Printf("Creating PR for branch '%s'...\n", ctx.Branch)
		ghCmd := exec.Command("gh", ghArgs...)
		ghCmd.Dir = ctx.Worktree
		out, err := ghCmd.Output()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("gh pr create failed: %s", string(exitErr.Stderr))
			}
			return err
		}

		prURL := strings.TrimSpace(string(out))
		ctx.PRURL = prURL
		if err := s.SaveStore(store); err != nil {
			return err
		}

		fmt.Printf("✓ Created PR: %s\n", prURL)
		return nil
	},
}

func buildPRTitle(branch string) string {
	// Remove prefix like feature/, fix/, etc.
	parts := strings.Split(branch, "/")
	name := parts[len(parts)-1]

	// Replace hyphens with spaces and capitalize
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")

	// Remove issue numbers
	re := regexp.MustCompile(`^\d+\s*`)
	name = re.ReplaceAllString(name, "")

	// Capitalize first letter
	if len(name) > 0 {
		name = strings.ToUpper(string(name[0])) + name[1:]
	}

	return name
}

func extractIssueFromURL(url string) string {
	re := regexp.MustCompile(`/issues/(\d+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

func init() {
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(prCmd)
}
