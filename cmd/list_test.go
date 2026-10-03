package cmd

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

var appleScriptStringTests = []struct {
	name string
	in   string
	want string
}{
	{name: "plain", in: `cd /w/x`, want: `"cd /w/x"`},
	{name: "space", in: `cd '/w/my repo'`, want: `"cd '/w/my repo'"`},
	{name: "single quote stays as is", in: `cd '/w/it'\''s'`, want: `"cd '/w/it'\\''s'"`},
	{name: "double quote", in: `a"b`, want: `"a\"b"`},
	{name: "backslash", in: `a\b`, want: `"a\\b"`},
	{name: "backslash before double quote escapes backslash first", in: `a\"b`, want: `"a\\\"b"`},
}

func TestAppleScriptString(t *testing.T) {
	for _, tt := range appleScriptStringTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := appleScriptString(tt.in); got != tt.want {
				t.Fatalf("appleScriptString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// osascript で文字列リテラルを評価し、元の文字列に戻ることを確かめる（端末は起動しない）。
func TestAppleScriptString_RoundTripsThroughOsascript(t *testing.T) {
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("osascript not available")
	}
	for _, tt := range appleScriptStringTests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := exec.Command("osascript", "-e", "return "+appleScriptString(tt.in)).Output()
			if err != nil {
				t.Fatalf("osascript failed for %q: %v", tt.in, err)
			}
			if got := strings.TrimSuffix(string(out), "\n"); got != tt.in {
				t.Fatalf("round trip = %q, want %q", got, tt.in)
			}
		})
	}
}

func TestMacTerminalScript(t *testing.T) {
	tests := []struct {
		name         string
		worktree     string
		wantTerminal string
		wantITerm    string
	}{
		{
			name:         "path with space",
			worktree:     `/w/my repo`,
			wantTerminal: `do script "cd '/w/my repo' && claude --resume 's1'"`,
			wantITerm:    `create window with default profile command "/bin/zsh -c 'cd '\\''/w/my repo'\\'' && claude --resume '\\''s1'\\'''"`,
		},
		{
			name:         "path with single quote",
			worktree:     `/w/it's`,
			wantTerminal: `do script "cd '/w/it'\\''s' && claude --resume 's1'"`,
			wantITerm:    `create window with default profile command "/bin/zsh -c 'cd '\\''/w/it'\\''\\'\\'''\\''s'\\'' && claude --resume '\\''s1'\\'''"`,
		},
		{
			name:         "path with double quote",
			worktree:     `/w/say "hi"`,
			wantTerminal: `do script "cd '/w/say \"hi\"' && claude --resume 's1'"`,
			wantITerm:    `create window with default profile command "/bin/zsh -c 'cd '\\''/w/say \"hi\"'\\'' && claude --resume '\\''s1'\\'''"`,
		},
		{
			name:         "path with backslash",
			worktree:     `/w/back\slash`,
			wantTerminal: `do script "cd '/w/back\\slash' && claude --resume 's1'"`,
			wantITerm:    `create window with default profile command "/bin/zsh -c 'cd '\\''/w/back\\slash'\\'' && claude --resume '\\''s1'\\'''"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := resumeShellCommand(model.Context{Worktree: tt.worktree, SessionID: "s1", Provider: model.ProviderClaude})
			if err != nil {
				t.Fatal(err)
			}
			script := macTerminalScript(cmd)
			lines := map[string]bool{}
			for _, l := range strings.Split(script, "\n") {
				lines[strings.TrimSpace(l)] = true
			}
			if !lines[tt.wantTerminal] {
				t.Errorf("Terminal.app line %q not found in script:\n%s", tt.wantTerminal, script)
			}
			if !lines[tt.wantITerm] {
				t.Errorf("iTerm line %q not found in script:\n%s", tt.wantITerm, script)
			}
		})
	}
}
