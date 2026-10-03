package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hiroyannnn/devctx/model"
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
	Short: "Setup Claude Code / Codex hooks for devctx integration",
	Long: `Configure Claude Code or Codex hooks to automatically register and update contexts.

This command outputs the JSON configuration to add to your settings.
Claude Code (default): add this to ~/.claude/settings.json or .claude/settings.json in your project.
Codex (--provider codex): add this to $CODEX_HOME/hooks.json (default ~/.codex/hooks.json).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		provider, err := parseHooksProvider(hooksProvider)
		if err != nil {
			return err
		}
		// Use PATH-based name so hooks survive binary rebuilds / user changes
		devctxPath := "devctx"

		if provider == model.ProviderCodex {
			return printCodexHooks(devctxPath)
		}

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

var (
	installHooks  bool
	hooksProvider string
)

func init() {
	hooksCmd.Flags().BoolVar(&installHooks, "install", false, "Automatically install hooks to ~/.claude/settings.json (or $CODEX_HOME/hooks.json with --provider codex)")
	hooksCmd.Flags().StringVar(&hooksProvider, "provider", "claude", "Agent provider to install hooks for (claude/codex)")

	hooksCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if !installHooks {
			return nil
		}
		provider, err := parseHooksProvider(hooksProvider)
		if err != nil {
			return err
		}
		if provider == model.ProviderCodex {
			return installCodexHooks()
		}
		return installHooksToSettings()
	}
}

// parseHooksProvider は hooks コマンドが対応する provider（claude / codex）だけを受け付ける。
// manual には hook の仕組みが無いので、ParseProvider が通っても弾く。
func parseHooksProvider(s string) (model.Provider, error) {
	provider, err := model.ParseProvider(s)
	if err != nil {
		return "", err
	}
	if provider != model.ProviderClaude && provider != model.ProviderCodex {
		return "", fmt.Errorf("hooks are only available for claude/codex (got %q)", s)
	}
	return provider, nil
}

func installHooksToSettings() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	// Use PATH-based name so hooks survive binary rebuilds / user changes
	if err := installHookSpecsToFile(settingsPath, devctxHookSpecs("devctx")); err != nil {
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

// installHookSpecsToFile は JSON ファイル（Claude の settings.json / Codex の hooks.json）の "hooks" に
// devctx の hook を追加・更新する。他のトップレベルキーや devctx 以外の hook はそのまま残す。
// Why not 型付き構造体で読み書き: description / timeout / statusMessage など未知のキーを書き戻しで落とすため。
func installHookSpecsToFile(path string, specs []hookEventSpec) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	// Read existing settings
	var settings map[string]interface{}
	data, err := os.ReadFile(path)
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

	// Add or update hooks
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
	}

	for _, spec := range specs {
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

	return os.WriteFile(path, output, 0644)
}

// codexHooksPath は Codex の hooks.json の場所を返す。CODEX_HOME 未設定なら ~/.codex。
func codexHooksPath() (string, error) {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return filepath.Join(dir, "hooks.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "hooks.json"), nil
}

func printCodexHooks(devctxPath string) error {
	hooks := make(map[string][]HookConfig)
	for _, spec := range codexHookSpecs(devctxPath) {
		hooks[spec.Event] = spec.Configs
	}
	jsonBytes, err := json.MarshalIndent(ClaudeSettings{Hooks: hooks}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println("Add the following to your Codex hooks ($CODEX_HOME/hooks.json, default ~/.codex/hooks.json):")
	fmt.Println()
	fmt.Println(string(jsonBytes))
	fmt.Println()
	fmt.Println("Or run: devctx hooks --install --provider codex to automatically add to the Codex hooks file")
	return nil
}

func installCodexHooks() error {
	path, err := codexHooksPath()
	if err != nil {
		return err
	}
	if err := installHookSpecsToFile(path, codexHookSpecs("devctx")); err != nil {
		return err
	}
	fmt.Printf("✓ Hooks installed to %s\n", path)
	fmt.Println("  Codex does not run new or changed hooks until you trust them.")
	fmt.Println("  Run /hooks in Codex to review and trust the devctx hooks (re-trust is needed whenever a devctx entry changes).")
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
		// 許可を承認してツールが動き出したら running に戻す（--quick の間引きで高頻度でも書き込みは増えない）
		{"PostToolUse", []HookConfig{{Hooks: command("touch --quick --track-state")}}},
		{"Stop", []HookConfig{
			{Hooks: command("roadmap analyze --if-stale --background")},
			{Hooks: command("touch --quick --track-state")},
		}},
		{"SessionEnd", []HookConfig{{Hooks: command("touch --track-state")}}},
	}
}

// codexHookSpecs は devctx が Codex に登録する hook 定義。
// Codex には Notification が無く、許可待ちは PermissionRequest で届く。
// Why not roadmap analyze: Stop ごとの LLM 解析は Claude 用の機能で、Codex では対象外。
func codexHookSpecs(devctxPath string) []hookEventSpec {
	command := func(args string) []Hook {
		return []Hook{{Type: "command", Command: devctxPath + " " + args}}
	}
	track := command("touch --quick --track-state --provider codex")
	return []hookEventSpec{
		{"SessionStart", []HookConfig{{Matcher: "startup|resume", Hooks: command("register --provider codex")}}},
		{"UserPromptSubmit", []HookConfig{{Hooks: track}}},
		{"PermissionRequest", []HookConfig{{Hooks: track}}},
		{"PostToolUse", []HookConfig{{Hooks: track}}},
		{"Stop", []HookConfig{{Hooks: track}}},
		{"SessionEnd", []HookConfig{{Hooks: command("touch --track-state --provider codex")}}},
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
		existBinary, _, existRest, _ := splitDevctxCommand(existCmd)
		_, newArgs, _, _ := splitDevctxCommand(newCmd)
		// Keep anything the user chained after the devctx command (e.g. "&& say done")
		existHook["command"] = strings.Join(append(append([]string{existBinary}, newArgs...), existRest...), " ")
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
			if _, _, _, ok := splitDevctxCommand(cmd); ok {
				result = append(result, hook)
			}
		}
	}
	return result
}

// splitDevctxCommand splits a hook command into the devctx binary part, its arguments,
// and the rest of a shell chain after it.
// e.g. "/path/to/devctx touch --quick && say hi" → ("/path/to/devctx", ["touch", "--quick"], ["&&", "say", "hi"], true)
func splitDevctxCommand(cmd string) (binary string, args, rest []string, ok bool) {
	fields := strings.Fields(cmd)
	for i, field := range fields {
		if filepath.Base(field) != "devctx" {
			continue
		}
		args = fields[i+1:]
		for j, arg := range args {
			if arg == "&&" || arg == "||" || arg == ";" || arg == "|" {
				return strings.Join(fields[:i+1], " "), args[:j], args[j:], true
			}
		}
		return strings.Join(fields[:i+1], " "), args, nil, true
	}
	return "", nil, nil, false
}

// devctxCommandPath returns the devctx subcommand path before the first flag.
// e.g. "/path/to/devctx roadmap analyze --if-stale" → "roadmap analyze", "devctx touch --quick" → "touch"
// Why not the first word only: "roadmap analyze" and "roadmap serve" are different hooks.
func devctxCommandPath(cmd string) string {
	_, args, _, ok := splitDevctxCommand(cmd)
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
