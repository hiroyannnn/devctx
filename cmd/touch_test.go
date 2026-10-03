package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

var touchNow = time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)

func TestApplyLastSeen_QuickThrottlesWithinFiveMinutes(t *testing.T) {
	ctx := &model.Context{LastSeen: touchNow.Add(-2 * time.Minute)}
	if applyLastSeen(ctx, touchNow, true) {
		t.Fatalf("quick touch within 5 minutes should be throttled")
	}
	if !ctx.LastSeen.Equal(touchNow.Add(-2 * time.Minute)) {
		t.Fatalf("throttled touch should not change last_seen")
	}
}

func TestApplyLastSeen_AccumulatesActiveTime(t *testing.T) {
	ctx := &model.Context{LastSeen: touchNow.Add(-10 * time.Minute)}
	if !applyLastSeen(ctx, touchNow, true) {
		t.Fatalf("quick touch after 10 minutes should update")
	}
	if !ctx.LastSeen.Equal(touchNow) || ctx.TotalTime != 10*time.Minute {
		t.Fatalf("last_seen=%v total=%v, want now and 10m", ctx.LastSeen, ctx.TotalTime)
	}
}

func TestApplyLastSeen_NonQuickAlwaysUpdates(t *testing.T) {
	ctx := &model.Context{LastSeen: touchNow.Add(-1 * time.Minute)}
	if !applyLastSeen(ctx, touchNow, false) {
		t.Fatalf("non-quick touch should always update")
	}
}

func TestApplyHookState(t *testing.T) {
	ctx := &model.Context{AgentState: model.AgentRunning}
	if !applyHookState(ctx, hookInput{HookEventName: "Stop"}, touchNow) {
		t.Fatalf("Stop should change state")
	}
	if ctx.AgentState != model.AgentTurnDone || !ctx.AgentStateAt.Equal(touchNow) {
		t.Fatalf("state=%q at=%v, want turn_done at now", ctx.AgentState, ctx.AgentStateAt)
	}
	if applyHookState(ctx, hookInput{HookEventName: "Notification", NotificationType: "auth_success"}, touchNow.Add(time.Minute)) {
		t.Fatalf("auth_success should not change state")
	}
	if !ctx.AgentStateAt.Equal(touchNow) {
		t.Fatalf("unchanged state should keep its timestamp")
	}
}

func TestParseHookInput(t *testing.T) {
	in, err := parseHookInput(strings.NewReader(`{"session_id":"c1","hook_event_name":"Notification","notification_type":"permission_prompt"}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if in.SessionID != "c1" || in.HookEventName != "Notification" || in.NotificationType != "permission_prompt" {
		t.Fatalf("unexpected hook input: %+v", in)
	}
}

func TestApplyHookState_SameStateIsNotAChange(t *testing.T) {
	ctx := &model.Context{AgentState: model.AgentRunning, AgentStateAt: touchNow}
	if applyHookState(ctx, hookInput{HookEventName: "UserPromptSubmit"}, touchNow.Add(time.Minute)) {
		t.Fatalf("running -> running should not be reported as a change (avoids rewriting the store on every prompt)")
	}
}

func TestParseHookInput_LongLine(t *testing.T) {
	long := strings.Repeat("x", 100*1024)
	in, err := parseHookInput(strings.NewReader(`{"session_id":"c1","hook_event_name":"Stop","message":"` + long + `"}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if in.SessionID != "c1" || in.HookEventName != "Stop" {
		t.Fatalf("hook input over 64KB should still be parsed: %+v", in)
	}
}

func TestParseHookInput_Empty(t *testing.T) {
	in, err := parseHookInput(strings.NewReader(""))
	if err != nil || in != (hookInput{}) {
		t.Fatalf("empty input = %+v, %v; want zero value and nil", in, err)
	}
}

func TestResolveTouchTarget(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "claude-ctx", SessionID: "same-id"},
		{Name: "codex-ctx", Provider: model.ProviderCodex, SessionID: "codex-id"},
	}}
	tests := []struct {
		name      string
		provider  model.Provider
		sessionID string
		args      []string
		want      string
	}{
		{"claude session", model.ProviderClaude, "same-id", nil, "claude-ctx"},
		{"codex session", model.ProviderCodex, "codex-id", nil, "codex-ctx"},
		{"provider mismatch does not resolve", model.ProviderClaude, "codex-id", nil, ""},
		{"unknown session", model.ProviderCodex, "nope", nil, ""},
		{"explicit name wins", model.ProviderCodex, "codex-id", []string{"other"}, "other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveTouchTarget(store, tt.provider, tt.sessionID, tt.args); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
