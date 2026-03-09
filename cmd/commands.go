package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const insightCommandContent = `現在のセッションのインサイトを devctx に保存します。

以下の手順で実行してください：

1. 現在のセッションの状態を分析し、以下の4つを簡潔に（各1行で）まとめる：
   - **goal**: このセッションが達成しようとしていること
   - **focus**: 今取り組んでいるサブタスク
   - **next**: 次にやるべきこと
   - **state**: active（作業進行中）/ waiting（入力待ち）/ idle（一段落）/ blocked（詰まっている）

2. 以下のコマンドを実行する（各値は分析結果に置き換える）：

` + "```bash\n" + `devctx insight --goal "セッションの目標" --focus "今やっていること" --next "次にやること" --state "active"
` + "```" + `

重要：
- 各フィールドは日本語で簡潔に記述すること
- state は active/waiting/idle/blocked のいずれか
- ユーザーのメモ（devctx note）がある場合はそれも考慮する
`

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

			"devctx-insight.md": insightCommandContent,
		}

		// Rule file for auto-execution after plan
		ruleContent := `# devctx insight 自動保存ルール

実装計画（plan）を作成・承認した後、または大きな作業の区切りで、
自動的に /devctx-insight を実行してセッションのインサイトを保存してください。

以下のタイミングで実行すること：
- 実装計画を作成した直後
- 大きなタスクが完了した時
- ブロッカーに遭遇した時
- セッションの方向性が変わった時
`

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

		// Install rule file
		rulesDir := filepath.Join(home, ".claude", "rules")
		if err := os.MkdirAll(rulesDir, 0755); err != nil {
			return err
		}
		rulePath := filepath.Join(rulesDir, "devctx-insight-auto.md")
		if err := os.WriteFile(rulePath, []byte(ruleContent), 0644); err != nil {
			return fmt.Errorf("failed to write rule: %w", err)
		}
		fmt.Printf("✓ Created %s\n", rulePath)

		fmt.Println()
		fmt.Println("Custom commands installed! Available in Claude Code:")
		fmt.Println("  /devctx-review  - Move to review status")
		fmt.Println("  /devctx-done    - Mark as done")
		fmt.Println("  /devctx-blocked - Mark as blocked")
		fmt.Println("  /devctx-note    - Add a note")
		fmt.Println("  /devctx-link    - Link Issue/PR")
		fmt.Println("  /devctx-status  - Show context status")
		fmt.Println("  /devctx-insight - Save session insights")
		fmt.Println()
		fmt.Println("Auto-execution rule installed:")
		fmt.Println("  Claude will automatically save insights after creating plans")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(commandsCmd)
	commandsCmd.Flags().Bool("install", false, "Install commands to ~/.claude/commands/")
}
