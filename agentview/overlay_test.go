package agentview

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

var (
	t0 = time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	// 取得開始は t0。hook がこれより前なら live が勝つ
	fetched = t0
)

func okSnap(sessions ...Session) Snapshot {
	return Snapshot{Sessions: sessions, FetchedAt: fetched, OK: true}
}

func claudeCtx(name, worktree, sessionID string) model.Context {
	return model.Context{Name: name, Worktree: worktree, SessionID: sessionID}
}

func TestOverlayMatchBySessionID(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "s1")
	snap := okSnap(Session{SessionID: "s1", Cwd: "/elsewhere", Status: "waiting", WaitingFor: "permission prompt"})
	got := Overlay([]model.Context{ctx}, snap)["a"]
	want := View{State: model.AgentNeedsInput, Reason: "permission prompt", Source: SourceLive}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestOverlayMatchByWorktreeFallback(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "")
	snap := okSnap(Session{SessionID: "s9", Cwd: "/w/a/sub", Toplevel: "/w/a", Status: "busy"})
	got := Overlay([]model.Context{ctx}, snap)["a"]
	if got.State != model.AgentRunning || got.Source != SourceLive {
		t.Errorf("%+v", got)
	}
}

func TestOverlayWorktreeMatchIsExactNotPrefixNorRepoRoot(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "")
	hook := model.AgentTurnDone
	ctx.AgentState = hook
	// cwd は worktree の配下だが toplevel は親リポジトリ → 一致させない
	snap := okSnap(Session{SessionID: "s9", Cwd: "/w/a/x", Toplevel: "/w", Status: "busy"})
	got := Overlay([]model.Context{ctx}, snap)["a"]
	if got != (View{State: hook, Source: SourceHook}) {
		t.Errorf("%+v", got)
	}
	// 非 git cwd（toplevel 空）は worktree 照合に使わない
	snap = okSnap(Session{SessionID: "s9", Cwd: "/w/a/x", Status: "busy"})
	got = Overlay([]model.Context{ctx}, snap)["a"]
	if got.Source != SourceHook {
		t.Errorf("%+v", got)
	}
}

func TestOverlayAmbiguousWorktreeKeepsHook(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "")
	ctx.AgentState = model.AgentRunning
	snap := okSnap(
		Session{SessionID: "x1", Cwd: "/w/a", Toplevel: "/w/a", Status: "idle"},
		Session{SessionID: "x2", Cwd: "/w/a", Toplevel: "/w/a", Status: "busy"},
	)
	got := Overlay([]model.Context{ctx}, snap)["a"]
	if got != (View{State: model.AgentRunning, Source: SourceHook}) {
		t.Errorf("%+v", got)
	}
}

func TestOverlaySessionIDWinsOverAmbiguity(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "x2")
	snap := okSnap(
		Session{SessionID: "x1", Cwd: "/w/a", Toplevel: "/w/a", Status: "idle"},
		Session{SessionID: "x2", Cwd: "/w/a", Toplevel: "/w/a", Status: "busy"},
	)
	got := Overlay([]model.Context{ctx}, snap)["a"]
	if got.State != model.AgentRunning || got.Source != SourceLive {
		t.Errorf("%+v", got)
	}
}

func TestOverlayWorktreeFallbackSkipsSessionOwnedByOtherContext(t *testing.T) {
	// 別 context が SessionID で所有している live セッションを worktree 経由で横取りしない
	a := claudeCtx("a", "/w/a", "")
	b := claudeCtx("b", "/w/a", "x1")
	snap := okSnap(Session{SessionID: "x1", Cwd: "/w/a", Toplevel: "/w/a", Status: "busy"})
	got := Overlay([]model.Context{a, b}, snap)
	if got["a"].Source == SourceLive {
		t.Errorf("a must not steal b's session: %+v", got["a"])
	}
	if got["b"].Source != SourceLive {
		t.Errorf("b: %+v", got["b"])
	}
}

