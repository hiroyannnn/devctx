package roadmap

import (
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// phaseCacheTTL は fast scan 結果の保持期間。/api/roadmap の応答キャッシュと同じ長さにし、
// 既に /api/roadmap が許していた以上に phase の表示を古くしない。
const phaseCacheTTL = cacheTTL

// phaseKey は fast scan の結果を決める入力。scanWithMode が読むのは Worktree と Branch だけなので、
// 同じ組の context は同じ phase になる（実データでは scan 対象 213 件に対し組は 13 通り）。
type phaseKey struct {
	worktree string
	branch   string
}

type cachedPhase struct {
	phase   model.Phase
	expires time.Time
}

// needsPhaseScan は表示時に fast scan で phase を補う context か。phase 記録済み・worktree 不明は scan しない。
func (s *Server) needsPhaseScan(ctx model.Context) bool {
	return ctx.Phase == "" && ctx.Worktree != "" && s.Scanner != nil
}

func (s *Server) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// scanPhases は active のうち scan が必要な context の phase を (Worktree, Branch) 単位で求める。
// 結果は phaseCacheTTL の間 Server に保持し、/api/roadmap と /api/roadmap-map で共有する。
// Why not Scanner 側にキャッシュ: RefreshPhase / ScanAll は CLI が使い、常に最新の phase を要る。
func (s *Server) scanPhases(active []model.Context) map[phaseKey]model.Phase {
	// 期限切れの scan 中に同時に来たリクエストが同じ組を重ねて scan しないよう、scan ごと直列化する
	s.phaseMu.Lock()
	defer s.phaseMu.Unlock()

	now := s.clock()
	if s.phaseCache == nil {
		s.phaseCache = make(map[phaseKey]cachedPhase)
	}
	// store から消えた組が残り続けないよう、期限切れは毎回捨てる
	for k, c := range s.phaseCache {
		if !now.Before(c.expires) {
			delete(s.phaseCache, k)
		}
	}

	phases := make(map[phaseKey]model.Phase)
	for _, ctx := range active {
		if !s.needsPhaseScan(ctx) {
			continue
		}
		key := phaseKey{ctx.Worktree, ctx.Branch}
		if _, done := phases[key]; done {
			continue
		}
		if c, ok := s.phaseCache[key]; ok {
			phases[key] = c.phase
			continue
		}
		phase := s.Scanner.scanWithMode(&ctx, ScanModeFast)
		phases[key] = phase
		s.phaseCache[key] = cachedPhase{phase: phase, expires: now.Add(phaseCacheTTL)}
	}
	return phases
}
