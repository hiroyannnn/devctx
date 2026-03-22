package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		// Use PATH-based name so hooks survive binary rebuilds / user changes
		devctxPath := "devctx"

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
					{
						Matcher: "resume",
						Hooks: []Hook{
							{
								Type:    "command",
								Command: devctxPath + " register",
							},
						},
					},
				},
				"Notification": {
					{
						Hooks: []Hook{
							{
								Type:    "command",
								Command: devctxPath + " touch --quick",
							},
						},
					},
				},
				"SessionEnd": {
					{
						Hooks: []Hook{
							{
								Type:    "command",
								Command: devctxPath + " touch",
							},
						},
					},
				},
				"Stop": {
					{
						Hooks: []Hook{
							{
								Type:    "command",
								Command: devctxPath + " roadmap analyze --if-stale --background",
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

	// Use PATH-based name so hooks survive binary rebuilds / user changes
	devctxPath := "devctx"

	// Add or update hooks
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
	}

	// Merge SessionStart hooks (startup + resume)
	hooks["SessionStart"] = mergeHookConfigs(hooks["SessionStart"],
		map[string]interface{}{
			"matcher": "startup",
			"hooks": []map[string]interface{}{
				{"type": "command", "command": devctxPath + " register"},
			},
		},
		map[string]interface{}{
			"matcher": "resume",
			"hooks": []map[string]interface{}{
				{"type": "command", "command": devctxPath + " register"},
			},
		},
	)

	// Merge Notification hooks (throttled last_seen update)
	hooks["Notification"] = mergeHookConfigs(hooks["Notification"],
		map[string]interface{}{
			"hooks": []map[string]interface{}{
				{"type": "command", "command": devctxPath + " touch --quick"},
			},
		},
	)

	// Merge SessionEnd hooks
	hooks["SessionEnd"] = mergeHookConfigs(hooks["SessionEnd"],
		map[string]interface{}{
			"hooks": []map[string]interface{}{
				{"type": "command", "command": devctxPath + " touch"},
			},
		},
	)

	// Merge Stop hooks (background insight analysis)
	hooks["Stop"] = mergeHookConfigs(hooks["Stop"],
		map[string]interface{}{
			"hooks": []map[string]interface{}{
				{"type": "command", "command": devctxPath + " roadmap analyze --if-stale --background"},
			},
		},
	)

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
	fmt.Println("  Run /hooks in Claude Code to review and approve.")
	fmt.Println()
	fmt.Println("Next:")
	fmt.Println("  devctx roadmap serve            # Open the Mind Map dashboard")
	fmt.Println()
	fmt.Println("Optional:")
	fmt.Println("  devctx commands --install        # Slash commands (/devctx-review, etc.)")
	fmt.Println("  eval \"$(devctx shell-init)\"      # Shell shortcuts (dx, dxl, etc.)")
	return nil
}

// mergeHookConfigs removes existing devctx hooks and appends the new configs.
// This ensures stale paths are replaced on re-install.
func mergeHookConfigs(existing interface{}, newConfigs ...map[string]interface{}) []interface{} {
	var configs []interface{}

	// Convert existing to slice if present
	if existing != nil {
		if existingSlice, ok := existing.([]interface{}); ok {
			configs = existingSlice
		}
	}

	// Remove any existing devctx hooks
	var filtered []interface{}
	for _, config := range configs {
		if configHasDevctx(config) {
			continue
		}
		filtered = append(filtered, config)
	}

	// Append new configs
	for _, nc := range newConfigs {
		filtered = append(filtered, nc)
	}
	return filtered
}

// configHasDevctx returns true if any hook command in the config contains "devctx".
func configHasDevctx(config interface{}) bool {
	configMap, ok := config.(map[string]interface{})
	if !ok {
		return false
	}
	hooksArr, ok := configMap["hooks"].([]interface{})
	if !ok {
		return false
	}
	for _, hook := range hooksArr {
		if hookMap, ok := hook.(map[string]interface{}); ok {
			if cmd, ok := hookMap["command"].(string); ok {
				if strings.Contains(cmd, "devctx") {
					return true
				}
			}
		}
	}
	return false
}
