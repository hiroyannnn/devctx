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
			if got := agentTag(tt.ctx, agentview.View{State: tt.ctx.AgentState}); got != tt.want {
				t.Fatalf("agentTag = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentTag_PendingReplacesReason(t *testing.T) {
	ctx := model.Context{AgentState: model.AgentNeedsInput}
	pending := &model.PendingRequest{Tool: "Bash", Kind: model.PendingBash, Summary: "Run tests"}
	tests := []struct {
		name string
		view agentview.View
		want string
	}{
		{"pending label", agentview.View{State: model.AgentNeedsInput, Pending: pending}, "claude · needs input · Bash: Run tests"},
		{"pending wins over waitingFor", agentview.View{State: model.AgentNeedsInput, Reason: "permission prompt", Pending: pending}, "claude · needs input · Bash: Run tests"},
		{"reason without pending", agentview.View{State: model.AgentNeedsInput, Reason: "permission prompt"}, "claude · needs input · permission prompt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentTag(ctx, tt.view); got != tt.want {
				t.Fatalf("agentTag = %q, want %q", got, tt.want)
			}
		})
	}
}
