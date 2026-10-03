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

// hooksFile は Claude の settings.json / Codex の hooks.json に共通する "hooks" の形。
type hooksFile struct {
	Hooks map[string][]HookConfig `json:"hooks"`
}

type HookConfig struct {
	Matcher string `json:"matcher,omitempty"`
	Hooks   []Hook `json:"hooks"`
}

type Hook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	// Async は非同期 hook（エージェントを待たせない）。Claude の PermissionRequest / PreToolUse と Codex の状態 hook で使う
	Async bool `json:"async,omitempty"`
	// Timeout は秒。Codex の SessionEnd は既定 1s と短いため明示する
	Timeout int `json:"timeout,omitempty"`
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
		if installHooks {
			if provider == model.ProviderCodex {
				return installCodexHooks()
			}
			return installHooksToSettings()
		}
		// Use PATH-based name so hooks survive binary rebuilds / user changes
		if provider == model.ProviderCodex {
			return printHooks(codexHookSpecs("devctx"),
				"Add the following to your Codex hooks ($CODEX_HOME/hooks.json, default ~/.codex/hooks.json):",
				"Or run: devctx hooks --install --provider codex to automatically add to the Codex hooks file")
		}
		return printHooks(devctxHookSpecs("devctx"),
			"Add the following to your Claude settings (~/.claude/settings.json):",
			"Or run: devctx hooks --install to automatically add to user settings")
	},
}

var (
	installHooks  bool
	hooksProvider string
)

func init() {
	hooksCmd.Flags().BoolVar(&installHooks, "install", false, "Automatically install hooks to ~/.claude/settings.json (or $CODEX_HOME/hooks.json with --provider codex)")
	hooksCmd.Flags().StringVar(&hooksProvider, "provider", "claude", "Agent provider to install hooks for (claude/codex)")
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

// codexHooksPath は Codex の hooks.json の場所を返す。
func codexHooksPath() (string, error) {
	home, err := codexHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "hooks.json"), nil
}

// printHooks は specs を JSON で表示する。Claude / Codex で header・footer だけが違う。
func printHooks(specs []hookEventSpec, header, footer string) error {
	jsonBytes, err := json.MarshalIndent(hooksFile{Hooks: hookConfigsByEvent(specs)}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(header)
	fmt.Println()
	fmt.Println(string(jsonBytes))
	fmt.Println()
	fmt.Println(footer)
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

// devctxCommand は devctx の 1 コマンドだけを実行する hook 列を返す。
func devctxCommand(devctxPath, args string) []Hook {
	return []Hook{{Type: "command", Command: devctxPath + " " + args}}
}

// devctxHookSpecs は devctx が Claude Code に登録する hook 定義（インストール順）。
// 表示（devctx hooks）とインストール（--install）の両方がこれを使う。
func devctxHookSpecs(devctxPath string) []hookEventSpec {
	command := func(args string) []Hook { return devctxCommand(devctxPath, args) }
	asyncCommand := []Hook{{Type: "command", Command: devctxPath + " touch --quick --track-state", Async: true}}
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
		// 何を待っているか（待ち要求）の記録。許可ダイアログは tool_name を持つ PermissionRequest で、
		// 質問 UI は許可ではないので PreToolUse を AskUserQuestion に限って拾う。
		// async にするのは、許可ダイアログの表示をこの hook で遅らせないため
		{"PermissionRequest", []HookConfig{{Hooks: asyncCommand}}},
		{"PreToolUse", []HookConfig{{Matcher: "AskUserQuestion", Hooks: asyncCommand}}},
	}
}

// codexHookSpecs は devctx が Codex に登録する hook 定義。
// Codex には Notification が無く、許可待ちは PermissionRequest で届く。
// Why not roadmap analyze: Stop ごとの LLM 解析は Claude 用の機能で、Codex では対象外。
func codexHookSpecs(devctxPath string) []hookEventSpec {
	touchCmd := devctxPath + " touch --quick --track-state --provider codex"
	// 状態更新はエージェントを待たせないよう async。register は以降の touch が context を見つけられるよう同期
	track := []Hook{{Type: "command", Command: touchCmd, Async: true}}
	// SessionEnd は Codex 側で同期固定・既定 1s のため、git を呼ぶ phase 更新を省く --quick にし timeout を明示する
	end := []Hook{{Type: "command", Command: touchCmd, Timeout: 3}}
	return []hookEventSpec{
		{"SessionStart", []HookConfig{{Matcher: "startup|resume", Hooks: devctxCommand(devctxPath, "register --provider codex")}}},
		{"UserPromptSubmit", []HookConfig{{Hooks: track}}},
		{"PermissionRequest", []HookConfig{{Hooks: track}}},
		// 許可が承認されてツールが動いたら running に戻す（承認後に Stop まで needs_input が残らないように）
		{"PostToolUse", []HookConfig{{Hooks: track}}},
		// 質問ツールは許可ではなく PreToolUse で待ちに入る。拒否には hook が無いので、ユーザー中断（Interrupt）で待ちを解消する
		{"PreToolUse", []HookConfig{{Matcher: "request_user_input", Hooks: track}}},
		{"Interrupt", []HookConfig{{Hooks: track}}},
		{"Stop", []HookConfig{{Hooks: track}}},
		{"SessionEnd", []HookConfig{{Hooks: end}}},
	}
}

// hookConfigsByEvent は specs をイベント名で引けるようにしたもの（表示用）。
func hookConfigsByEvent(specs []hookEventSpec) map[string][]HookConfig {
	configs := make(map[string][]HookConfig, len(specs))
	for _, spec := range specs {
		configs[spec.Event] = spec.Configs
	}
	return configs
}

// hookConfigMap は HookConfig を settings.json から読んだのと同じ汎用表現に変換する。
func hookConfigMap(c HookConfig) map[string]interface{} {
	hooks := make([]interface{}, len(c.Hooks))
	for i, h := range c.Hooks {
		m := map[string]interface{}{"type": h.Type, "command": h.Command}
		if h.Async {
			m["async"] = true
		}
		if h.Timeout > 0 {
			m["timeout"] = h.Timeout
		}
		hooks[i] = m
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
		// 実行方式（async / timeout）も devctx 管理下の項目なので新しい定義に揃える
		for _, key := range []string{"async", "timeout"} {
			if v, ok := newHooks[0][key]; ok {
				existHook[key] = v
			} else {
				delete(existHook, key)
			}
		}
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
