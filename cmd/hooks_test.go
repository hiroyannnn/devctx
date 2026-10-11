package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func hookConfig(matcher string, commands ...string) map[string]interface{} {
	var hooks []interface{}
	for _, c := range commands {
		hooks = append(hooks, map[string]interface{}{"type": "command", "command": c})
	}
	config := map[string]interface{}{"hooks": hooks}
	if matcher != "" {
		config["matcher"] = matcher
	}
	return config
}

// existingHooks は settings.json から読んだのと同じ形（[]interface{}）に変換する。
func existingHooks(t *testing.T, configs ...map[string]interface{}) interface{} {
	t.Helper()
	data, err := json.Marshal(configs)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func commandsOf(t *testing.T, configs []interface{}) []string {
	t.Helper()
	data, err := json.Marshal(configs)
	if err != nil {
		t.Fatal(err)
	}
	var parsed []struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	var cmds []string
	for _, c := range parsed {
		for _, h := range c.Hooks {
			cmds = append(cmds, h.Command)
		}
	}
	return cmds
}

func assertCommands(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commands = %q, want %q", got, want)
		}
	}
}

func TestMergeHookConfigs_AddsToEmpty(t *testing.T) {
	got := mergeHookConfigs(nil, hookConfig("", "devctx touch --quick --track-state"))
	assertCommands(t, commandsOf(t, got), "devctx touch --quick --track-state")
}

func TestMergeHookConfigs_UpgradesExistingDevctxCommand(t *testing.T) {
	existing := existingHooks(t, hookConfig("", "devctx touch --quick"))
	got := mergeHookConfigs(existing, hookConfig("", "devctx touch --quick --track-state"))
	assertCommands(t, commandsOf(t, got), "devctx touch --quick --track-state")
}

func TestMergeHookConfigs_UpgradeKeepsCustomBinaryPath(t *testing.T) {
	existing := existingHooks(t, hookConfig("", "/Users/me/GitHub/devctx/devctx touch --quick"))
	got := mergeHookConfigs(existing, hookConfig("", "devctx touch --quick --track-state"))
	assertCommands(t, commandsOf(t, got), "/Users/me/GitHub/devctx/devctx touch --quick --track-state")
}

func TestMergeHookConfigs_KeepsOtherHooks(t *testing.T) {
	existing := existingHooks(t,
		hookConfig("", "say done"),
		hookConfig("", "devctx roadmap analyze --if-stale --background"),
	)
	got := mergeHookConfigs(existing, hookConfig("", "devctx touch --quick --track-state"))
	assertCommands(t, commandsOf(t, got),
		"say done",
		"devctx roadmap analyze --if-stale --background",
		"devctx touch --quick --track-state",
	)
}

func TestMergeHookConfigs_DifferentMatcherIsSeparate(t *testing.T) {
	existing := existingHooks(t, hookConfig("startup", "devctx register"))
	got := mergeHookConfigs(existing, hookConfig("startup", "devctx register"), hookConfig("resume", "devctx register"))
	assertCommands(t, commandsOf(t, got), "devctx register", "devctx register")
}

func TestDevctxHookConfigs_TracksAgentState(t *testing.T) {
	configs := hookConfigsByEvent(devctxHookSpecs("devctx"))
	want := map[string][]string{
		"SessionStart":     {"devctx register", "devctx register", "devctx register"},
		"UserPromptSubmit": {"devctx touch --quick --track-state"},
		"Notification":     {"devctx touch --quick --track-state"},
		"Stop":             {"devctx roadmap analyze --if-stale --background", "devctx touch --quick --track-state"},
		"SessionEnd":       {"devctx touch --track-state"},
		// 何を待っているかの記録。許可ダイアログは全ツール、質問は AskUserQuestion だけ
		"PermissionRequest": {"devctx touch --quick --track-state"},
		"PreToolUse":        {"devctx touch --quick --track-state"},
	}
	if len(configs) != len(want) {
		t.Fatalf("events = %d, want %d", len(configs), len(want))
	}
	for event, wantCmds := range want {
		var got []string
		for _, c := range configs[event] {
			for _, h := range c.Hooks {
				got = append(got, h.Command)
			}
		}
		assertCommands(t, got, wantCmds...)
	}
}

