package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "devctx",
	Short: "Manage Claude Code sessions and git worktrees",
	Long: `devctx helps you manage the relationship between Claude Code sessions,
git worktrees, and development context with a kanban-style interface.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Skip setup prompt for certain commands
		if cmd.Name() == "hooks" || cmd.Name() == "help" || cmd.Name() == "completion" {
			return
		}

		// Skip setup prompt when called from hook (stdin is pipe)
		// This prevents consuming JSON data meant for register/touch commands
		stat, err := os.Stdin.Stat()
		if err == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
			return
		}

		// Check if this is first run and hooks aren't installed
		if !isFirstRunComplete() && !areHooksInstalled() {
			promptFirstTimeSetup()
			markFirstRunComplete()
		}

		// Update check (non-blocking)
		if !shouldSkipUpdateCheck(cmd) {
			startUpdateCheck()
		}
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		showUpdateNotification()
	},
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
	rootCmd.AddCommand(versionCmd)
}

func isFirstRunComplete() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return true
	}

	flagFile := filepath.Join(home, ".config", "devctx", ".initialized")
	_, err = os.Stat(flagFile)
	return err == nil
}

func markFirstRunComplete() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	flagFile := filepath.Join(home, ".config", "devctx", ".initialized")
	os.MkdirAll(filepath.Dir(flagFile), 0755)
	os.WriteFile(flagFile, []byte("1"), 0644)
}

func areHooksInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return false
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return false
	}

	// Check if devctx hooks are present
	for _, eventHooks := range hooks {
		if hooksList, ok := eventHooks.([]interface{}); ok {
			for _, hook := range hooksList {
				if hookMap, ok := hook.(map[string]interface{}); ok {
					if innerHooks, ok := hookMap["hooks"].([]interface{}); ok {
						for _, h := range innerHooks {
							if hm, ok := h.(map[string]interface{}); ok {
								if cmd, ok := hm["command"].(string); ok {
									if strings.Contains(cmd, "devctx") {
										return true
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return false
}

func promptFirstTimeSetup() {
	fmt.Println("Welcome to devctx!")
	fmt.Println()
	fmt.Println("Would you like to set up Claude Code hooks for automatic session tracking?")
	fmt.Println("This allows devctx to automatically register sessions when you start Claude Code.")
	fmt.Println()
	fmt.Print("Install hooks? [Y/n]: ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" || input == "y" || input == "yes" {
		if err := installHooksToSettings(); err != nil {
			fmt.Printf("Failed to install hooks: %v\n", err)
			fmt.Println("You can manually run 'devctx hooks --install' later.")
		}
	} else {
		fmt.Println("Skipped. You can run 'devctx hooks --install' later.")
	}
	fmt.Println()
}
