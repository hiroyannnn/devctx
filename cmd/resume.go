package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume <name>",
	Short: "Change to worktree directory and resume Claude session",
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

		// Check if worktree exists
		if _, err := os.Stat(ctx.Worktree); os.IsNotExist(err) {
			return fmt.Errorf("worktree directory does not exist: %s", ctx.Worktree)
		}

		agentCmd, err := agentResumeCommand(*ctx)
		if err != nil {
			return err
		}

		// Print instructions for shell integration
		// The actual cd must happen in the shell, so we output commands
		fmt.Printf("# Run the following commands:\n")
		fmt.Printf("cd %s\n", ctx.Worktree)
		fmt.Println(agentCmd)

		return nil
	},
}

// ResumeShell can be used with eval $(devctx resume-shell <name>)
var resumeShellCmd = &cobra.Command{
	Use:    "resume-shell <name>",
	Short:  "Output shell commands to resume a context (use with eval)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
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

		// Output commands that can be eval'd
		shellCmd, err := resumeShellCommand(*ctx)
		if err != nil {
			return err
		}
		fmt.Println(shellCmd)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(resumeShellCmd)
}

// LaunchInNewTerminal launches claude in a new terminal window/tab
func LaunchInNewTerminal(worktree, sessionID string) error {
	// For Ghostty, we can use ghostty CLI or just print instructions
	// This is OS/terminal specific, so we'll just exec directly for now
	claudeArgs := []string{}
	if sessionID != "" {
		claudeArgs = append(claudeArgs, "--resume", sessionID)
	}

	cmd := exec.Command("claude", claudeArgs...)
	cmd.Dir = worktree
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// agentResumeCommand は context のエージェントを再開するコマンドを返す。
// 再開方法が未実装の provider はエラーにする（誤って claude --resume を出さないため）。
func agentResumeCommand(ctx model.Context) (string, error) {
	switch p := ctx.EffectiveProvider(); p {
	case model.ProviderClaude:
		if ctx.SessionID != "" {
			return fmt.Sprintf("claude --resume '%s'", ctx.SessionID), nil
		}
		return "claude", nil
	default:
		return "", fmt.Errorf("resume is not supported for provider %q yet", p)
	}
}

// resumeShellCommand は worktree へ移動してエージェントを再開する shell コマンドを返す。
func resumeShellCommand(ctx model.Context) (string, error) {
	agentCmd, err := agentResumeCommand(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("cd '%s' && %s", ctx.Worktree, agentCmd), nil
}