func TestOverlayHookPrecedence(t *testing.T) {
	snap := okSnap(Session{SessionID: "s1", Status: "busy"})

	ended := claudeCtx("ended", "/w", "s1")
	ended.AgentState = model.AgentEnded
	ended.AgentStateAt = t0.Add(-time.Hour)

	fresher := claudeCtx("fresher", "/w", "s1")
	fresher.AgentState = model.AgentNeedsInput
	fresher.AgentStateAt = t0.Add(time.Second)

	older := claudeCtx("older", "/w", "s1")
	older.AgentState = model.AgentNeedsInput
	older.AgentStateAt = t0.Add(-time.Second)

	got := Overlay([]model.Context{ended, fresher, older}, snap)
	if got["ended"] != (View{State: model.AgentEnded, Source: SourceHook}) {
		t.Errorf("ended: %+v", got["ended"])
	}
	if got["fresher"] != (View{State: model.AgentNeedsInput, Source: SourceHook}) {
		t.Errorf("fresher: %+v", got["fresher"])
	}
	if got["older"] != (View{State: model.AgentRunning, Source: SourceLive}) {
		t.Errorf("older: %+v", got["older"])
	}
}

func TestOverlayIdleClearsReason(t *testing.T) {
	ctx := claudeCtx("a", "/w", "s1")
	snap := okSnap(Session{SessionID: "s1", Status: "idle", WaitingFor: "stale"})
	if got := Overlay([]model.Context{ctx}, snap)["a"]; got != (View{State: model.AgentTurnDone, Source: SourceLive}) {
		t.Errorf("%+v", got)
	}
}

func TestOverlayFallbacks(t *testing.T) {
	hooked := claudeCtx("hooked", "/w", "s1")
	hooked.AgentState = model.AgentTurnDone
	bare := claudeCtx("bare", "/w2", "s2")

	// 取得失敗
	got := Overlay([]model.Context{hooked, bare}, Snapshot{FetchedAt: fetched})
	if got["hooked"] != (View{State: model.AgentTurnDone, Source: SourceHook}) || got["bare"] != (View{}) {
		t.Errorf("%+v", got)
	}
	// OK だが空: 不在を ended と推論しない
	got = Overlay([]model.Context{hooked, bare}, okSnap())
	if got["hooked"] != (View{State: model.AgentTurnDone, Source: SourceHook}) || got["bare"] != (View{}) {
		t.Errorf("%+v", got)
	}
	// 未知 status
	got = Overlay([]model.Context{hooked}, okSnap(Session{SessionID: "s1", Status: "weird"}))
	if got["hooked"].Source != SourceHook {
		t.Errorf("%+v", got)
	}
}

// codex は live を重ねず hook 状態の View になる。全 context に View があり、呼び出し側が直接引ける。
func TestOverlayNonClaudeGetsHookView(t *testing.T) {
	codex := claudeCtx("cx", "/w", "s1")
	codex.Provider = model.ProviderCodex
	codex.AgentState = model.AgentTurnDone
	bare := claudeCtx("bare", "/w2", "")
	bare.Provider = model.ProviderCodex
	snap := okSnap(Session{SessionID: "s1", Status: "busy"})
	for name, got := range Overlay([]model.Context{codex, bare}, snap) {
		want := View{}
		if name == "cx" {
			want = View{State: model.AgentTurnDone, Source: SourceHook}
		}
		if got != want {
			t.Errorf("%s: got %+v want %+v", name, got, want)
		}
	}
}

func TestOverlayDoesNotMutateContexts(t *testing.T) {
	ctxs := []model.Context{claudeCtx("a", "/w", "s1")}
	Overlay(ctxs, okSnap(Session{SessionID: "s1", Status: "busy"}))
	if ctxs[0].AgentState != "" || !ctxs[0].AgentStateAt.IsZero() {
		t.Errorf("mutated: %+v", ctxs[0])
	}
}

func TestOverlayNormalizesSymlinks(t *testing.T) {
	real := resolved(t, t.TempDir())
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	ctx := claudeCtx("a", link+"/", "")
	snap := okSnap(Session{SessionID: "s9", Cwd: "c", Toplevel: real, Status: "busy"})
	got := Overlay([]model.Context{ctx}, snap)["a"]
	if got.Source != SourceLive {
		t.Errorf("symlink / 末尾スラッシュ差を吸収する: %+v", got)
	}
}

func TestOverlayWorktreeFallbackIsOneToOne(t *testing.T) {
	// discover で取り込んだ過去セッションなど、同じ worktree に Claude の context が複数あるとき、
	// 1 つの live セッションを全員に当てはめない（どれのセッションか判別できないため hook を維持）
	old1 := claudeCtx("myops-1", "/w/myops", "old-1")
	old2 := claudeCtx("myops-2", "/w/myops", "old-2")
	snap := okSnap(Session{SessionID: "live-x", Cwd: "/w/myops", Toplevel: "/w/myops", Status: "busy"})
	views := Overlay([]model.Context{old1, old2}, snap)
	for _, name := range []string{"myops-1", "myops-2"} {
		if views[name].Source == SourceLive {
			t.Errorf("%s got live state from an ambiguous worktree match: %+v", name, views[name])
		}
	}
}

