package agentview

import (
	"os/exec"
	"path/filepath"
	"strings"

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
}

// HookView は context の hook 由来の状態をそのまま View にする。
func HookView(ctx model.Context) View {
	v := View{State: ctx.AgentState}
	if ctx.AgentState != "" {
		v.Source = SourceHook
	}
	return v
}

// ViewFor は overlay 結果から ctx の View を引く。overlay 対象外（codex 等）や nil は hook 状態。
func ViewFor(views map[string]View, ctx model.Context) View {
	if v, ok := views[ctx.Name]; ok {
		return v
	}
	return HookView(ctx)
}

// GitToplevel は cwd の git toplevel を返す（git 管理外・失敗は空文字）。
// cmd/register.go の getWorktreeRoot と同じく cmd.Dir 方式で、`git -C` は使わない。
func GitToplevel(cwd string) string {
	if cwd == "" {
		return ""
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
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

// Overlay は claude の context ごとに、live（agent view）と hook を突き合わせた表示状態を返す。
// キーは context Name。codex / manual は対象外。
//
// 照合は SessionID の完全一致を優先し、無ければ「live セッションの git toplevel が
// context の worktree と一致し、かつ一意」の場合だけ採用する。リポジトリルートや
// パス接頭辞での照合は、別 worktree のセッションを誤って結び付けるので行わない。
func Overlay(contexts []model.Context, snap Snapshot, toplevel func(cwd string) string) map[string]View {
	if toplevel == nil {
		toplevel = GitToplevel
	}
	views := make(map[string]View)
	if len(contexts) == 0 {
		return views
	}

	bySession := make(map[string]Session, len(snap.Sessions))
	for _, s := range snap.Sessions {
		if _, dup := bySession[s.SessionID]; !dup {
			bySession[s.SessionID] = s
		}
	}
	// 他 context が SessionID で所有しているセッションは worktree 照合の候補から外す
	owned := make(map[string]bool)
	for _, c := range contexts {
		if c.EffectiveProvider() == model.ProviderClaude && c.SessionID != "" {
			owned[c.SessionID] = true
		}
	}

	// git 呼び出しは cwd ごとに 1 回だけ
	topCache := make(map[string]string)
	liveByWorktree := make(map[string][]Session)
	if snap.OK {
		for _, s := range snap.Sessions {
			if owned[s.SessionID] {
				continue
			}
			top, ok := topCache[s.Cwd]
			if !ok {
				top = normalizePath(toplevel(s.Cwd))
				topCache[s.Cwd] = top
			}
			if top != "" {
				liveByWorktree[top] = append(liveByWorktree[top], s)
			}
		}
	}

	// worktree 照合の候補になる context（SessionID で live と一致しない Claude context）を worktree ごとに数える。
	// discover で取り込んだ過去セッションなどが同じ worktree に並ぶと、1 つの live を全員に当てはめてしまうため
	fallbackCandidates := make(map[string]int)
	for _, c := range contexts {
		if c.EffectiveProvider() != model.ProviderClaude {
			continue
		}
		if _, matched := bySession[c.SessionID]; matched && c.SessionID != "" {
			continue
		}
		if wt := normalizePath(c.Worktree); wt != "" {
			fallbackCandidates[wt]++
		}
	}

	for _, ctx := range contexts {
		if ctx.EffectiveProvider() != model.ProviderClaude {
			continue
		}
		views[ctx.Name] = overlayOne(ctx, snap, bySession, liveByWorktree, fallbackCandidates)
	}
	return views
}

func overlayOne(ctx model.Context, snap Snapshot, bySession map[string]Session, liveByWorktree map[string][]Session, fallbackCandidates map[string]int) View {
	hook := HookView(ctx)
	if !snap.OK {
		return hook
	}
	// ended は終着点。agent view に残っていても hook の終了を覆さない
	if ctx.AgentState == model.AgentEnded {
		return hook
	}
	// 取得開始後に届いた hook は live より新しい
	if ctx.AgentStateAt.After(snap.FetchedAt) {
		return hook
	}

	sess, found := Session{}, false
	if ctx.SessionID != "" {
		sess, found = bySession[ctx.SessionID]
	}
	if !found {
		if wt := normalizePath(ctx.Worktree); wt != "" {
			// live も context も 1 対 1 に決まるときだけ当てはめる。どちらかが複数なら曖昧なので hook を残す
			if cands := liveByWorktree[wt]; len(cands) == 1 && fallbackCandidates[wt] == 1 {
				sess, found = cands[0], true
			}
		}
	}
	if !found {
		return hook
	}
	state, reason, ok := sess.AgentState()
	if !ok {
		return hook
	}
	return View{State: state, Reason: reason, Source: SourceLive}
}
