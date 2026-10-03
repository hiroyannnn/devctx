package cmd

import (
	"github.com/hiroyannnn/devctx/agentview"
	"github.com/hiroyannnn/devctx/model"
)

// agentTag は「provider · 状態 · 待ちの理由」の表示文字列を返す（状態が未観測なら provider のみ）。
func agentTag(ctx model.Context, view agentview.View) string {
	tag := string(ctx.EffectiveProvider())
	if label := view.State.Label(); label != "" {
		tag += " · " + label
		if view.Reason != "" {
			tag += " · " + view.Reason
		}
	}
	return tag
}
