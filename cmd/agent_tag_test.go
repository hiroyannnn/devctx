package cmd

import (
	"testing"

	"github.com/hiroyannnn/devctx/agentview"
	"github.com/hiroyannnn/devctx/model"
)

func TestAgentTag(t *testing.T) {
	tests := []struct {
		name string
		ctx  model.Context
		want string
	}{
		{name: "legacy context", ctx: model.Context{}, want: "claude"},
		{name: "codex running", ctx: model.Context{Provider: model.ProviderCodex, AgentState: model.AgentRunning}, want: "codex · running"},
		{name: "claude turn done", ctx: model.Context{AgentState: model.AgentTurnDone}, want: "claude · turn done"},
		{name: "needs input", ctx: model.Context{AgentState: model.AgentNeedsInput}, want: "claude · needs input"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentTag(tt.ctx, agentview.HookView(tt.ctx)); got != tt.want {
				t.Fatalf("agentTag = %q, want %q", got, tt.want)
			}
		})
	}
}