// /clear は新しい session_id で始まるが SessionStart の source は "clear" で、startup|resume には当たらない。
// register が走らないと touch が context を見つけられず、clear 後に貼った marker が黙って捨てられる。
func TestDevctxHookSpecs_SessionStartCoversClearButNotCompact(t *testing.T) {
	var matchers []string
	for _, c := range hookConfigsByEvent(devctxHookSpecs("devctx"))["SessionStart"] {
		matchers = append(matchers, c.Matcher)
	}
	if !reflect.DeepEqual(matchers, []string{"startup", "resume", "clear"}) {
		t.Fatalf("SessionStart matchers = %v, want startup/resume/clear (compact keeps the same session)", matchers)
	}
}

func TestDevctxHookSpecs_PendingRequestHooks(t *testing.T) {
	configs := hookConfigsByEvent(devctxHookSpecs("devctx"))
	perm := configs["PermissionRequest"]
	if len(perm) != 1 || perm[0].Matcher != "" || !perm[0].Hooks[0].Async {
		t.Errorf("PermissionRequest = %+v, want all tools, async", perm)
	}
	pre := configs["PreToolUse"]
	if len(pre) != 1 || pre[0].Matcher != "AskUserQuestion" || !pre[0].Hooks[0].Async {
		t.Errorf("PreToolUse = %+v, want AskUserQuestion matcher, async", pre)
	}
}

func TestInstallHookSpecsToFile_ClaudeIsIdempotentAndEmitsAsync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	read := func() map[string]interface{} {
		t.Helper()
		if err := installHookSpecsToFile(path, devctxHookSpecs("devctx")); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]interface{}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	first := read()
	hooks := first["hooks"].(map[string]interface{})
	pre := hooks["PreToolUse"].([]interface{})
	if len(pre) != 1 {
		t.Fatalf("PreToolUse configs = %d, want 1", len(pre))
	}
	cfg := pre[0].(map[string]interface{})
	h := cfg["hooks"].([]interface{})[0].(map[string]interface{})
	if cfg["matcher"] != "AskUserQuestion" || h["async"] != true {
		t.Fatalf("PreToolUse = %v", cfg)
	}
	if _, ok := hooks["PermissionRequest"]; !ok {
		t.Fatal("missing PermissionRequest")
	}
	if second := read(); !reflect.DeepEqual(first, second) {
		t.Fatalf("second install changed the file:\nfirst:  %v\nsecond: %v", first, second)
	}
}

func TestInstallConfigsAreRecognizedAsDevctx(t *testing.T) {
	for event, configs := range hookConfigsByEvent(devctxHookSpecs("devctx")) {
		for _, c := range configs {
			if len(findDevctxHooks(hookConfigMap(c))) == 0 {
				t.Fatalf("%s config %+v is not recognized as a devctx hook", event, c)
			}
		}
	}
}

func TestMergeHookConfigs_DoesNotRewriteDifferentRoadmapOperation(t *testing.T) {
	existing := existingHooks(t, hookConfig("", "devctx roadmap serve --port 4000"))
	got := mergeHookConfigs(existing, hookConfig("", "devctx roadmap analyze --if-stale --background"))
	assertCommands(t, commandsOf(t, got),
		"devctx roadmap serve --port 4000",
		"devctx roadmap analyze --if-stale --background",
	)
}

func TestMergeHookConfigs_MatchesAnyDevctxHookInConfig(t *testing.T) {
	existing := existingHooks(t, hookConfig("", "devctx touch --quick", "devctx roadmap analyze --if-stale"))
	got := mergeHookConfigs(existing,
		hookConfig("", "devctx roadmap analyze --if-stale --background"),
		hookConfig("", "devctx touch --quick --track-state"),
	)
	assertCommands(t, commandsOf(t, got),
		"devctx touch --quick --track-state",
		"devctx roadmap analyze --if-stale --background",
	)
}

