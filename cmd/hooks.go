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

	for _, spec := range devctxHookSpecs(devctxPath) {
		var newConfigs []map[string]interface{}
		for _, c := range spec.Configs {
			newConfigs = append(newConfigs, hookConfigMap(c))
		}
		hooks[spec.Event] = mergeHookConfigs(hooks[spec.Event], newConfigs...)
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

// hookEventSpec は 1 イベント分の devctx hook 定義。
type hookEventSpec struct {
	Event   string
	Configs []HookConfig
}

// devctxHookSpecs は devctx が Claude Code に登録する hook 定義（インストール順）。
// 表示（devctx hooks）とインストール（--install）の両方がこれを使う。
func devctxHookSpecs(devctxPath string) []hookEventSpec {
	command := func(args string) []Hook {
		return []Hook{{Type: "command", Command: devctxPath + " " + args}}
	}
	return []hookEventSpec{
		{"SessionStart", []HookConfig{
			{Matcher: "startup", Hooks: command("register")},
			{Matcher: "resume", Hooks: command("register")},
		}},
		// Agent state: running on prompt, waiting on notification / turn end
		{"UserPromptSubmit", []HookConfig{{Hooks: command("touch --quick --track-state")}}},
		{"Notification", []HookConfig{{Hooks: command("touch --quick --track-state")}}},
		{"Stop", []HookConfig{
			{Hooks: command("roadmap analyze --if-stale --background")},
			{Hooks: command("touch --quick --track-state")},
		}},
		{"SessionEnd", []HookConfig{{Hooks: command("touch --track-state")}}},
	}
}

// devctxHookConfigs は devctxHookSpecs をイベント名で引けるようにしたもの（表示用）。
func devctxHookConfigs(devctxPath string) map[string][]HookConfig {
	configs := make(map[string][]HookConfig)
	for _, spec := range devctxHookSpecs(devctxPath) {
		configs[spec.Event] = spec.Configs
	}
	return configs
}

// hookConfigMap は HookConfig を settings.json から読んだのと同じ汎用表現に変換する。
func hookConfigMap(c HookConfig) map[string]interface{} {
	hooks := make([]interface{}, len(c.Hooks))
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
		newHooks := findDevctxHooks(nc)
		if len(newHooks) == 0 {
			configs = append(configs, nc)
			continue
		}
		newCmd, _ := newHooks[0]["command"].(string)
		newPath := devctxCommandPath(newCmd)
		newMatcher, _ := nc["matcher"].(string)

		existHook := findMatchingDevctxHook(configs, newPath, newMatcher)
		if existHook == nil {
			configs = append(configs, nc)
			continue
		}
		existCmd, _ := existHook["command"].(string)
		existBinary, _, _ := splitDevctxCommand(existCmd)
		_, newArgs, _ := splitDevctxCommand(newCmd)
		existHook["command"] = strings.Join(append([]string{existBinary}, newArgs...), " ")
	}
	return configs
}

// findMatchingDevctxHook returns the devctx hook entry with the same command path
// (e.g. "roadmap analyze") under the same matcher, searching every hook in every config.
func findMatchingDevctxHook(configs []interface{}, path, matcher string) map[string]interface{} {
	for _, config := range configs {
		configMap, _ := config.(map[string]interface{})
		if existMatcher, _ := configMap["matcher"].(string); existMatcher != matcher {
			continue
		}
		for _, hook := range findDevctxHooks(config) {
			if cmd, _ := hook["command"].(string); devctxCommandPath(cmd) == path {
				return hook
			}
		}
	}
	return nil
}

// findDevctxHooks returns the hook entries running devctx in a hook config.
// Configs must use the settings.json shape ([]interface{}); hookConfigMap builds that shape.
func findDevctxHooks(config interface{}) []map[string]interface{} {
	configMap, _ := config.(map[string]interface{})
	hooks, _ := configMap["hooks"].([]interface{})
	var result []map[string]interface{}
	for _, h := range hooks {
		hook, _ := h.(map[string]interface{})
		if cmd, ok := hook["command"].(string); ok {
			if _, _, ok := splitDevctxCommand(cmd); ok {
				result = append(result, hook)
			}
		}
	}
	return result
}

// splitDevctxCommand splits a hook command into the devctx binary part and its arguments.
// e.g. "/path/to/devctx touch --quick" → ("/path/to/devctx", ["touch", "--quick"], true)
func splitDevctxCommand(cmd string) (binary string, args []string, ok bool) {
	fields := strings.Fields(cmd)
	for i, field := range fields {
		if filepath.Base(field) == "devctx" {
			return strings.Join(fields[:i+1], " "), fields[i+1:], true
		}
	}
	return "", nil, false
}

// devctxCommandPath returns the devctx subcommand path before the first flag.
// e.g. "/path/to/devctx roadmap analyze --if-stale" → "roadmap analyze", "devctx touch --quick" → "touch"
// Why not the first word only: "roadmap analyze" and "roadmap serve" are different hooks.
func devctxCommandPath(cmd string) string {
	_, args, ok := splitDevctxCommand(cmd)
	if !ok {
		return ""
	}
	var path []string
	for _, part := range args {
		if strings.HasPrefix(part, "-") {
			break
		}
		path = append(path, part)
	}
	return strings.Join(path, " ")
}
