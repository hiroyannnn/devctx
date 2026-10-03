package model

import (
	"encoding/json"
	"time"
)

// AgentState は hook から観測したエージェントの状態。
// insight の AttentionState（LLM 推論 / 手入力）とは別に、イベントから機械的に決まる。
type AgentState string

const (
	AgentRunning    AgentState = "running"
	AgentNeedsInput AgentState = "needs_input"
	AgentTurnDone   AgentState = "turn_done"
	AgentEnded      AgentState = "ended"
)

// WaitsForUser はユーザーの対応を待っている状態かを返す。
func (s AgentState) WaitsForUser() bool {
	return s == AgentNeedsInput || s == AgentTurnDone
}

// AgentStateFromHook は hook イベントから次の状態を決める。状態を変えない場合は false を返す。
// ended は終着点で、hook では戻さない（再開時は SessionStart の register が状態をクリアする）。
func AgentStateFromHook(event, notificationType string, current AgentState) (AgentState, bool) {
	if current == AgentEnded {
		return "", false
	}
	switch event {
	case "UserPromptSubmit", "PostToolUse":
		// PostToolUse: 許可待ち（PermissionRequest）はツール実行後に解消する。承認後に running へ戻すための信号で、
		// 同じ状態の再記録は呼び出し側が間引くので高頻度でも store は書き換わらない
		return AgentRunning, true
	case "PermissionRequest":
		// Codex には Notification が無く、許可待ちは PermissionRequest で通知される
		return AgentNeedsInput, true
	case "Stop", "Interrupt":
		// Interrupt: Codex の user interrupt。拒否には専用 hook が無く、中断が待ちの解消を知る唯一の手がかり
		return AgentTurnDone, true
	case "SessionEnd":
		return AgentEnded, true
	case "Notification":
		switch notificationType {
		case "permission_prompt", "elicitation_dialog":
			return AgentNeedsInput, true
		case "idle_prompt":
			// ターン完了後の放置でも発火する。確認待ちを入力待ちで上書きしない
			if current == AgentTurnDone {
				return "", false
			}
			return AgentNeedsInput, true
		}
	}
	return "", false
}

// SetAgentState は状態と観測時刻を記録する。
func (c *Context) SetAgentState(state AgentState, now time.Time) {
	c.AgentState = state
	c.AgentStateAt = now
}

// Label は表示用の短いラベルを返す。未観測（空）は空文字。
func (s AgentState) Label() string {
	switch s {
	case AgentNeedsInput:
		return "needs input"
	case AgentTurnDone:
		return "turn done"
	default:
		return string(s)
	}
}

// HookEvent は状態遷移に使う hook イベントの項目。
type HookEvent struct {
	Name             string
	NotificationType string
	ToolName         string
	ToolInput        json.RawMessage
}

// ApplyHookEvent は hook イベントを ctx に適用し、記録すべき変化があれば true を返す。
// 状態と待ち要求の遷移をここに一本化するのは、ロック無しの no-op 判定とロック内の更新が
// 食い違うと「同じ状態で要求だけ違う」イベントを取りこぼすため。
// 呼び出し側の古いイベント破棄（AgentStateAt との比較）はここでは行わない。
func ApplyHookEvent(ctx *Context, ev HookEvent, now time.Time) bool {
	if ctx.AgentState == AgentEnded {
		return false
	}
	next, pending := ctx.AgentState, ctx.PendingRequest
	switch ev.Name {
	case "PermissionRequest":
		next = AgentNeedsInput
		p := ClassifyPending(ev.ToolName, ev.ToolInput, now)
		pending = &p
	case "PreToolUse":
		// Claude / Codex とも許可ダイアログとは別に、質問ツールだけは PreToolUse で待ちに入る
		if ev.ToolName != "AskUserQuestion" && ev.ToolName != "request_user_input" {
			return false
		}
		next = AgentNeedsInput
		p := ClassifyPending(ev.ToolName, ev.ToolInput, now)
		pending = &p
	case "PostToolUse":
		// 並列ツールの別の 1 件が終わっただけなら、まだ許可待ちが続いている
		if pending != nil && !pending.matches(ev.ToolName, ev.ToolInput) {
			return false
		}
		next, pending = AgentRunning, nil
	default:
		state, ok := AgentStateFromHook(ev.Name, ev.NotificationType, ctx.AgentState)
		if !ok {
			return false
		}
		next = state
		// Notification の needs_input は種別だけで要求の中身を持たない。hook で得た要求を消さない
		if state != AgentNeedsInput || ev.Name != "Notification" {
			pending = nil
		}
	}
	if next == ctx.AgentState && samePending(pending, ctx.PendingRequest) {
		return false
	}
	ctx.SetAgentState(next, now)
	ctx.PendingRequest = pending
	return true
}

// samePending は At を除いて待ち要求が同じかを返す。再送された同一 hook で store を書き換えないため。
func samePending(a, b *PendingRequest) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Tool == b.Tool && a.Kind == b.Kind && a.Summary == b.Summary && a.InputHash == b.InputHash
}
