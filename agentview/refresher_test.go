package agentview

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestRefresherFirstCallIsNotOKThenFillsInBackground(t *testing.T) {
	var calls int32
	l := NewRefresher(func(context.Context) Snapshot {
		atomic.AddInt32(&calls, 1)
		return Snapshot{OK: true, FetchedAt: time.Now(), Sessions: []Session{{SessionID: "s"}}}
	})
	if l.Snapshot().OK {
		t.Fatal("初回は取得前なので OK=false（ハンドラを claude 待ちでブロックしない）")
	}
	waitFor(t, func() bool { return l.Snapshot().OK })
}

func TestRefresherAtMostOneInFlight(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	l := NewRefresher(func(context.Context) Snapshot {
		atomic.AddInt32(&calls, 1)
		<-release
		return Snapshot{OK: true, FetchedAt: time.Now()}
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

func TestRefresherRefreshIntervalAndStaleness(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }

	var calls int32
	l := NewRefresher(func(context.Context) Snapshot {
		atomic.AddInt32(&calls, 1)
		return Snapshot{OK: true, FetchedAt: clock()}
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

func TestRefresherStaleSnapshotIsNotOK(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }

	block := make(chan struct{})
	var first int32 = 1
	l := NewRefresher(func(context.Context) Snapshot {
		if atomic.CompareAndSwapInt32(&first, 1, 0) {
			return Snapshot{OK: true, FetchedAt: clock()}
		}
		<-block // 以降の取得は返らない（claude がハングした状況）
		return Snapshot{}
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

// 取得が失敗しても（panic でも）in-flight が解除され、次の Snapshot で再取得できること。
func TestRefresherRecoversAfterFailedFetch(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }

	var calls int32
	release := make(chan struct{})
	l := NewRefresher(func(context.Context) Snapshot {
		if atomic.AddInt32(&calls, 1) == 1 {
			<-release
			return Snapshot{FetchedAt: clock()} // OK=false（失敗）
		}
		return Snapshot{OK: true, FetchedAt: clock()}
	})
	l.now = clock

	l.Snapshot()
	waitFor(t, func() bool { return atomic.LoadInt32(&calls) == 1 })
	close(release)
	advance(6 * time.Second)

	waitFor(t, func() bool { l.Snapshot(); return atomic.LoadInt32(&calls) >= 2 })
	waitFor(t, func() bool { return l.Snapshot().OK })
}

func TestRefresherClearsInflightOnPanic(t *testing.T) {
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }

	var calls int32
	l := NewRefresher(func(context.Context) Snapshot {
		if atomic.AddInt32(&calls, 1) == 1 {
			panic("boom")
		}
		return Snapshot{OK: true, FetchedAt: clock()}
	})
	l.now = clock

	l.Snapshot()
	waitFor(t, func() bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		return atomic.LoadInt32(&calls) == 1 && !l.inflight
	})
}
