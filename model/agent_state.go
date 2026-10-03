package model

import "time"

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
func AgentStateFromHook(event, notificationType string, current AgentState) (AgentState, bool) {
	switch event {
	case "UserPromptSubmit":
		return AgentRunning, true
	case "PostToolUse":
		// 許可待ち（PermissionRequest）はツール実行後に解消する。承認後に running へ戻すための信号で、
		// 同じ状態の再記録は呼び出し側が間引くので高頻度でも store は書き換わらない
		return AgentRunning, true
	case "PermissionRequest":
		// Codex には Notification が無く、許可待ちは PermissionRequest で通知される
		return AgentNeedsInput, true
	case "Stop":
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
