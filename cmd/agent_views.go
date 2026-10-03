package cmd

import (
	"context"

	"github.com/hiroyannnn/devctx/agentview"
	"github.com/hiroyannnn/devctx/model"
)

// fetchAgentSnapshot は `claude agents --json` を取得する。テストでは差し替える。
// hook 経路（register / touch / roadmap analyze）からは呼ばない。hook は高頻度で、
// 取得に 150ms〜2s かかる claude 実行を毎回挟むと Claude Code 側の体感を悪化させるため。
var fetchAgentSnapshot = agentview.Fetch

// liveViews は snapshot の供給元を持ち、context 一覧に重ねた表示状態を返す。
// 供給元は 2 種類で、どちらも描画・tick の経路で同期取得はしない（watch / TUI は Refresher）。
type liveViews struct {
	snapshot func() agentview.Snapshot
}

// newLiveViews は一回きりの表示（list / status）用。呼ぶ時点で 1 回だけ同期取得する。
func newLiveViews() *liveViews {
	return &liveViews{snapshot: func() agentview.Snapshot { return fetchAgentSnapshot(context.Background()) }}
}

// newWatchLiveViews は watch / TUI 用。取得はバックグラウンドの Refresher に任せ、
// views() は直近の snapshot を読むだけ。初回は取得前なので hook 状態で描き、取得後の tick で live に変わる。
// 同期取得にしない理由: 2 秒 tick のたびに claude（150ms〜2s）を待つと入力が引っかかるため。
func newWatchLiveViews() *liveViews {
	r := agentview.NewRefresher(fetchAgentSnapshot)
	return &liveViews{snapshot: r.Snapshot}
}

// views は all（表示対象に絞らない store の全 context）に overlay を適用し、名前で引ける map を返す。
// 表示側は自分が描画する context だけを名前で引く。
func (l *liveViews) views(all []model.Context) map[string]agentview.View {
	return agentview.Overlay(all, l.snapshot())
}
