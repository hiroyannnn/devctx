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

// toplevelOf は cwd → toplevel の固定表で git 呼び出しを置き換える。
func toplevelOf(table map[string]string) func(string) string {
	return func(cwd string) string { return table[cwd] }
}

func claudeCtx(name, worktree, sessionID string) model.Context {
	return model.Context{Name: name, Worktree: worktree, SessionID: sessionID}
}

func TestOverlayMatchBySessionID(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "s1")
	snap := okSnap(Session{SessionID: "s1", Cwd: "/elsewhere", Status: "waiting", WaitingFor: "permission prompt"})
	got := Overlay([]model.Context{ctx}, snap, toplevelOf(nil))["a"]
	want := View{State: model.AgentNeedsInput, Reason: "permission prompt", Source: SourceLive}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestOverlayMatchByWorktreeFallback(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "")
	snap := okSnap(Session{SessionID: "s9", Cwd: "/w/a/sub", Status: "busy"})
	got := Overlay([]model.Context{ctx}, snap, toplevelOf(map[string]string{"/w/a/sub": "/w/a"}))["a"]
	if got.State != model.AgentRunning || got.Source != SourceLive {
		t.Errorf("%+v", got)
	}
}

func TestOverlayWorktreeMatchIsExactNotPrefixNorRepoRoot(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "")
	hook := model.AgentTurnDone
	ctx.AgentState = hook
	// cwd は worktree の配下だが toplevel は親リポジトリ → 一致させない
	snap := okSnap(Session{SessionID: "s9", Cwd: "/w/a/x", Status: "busy"})
	got := Overlay([]model.Context{ctx}, snap, toplevelOf(map[string]string{"/w/a/x": "/w"}))["a"]
	if got != (View{State: hook, Source: SourceHook}) {
		t.Errorf("%+v", got)
	}
	// 非 git cwd（toplevel 空）は worktree 照合に使わない
	got = Overlay([]model.Context{ctx}, snap, toplevelOf(nil))["a"]
	if got.Source != SourceHook {
		t.Errorf("%+v", got)
	}
}

func TestOverlayAmbiguousWorktreeKeepsHook(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "")
	ctx.AgentState = model.AgentRunning
	snap := okSnap(
		Session{SessionID: "x1", Cwd: "/w/a", Status: "idle"},
		Session{SessionID: "x2", Cwd: "/w/a", Status: "busy"},
	)
	got := Overlay([]model.Context{ctx}, snap, toplevelOf(map[string]string{"/w/a": "/w/a"}))["a"]
	if got != (View{State: model.AgentRunning, Source: SourceHook}) {
		t.Errorf("%+v", got)
	}
}

func TestOverlaySessionIDWinsOverAmbiguity(t *testing.T) {
	ctx := claudeCtx("a", "/w/a", "x2")
	snap := okSnap(
		Session{SessionID: "x1", Cwd: "/w/a", Status: "idle"},
		Session{SessionID: "x2", Cwd: "/w/a", Status: "busy"},
	)
	got := Overlay([]model.Context{ctx}, snap, toplevelOf(map[string]string{"/w/a": "/w/a"}))["a"]
	if got.State != model.AgentRunning || got.Source != SourceLive {
		t.Errorf("%+v", got)
	}
}

func TestOverlayWorktreeFallbackSkipsSessionOwnedByOtherContext(t *testing.T) {
	// 別 context が SessionID で所有している live セッションを worktree 経由で横取りしない
	a := claudeCtx("a", "/w/a", "")
	b := claudeCtx("b", "/w/a", "x1")
	snap := okSnap(Session{SessionID: "x1", Cwd: "/w/a", Status: "busy"})
	got := Overlay([]model.Context{a, b}, snap, toplevelOf(map[string]string{"/w/a": "/w/a"}))
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

	got := Overlay([]model.Context{ended, fresher, older}, snap, toplevelOf(nil))
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
	if got := Overlay([]model.Context{ctx}, snap, toplevelOf(nil))["a"]; got != (View{State: model.AgentTurnDone, Source: SourceLive}) {
		t.Errorf("%+v", got)
	}
}

func TestOverlayFallbacks(t *testing.T) {
	hooked := claudeCtx("hooked", "/w", "s1")
	hooked.AgentState = model.AgentTurnDone
	bare := claudeCtx("bare", "/w2", "s2")

	// 取得失敗
	got := Overlay([]model.Context{hooked, bare}, Snapshot{FetchedAt: fetched}, toplevelOf(nil))
	if got["hooked"] != (View{State: model.AgentTurnDone, Source: SourceHook}) || got["bare"] != (View{}) {
		t.Errorf("%+v", got)
	}
	// OK だが空: 不在を ended と推論しない
	got = Overlay([]model.Context{hooked, bare}, okSnap(), toplevelOf(nil))
	if got["hooked"] != (View{State: model.AgentTurnDone, Source: SourceHook}) || got["bare"] != (View{}) {
		t.Errorf("%+v", got)
	}
	// 未知 status
	got = Overlay([]model.Context{hooked}, okSnap(Session{SessionID: "s1", Status: "weird"}), toplevelOf(nil))
	if got["hooked"].Source != SourceHook {
		t.Errorf("%+v", got)
	}
}

func TestOverlayOnlyClaude(t *testing.T) {
	codex := claudeCtx("cx", "/w", "s1")
	codex.Provider = model.ProviderCodex
	got := Overlay([]model.Context{codex}, okSnap(Session{SessionID: "s1", Status: "busy"}), toplevelOf(nil))
	if _, ok := got["cx"]; ok {
		t.Errorf("codex は対象外: %+v", got)
	}
}

func TestOverlayDoesNotMutateContexts(t *testing.T) {
	ctxs := []model.Context{claudeCtx("a", "/w", "s1")}
	Overlay(ctxs, okSnap(Session{SessionID: "s1", Status: "busy"}), toplevelOf(nil))
	if ctxs[0].AgentState != "" || !ctxs[0].AgentStateAt.IsZero() {
		t.Errorf("mutated: %+v", ctxs[0])
	}
}

func TestOverlayNormalizesSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	ctx := claudeCtx("a", link+"/", "")
	snap := okSnap(Session{SessionID: "s9", Cwd: "c", Status: "busy"})
	got := Overlay([]model.Context{ctx}, snap, toplevelOf(map[string]string{"c": real}))["a"]
	if got.Source != SourceLive {
		t.Errorf("symlink / 末尾スラッシュ差を吸収する: %+v", got)
	}
}

func TestOverlayToplevelMemoizedPerCwd(t *testing.T) {
	calls := map[string]int{}
	top := func(cwd string) string { calls[cwd]++; return "/w/a" }
	ctxs := []model.Context{claudeCtx("a", "/w/a", ""), claudeCtx("b", "/w/b", "")}
	snap := okSnap(Session{SessionID: "x", Cwd: "/w/a", Status: "busy"})
	Overlay(ctxs, snap, top)
	if calls["/w/a"] != 1 {
		t.Errorf("calls = %v", calls)
	}
}

func TestViewFor(t *testing.T) {
	ctx := claudeCtx("a", "/w", "")
	ctx.AgentState = model.AgentRunning
	if got := ViewFor(nil, ctx); got != (View{State: model.AgentRunning, Source: SourceHook}) {
		t.Errorf("%+v", got)
	}
	views := map[string]View{"a": {State: model.AgentTurnDone, Source: SourceLive}}
	if got := ViewFor(views, ctx); got.State != model.AgentTurnDone {
		t.Errorf("%+v", got)
	}
}
