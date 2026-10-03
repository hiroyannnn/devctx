package agentview

import (
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// View 由来の識別子。UI はこの値をそのまま表示に使う。
const (
	SourceLive = "live"
	SourceHook = "hook"
)

// View は表示用の状態。model.Context には書き戻さない（store に overlay 値が永続化されるのを避ける）。
type View struct {
	State  model.AgentState
	Reason string
	Source string // SourceLive / SourceHook / ""（未観測）
	// Pending は何を待っているか。hook が記録した要求で、live の待ちと矛盾しないときだけ載せる
	Pending *model.PendingRequest
}

// Detail は待ちの詳細表示を返す。待ち要求があればそのラベル、なければ live の待ち理由。
// 要求を理由より優先するのは、"permission prompt" より "Bash: Run tests" の方が何を許可するかが分かるため。
func (v View) Detail() string {
	if v.Pending != nil {
		return v.Pending.Label()
	}
	return v.Reason
}

// hookView は context の hook 由来の状態をそのまま View にする。
func hookView(ctx model.Context) View {
	v := View{State: ctx.AgentState}
	if ctx.AgentState != "" {
		v.Source = SourceHook
	}
	if ctx.AgentState == model.AgentNeedsInput {
		v.Pending = ctx.PendingRequest
	}
	return v
}

// GitToplevel は dir の git toplevel を返す（git 管理外・失敗は空文字）。
// `git -C` ではなく cmd.Dir 方式にしているのは、他リポジトリの git 操作を cd 方式に揃える運用に合わせるため。
// dir が空だとプロセスの cwd で実行される。agent view の cwd 空は呼び出し側（fetchAt）で除外する。
func GitToplevel(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// normalizePath は比較用にパスを正規化する。symlink 解決に失敗（不在等）したら Clean のみ。
func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// Overlay は全 context の表示状態を返す。キーは context Name。
// claude の context は live（agent view）と hook を突き合わせ、codex / manual は hook 状態のまま。
// 呼び出し側が views[ctx.Name] を直接引けるよう、nil 判定や hook へのフォールバックを持たせず全件を埋める。
//
// 照合は SessionID の完全一致を優先し、無ければ「live セッションの git toplevel が
// context の worktree と一致し、かつ一意」の場合だけ採用する。リポジトリルートや
// パス接頭辞での照合は、別 worktree のセッションを誤って結び付けるので行わない。
func Overlay(contexts []model.Context, snap Snapshot) map[string]View {
	views := make(map[string]View, len(contexts))
	if !snap.OK {
		for _, c := range contexts {
			views[c.Name] = hookView(c)
		}
		return views
	}

	idx := newSessionIndex(contexts, snap.Sessions)
	for _, c := range contexts {
		if c.EffectiveProvider() != model.ProviderClaude {
			views[c.Name] = hookView(c)
			continue
		}
		views[c.Name] = overlayOne(c, snap.FetchedAt, idx)
	}
	return views
}

// sessionIndex は照合に使う live セッションの索引。
type sessionIndex struct {
	bySession      map[string]Session
	liveByWorktree map[string][]Session
	// worktrees は context ごとの正規化済み worktree。照合と候補数の集計で同じ値を使い回す
	worktrees map[string]string
	// fallbackCandidates は worktree 照合の候補になる context（SessionID で live と一致しない Claude context）の数。
	// discover で取り込んだ過去セッションなどが同じ worktree に並ぶと、1 つの live を全員に当てはめてしまうため
	fallbackCandidates map[string]int
}

func newSessionIndex(contexts []model.Context, sessions []Session) sessionIndex {
	idx := sessionIndex{
		bySession:          make(map[string]Session, len(sessions)),
		liveByWorktree:     make(map[string][]Session),
		worktrees:          make(map[string]string, len(contexts)),
		fallbackCandidates: make(map[string]int),
	}
	for _, s := range sessions {
		if _, dup := idx.bySession[s.SessionID]; !dup {
			idx.bySession[s.SessionID] = s
		}
	}
	// 他 context が SessionID で所有しているセッションは worktree 照合の候補から外す
	owned := make(map[string]bool)
	for _, c := range contexts {
		if c.EffectiveProvider() == model.ProviderClaude {
			owned[c.SessionID] = true
		}
		idx.worktrees[c.Name] = normalizePath(c.Worktree)
	}
	for _, s := range sessions {
		if !owned[s.SessionID] && s.Toplevel != "" {
			idx.liveByWorktree[s.Toplevel] = append(idx.liveByWorktree[s.Toplevel], s)
		}
	}
	for _, c := range contexts {
		if c.EffectiveProvider() != model.ProviderClaude {
			continue
		}
		if _, matched := idx.bySession[c.SessionID]; matched {
			continue
		}
		if wt := idx.worktrees[c.Name]; wt != "" {
			idx.fallbackCandidates[wt]++
		}
	}
	return idx
}

// matchSession は ctx に対応する live セッションを返す。SessionID 一致が優先で、
// 無ければ live も context も 1 対 1 に決まる worktree 一致だけを採る（どちらかが複数なら曖昧）。
func (idx sessionIndex) matchSession(ctx model.Context) (Session, bool) {
	if s, ok := idx.bySession[ctx.SessionID]; ok {
		return s, true
	}
	wt := idx.worktrees[ctx.Name]
	if wt == "" {
		return Session{}, false
	}
	if cands := idx.liveByWorktree[wt]; len(cands) == 1 && idx.fallbackCandidates[wt] == 1 {
		return cands[0], true
	}
	return Session{}, false
}

func overlayOne(ctx model.Context, fetchedAt time.Time, idx sessionIndex) View {
	hook := hookView(ctx)
	// ended は終着点。agent view に残っていても hook の終了を覆さない。
	// 取得開始後に届いた hook は live より新しい
	if ctx.AgentState == model.AgentEnded || ctx.AgentStateAt.After(fetchedAt) {
		return hook
	}
	sess, ok := idx.matchSession(ctx)
	if !ok {
		return hook
	}
	state, reason, ok := sess.AgentState()
	if !ok {
		return hook
	}
	v := View{State: state, Reason: reason, Source: SourceLive}
	if state == model.AgentNeedsInput && ctx.PendingRequest != nil && pendingMatchesLive(ctx.PendingRequest, reason) {
		v.Pending = ctx.PendingRequest
	}
	return v
}

// pendingMatchesLive は hook の待ち要求が live の待ち理由と同じものを指していそうかを返す。
// 承認後に別の待ちへ移っても hook 側の要求は残る（Claude には PostToolUse が無い）ため、
// 食い違う要求を表示して誤解させるより捨てる。sandbox / dialog 等は hook で分類できないので載せない。
func pendingMatchesLive(p *model.PendingRequest, waitingFor string) bool {
	switch waitingFor {
	case waitingForPermission:
		return p.Kind != model.PendingQuestion
	case waitingForInput:
		return p.Kind == model.PendingQuestion
	}
	return false
}
