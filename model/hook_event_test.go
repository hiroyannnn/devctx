package model

import (
	"encoding/json"
	"testing"
	"time"
)

var hookNow = time.Date(2026, 10, 4, 13, 0, 0, 0, time.UTC)

func bashInput(cmd string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"command": cmd, "description": "d-" + cmd})
	return b
}

func pendingFor(tool string, input json.RawMessage) *PendingRequest {
	p := ClassifyPending(tool, input, hookNow.Add(-time.Minute))
	return &p
}

func TestApplyHookEvent(t *testing.T) {
	tests := []struct {
		name        string
		ctx         Context
		ev          HookEvent
		wantChanged bool
		wantState   AgentState
		wantPending string // 期待する Pending の Summary。空は nil を期待
	}{
		{
			name:        "permission request records pending",
			ctx:         Context{AgentState: AgentRunning},
			ev:          HookEvent{Name: "PermissionRequest", ToolName: "Bash", ToolInput: bashInput("ls")},
			wantChanged: true, wantState: AgentNeedsInput, wantPending: "d-ls",
		},
		{
			name:        "same state different request is a change",
			ctx:         Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))},
			ev:          HookEvent{Name: "PermissionRequest", ToolName: "Bash", ToolInput: bashInput("pwd")},
			wantChanged: true, wantState: AgentNeedsInput, wantPending: "d-pwd",
		},
		{
			name:        "same state same request is not a change",
			ctx:         Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))},
			ev:          HookEvent{Name: "PermissionRequest", ToolName: "Bash", ToolInput: bashInput("ls")},
			wantChanged: false, wantState: AgentNeedsInput, wantPending: "d-ls",
		},
		{
			name:        "pre tool use AskUserQuestion records question",
			ctx:         Context{AgentState: AgentRunning},
			ev:          HookEvent{Name: "PreToolUse", ToolName: "AskUserQuestion", ToolInput: json.RawMessage(`{"questions":[{"header":"H"}]}`)},
			wantChanged: true, wantState: AgentNeedsInput, wantPending: "H",
		},
		{
			name:        "pre tool use request_user_input records question",
			ctx:         Context{AgentState: AgentRunning},
			ev:          HookEvent{Name: "PreToolUse", ToolName: "request_user_input", ToolInput: json.RawMessage(`{"questions":[{"header":"Q"}]}`)},
			wantChanged: true, wantState: AgentNeedsInput, wantPending: "Q",
		},
		{
			name:        "pre tool use other tool is ignored",
			ctx:         Context{AgentState: AgentRunning},
			ev:          HookEvent{Name: "PreToolUse", ToolName: "Bash", ToolInput: bashInput("ls")},
			wantChanged: false, wantState: AgentRunning,
		},
		{
			name:        "post tool use matching pending clears and runs",
			ctx:         Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))},
			ev:          HookEvent{Name: "PostToolUse", ToolName: "Bash", ToolInput: bashInput("ls")},
			wantChanged: true, wantState: AgentRunning,
		},
		{
			name:        "post tool use of another parallel tool keeps waiting",
			ctx:         Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))},
			ev:          HookEvent{Name: "PostToolUse", ToolName: "Bash", ToolInput: bashInput("pwd")},
			wantChanged: false, wantState: AgentNeedsInput, wantPending: "d-ls",
		},
		{
			name:        "post tool use with different tool name keeps waiting",
			ctx:         Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))},
			ev:          HookEvent{Name: "PostToolUse", ToolName: "Edit", ToolInput: bashInput("ls")},
			wantChanged: false, wantState: AgentNeedsInput, wantPending: "d-ls",
		},
		{
			name:        "post tool use without pending returns to running",
			ctx:         Context{AgentState: AgentNeedsInput},
			ev:          HookEvent{Name: "PostToolUse", ToolName: "Bash", ToolInput: bashInput("ls")},
			wantChanged: true, wantState: AgentRunning,
		},
		{
			name:        "prompt submit clears pending",
			ctx:         Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))},
			ev:          HookEvent{Name: "UserPromptSubmit"},
			wantChanged: true, wantState: AgentRunning,
		},
		{
			name:        "stop clears pending",
			ctx:         Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))},
			ev:          HookEvent{Name: "Stop"},
			wantChanged: true, wantState: AgentTurnDone,
		},
		{
			name:        "interrupt clears pending and ends the turn",
			ctx:         Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))},
			ev:          HookEvent{Name: "Interrupt"},
			wantChanged: true, wantState: AgentTurnDone,
		},
		{
			name:        "session end clears pending",
			ctx:         Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))},
			ev:          HookEvent{Name: "SessionEnd"},
			wantChanged: true, wantState: AgentEnded,
		},
		{
			name:        "notification permission_prompt keeps existing pending",
			ctx:         Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))},
			ev:          HookEvent{Name: "Notification", NotificationType: "permission_prompt"},
			wantChanged: false, wantState: AgentNeedsInput, wantPending: "d-ls",
		},
		{
			name:        "notification moves running to needs_input without pending",
			ctx:         Context{AgentState: AgentRunning},
			ev:          HookEvent{Name: "Notification", NotificationType: "permission_prompt"},
			wantChanged: true, wantState: AgentNeedsInput,
		},
		{
			name:        "idle prompt after turn done changes nothing",
			ctx:         Context{AgentState: AgentTurnDone},
			ev:          HookEvent{Name: "Notification", NotificationType: "idle_prompt"},
			wantChanged: false, wantState: AgentTurnDone,
		},
		{
			name:        "ended is terminal",
			ctx:         Context{AgentState: AgentEnded},
			ev:          HookEvent{Name: "PermissionRequest", ToolName: "Bash", ToolInput: bashInput("ls")},
			wantChanged: false, wantState: AgentEnded,
		},
		{
			name:        "unknown event",
			ctx:         Context{AgentState: AgentRunning},
			ev:          HookEvent{Name: "SomethingNew"},
			wantChanged: false, wantState: AgentRunning,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.ctx
			if got := ApplyHookEvent(&ctx, tt.ev, hookNow); got != tt.wantChanged {
				t.Fatalf("changed = %v, want %v", got, tt.wantChanged)
			}
			if ctx.AgentState != tt.wantState {
				t.Fatalf("state = %q, want %q", ctx.AgentState, tt.wantState)
			}
			switch {
			case tt.wantPending == "" && ctx.PendingRequest != nil:
				t.Fatalf("pending = %+v, want nil", ctx.PendingRequest)
			case tt.wantPending != "" && (ctx.PendingRequest == nil || ctx.PendingRequest.Summary != tt.wantPending):
				t.Fatalf("pending = %+v, want summary %q", ctx.PendingRequest, tt.wantPending)
			}
			if tt.wantChanged && !ctx.AgentStateAt.Equal(hookNow) {
				t.Errorf("AgentStateAt = %v, want now", ctx.AgentStateAt)
			}
			if !tt.wantChanged && ctx.AgentStateAt != tt.ctx.AgentStateAt {
				t.Errorf("unchanged event must keep AgentStateAt")
			}
		})
	}
}

func TestApplyHookEvent_PendingAtDoesNotCountAsDifference(t *testing.T) {
	ctx := Context{AgentState: AgentNeedsInput, PendingRequest: pendingFor("Bash", bashInput("ls"))}
	ev := HookEvent{Name: "PermissionRequest", ToolName: "Bash", ToolInput: bashInput("ls")}
	if ApplyHookEvent(&ctx, ev, hookNow.Add(time.Hour)) {
		t.Fatal("a redelivered identical request must not rewrite the store")
	}
}
