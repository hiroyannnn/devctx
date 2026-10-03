package cmd

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/agentview"
	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// 実 claude を呼ばないよう、テストでは取得関数を必ず差し替える。
func stubSnapshot(t *testing.T, snap agentview.Snapshot) *int {
	t.Helper()
	calls := 0
	orig := fetchAgentSnapshot
	fetchAgentSnapshot = func() agentview.Snapshot { calls++; return snap }
	t.Cleanup(func() { fetchAgentSnapshot = orig })
	return &calls
}

func liveSnap(sessions ...agentview.Session) agentview.Snapshot {
	return agentview.Snapshot{OK: true, FetchedAt: time.Now().Add(-time.Second), Sessions: sessions}
}

func TestAgentTagWithReason(t *testing.T) {
	ctx := model.Context{SessionID: "s1"}
	view := agentview.View{State: model.AgentNeedsInput, Reason: "permission prompt", Source: agentview.SourceLive}
	if got, want := agentTag(ctx, view), "claude · needs input · permission prompt"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if got, want := agentTag(ctx, agentview.View{State: model.AgentRunning, Source: agentview.SourceLive}), "claude · running"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFormatCardShowsLiveReason(t *testing.T) {
	ctx := model.Context{Name: "a"}
	view := agentview.View{State: model.AgentNeedsInput, Reason: "input needed", Source: agentview.SourceLive}
	if card := formatCard(ctx, view); !strings.Contains(card, "claude · needs input · input needed") {
		t.Errorf("card: %q", card)
	}
}

// one-shot（list / status）は呼び出しごとに同期取得する。
func TestLiveViewsOneShotFetchesSynchronously(t *testing.T) {
	calls := stubSnapshot(t, liveSnap(agentview.Session{SessionID: "s1", Status: "busy"}))
	l := newLiveViews()
	if v := l.views([]model.Context{{Name: "a", SessionID: "s1"}})["a"]; v.State != model.AgentRunning {
		t.Errorf("%+v", v)
	}
	if *calls != 1 {
		t.Errorf("calls = %d", *calls)
	}
}

// watch / TUI の views() は同期取得しない。初回は hook 状態で、背景取得が済むと live に変わる。
func TestWatchLiveViewsNeverBlocksOnFetch(t *testing.T) {
	release := make(chan struct{})
	fetched := make(chan struct{}, 1)
	orig := fetchAgentSnapshot
	fetchAgentSnapshot = func() agentview.Snapshot {
		fetched <- struct{}{}
		<-release
		return liveSnap(agentview.Session{SessionID: "s1", Status: "busy"})
	}
	t.Cleanup(func() { fetchAgentSnapshot = orig })

	l := newWatchLiveViews()
	ctx := model.Context{Name: "a", SessionID: "s1", AgentState: model.AgentTurnDone, AgentStateAt: time.Now().Add(-time.Hour)}
	done := make(chan agentview.View)
	go func() { done <- l.views([]model.Context{ctx})["a"] }()
	select {
	case v := <-done:
		if v.Source != agentview.SourceHook {
			t.Errorf("取得前は hook: %+v", v)
		}
	case <-time.After(time.Second):
		t.Fatal("views() が取得を待ってブロックした")
	}

	<-fetched
	close(release)
	waitLive(t, func() bool {
		return l.views([]model.Context{ctx})["a"].Source == agentview.SourceLive
	})
}

func waitLive(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for live snapshot")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTuiItemShowsLiveReasonAndStoreStaysClean(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := storage.New()
	if err != nil {
		t.Fatal(err)
	}
	store := &model.Store{Contexts: []model.Context{{
		Name: "a", Status: model.StatusInProgress, SessionID: "s1",
		AgentState: model.AgentTurnDone, AgentStateAt: time.Now().Add(-time.Hour),
	}}}
	if err := s.SaveStore(store); err != nil {
		t.Fatal(err)
	}
	stubSnapshot(t, liveSnap(agentview.Session{SessionID: "s1", Status: "waiting", WaitingFor: "permission prompt"}))

	m := newTuiModel(store, s)
	if len(m.list.Items()) != 1 {
		t.Fatalf("items = %d", len(m.list.Items()))
	}
	// 初回は取得前。背景取得が済んだ tick で live に変わる
	waitLive(t, func() bool {
		next, _ := m.Update(tuiTickMsg{})
		m = next.(tuiModel)
		return strings.Contains(m.list.Items()[0].(contextItem).Description(), "claude · needs input · permission prompt")
	})

	// store を保存する操作（移動）でも live の値は永続化されない
	m.moveSelected(model.StatusReview)
	loaded, err := s.LoadStore()
	if err != nil {
		t.Fatal(err)
	}
	c := loaded.FindByName("a")
	if c.AgentState != model.AgentTurnDone || c.Status != model.StatusReview {
		t.Errorf("overlay 値が永続化された / 移動されていない: %+v", c)
	}
}

func TestKanbanModelUsesLiveViewsAndStoreStaysClean(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := storage.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveStore(&model.Store{Contexts: []model.Context{{
		Name: "a", Status: model.StatusInProgress, SessionID: "s1", LastSeen: time.Now(),
		AgentState: model.AgentTurnDone, AgentStateAt: time.Now().Add(-time.Hour),
	}}}); err != nil {
		t.Fatal(err)
	}
	stubSnapshot(t, liveSnap(agentview.Session{SessionID: "s1", Status: "busy"}))

	m := newKanbanModel(s)
	waitLive(t, func() bool {
		next, _ := m.Update(tickMsg(time.Now()))
		m = next.(kanbanModel)
		return m.views["a"].Source == agentview.SourceLive
	})
	if v := m.views["a"]; v.State != model.AgentRunning {
		t.Fatalf("views: %+v", m.views)
	}
	if out := m.View(); !strings.Contains(out, "claude · running") {
		t.Errorf("view lacks live tag:\n%s", out)
	}
	m.moveSelectedTo(model.StatusReview, "moved")
	loaded, _ := s.LoadStore()
	if c := loaded.FindByName("a"); c.AgentState != model.AgentTurnDone || c.Status != model.StatusReview {
		t.Errorf("overlay 値が永続化された: %+v", c)
	}
}

func TestListFzfAndNamesOnlyDoNotFetchAgentView(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := fetchAgentSnapshot
	fetchAgentSnapshot = func() agentview.Snapshot {
		t.Fatal("fzf / names-only は hot path。claude agents を呼んではいけない")
		return agentview.Snapshot{}
	}
	defer func() { fetchAgentSnapshot = orig }()
	defer func() { listFzf, listNamesOnly = false, false }()

	for _, mode := range []string{"fzf", "names"} {
		listFzf, listNamesOnly = mode == "fzf", mode == "names"
		if err := listCmd.RunE(listCmd, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGetLiveStatusesPrecedence(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "live", Status: model.StatusInProgress},
		{Name: "hook", Status: model.StatusInProgress},
		{Name: "ended", Status: model.StatusInProgress},
		{Name: "none", Status: model.StatusInProgress},
	}}
	views := map[string]agentview.View{
		"live":  {State: model.AgentNeedsInput, Reason: "permission prompt", Source: agentview.SourceLive},
		"hook":  {State: model.AgentRunning, Source: agentview.SourceHook},
		"ended": {State: model.AgentEnded, Source: agentview.SourceHook},
	}
	got := map[string]LiveStatus{}
	for _, ls := range getLiveStatuses(store, views) {
		got[ls.Context.Name] = ls
	}
	if g := got["live"]; g.SessionStatus != SessionStatusWaiting || g.Reason != "permission prompt" {
		t.Errorf("live: %+v", g)
	}
	// hook の状態は古いまま残りうる（昨日の turn_done 等）ため、status では使わず従来の transcript 推論に任せる
	if g := got["hook"]; g.SessionStatus != SessionStatusOffline || g.Source != "" {
		t.Errorf("hook state must not override transcript inference: %+v", g)
	}
	if g := got["ended"]; g.SessionStatus != SessionStatusOffline {
		t.Errorf("ended: %+v", g)
	}
	if g := got["none"]; g.SessionStatus != SessionStatusOffline { // transcript なし → 従来推論（offline）
		t.Errorf("none: %+v", g)
	}
}

// Done の context A が所有する live セッションを、同じ worktree の B に当てはめない。
func TestTuiItemsDoNotStealLiveSessionOfDoneContext(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "a", Status: model.StatusDone, Worktree: "/w/x", SessionID: "live-a"},
		{Name: "b", Status: model.StatusInProgress, Worktree: "/w/x", SessionID: "old-b",
			AgentState: model.AgentTurnDone, AgentStateAt: time.Now().Add(-time.Hour)},
	}}
	stubSnapshot(t, liveSnap(agentview.Session{SessionID: "live-a", Cwd: "/w/x", Toplevel: "/w/x", Status: "busy"}))

	for _, it := range buildItems(store, newLiveViews().views(store.Contexts)) {
		ci := it.(contextItem)
		if ci.ctx.Name == "b" && ci.view.Source == agentview.SourceLive {
			t.Errorf("b が Done の a の live セッションを奪った: %+v", ci.view)
		}
	}
}