// 表示対象外（Done など）の context が所有する live セッションは、同じ worktree の
// 別 context に当てはめない。Overlay には store の全 context を渡す前提の回帰テスト。
func TestOverlayDoneContextKeepsOwningItsLiveSession(t *testing.T) {
	a := claudeCtx("a", "/w/x", "live-a")
	a.Status = model.StatusDone
	b := claudeCtx("b", "/w/x", "old-b")
	snap := okSnap(Session{SessionID: "live-a", Cwd: "/w/x", Toplevel: "/w/x", Status: "busy"})

	all := Overlay([]model.Context{a, b}, snap)
	if all["a"].Source != SourceLive {
		t.Errorf("a は SessionID 一致で live: %+v", all["a"])
	}
	if all["b"].Source == SourceLive {
		t.Errorf("b が a の live セッションを奪った: %+v", all["b"])
	}
}

func pendingOf(kind model.PendingKind, summary string) *model.PendingRequest {
	return &model.PendingRequest{Tool: "T", Kind: kind, Summary: summary, At: t0}
}

func TestOverlayPending_HookNeedsInputCarriesPending(t *testing.T) {
	p := pendingOf(model.PendingBash, "Run tests")
	ctx := claudeCtx("a", "/w/a", "s1")
	ctx.AgentState = model.AgentNeedsInput
	ctx.PendingRequest = p
	// agent view が使えない・照合できない場合は hook の状態をそのまま使う
	if got := Overlay([]model.Context{ctx}, Snapshot{})["a"]; got.Pending != p {
		t.Errorf("no snapshot: pending = %+v", got.Pending)
	}
	codex := ctx
	codex.Provider = model.ProviderCodex
	if got := Overlay([]model.Context{codex}, okSnap())["a"]; got.Pending != p {
		t.Errorf("codex: pending = %+v", got.Pending)
	}
}

func TestOverlayPending_HookNonNeedsInputHasNoPending(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "s1")
	ctx.AgentState = model.AgentTurnDone
	ctx.PendingRequest = pendingOf(model.PendingBash, "stale")
	if got := Overlay([]model.Context{ctx}, Snapshot{})["a"]; got.Pending != nil {
		t.Errorf("pending = %+v, want nil", got.Pending)
	}
}

func TestOverlayPending_LiveConsistency(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		waitingFor string
		kind       model.PendingKind
		want       bool
	}{
		{"permission prompt with bash", "waiting", "permission prompt", model.PendingBash, true},
		{"permission prompt with plan", "waiting", "permission prompt", model.PendingPlan, true},
		{"permission prompt with question is inconsistent", "waiting", "permission prompt", model.PendingQuestion, false},
		{"input needed with question", "waiting", "input needed", model.PendingQuestion, true},
		{"input needed with bash is inconsistent", "waiting", "input needed", model.PendingBash, false},
		{"sandbox request is not classified", "waiting", "sandbox request", model.PendingBash, false},
		{"dialog open is not classified", "waiting", "dialog open", model.PendingBash, false},
		{"busy drops pending", "busy", "", model.PendingBash, false},
		{"idle drops pending", "idle", "", model.PendingBash, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := pendingOf(tt.kind, "x")
			ctx := claudeCtx("a", "/w/a", "s1")
			ctx.AgentState = model.AgentNeedsInput
			ctx.AgentStateAt = t0.Add(-time.Minute)
			ctx.PendingRequest = p
			snap := okSnap(Session{SessionID: "s1", Status: tt.status, WaitingFor: tt.waitingFor})
			got := Overlay([]model.Context{ctx}, snap)["a"]
			if got.Source != SourceLive {
				t.Fatalf("source = %q", got.Source)
			}
			if (got.Pending == p) != tt.want || (!tt.want && got.Pending != nil) {
				t.Errorf("pending = %+v, want attached=%v", got.Pending, tt.want)
			}
		})
	}
}

func TestOverlayPending_LiveWaitingWithoutHookPending(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "s1")
	snap := okSnap(Session{SessionID: "s1", Status: "waiting", WaitingFor: "permission prompt"})
	if got := Overlay([]model.Context{ctx}, snap)["a"]; got.Pending != nil {
		t.Errorf("pending = %+v, want nil", got.Pending)
	}
}
