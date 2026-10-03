package roadmap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/agentview"
	"github.com/hiroyannnn/devctx/model"
)

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for condition")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLiveRefresherFirstCallIsNotOKThenFillsInBackground(t *testing.T) {
	var calls int32
	l := NewLiveRefresher(func(context.Context) agentview.Snapshot {
		atomic.AddInt32(&calls, 1)
		return agentview.Snapshot{OK: true, FetchedAt: time.Now(), Sessions: []agentview.Session{{SessionID: "s"}}}
	})
	if l.Snapshot().OK {
		t.Fatal("初回は取得前なので OK=false（ハンドラを claude 待ちでブロックしない）")
	}
	waitFor(t, func() bool { return l.Snapshot().OK })
}

func TestLiveRefresherAtMostOneInFlight(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	l := NewLiveRefresher(func(context.Context) agentview.Snapshot {
		atomic.AddInt32(&calls, 1)
		<-release
		return agentview.Snapshot{OK: true, FetchedAt: time.Now()}
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); l.Snapshot() }()
	}
	wg.Wait()
	time.Sleep(20 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("in flight 中の追加取得は不可: %d", n)
	}
	close(release)
	waitFor(t, func() bool { return l.Snapshot().OK })
}

func TestLiveRefresherRefreshIntervalAndStaleness(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }

	var calls int32
	l := NewLiveRefresher(func(context.Context) agentview.Snapshot {
		atomic.AddInt32(&calls, 1)
		return agentview.Snapshot{OK: true, FetchedAt: clock()}
	})
	l.now = clock

	l.Snapshot()
	waitFor(t, func() bool { return atomic.LoadInt32(&calls) == 1 && l.Snapshot().OK })

	// interval(5s) 未満では再取得しない
	advance(3 * time.Second)
	l.Snapshot()
	time.Sleep(20 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("間隔内の再取得: %d", n)
	}

	// 5s 超で再取得が走る
	advance(3 * time.Second)
	l.Snapshot()
	waitFor(t, func() bool { return atomic.LoadInt32(&calls) == 2 })
}

func TestLiveRefresherStaleSnapshotIsNotOK(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }

	block := make(chan struct{})
	var first int32 = 1
	l := NewLiveRefresher(func(context.Context) agentview.Snapshot {
		if atomic.CompareAndSwapInt32(&first, 1, 0) {
			return agentview.Snapshot{OK: true, FetchedAt: clock()}
		}
		<-block // 以降の取得は返らない（claude がハングした状況）
		return agentview.Snapshot{}
	})
	defer close(block)
	l.now = clock

	l.Snapshot()
	waitFor(t, func() bool { return l.Snapshot().OK })

	mu.Lock()
	now = now.Add(16 * time.Second)
	mu.Unlock()
	if l.Snapshot().OK {
		t.Fatal("15s より古い snapshot は live として使わない")
	}
}

type fakeLive struct{ snap agentview.Snapshot }

func (f fakeLive) Snapshot() agentview.Snapshot { return f.snap }

func TestAPIsOverlayLiveState(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	store := &model.Store{Contexts: []model.Context{
		{
			Name: "feat-x", Worktree: "/w/feat-x", Status: model.StatusInProgress, Phase: model.PhaseIdle,
			RepoRoot: "/repo", CreatedAt: now, LastSeen: now, SessionID: "sess-1",
			AgentState: model.AgentTurnDone, AgentStateAt: now,
		},
	}}
	snap := agentview.Snapshot{OK: true, FetchedAt: now.Add(time.Hour), Sessions: []agentview.Session{
		{SessionID: "sess-1", Status: "waiting", WaitingFor: "permission prompt"},
	}}
	server := &Server{StoreLoader: &mockStoreLoader{store: store}, Live: fakeLive{snap}}

	handlers := map[string]func(http.ResponseWriter, *http.Request){
		"/api/roadmap":       server.handleAPIRoadmap,
		"/api/roadmap-map":   server.handleAPIRoadmapMap,
		"/api/roadmap-graph": server.handleAPIRoadmapGraph,
	}
	for path, handler := range handlers {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest("GET", path, nil))
			body := w.Body.String()
			for _, want := range []string{
				`"agent_state":"needs_input"`,
				`"agent_state_label":"needs input"`,
				`"agent_waiting":true`,
				`"agent_waiting_for":"permission prompt"`,
				`"agent_state_source":"live"`,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s response lacks %s:\n%s", path, want, body)
				}
			}
		})
	}
	// store の値は書き換わらない
	if store.Contexts[0].AgentState != model.AgentTurnDone {
		t.Errorf("overlay が store を汚染: %v", store.Contexts[0].AgentState)
	}
}

