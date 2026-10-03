package cmd

import (
	"encoding/json"
	"errors"
	"time"
)

// codexSessionMeta は rollout ファイル 1 行目 (session_meta) のうち devctx が使う項目。
type codexSessionMeta struct {
	SessionID    string
	Cwd          string
	GitBranch    string
	Originator   string
	Source       string // source が文字列のときだけ入る（オブジェクトは種別判定に使わないので空）
	ThreadSource string
	Timestamp    time.Time
}

// parseCodexSessionMeta は rollout の 1 行目を解釈する。
// Codex のバージョンで項目が増減するため、必要な項目以外は厳密に検証しない。
func parseCodexSessionMeta(line []byte) (codexSessionMeta, error) {
	var raw struct {
		Type    string `json:"type"`
		Payload struct {
			ID           string          `json:"id"`
			SessionID    string          `json:"session_id"`
			Timestamp    string          `json:"timestamp"`
			Cwd          string          `json:"cwd"`
			Originator   string          `json:"originator"`
			Source       json.RawMessage `json:"source"`
			ThreadSource string          `json:"thread_source"`
			Git          *struct {
				Branch string `json:"branch"`
			} `json:"git"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return codexSessionMeta{}, err
	}
	if raw.Type != "session_meta" {
		return codexSessionMeta{}, errors.New("first line is not session_meta")
	}
	p := raw.Payload
	id := p.ID
	if id == "" {
		id = p.SessionID
	}
	if id == "" {
		return codexSessionMeta{}, errors.New("session_meta has no id")
	}

	meta := codexSessionMeta{
		SessionID:    id,
		Cwd:          p.Cwd,
		Originator:   p.Originator,
		ThreadSource: p.ThreadSource,
	}
	// source はオブジェクト（subagent 等）のこともあるため、文字列として読めたときだけ採用する
	var src string
	if json.Unmarshal(p.Source, &src) == nil {
		meta.Source = src
	}
	if p.Git != nil {
		meta.GitBranch = p.Git.Branch
	}
	if t, err := time.Parse(time.RFC3339Nano, p.Timestamp); err == nil {
		meta.Timestamp = t
	}
	return meta, nil
}

// isInteractiveCodexSession は人間が対話しているセッションだけを true にする。
// codex exec・guardian review・subagent 等の自動生成セッションは数が多く、
// カンバンに取り込むとノイズになるため除外する。
// thread_source は新しい Codex でのみ出力されるので、無い場合は source / originator で推定する。
func isInteractiveCodexSession(meta codexSessionMeta) bool {
	if meta.ThreadSource != "" {
		return meta.ThreadSource == "user"
	}
	return meta.Source != "exec" &&
		meta.Originator != "Claude Code" &&
		meta.Originator != "codex_exec"
}
