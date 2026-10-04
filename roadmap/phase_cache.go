package roadmap

import "github.com/hiroyannnn/devctx/model"

// phaseKey は fast scan の結果を決める入力。scanWithMode が読むのは Worktree と Branch だけなので、
// 同じ組の context は同じ phase になる（実データでは scan 対象 213 件に対し組は 13 通り）。
type phaseKey struct {
	worktree string
	branch   string
}

// needsPhaseScan は表示時に fast scan で phase を補う context か。phase 記録済み・worktree 不明は scan しない。
func (s *Server) needsPhaseScan(ctx model.Context) bool {
	return ctx.Phase == "" && ctx.Worktree != "" && s.Scanner != nil
}

// scanPhases は active のうち scan が必要な context の phase を (Worktree, Branch) 単位で求める。
func (s *Server) scanPhases(active []model.Context) map[phaseKey]model.Phase {
	phases := make(map[phaseKey]model.Phase)
	for _, ctx := range active {
		if !s.needsPhaseScan(ctx) {
			continue
		}
		key := phaseKey{ctx.Worktree, ctx.Branch}
		if _, done := phases[key]; done {
			continue
		}
		phases[key] = s.Scanner.scanWithMode(&ctx, ScanModeFast)
	}
	return phases
}
