package cmd

import (
	"encoding/json"
	"testing"
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
	configs := devctxHookConfigs("devctx")
	want := map[string][]string{
		"SessionStart":     {"devctx register", "devctx register"},
		"UserPromptSubmit": {"devctx touch --quick --track-state"},
		"Notification":     {"devctx touch --quick --track-state"},
		"Stop":             {"devctx roadmap analyze --if-stale --background", "devctx touch --quick --track-state"},
		"SessionEnd":       {"devctx touch --track-state"},
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

func TestInstallConfigsAreRecognizedAsDevctx(t *testing.T) {
	for event, configs := range devctxHookConfigs("devctx") {
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
