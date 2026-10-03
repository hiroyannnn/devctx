package roadmap

import (
	"context"
	"sync"
	"time"

	"github.com/hiroyannnn/devctx/agentview"
)

const (
	liveRefreshInterval = 5 * time.Second
	liveMaxAge          = 15 * time.Second
	liveFetchTimeout    = 3 * time.Second
)

// LiveSource は agent view の最新 snapshot を返す。ハンドラはこれを読むだけで claude を待たない。
type LiveSource interface {
	Snapshot() agentview.Snapshot
}

// LiveRefresher は `claude agents --json` の結果をバックグラウンドで更新する LiveSource。
//
// 定期 goroutine は持たず、Snapshot() が呼ばれたとき（＝ダッシュボードが使われているとき）
// だけ、間隔を過ぎていて取得中でなければ 1 本だけ取得を起動する。
// ticker 方式だと誰も見ていなくても claude を叩き続け、停止管理も必要になるため採らない。
type LiveRefresher struct {
	fetch func(ctx context.Context) agentview.Snapshot
	now   func() time.Time

	mu        sync.Mutex
	snap      agentview.Snapshot
	have      bool
	inflight  bool
	lastStart time.Time
}

// NewLiveRefresher は fetch（nil なら実 claude を叩く agentview.Fetch）で更新する LiveRefresher を作る。
func NewLiveRefresher(fetch func(ctx context.Context) agentview.Snapshot) *LiveRefresher {
	if fetch == nil {
		fetch = func(ctx context.Context) agentview.Snapshot { return agentview.Fetch(ctx, nil) }
	}
	return &LiveRefresher{fetch: fetch, now: time.Now}
}

// Snapshot は最新の snapshot を返す。未取得、または liveMaxAge より古い場合は OK=false
// （呼び出し側は hook 状態を使う）。必要なら裏で再取得を起動する。
func (l *LiveRefresher) Snapshot() agentview.Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if !l.inflight && (!l.have || now.Sub(l.lastStart) >= liveRefreshInterval) {
		l.inflight = true
		l.lastStart = now
		go l.refresh()
	}

	if !l.have {
		return agentview.Snapshot{}
	}
	snap := l.snap
	if now.Sub(snap.FetchedAt) > liveMaxAge {
		snap.OK = false
	}
	return snap
}

func (l *LiveRefresher) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), liveFetchTimeout)
	defer cancel()

	// 失敗・panic でも inflight を必ず戻す。戻し忘れると以後の再取得が永久に止まる。
	// panic は握りつぶして取得失敗（OK=false）扱いにする。バックグラウンド取得の都合で
	// ダッシュボード全体を落とすより、hook 状態へのフォールバックを優先する。
	snap := agentview.Snapshot{FetchedAt: l.now()}
	defer func() {
		_ = recover()
		l.mu.Lock()
		l.snap = snap
		l.have = true
		l.inflight = false
		l.mu.Unlock()
	}()
	snap = l.fetch(ctx)
}