func TestAPIsWithoutLiveReportHookSource(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	store := &model.Store{Contexts: []model.Context{{
		Name: "feat-x", Worktree: "/w/feat-x", Status: model.StatusInProgress, Phase: model.PhaseIdle,
		RepoRoot: "/repo", CreatedAt: now, LastSeen: now, AgentState: model.AgentTurnDone, AgentStateAt: now,
	}}}
	server := &Server{StoreLoader: &mockStoreLoader{store: store}}
	w := httptest.NewRecorder()
	server.handleAPIRoadmapMap(w, httptest.NewRequest("GET", "/api/roadmap-map", nil))
	body := w.Body.String()
	if !strings.Contains(body, `"agent_state_source":"hook"`) || strings.Contains(body, "agent_waiting_for") {
		t.Errorf("%s", body)
	}
}

// UI は待ちの理由をサーバー提供フィールドから表示する（JS 側で状態を再導出しない）
func TestIndexRendersWaitingReason(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).handleIndex(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Body.String(), "agent_waiting_for") {
		t.Error("index.html が agent_waiting_for を参照していない")
	}
}

// 取得が失敗しても（panic でも）in-flight が解除され、次の Snapshot で再取得できること。
func TestLiveRefresherRecoversAfterFailedFetch(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }

	var calls int32
	release := make(chan struct{})
	l := NewLiveRefresher(func(context.Context) agentview.Snapshot {
		if atomic.AddInt32(&calls, 1) == 1 {
			<-release
			return agentview.Snapshot{FetchedAt: clock()} // OK=false（失敗）
		}
		return agentview.Snapshot{OK: true, FetchedAt: clock()}
	})
	l.now = clock

	l.Snapshot()
	waitFor(t, func() bool { return atomic.LoadInt32(&calls) == 1 })
	close(release)
	advance(6 * time.Second)

	waitFor(t, func() bool { l.Snapshot(); return atomic.LoadInt32(&calls) >= 2 })
	waitFor(t, func() bool { return l.Snapshot().OK })
}

func TestLiveRefresherClearsInflightOnPanic(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }

	var calls int32
	l := NewLiveRefresher(func(context.Context) agentview.Snapshot {
		if atomic.AddInt32(&calls, 1) == 1 {
			panic("boom")
		}
		return agentview.Snapshot{OK: true, FetchedAt: clock()}
	})
	l.now = clock

	l.Snapshot()
	waitFor(t, func() bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		return atomic.LoadInt32(&calls) == 1 && !l.inflight
	})
}

func TestAPIsDoNotStealLiveSessionOfDoneContext(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	store := &model.Store{Contexts: []model.Context{
		{Name: "a", Worktree: "/w/x", Status: model.StatusDone, RepoRoot: "/repo", CreatedAt: now, LastSeen: now, SessionID: "live-a"},
		{Name: "b", Worktree: "/w/x", Status: model.StatusInProgress, RepoRoot: "/repo", CreatedAt: now, LastSeen: now,
			SessionID: "old-b", AgentState: model.AgentTurnDone, AgentStateAt: now},
	}}
	snap := agentview.Snapshot{OK: true, FetchedAt: now.Add(time.Hour), Sessions: []agentview.Session{
		{SessionID: "live-a", Cwd: "/w/x", Status: "busy"},
	}}
	server := &Server{
		StoreLoader: &mockStoreLoader{store: store}, Live: fakeLive{snap},
		Toplevel: func(string) string { return "/w/x" },
	}
	w := httptest.NewRecorder()
	server.handleAPIRoadmap(w, httptest.NewRequest("GET", "/api/roadmap", nil))
	if body := w.Body.String(); strings.Contains(body, `"agent_state_source":"live"`) {
		t.Fatalf("b が Done の a の live セッションを奪った:\n%s", body)
	}
}
