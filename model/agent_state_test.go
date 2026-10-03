package model

import (
	"testing"
	"time"
)

func TestAgentStateFromHook(t *testing.T) {
	tests := []struct {
		name             string
		event            string
		notificationType string
		current          AgentState
		want             AgentState
		wantChange       bool
	}{
		{name: "prompt submitted", event: "UserPromptSubmit", current: AgentTurnDone, want: AgentRunning, wantChange: true},
		{name: "turn finished", event: "Stop", current: AgentRunning, want: AgentTurnDone, wantChange: true},
		{name: "session ended", event: "SessionEnd", current: AgentTurnDone, want: AgentEnded, wantChange: true},
		{name: "permission prompt", event: "Notification", notificationType: "permission_prompt", current: AgentRunning, want: AgentNeedsInput, wantChange: true},
		{name: "elicitation dialog", event: "Notification", notificationType: "elicitation_dialog", current: AgentRunning, want: AgentNeedsInput, wantChange: true},
		{name: "idle after turn keeps turn_done", event: "Notification", notificationType: "idle_prompt", current: AgentTurnDone, wantChange: false},
		{name: "idle while running needs input", event: "Notification", notificationType: "idle_prompt", current: AgentRunning, want: AgentNeedsInput, wantChange: true},
		{name: "auth success is not waiting", event: "Notification", notificationType: "auth_success", current: AgentRunning, wantChange: false},
		{name: "permission request (Codex)", event: "PermissionRequest", current: AgentRunning, want: AgentNeedsInput, wantChange: true},
		{name: "tool finished returns to running after approval", event: "PostToolUse", current: AgentNeedsInput, want: AgentRunning, wantChange: true},
		{name: "ended is terminal", event: "UserPromptSubmit", current: AgentEnded, wantChange: false},
		{name: "unknown event", event: "PreToolUse", current: AgentRunning, wantChange: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := AgentStateFromHook(tt.event, tt.notificationType, tt.current)
			if changed != tt.wantChange {
				t.Fatalf("changed = %v, want %v", changed, tt.wantChange)
			}
			if changed && got != tt.want {
				t.Fatalf("state = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentStateWaitsForUser(t *testing.T) {
	for state, want := range map[AgentState]bool{
		AgentNeedsInput: true,
		AgentTurnDone:   true,
		AgentRunning:    false,
		AgentEnded:      false,
		"":              false,
	} {
		if got := state.WaitsForUser(); got != want {
			t.Fatalf("%q.WaitsForUser() = %v, want %v", state, got, want)
		}
	}
}

func TestContextSetAgentState(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	var ctx Context
	ctx.SetAgentState(AgentRunning, now)
	if ctx.AgentState != AgentRunning || !ctx.AgentStateAt.Equal(now) {
		t.Fatalf("SetAgentState did not record state and time: %+v", ctx)
	}
}
