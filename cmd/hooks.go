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

		settings := ClaudeSettings{Hooks: devctxHookConfigs(devctxPath)}

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

	configs := devctxHookConfigs(devctxPath)
	for _, event := range devctxHookEvents {
		var newConfigs []map[string]interface{}
		for _, c := range configs[event] {
			newConfigs = append(newConfigs, hookConfigMap(c))
		}
		hooks[event] = mergeHookConfigs(hooks[event], newConfigs...)
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

// devctxHookEvents は devctx が hook を登録するイベント（インストール順）。
var devctxHookEvents = []string{"SessionStart", "UserPromptSubmit", "Notification", "Stop", "SessionEnd"}

// devctxHookConfigs は devctx が Claude Code に登録する hook 定義。
// 表示（devctx hooks）とインストール（--install）の両方がこれを使う。
func devctxHookConfigs(devctxPath string) map[string][]HookConfig {
	command := func(args string) []Hook {
		return []Hook{{Type: "command", Command: devctxPath + " " + args}}
	}
	return map[string][]HookConfig{
		"SessionStart": {
			{Matcher: "startup", Hooks: command("register")},
			{Matcher: "resume", Hooks: command("register")},
		},
		// Agent state: running on prompt, waiting on notification / turn end
		"UserPromptSubmit": {{Hooks: command("touch --quick --track-state")}},
		"Notification":     {{Hooks: command("touch --quick --track-state")}},
		"Stop": {
			{Hooks: command("roadmap analyze --if-stale --background")},
			{Hooks: command("touch --quick --track-state")},
		},
		"SessionEnd": {{Hooks: command("touch --track-state")}},
	}
}

// hookConfigMap は HookConfig を settings.json の汎用マップ表現に変換する。
func hookConfigMap(c HookConfig) map[string]interface{} {
	hooks := make([]map[string]interface{}, len(c.Hooks))
	for i, h := range c.Hooks {
		hooks[i] = map[string]interface{}{"type": h.Type, "command": h.Command}
	}
	config := map[string]interface{}{"hooks": hooks}
	if c.Matcher != "" {
		config["matcher"] = c.Matcher
	}
	return config
}

// mergeHookConfigs adds devctx hooks to existing hook configs.
// If a config with a devctx command of the same subcommand + matcher exists,
// that devctx command is upgraded to the new arguments (keeping its binary path).
// Non-devctx hooks are never removed or modified.
// Why not leave existing devctx entries untouched: users who installed an older
// version would never receive new flags such as --track-state.
func mergeHookConfigs(existing interface{}, newConfigs ...map[string]interface{}) []interface{} {
	var configs []interface{}

	// Convert existing to slice if present
	if existing != nil {
		if existingSlice, ok := existing.([]interface{}); ok {
			configs = existingSlice
		}
	}

	for _, nc := range newConfigs {
		newHook := findDevctxHook(nc)
		if newHook == nil {
			configs = append(configs, nc)
			continue
		}
		newCmd, _ := newHook["command"].(string)
		newSub := devctxSubcommand(newCmd)
		newMatcher, _ := nc["matcher"].(string)

		var existHook map[string]interface{}
		for _, config := range configs {
			hook := findDevctxHook(config)
			if hook == nil {
				continue
			}
			existCmd, _ := hook["command"].(string)
			if devctxSubcommand(existCmd) != newSub {
				continue
			}
			configMap, _ := config.(map[string]interface{})
			if existMatcher, _ := configMap["matcher"].(string); existMatcher == newMatcher {
				existHook = hook
				break
			}
		}
		if existHook == nil {
			configs = append(configs, nc)
			continue
		}
		existCmd, _ := existHook["command"].(string)
		existBinary, _, _ := splitDevctxCommand(existCmd)
		_, newArgs, _ := splitDevctxCommand(newCmd)
		existHook["command"] = existBinary + " " + newArgs
	}
	return configs
}

// findDevctxHook returns the first hook entry running devctx in a hook config, or nil.
// Configs read from settings.json hold []interface{}, while configs built in code hold
// []map[string]interface{}; both must be handled or new configs are never recognized.
func findDevctxHook(config interface{}) map[string]interface{} {
	configMap, ok := config.(map[string]interface{})
	if !ok {
		return nil
	}
	var hooks []map[string]interface{}
	switch arr := configMap["hooks"].(type) {
	case []map[string]interface{}:
		hooks = arr
	case []interface{}:
		for _, h := range arr {
			if hookMap, ok := h.(map[string]interface{}); ok {
				hooks = append(hooks, hookMap)
			}
		}
	}
	for _, hook := range hooks {
		if cmd, ok := hook["command"].(string); ok {
			if _, _, ok := splitDevctxCommand(cmd); ok {
				return hook
			}
		}
	}
	return nil
}

// splitDevctxCommand splits a hook command into the devctx binary part and its arguments.
// e.g. "/path/to/devctx touch --quick" → ("/path/to/devctx", "touch --quick", true)
func splitDevctxCommand(cmd string) (binary, args string, ok bool) {
	offset := 0
	for _, field := range strings.Fields(cmd) {
		idx := strings.Index(cmd[offset:], field) + offset
		end := idx + len(field)
		if filepath.Base(field) == "devctx" {
			return cmd[:end], strings.TrimSpace(cmd[end:]), true
		}
		offset = end
	}
	return "", "", false
}

// devctxSubcommand extracts the subcommand from a devctx command string.
// e.g. "/path/to/devctx register" → "register", "devctx touch --quick" → "touch"
func devctxSubcommand(cmd string) string {
	_, args, ok := splitDevctxCommand(cmd)
	if !ok {
		return ""
	}
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