// TUI は操作がなくても定期的に再描画し、tick で直近の snapshot を取り込む。
// 取得は Refresher の背景 goroutine で、Update は同期取得しない（tick のたびに claude を待たない）。
func TestTuiTickPicksUpBackgroundSnapshotWithoutFetchingInUpdate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := storage.New()
	if err != nil {
		t.Fatal(err)
	}
	store := &model.Store{Contexts: []model.Context{{
		Name: "a", Status: model.StatusInProgress, SessionID: "s1",
		AgentState: model.AgentTurnDone, AgentStateAt: time.Now().Add(-time.Hour),
	}}}
	var mu sync.Mutex
	status := "busy"
	orig := fetchAgentSnapshot
	fetchAgentSnapshot = func() agentview.Snapshot {
		mu.Lock()
		defer mu.Unlock()
		return liveSnap(agentview.Session{SessionID: "s1", Status: status})
	}
	t.Cleanup(func() { fetchAgentSnapshot = orig })

	m := newTuiModel(store, s)
	desc := func() string { return m.list.Items()[0].(contextItem).Description() }
	if m.Init() == nil {
		t.Fatal("Init は最初の tick を返す")
	}

	waitLive(t, func() bool {
		next, cmd := m.Update(tuiTickMsg{})
		m = next.(tuiModel)
		if cmd == nil {
			t.Fatal("tick は次の tick を予約する")
		}
		return strings.Contains(desc(), "claude · running")
	})
	if store.Contexts[0].AgentState != model.AgentTurnDone {
		t.Errorf("overlay 値が store に入った: %v", store.Contexts[0].AgentState)
	}
}
