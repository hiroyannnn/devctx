// Package agentview は `claude agents --json`（Claude Code の agent view）から
// 生きているセッションの状態を取得し、hook 由来の状態に重ねて表示するための層。
//
// hook は登録・終了の信号源として残し、agent view は「いま生きているか・何待ちか」の
// 参照専用ソースとして扱う。store には一切書き戻さない。
package agentview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// Session は agent view が返す 1 セッション。
type Session struct {
	PID        int
	SessionID  string
	Cwd        string
	Kind       string
	Name       string
	Status     string // busy / waiting / idle
	WaitingFor string // waiting のときの理由（permission prompt 等）
	State      string // background セッションの state
	StartedAt  time.Time
}

// Snapshot はある時点の agent view。
type Snapshot struct {
	Sessions []Session
	// FetchedAt は取得を「開始」した時刻。取得中に届いた hook を古い live で上書きしないための基準
	FetchedAt time.Time
	// OK が false なら live 情報なし（取得失敗）。呼び出し側は hook 状態にフォールバックする
	OK bool
}

type rawSession struct {
	PID        int    `json:"pid"`
	Cwd        string `json:"cwd"`
	Kind       string `json:"kind"`
	StartedAt  int64  `json:"startedAt"`
	SessionID  string `json:"sessionId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	WaitingFor string `json:"waitingFor"`
	State      string `json:"state"`
}

// Parse は `claude agents --json` の出力を解釈する。
// 未知フィールドは無視し、sessionId か status が無い行は状態として使えないので捨てる。
func Parse(data []byte) ([]Session, error) {
	var rows []rawSession
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parse claude agents output: %w", err)
	}
	sessions := make([]Session, 0, len(rows))
	for _, r := range rows {
		if r.SessionID == "" || r.Status == "" {
			continue
		}
		s := Session{
			PID: r.PID, SessionID: r.SessionID, Cwd: r.Cwd, Kind: r.Kind, Name: r.Name,
			Status: r.Status, WaitingFor: r.WaitingFor, State: r.State,
		}
		if r.StartedAt > 0 {
			s.StartedAt = time.UnixMilli(r.StartedAt)
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// AgentState は agent view の status を devctx の AgentState に写す。
// 未知の status は live 状態なし（ok=false）として扱い、hook 状態を残す。
func (s Session) AgentState() (state model.AgentState, reason string, ok bool) {
	switch s.Status {
	case "busy":
		return model.AgentRunning, "", true
	case "waiting":
		return model.AgentNeedsInput, s.WaitingFor, true
	case "idle":
		return model.AgentTurnDone, "", true
	}
	return "", "", false
}

// Runner は agent view の生出力を返す。テストでは fake を注入し、実 claude は実行しない。
type Runner func(ctx context.Context) ([]byte, error)

const (
	fetchTimeout   = 2 * time.Second
	maxOutputBytes = 1 << 20
)

// DefaultRunner は `claude agents --json` を実行する。
// stdin は渡さず、出力上限を設けて暴走出力でメモリを食わないようにする。
func DefaultRunner(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "agents", "--json")
	var buf bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &buf, remaining: maxOutputBytes}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// limitedWriter は上限超過で書き込みエラーにし、子プロセスを止める。
type limitedWriter struct {
	w         io.Writer
	remaining int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > l.remaining {
		return 0, fmt.Errorf("claude agents output exceeds %d bytes", maxOutputBytes)
	}
	l.remaining -= len(p)
	return l.w.Write(p)
}

// Fetch は run（nil なら DefaultRunner）で agent view を取得する。
// 失敗・タイムアウト・パースエラーは OK=false。空配列は OK=true だが「live 情報なし」であり、
// 不在をもって終了とは推論しない（制限環境では生存中でも空が返りうる）。
func Fetch(ctx context.Context, run Runner) Snapshot {
	return fetchAt(ctx, run, time.Now)
}

func fetchAt(ctx context.Context, run Runner, now func() time.Time) Snapshot {
	if run == nil {
		run = DefaultRunner
	}
	snap := Snapshot{FetchedAt: now()}
	data, err := run(ctx)
	if err != nil {
		return snap
	}
	sessions, err := Parse(data)
	if err != nil {
		return snap
	}
	snap.Sessions = sessions
	snap.OK = true
	return snap
}
