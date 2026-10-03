package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// PendingKind は待っている要求の種別。表示と整合チェックだけに使い、細かい分類はしない。
type PendingKind string

const (
	PendingBash     PendingKind = "bash"
	PendingEdit     PendingKind = "edit"
	PendingMCP      PendingKind = "mcp"
	PendingQuestion PendingKind = "question"
	PendingPlan     PendingKind = "plan"
	PendingNetwork  PendingKind = "network"
	PendingOther    PendingKind = "other"
)

// PendingRequest は agent がユーザーの応答を待っている要求 1 件。
// Why 単一スロット: 並列要求は hook から開始・終了を対応付けられないため、最後に来た 1 件だけを持つ。
type PendingRequest struct {
	Tool string      `yaml:"tool"`
	Kind PendingKind `yaml:"kind"`
	// Summary は自由記述を保存しない要約（Bash の description / プログラム名 / ファイル名 / MCP の server/tool / 質問の header）。
	// Why not コマンド本文: トークンや鍵が含まれうるため store に残さない
	Summary string `yaml:"summary,omitempty"`
	// InputHash は正規化した tool_input の sha256 先頭 16 桁。本文を持たずに PostToolUse と対応付けるために使う
	InputHash string    `yaml:"input_hash,omitempty"`
	At        time.Time `yaml:"at"`
}

const (
	pendingSummaryMax  = 80
	pendingQuestionMax = 30
	networkAccessTag   = "network-access "
)

// matches は PostToolUse の tool_name / tool_input が、この待ち要求と同じツール呼び出しかを返す。
// tool_use_id が PermissionRequest に無いため、tool_input のハッシュで対応付ける。
func (p PendingRequest) matches(toolName string, toolInput json.RawMessage) bool {
	return p.Tool == toolName && p.InputHash == ClassifyPending(toolName, toolInput, time.Time{}).InputHash
}

// Label は表示用の短い文字列を返す。MCP は Tool 名（mcp__server__tool）が冗長なので Summary だけにする。
func (p PendingRequest) Label() string {
	if p.Kind == PendingMCP && p.Summary != "" {
		return p.Summary
	}
	if p.Summary == "" {
		return p.Tool
	}
	return p.Tool + ": " + p.Summary
}

// canonicalJSON はキー順・空白を揃えた JSON を返す（不正なら生バイト）。
// 生バイトのまま比べないのは、PreToolUse / PermissionRequest と PostToolUse で直列化が同一である保証が無く、
// ずれると待ち要求が PostToolUse で解消されずターン終了まで残るため。
func canonicalJSON(raw json.RawMessage) []byte {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// ClassifyPending は PermissionRequest 等の tool_name / tool_input から待ち要求を作る。
// tool_input が壊れていても分類は落とさず、Summary を空にして返す（待ち状態そのものは記録したいため）。
func ClassifyPending(toolName string, toolInput json.RawMessage, now time.Time) PendingRequest {
	p := PendingRequest{Tool: toolName, Kind: PendingOther, At: now}
	if len(toolInput) > 0 {
		sum := sha256.Sum256(canonicalJSON(toolInput))
		p.InputHash = hex.EncodeToString(sum[:])[:16]
	}

	var in struct {
		Command     json.RawMessage `json:"command"`
		Description string          `json:"description"`
		FilePath    string          `json:"file_path"`
		NotebookPth string          `json:"notebook_path"`
		URL         string          `json:"url"`
		Questions   []struct {
			Header string `json:"header"`
		} `json:"questions"`
	}
	_ = json.Unmarshal(toolInput, &in) // 失敗時はゼロ値のまま Summary 無しで返す

	switch {
	case toolName == "Bash":
		if target, ok := strings.CutPrefix(in.Description, networkAccessTag); ok {
			p.Kind = PendingNetwork
			p.Summary = truncateRunes(strings.TrimSpace(target), pendingSummaryMax)
			break
		}
		p.Kind = PendingBash
		if in.Description != "" {
			p.Summary = truncateRunes(in.Description, pendingSummaryMax)
		} else {
			p.Summary = programName(commandText(in.Command))
		}
	case toolName == "Edit" || toolName == "Write" || toolName == "MultiEdit" || toolName == "NotebookEdit":
		p.Kind = PendingEdit
		path := in.FilePath
		if path == "" {
			path = in.NotebookPth
		}
		if path != "" {
			p.Summary = filepath.Base(path)
		}
	case toolName == "apply_patch":
		p.Kind = PendingEdit
		p.Summary = firstPatchFile(commandText(in.Command))
	case strings.HasPrefix(toolName, "mcp__"):
		p.Kind = PendingMCP
		if server, tool, ok := strings.Cut(strings.TrimPrefix(toolName, "mcp__"), "__"); ok {
			p.Summary = server + "/" + tool
		}
	case toolName == "AskUserQuestion" || toolName == "request_user_input":
		p.Kind = PendingQuestion
		if len(in.Questions) > 0 {
			p.Summary = truncateRunes(in.Questions[0].Header, pendingQuestionMax)
		}
	case toolName == "ExitPlanMode":
		p.Kind = PendingPlan
	case toolName == "WebFetch":
		p.Kind = PendingNetwork
		if u, err := url.Parse(in.URL); err == nil {
			p.Summary = truncateRunes(u.Host, pendingSummaryMax)
		}
	}
	return p
}

// commandText は Codex の command（文字列 / 文字列配列）を改行区切りの 1 文字列にする。
func commandText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []string
	if json.Unmarshal(raw, &parts) == nil {
		return strings.Join(parts, "\n")
	}
	return ""
}

// programName は最初の非空行の先頭トークンの basename を返す。引数に秘密が載りうるので先頭語以外は捨てる。
func programName(command string) string {
	for _, line := range strings.Split(command, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			return filepath.Base(fields[0])
		}
	}
	return ""
}

func firstPatchFile(patch string) string {
	for _, line := range strings.Split(patch, "\n") {
		for _, prefix := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: "} {
			if path, ok := strings.CutPrefix(line, prefix); ok {
				if path = strings.TrimSpace(path); path != "" {
					return filepath.Base(path)
				}
			}
		}
	}
	return ""
}

// truncateRunes は max ルーン以内に収める。超える場合は末尾を … にして max ルーンちょうどにする。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