func TestDevctxCommandPath(t *testing.T) {
	tests := map[string]string{
		"devctx touch --quick":                      "touch",
		"devctx roadmap analyze --if-stale":         "roadmap analyze",
		"/opt/bin/devctx roadmap serve --port 4000": "roadmap serve",
		"devctx register":                           "register",
		"/Users/me/GitHub/devctx/devctx touch -q":   "touch",
		"echo hi": "",
	}
	for in, want := range tests {
		if got := devctxCommandPath(in); got != want {
			t.Fatalf("devctxCommandPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeHookConfigs_UpgradeKeepsChainedCommands(t *testing.T) {
	existing := existingHooks(t, hookConfig("", "devctx touch --quick && say done"))
	got := mergeHookConfigs(existing, hookConfig("", "devctx touch --quick --track-state"))
	assertCommands(t, commandsOf(t, got), "devctx touch --quick --track-state && say done")
}

func TestCodexHookSpecs(t *testing.T) {
	specs := codexHookSpecs("devctx")
	type entry struct{ matcher, command string }
	want := map[string][]entry{
		"SessionStart":      {{"startup|resume", "devctx register --provider codex"}},
		"UserPromptSubmit":  {{"", "devctx touch --quick --track-state --provider codex"}},
		"PermissionRequest": {{"", "devctx touch --quick --track-state --provider codex"}},
		"PostToolUse":       {{"", "devctx touch --quick --track-state --provider codex"}},
		"PreToolUse":        {{"request_user_input", "devctx touch --quick --track-state --provider codex"}},
		"Interrupt":         {{"", "devctx touch --quick --track-state --provider codex"}},
		"Stop":              {{"", "devctx touch --quick --track-state --provider codex"}},
		"SessionEnd":        {{"", "devctx touch --quick --track-state --provider codex"}},
	}
	if len(specs) != len(want) {
		t.Fatalf("events = %d, want %d", len(specs), len(want))
	}
	for _, spec := range specs {
		var got []entry
		for _, c := range spec.Configs {
			for _, h := range c.Hooks {
				got = append(got, entry{c.Matcher, h.Command})
			}
		}
		if !reflect.DeepEqual(got, want[spec.Event]) {
			t.Errorf("%s = %v, want %v", spec.Event, got, want[spec.Event])
		}
		h := spec.Configs[0].Hooks[0]
		switch spec.Event {
		case "SessionStart":
			// register は同期: 以降の touch が context を見つけられるように先に登録を終える
			if h.Async {
				t.Errorf("SessionStart must be synchronous")
			}
		case "SessionEnd":
			// Codex の SessionEnd は同期固定・既定 1s のため、timeout を明示する
			if h.Async || h.Timeout != 3 {
				t.Errorf("SessionEnd = async %v timeout %d, want sync with timeout 3", h.Async, h.Timeout)
			}
		default:
			if !h.Async {
				t.Errorf("%s touch hook should be async so it never blocks the agent", spec.Event)
			}
		}
	}
}

func TestHookConfigMap_EmitsAsyncAndTimeout(t *testing.T) {
	m := hookConfigMap(HookConfig{Hooks: []Hook{{Type: "command", Command: "devctx touch", Async: true, Timeout: 3}}})
	h := m["hooks"].([]interface{})[0].(map[string]interface{})
	if h["async"] != true || h["timeout"] != 3 {
		t.Fatalf("hook map = %v, want async and timeout", h)
	}
	plain := hookConfigMap(HookConfig{Hooks: []Hook{{Type: "command", Command: "devctx register"}}})
	ph := plain["hooks"].([]interface{})[0].(map[string]interface{})
	if _, ok := ph["async"]; ok {
		t.Fatalf("async should be omitted when false: %v", ph)
	}
}

func TestMergeHookConfigs_UpgradeSyncsAsyncAndTimeout(t *testing.T) {
	existing := existingHooks(t, hookConfig("", "devctx touch --quick --provider codex"))
	newConfig := hookConfigMap(HookConfig{Hooks: []Hook{{Type: "command", Command: "devctx touch --quick --track-state --provider codex", Async: true}}})
	got := mergeHookConfigs(existing, newConfig)
	h := got[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	if h["command"] != "devctx touch --quick --track-state --provider codex" || h["async"] != true {
		t.Fatalf("devctx-owned entry should take the new command and async flag: %v", h)
	}
}

func TestInstallCodexHooks_PreservesExistingAndIsIdempotent(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(codexHome, "hooks.json")
	initial := `{
  "description": "my hooks",
  "hooks": {
    "SessionStart": [
      {"matcher": "startup", "hooks": [{"type": "command", "command": "other-tool start", "timeout": 5, "statusMessage": "starting"}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	install := func() map[string]interface{} {
		t.Helper()
		if err := installCodexHooks(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]interface{}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	first := install()
	if first["description"] != "my hooks" {
		t.Fatalf("description lost: %v", first["description"])
	}
	hooks := first["hooks"].(map[string]interface{})
	start := hooks["SessionStart"].([]interface{})
	if len(start) != 2 {
		t.Fatalf("SessionStart configs = %d, want 2 (other + devctx)", len(start))
	}
	other := start[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	if other["command"] != "other-tool start" || other["timeout"] != float64(5) || other["statusMessage"] != "starting" {
		t.Fatalf("unrelated hook was modified: %v", other)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PermissionRequest", "PostToolUse", "PreToolUse", "Interrupt", "Stop", "SessionEnd"} {
		if _, ok := hooks[event]; !ok {
			t.Errorf("missing %s", event)
		}
	}

	second := install()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("second install changed the file:\nfirst:  %v\nsecond: %v", first, second)
	}
}

func TestInstallCodexHooks_CreatesMissingFile(t *testing.T) {
	codexHome := filepath.Join(t.TempDir(), "nested", ".codex")
	t.Setenv("CODEX_HOME", codexHome)
	if err := installCodexHooks(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "hooks.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCodexHooksPath_DefaultsToHomeDotCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	t.Setenv("HOME", home)
	got, err := codexHooksPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".codex", "hooks.json"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInstallHooksToSettings_UpgradesWithoutPostToolUse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// 旧バージョンのインストール結果: --track-state なし
	old := `{"model":"opus","hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"/bin/devctx touch --quick"}]}]}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installHooksToSettings(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "opus" {
		t.Fatalf("unrelated key lost: %v", got["model"])
	}
	hooks := got["hooks"].(map[string]interface{})
	// Claude の状態は agent view（claude agents --json）から取る方針のため、ツール呼び出しごとの hook は入れない
	if _, ok := hooks["PostToolUse"]; ok {
		t.Fatalf("PostToolUse should not be installed for Claude")
	}
	prompt := hooks["UserPromptSubmit"].([]interface{})[0].(map[string]interface{})["hooks"].([]interface{})[0].(map[string]interface{})
	if prompt["command"] != "/bin/devctx touch --quick --track-state" {
		t.Fatalf("existing hook not upgraded: %v", prompt["command"])
	}
}

func TestParseHooksProvider(t *testing.T) {
	for in, want := range map[string]model.Provider{"": model.ProviderClaude, "claude": model.ProviderClaude, "codex": model.ProviderCodex} {
		got, err := parseHooksProvider(in)
		if err != nil || got != want {
			t.Errorf("parseHooksProvider(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"manual", "gemini"} {
		if _, err := parseHooksProvider(in); err == nil {
			t.Errorf("parseHooksProvider(%q) should fail", in)
		}
	}
}
