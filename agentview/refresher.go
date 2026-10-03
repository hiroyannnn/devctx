package agentview

import (
	"context"
	"sync"
	"time"
)

const (
	refreshInterval = 5 * time.Second
	maxAge          = 15 * time.Second
	refreshTimeout  = 3 * time.Second
)

// Refresher は `claude agents --json` の結果をバックグラウンドで更新する 呼び出し側の snapshot 供給元。
//
// 定期 goroutine は持たず、Snapshot() が呼ばれたとき（＝ダッシュボード・watch・TUI が使われているとき）
// だけ、間隔を過ぎていて取得中でなければ 1 本だけ取得を起動する。
// ticker 方式だと誰も見ていなくても claude を叩き続け、停止管理も必要になるため採らない。
type Refresher struct {
	fetch func(ctx context.Context) Snapshot
	now   func() time.Time

	mu        sync.Mutex
	snap      Snapshot
	have      bool
	inflight  bool
	lastStart time.Time
}

// NewRefresher は fetch（nil なら実 claude を叩く Fetch）で更新する Refresher を作る。
func NewRefresher(fetch func(ctx context.Context) Snapshot) *Refresher {
	if fetch == nil {
		fetch = func(ctx context.Context) Snapshot { return Fetch(ctx, nil) }
	}
	return &Refresher{fetch: fetch, now: time.Now}
}

// Snapshot は最新の snapshot を返す。未取得、または maxAge より古い場合は OK=false
// （呼び出し側は hook 状態を使う）。必要なら裏で再取得を起動する。
func (l *Refresher) Snapshot() Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if !l.inflight && (!l.have || now.Sub(l.lastStart) >= refreshInterval) {
		l.inflight = true
		l.lastStart = now
		go l.refresh()
	}

	if !l.have {
		return Snapshot{}
	}
	snap := l.snap
	if now.Sub(snap.FetchedAt) > maxAge {
		snap.OK = false
	}
	return snap
}

func (l *Refresher) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	// 失敗・panic でも inflight を必ず戻す。戻し忘れると以後の再取得が永久に止まる。
	// panic は握りつぶして取得失敗（OK=false）扱いにする。バックグラウンド取得の都合で
	// ダッシュボード全体を落とすより、hook 状態へのフォールバックを優先する。
	snap := Snapshot{FetchedAt: l.now()}
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
