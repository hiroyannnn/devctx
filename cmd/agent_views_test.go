package cmd

import (
	"strings"
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

func TestLiveViewsFetchesOncePerCallWhenNoThrottle(t *testing.T) {
	calls := stubSnapshot(t, liveSnap(agentview.Session{SessionID: "s1", Status: "busy"}))
	l := newLiveViews(0)
	ctxs := []model.Context{{Name: "a", SessionID: "s1"}}
	if v := l.views(ctxs)["a"]; v.State != model.AgentRunning {
		t.Errorf("%+v", v)
	}
	l.views(ctxs)
	if *calls != 2 {
		t.Errorf("throttle 0 は毎回取得: %d", *calls)
	}
}

func TestLiveViewsThrottleReusesSnapshotButRecomputesOverlay(t *testing.T) {
	calls := stubSnapshot(t, liveSnap(agentview.Session{SessionID: "s1", Status: "busy"}))
	l := newLiveViews(5 * time.Second)
	ctx := model.Context{Name: "a", SessionID: "s1"}
	l.views([]model.Context{ctx})

	// 間隔内の再描画では claude を呼ばず、hook の更新だけは即反映される（取得開始より新しい hook は hook 優先）
	ctx.AgentState = model.AgentNeedsInput
	ctx.AgentStateAt = time.Now().Add(time.Hour)
	v := l.views([]model.Context{ctx})["a"]
	if *calls != 1 {
		t.Errorf("calls = %d", *calls)
	}
	if v.Source != agentview.SourceHook || v.State != model.AgentNeedsInput {
		t.Errorf("%+v", v)
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
	items := m.list.Items()
	if len(items) != 1 {
		t.Fatalf("items = %d", len(items))
	}
	if desc := items[0].(contextItem).Description(); !strings.Contains(desc, "claude · needs input · permission prompt") {
		t.Errorf("desc: %q", desc)
	}

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
	if v := m.views["a"]; v.State != model.AgentRunning || v.Source != agentview.SourceLive {
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
	if g := got["hook"]; g.SessionStatus != SessionStatusActive {
		t.Errorf("hook: %+v", g)
	}
	if g := got["ended"]; g.SessionStatus != SessionStatusOffline {
		t.Errorf("ended: %+v", g)
	}
	if g := got["none"]; g.SessionStatus != SessionStatusOffline { // transcript なし → 従来推論（offline）
		t.Errorf("none: %+v", g)
	}
}
