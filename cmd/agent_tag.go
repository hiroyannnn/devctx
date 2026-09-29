package cmd

import "github.com/hiroyannnn/devctx/model"

// agentTag は「provider · 状態」の表示文字列を返す（状態が未観測なら provider のみ）。
func agentTag(ctx model.Context) string {
	tag := string(ctx.EffectiveProvider())
	if label := ctx.AgentState.Label(); label != "" {
		tag += " · " + label
	}
	return tag
}
