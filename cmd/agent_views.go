package cmd

import (
	"context"
	"time"

	"github.com/hiroyannnn/devctx/agentview"
	"github.com/hiroyannnn/devctx/model"
)

// fetchAgentSnapshot は `claude agents --json` を取得する。テストでは差し替える。
// hook 経路（register / touch / roadmap analyze）からは呼ばない。hook は高頻度で、
// 取得に 150ms〜2s かかる claude 実行を毎回挟むと Claude Code 側の体感を悪化させるため。
var fetchAgentSnapshot = func() agentview.Snapshot {
	return agentview.Fetch(context.Background(), nil)
}

// memoToplevel は cwd ごとの git toplevel を保持する。再描画のたびに git を呼ばないため、
// TUI のようにプロセス寿命の長い呼び出し元はこれを使い回す。
func memoToplevel() func(string) string {
	cache := make(map[string]string)
	return func(cwd string) string {
		if top, ok := cache[cwd]; ok {
			return top
		}
		top := agentview.GitToplevel(cwd)
		cache[cwd] = top
		return top
	}
}

// liveViews は agent view の snapshot を保持し、context 一覧に重ねた表示状態を返す。
// 描画のたびに claude を実行せずに済むよう、minInterval 内は直前の snapshot を使い回す
// （Overlay 自体は毎回計算するので、hook の更新は即座に反映される）。
type liveViews struct {
	minInterval time.Duration
	toplevel    func(string) string
	snap        agentview.Snapshot
	fetched     bool
	now         func() time.Time
}

func newLiveViews(minInterval time.Duration) *liveViews {
	return &liveViews{minInterval: minInterval, toplevel: memoToplevel(), now: time.Now}
}

// views は all（表示対象に絞らない store の全 context）に overlay を適用し、名前で引ける map を返す。
// 表示側は自分が描画する context だけを名前で引く。
func (l *liveViews) views(all []model.Context) map[string]agentview.View {
	if !l.fetched || l.now().Sub(l.snap.FetchedAt) >= l.minInterval {
		l.snap = fetchAgentSnapshot()
		l.fetched = true
	}
	return agentview.Overlay(all, l.snap, l.toplevel)
}
