package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type ClaudeSettings struct {
	Hooks map[string][]HookConfig `json:"hooks"`
}

type HookConfig struct {
	Matcher string `json:"matcher,omitempty"`
	Hooks   []Hook `json:"hooks"`
}

type Hook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Setup Claude Code hooks for devctx integration",
	Long: `Configure Claude Code hooks to automatically register and update contexts.

This command outputs the JSON configuration to add to your Claude settings.
Add this to ~/.claude/settings.json or .claude/settings.json in your project.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get devctx binary path
		devctxPath, err := os.Executable()
		if err != nil {
			devctxPath = "devctx" // fallback to PATH
		}

		settings := ClaudeSettings{
			Hooks: map[string][]HookConfig{
				"SessionStart": {
					{
						Matcher: "startup",
						Hooks: []Hook{
							{
								Type:    "command",
								Command: devctxPath + " register",
							},
						},
					},
				},
				"SessionEnd": {
					{
						Matcher: "",
						Hooks: []Hook{
							{
								Type:    "command",
								Command: devctxPath + " touch",
							},
						},
					},
				},
			},
		}

		jsonBytes, err := json.MarshalIndent(settings, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println("Add the following to your Claude settings (~/.claude/settings.json):")
		fmt.Println()
		fmt.Println(string(jsonBytes))
		fmt.Println()
		fmt.Println("Or run: devctx hooks --install to automatically add to user settings")

		return nil
	},
}

var installHooks bool

func init() {
	hooksCmd.Flags().BoolVar(&installHooks, "install", false, "Automatically install hooks to ~/.claude/settings.json")

	hooksCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if installHooks {
			return installHooksToSettings()
		}
		return nil
	}
}

func installHooksToSettings() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// Ensure .claude directory exists
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return err
	}

	// Read existing settings
	var settings map[string]interface{}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		settings = make(map[string]interface{})
	} else {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse existing settings: %w", err)
		}
	}

	devctxPath, err := os.Executable()
	if err != nil {
		devctxPath = "devctx"
	}

	// Add or update hooks
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
	}

	hooks["SessionStart"] = []map[string]interface{}{
		{
			"matcher": "startup",
			"hooks": []map[string]interface{}{
				{
					"type":    "command",
					"command": devctxPath + " register",
				},
			},
		},
	}

	hooks["SessionEnd"] = []map[string]interface{}{
		{
			"matcher": "",
			"hooks": []map[string]interface{}{
				{
					"type":    "command",
					"command": devctxPath + " touch",
				},
			},
		},
	}

	settings["hooks"] = hooks

	// Write back
	output, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(settingsPath, output, 0644); err != nil {
		return err
	}

	fmt.Printf("✓ Hooks installed to %s\n", settingsPath)
	fmt.Println("Note: You may need to run /hooks in Claude Code to review and approve the changes.")
	return nil
}
