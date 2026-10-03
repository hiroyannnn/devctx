package cmd

import (
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func TestResumeShellCommand(t *testing.T) {
	tests := []struct {
		name string
		ctx  model.Context
		want string
	}{
		{
			name: "claude with session",
			ctx:  model.Context{Worktree: "/w/feat-x", SessionID: "s1", Provider: model.ProviderClaude},
			want: "cd '/w/feat-x' && claude --resume 's1'",
		},
		{
			name: "legacy context without provider is claude",
			ctx:  model.Context{Worktree: "/w/feat-x", SessionID: "s1"},
			want: "cd '/w/feat-x' && claude --resume 's1'",
		},
		{
			name: "claude without session starts fresh",
			ctx:  model.Context{Worktree: "/w/feat-x"},
			want: "cd '/w/feat-x' && claude",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resumeShellCommand(tt.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("resumeShellCommand = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResumeShellCommand_UnsupportedProvider(t *testing.T) {
	_, err := resumeShellCommand(model.Context{Worktree: "/w/feat-x", SessionID: "x1", Provider: model.ProviderCodex})
	if err == nil || !strings.Contains(err.Error(), "codex") {
		t.Fatalf("err = %v, want unsupported provider error mentioning codex", err)
	}
}

func TestNeedsSessionNameRefresh(t *testing.T) {
	if !needsSessionNameRefresh(model.Context{TranscriptPath: "/t/a.jsonl"}) {
		t.Fatalf("claude context with transcript and no session name should be refreshed")
	}
	if needsSessionNameRefresh(model.Context{TranscriptPath: "/t/a.jsonl", SessionName: "slug"}) {
		t.Fatalf("context that already has a session name should not be refreshed")
	}
	if needsSessionNameRefresh(model.Context{TranscriptPath: "/t/a.jsonl", Provider: model.ProviderCodex}) {
		t.Fatalf("non-claude transcripts should not be parsed as Claude Code transcripts")
	}
}
