package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var commandsCmd = &cobra.Command{
	Use:   "commands",
	Short: "Setup Claude Code custom commands for devctx",
	Long: `Generate Claude Code custom commands for devctx integration.

This creates slash commands that can be used within Claude Code:
  /devctx-review  - Move current context to review
  /devctx-done    - Mark current context as done
  /devctx-blocked - Mark current context as blocked
  /devctx-note    - Add a note to current context`,
	RunE: func(cmd *cobra.Command, args []string) error {
		installCommands, _ := cmd.Flags().GetBool("install")

		commands := map[string]string{
			"devctx-review.md": `このコンテキストをレビュー状態に移動します。

以下のコマンドを実行してください：
` + "```bash\n" + `devctx move $(basename $(pwd)) review
` + "```",

			"devctx-done.md": `このコンテキストを完了としてマークします。

以下のコマンドを実行してください：
` + "```bash\n" + `devctx archive $(basename $(pwd))
` + "```",

			"devctx-blocked.md": `このコンテキストをブロック状態に移動します。

以下のコマンドを実行してください：
` + "```bash\n" + `devctx move $(basename $(pwd)) blocked
` + "```",

			"devctx-note.md": `このコンテキストにメモを追加します。

$ARGUMENTS をメモとして記録します。

以下のコマンドを実行してください：
` + "```bash\n" + `devctx note $(basename $(pwd)) "$ARGUMENTS"
` + "```",

			"devctx-link.md": `このコンテキストに GitHub Issue または PR をリンクします。

$ARGUMENTS を Issue/PR URL として記録します。

以下のコマンドを実行してください：
` + "```bash\n" + `devctx link $(basename $(pwd)) "$ARGUMENTS"
` + "```",

			"devctx-status.md": `現在のコンテキストの状態を表示します。

以下のコマンドを実行してください：
` + "```bash\n" + `devctx show $(basename $(pwd))
` + "```",
		}

		if !installCommands {
			fmt.Println("Claude Code custom commands for devctx:")
			fmt.Println()
			for name, content := range commands {
				fmt.Printf("=== %s ===\n", name)
				fmt.Println(content)
				fmt.Println()
			}
			fmt.Println("Run 'devctx commands --install' to install to ~/.claude/commands/")
			return nil
		}

		// Install commands
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		commandsDir := filepath.Join(home, ".claude", "commands")
		if err := os.MkdirAll(commandsDir, 0755); err != nil {
			return err
		}

		for name, content := range commands {
			path := filepath.Join(commandsDir, name)
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", name, err)
			}
			fmt.Printf("✓ Created %s\n", path)
		}

		fmt.Println()
		fmt.Println("Custom commands installed! Available in Claude Code:")
		fmt.Println("  /devctx-review  - Move to review status")
		fmt.Println("  /devctx-done    - Mark as done")
		fmt.Println("  /devctx-blocked - Mark as blocked")
		fmt.Println("  /devctx-note    - Add a note")
		fmt.Println("  /devctx-link    - Link Issue/PR")
		fmt.Println("  /devctx-status  - Show context status")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(commandsCmd)
	commandsCmd.Flags().Bool("install", false, "Install commands to ~/.claude/commands/")
}
