package roadmap

import (
	"sync"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// phaseCacheTTL は fast scan 結果の保持期間。/api/roadmap の応答キャッシュと同じ長さにし、
// 既に /api/roadmap が許していた以上に phase の表示を古くしない。
const phaseCacheTTL = cacheTTL

// phaseScanWorkers は cache miss の scan を同時に走らせる worktree 数の上限。
// git subprocess を一度に起こしすぎないよう、CPU 数程度に抑える。
const phaseScanWorkers = 8

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
//
// validUntil は返した phase のうち最も早い期限（scan 不要で phase を 1 つも使わなければ zero）。
// 結果を更にキャッシュする呼び出し側は、これを超えて使わない（二重のキャッシュで古さが TTL を超えないように）。
func (s *Server) scanPhases(active []model.Context) (phases map[phaseKey]model.Phase, validUntil time.Time) {
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

	phases = make(map[phaseKey]model.Phase)
	use := func(expires time.Time) {
		if validUntil.IsZero() || expires.Before(validUntil) {
			validUntil = expires
		}
	}
	// misses は cache に無い組を worktree ごとにまとめたもの
	var misses [][]model.Context
	missIndex := make(map[string]int)
	queued := make(map[phaseKey]bool)
	for _, ctx := range active {
		if !s.needsPhaseScan(ctx) {
			continue
		}
		key := phaseKey{ctx.Worktree, ctx.Branch}
		if c, ok := s.phaseCache[key]; ok {
			phases[key] = c.phase
			use(c.expires)
			continue
		}
		if queued[key] {
			continue
		}
		queued[key] = true
		i, ok := missIndex[ctx.Worktree]
		if !ok {
			i = len(misses)
			missIndex[ctx.Worktree] = i
			misses = append(misses, nil)
		}
		misses[i] = append(misses[i], ctx)
	}

	scanned := s.scanMisses(misses)
	// TTL は scan を終えた時刻から数える。開始時刻からだと、TTL より長い scan の結果が保存した時点で切れている
	expires := s.clock().Add(phaseCacheTTL)
	for key, phase := range scanned {
		phases[key] = phase
		s.phaseCache[key] = cachedPhase{phase: phase, expires: expires}
		use(expires)
	}
	return phases, validUntil
}

// scanMisses は worktree ごとの組を並列に scan する（同じ worktree の組はその中で順に scan する）。
// Why not 組ごとに並列: 同じ worktree の別 branch を同時に scan すると git status が同じ index を
// 取り合う。index.lock を取れなければ refresh を諦めるだけで結果は変わらないが、無駄に競合させない。
func (s *Server) scanMisses(byWorktree [][]model.Context) map[phaseKey]model.Phase {
	var mu sync.Mutex
	result := make(map[phaseKey]model.Phase)
	jobs := make(chan []model.Context)
	var wg sync.WaitGroup
	for range min(phaseScanWorkers, len(byWorktree)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctxs := range jobs {
				for _, ctx := range ctxs {
					phase := s.Scanner.scanWithMode(&ctx, ScanModeFast)
					mu.Lock()
					result[phaseKey{ctx.Worktree, ctx.Branch}] = phase
					mu.Unlock()
				}
			}
		}()
	}
	for _, ctxs := range byWorktree {
		jobs <- ctxs
	}
	close(jobs)
	wg.Wait()
	return result
}
