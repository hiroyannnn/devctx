package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func TestAdapterFor(t *testing.T) {
	a, ok := adapterFor(model.ProviderClaude)
	if !ok || a.Provider() != model.ProviderClaude {
		t.Fatalf("adapterFor(claude) = %v, %v", a, ok)
	}
	if _, ok := adapterFor(model.ProviderManual); ok {
		t.Fatalf("manual must not have an adapter")
	}
}

func TestAgentResumeCommand_NoAdapter(t *testing.T) {
	_, err := agentResumeCommand(model.Context{Provider: model.ProviderManual})
	want := `resume is not supported for provider "manual" yet`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestClaudeAdapter_AgentCommand(t *testing.T) {
	a := claudeAdapter{}
	if got, _ := a.AgentCommand(model.Context{SessionID: "s1"}); got != "claude --resume 's1'" {
		t.Fatalf("got %q", got)
	}
	if got, _ := a.AgentCommand(model.Context{}); got != "claude" {
		t.Fatalf("got %q", got)
	}
}

func TestClaudeAdapter_Discover(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "-w-proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := `{"cwd":"/w/proj","slug":"cool-slug"}` + "\n" + `{"type":"user"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}

	store := &model.Store{}
	store.Add(model.Context{Name: "reg", SessionID: "sess-2"})
	got, err := claudeAdapter{}.Discover(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	s := got[0]
	if s.Provider != model.ProviderClaude || s.SessionID != "sess-1" || s.ProjectPath != "/w/proj" ||
		s.SessionName != "cool-slug" || s.MessageCount != 2 || s.ProjectHash != "-w-proj" || s.IsRegistered ||
		!strings.HasSuffix(s.TranscriptPath, "sess-1.jsonl") {
		t.Fatalf("unexpected: %+v", s)
	}
}
