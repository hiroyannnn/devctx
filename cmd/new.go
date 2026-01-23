package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	newBase   string
	newNoCd   bool
	newNoClaude bool
)

var newCmd = &cobra.Command{
	Use:   "new <branch-name>",
	Short: "Create a new worktree and start Claude session",
	Long: `Create a new git worktree for the given branch and optionally start a Claude session.

Examples:
  devctx new feature/auth           # Creates worktree at ../worktrees/auth
  devctx new feature/auth --base main  # Branch from main
  devctx new fix/bug-123            # Creates worktree at ../worktrees/bug-123

The worktree is created in a 'worktrees' directory relative to the main repo.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		branchName := args[0]

		// Get current repo root
		repoRoot, err := getRepoRoot()
		if err != nil {
			return fmt.Errorf("not in a git repository: %w", err)
		}

		// Determine worktree name (last part of branch)
		parts := strings.Split(branchName, "/")
		worktreeName := parts[len(parts)-1]

		// Determine worktree path
		worktreesDir := filepath.Join(filepath.Dir(repoRoot), "worktrees")
		worktreePath := filepath.Join(worktreesDir, worktreeName)

		// Check if worktree already exists
		if _, err := os.Stat(worktreePath); err == nil {
			return fmt.Errorf("worktree already exists: %s", worktreePath)
		}

		// Ensure worktrees directory exists
		if err := os.MkdirAll(worktreesDir, 0755); err != nil {
			return fmt.Errorf("failed to create worktrees directory: %w", err)
		}

		// Create the worktree
		fmt.Printf("Creating worktree for branch '%s'...\n", branchName)

		var gitArgs []string
		if newBase != "" {
			// Create new branch from base
			gitArgs = []string{"worktree", "add", "-b", branchName, worktreePath, newBase}
		} else {
			// Check if branch exists
			checkCmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
			checkCmd.Dir = repoRoot
			if checkCmd.Run() == nil {
				// Branch exists, just add worktree
				gitArgs = []string{"worktree", "add", worktreePath, branchName}
			} else {
				// Branch doesn't exist, create from current HEAD
				gitArgs = []string{"worktree", "add", "-b", branchName, worktreePath}
			}
		}

		gitCmd := exec.Command("git", gitArgs...)
		gitCmd.Dir = repoRoot
		gitCmd.Stdout = os.Stdout
		gitCmd.Stderr = os.Stderr
		if err := gitCmd.Run(); err != nil {
			return fmt.Errorf("failed to create worktree: %w", err)
		}

		fmt.Printf("✓ Created worktree at %s\n", worktreePath)

		// Output commands for shell integration
		if newNoCd && newNoClaude {
			return nil
		}

		fmt.Println()
		fmt.Println("# Run the following commands:")
		fmt.Printf("cd %s\n", worktreePath)
		if !newNoClaude {
			fmt.Println("claude")
		}

		return nil
	},
}

// newShellCmd outputs shell commands that can be eval'd
var newShellCmd = &cobra.Command{
	Use:    "new-shell <branch-name>",
	Short:  "Output shell commands to create worktree and start Claude (use with eval)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		branchName := args[0]

		// Get current repo root
		repoRoot, err := getRepoRoot()
		if err != nil {
			return fmt.Errorf("not in a git repository: %w", err)
		}

		// Determine worktree name and path
		parts := strings.Split(branchName, "/")
		worktreeName := parts[len(parts)-1]
		worktreesDir := filepath.Join(filepath.Dir(repoRoot), "worktrees")
		worktreePath := filepath.Join(worktreesDir, worktreeName)

		// Check if already exists
		if _, err := os.Stat(worktreePath); err == nil {
			// Already exists, just cd and claude
			fmt.Printf("cd '%s' && claude", worktreePath)
			return nil
		}

		// Create worktrees dir if needed
		if err := os.MkdirAll(worktreesDir, 0755); err != nil {
			return err
		}

		// Determine git command
		var gitArgs string
		if newBase != "" {
			gitArgs = fmt.Sprintf("git worktree add -b '%s' '%s' '%s'", branchName, worktreePath, newBase)
		} else {
			checkCmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branchName)
			checkCmd.Dir = repoRoot
			if checkCmd.Run() == nil {
				gitArgs = fmt.Sprintf("git worktree add '%s' '%s'", worktreePath, branchName)
			} else {
				gitArgs = fmt.Sprintf("git worktree add -b '%s' '%s'", branchName, worktreePath)
			}
		}

		fmt.Printf("%s && cd '%s' && claude", gitArgs, worktreePath)
		return nil
	},
}

func getRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func init() {
	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(newShellCmd)

	newCmd.Flags().StringVar(&newBase, "base", "", "Base branch to create from (e.g., main)")
	newCmd.Flags().BoolVar(&newNoCd, "no-cd", false, "Don't output cd command")
	newCmd.Flags().BoolVar(&newNoClaude, "no-claude", false, "Don't output claude command")

	newShellCmd.Flags().StringVar(&newBase, "base", "", "Base branch to create from")
}
