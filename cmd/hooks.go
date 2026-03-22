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

// mergeHookConfigs appends devctx hooks that don't already exist.
// If an existing config already has a devctx command with the same
// subcommand, it is left untouched. Never removes or modifies existing entries.
func mergeHookConfigs(existing interface{}, newConfigs ...map[string]interface{}) []interface{} {
	var configs []interface{}

	// Convert existing to slice if present
	if existing != nil {
		if existingSlice, ok := existing.([]interface{}); ok {
			configs = existingSlice
		}
	}

	for _, nc := range newConfigs {
		newCmd := extractDevctxCommand(nc)
		if newCmd == "" {
			configs = append(configs, nc)
			continue
		}
		newSub := devctxSubcommand(newCmd)

		// Check if a devctx hook with the same subcommand + matcher already exists
		newMatcher, _ := nc["matcher"].(string)
		found := false
		for _, config := range configs {
			existCmd := extractDevctxCommand(config)
			if existCmd == "" || devctxSubcommand(existCmd) != newSub {
				continue
			}
			if configMap, ok := config.(map[string]interface{}); ok {
				existMatcher, _ := configMap["matcher"].(string)
				if existMatcher == newMatcher {
					found = true
					break
				}
			}
		}
		if !found {
			configs = append(configs, nc)
		}
	}
	return configs
}

// extractDevctxCommand returns the first devctx command from a hook config, or "".
func extractDevctxCommand(config interface{}) string {
	configMap, ok := config.(map[string]interface{})
	if !ok {
		return ""
	}
	hooksArr, ok := configMap["hooks"].([]interface{})
	if !ok {
		return ""
	}
	for _, hook := range hooksArr {
		if hookMap, ok := hook.(map[string]interface{}); ok {
			if cmd, ok := hookMap["command"].(string); ok {
				if strings.Contains(cmd, "devctx") {
					return cmd
				}
			}
		}
	}
	return ""
}

// devctxSubcommand extracts the subcommand from a devctx command string.
// e.g. "/path/to/devctx register" → "register", "devctx touch --quick" → "touch"
func devctxSubcommand(cmd string) string {
	idx := strings.Index(cmd, "devctx")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(cmd[idx+len("devctx"):])
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
