package roadmap

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// dirGitRunner は worktree ごとに結果を返し、scan の回数（rev-parse --git-dir）を worktree 別に数える。
// handler からの並列呼び出しに耐えるよう mutex で守る。
type dirGitRunner struct {
	mu      sync.Mutex
	results map[string]map[string]mockResult
	scans   map[string]int
}

func (g *dirGitRunner) Run(dir string, args ...string) (string, error) {
	key := strings.Join(args, " ")
	g.mu.Lock()
	defer g.mu.Unlock()
	if key == "rev-parse --git-dir" {
		if g.scans == nil {
			g.scans = map[string]int{}
		}
		g.scans[dir]++
	}
	if r, ok := g.results[dir][key]; ok {
		return r.output, r.err
	}
	return "", fmt.Errorf("command not mocked: git %s (dir %s)", key, dir)
}

func (g *dirGitRunner) scanCount(dir string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.scans[dir]
}

// /w/a は feat が push 済み（pushed）、other は remote に無く main より先行（committed）。
func newPhaseTestServer() (*Server, *dirGitRunner) {
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	ctx := func(name, wt, br string, phase model.Phase) model.Context {
		return model.Context{Name: name, Worktree: wt, Branch: br, Phase: phase, Status: model.StatusInProgress, CreatedAt: now, LastSeen: now}
	}
	store := &model.Store{Contexts: []model.Context{
		ctx("a1", "/w/a", "feat", ""),
		ctx("a2", "/w/a", "feat", ""),
		ctx("a3", "/w/a", "feat", ""),
		ctx("a4", "/w/a", "other", ""),
		ctx("b1", "/w/b", "x", model.PhasePushed),
	}}
	git := &dirGitRunner{results: map[string]map[string]mockResult{
		"/w/a": {
			"rev-parse --git-dir":             {output: ".git"},
			"rev-parse --verify origin/main":  {output: "abc"},
			"rev-parse --verify origin/feat":  {output: "def"},
			"log origin/feat..HEAD --oneline": {output: ""},
			"rev-parse --verify origin/other": {err: fmt.Errorf("not found")},
			"log origin/main..HEAD --oneline": {output: "abc1234 wip"},
		},
	}}
	server := &Server{
		StoreLoader: &mockStoreLoader{store: store},
		Scanner:     &Scanner{Git: git, Gh: &mockGhRunner{}},
	}
	return server, git
}

func getRoadmapMapPhases(t *testing.T, server *Server) map[string]model.Phase {
	t.Helper()
	w := httptest.NewRecorder()
	server.handleAPIRoadmapMap(w, httptest.NewRequest("GET", "/api/roadmap-map", nil))
	var groups []ProjectGroup
	if err := json.Unmarshal(w.Body.Bytes(), &groups); err != nil {
		t.Fatalf("failed to parse JSON: %v (body %s)", err, w.Body.String())
	}
	phases := map[string]model.Phase{}
	for _, g := range groups {
		for _, e := range g.Sessions {
			phases[e.Name] = e.Phase
		}
	}
	return phases
}

func TestRoadmapMap_ScansEachWorktreeBranchOnce(t *testing.T) {
	server, git := newPhaseTestServer()

	phases := getRoadmapMapPhases(t, server)

	want := map[string]model.Phase{
		"a1": model.PhasePushed, "a2": model.PhasePushed, "a3": model.PhasePushed,
		"a4": model.PhaseCommitted,
		"b1": model.PhasePushed, // phase 記録済みは scan しない
	}
	for name, p := range want {
		if phases[name] != p {
			t.Errorf("phase[%s] = %q, want %q", name, phases[name], p)
		}
	}
	// (/w/a, feat) と (/w/a, other) の 2 通りだけ scan する
	if got := git.scanCount("/w/a"); got != 2 {
		t.Errorf("scans of /w/a = %d, want 2", got)
	}
	if got := git.scanCount("/w/b"); got != 0 {
		t.Errorf("scans of /w/b = %d, want 0", got)
	}
}

func TestRoadmapAPIs_SharePhaseCacheWithinTTL(t *testing.T) {
	server, git := newPhaseTestServer()
	clock := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	server.now = func() time.Time { return clock }

	getRoadmapMapPhases(t, server)
	// /api/roadmap は自前の応答キャッシュが空なので entry を組み立てるが、phase は map の scan 結果を使う
	server.handleAPIRoadmap(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/roadmap", nil))
	clock = clock.Add(phaseCacheTTL - time.Millisecond)
	phases := getRoadmapMapPhases(t, server)

	if got := git.scanCount("/w/a"); got != 2 {
		t.Errorf("scans of /w/a within TTL = %d, want 2", got)
	}
	if phases["a1"] != model.PhasePushed || phases["a4"] != model.PhaseCommitted {
		t.Errorf("cached phases = %v, want a1=pushed a4=committed", phases)
	}

	clock = clock.Add(time.Millisecond)
	getRoadmapMapPhases(t, server)

	if got := git.scanCount("/w/a"); got != 4 {
		t.Errorf("scans of /w/a after TTL = %d, want 4", got)
	}
}

// overlapGitRunner は git 呼び出しの同時実行数を、全体と worktree 別に記録する。
// 最初の呼び出しは、別の呼び出しが重なるまで（最大 1 秒）待つ。sleep の長さとスケジューリングに頼らず重なりを観測するため。
// 直列の実装では重なりが来ないので timeout して先へ進み、maxAll が 1 のままになる。
type overlapGitRunner struct {
	*dirGitRunner
	mu        sync.Mutex
	inflight  int
	byDir     map[string]int
	maxAll    int
	maxPerDir int
	gated     bool
}

func (o *overlapGitRunner) Run(dir string, args ...string) (string, error) {
	o.mu.Lock()
	o.inflight++
	o.byDir[dir]++
	o.maxAll = max(o.maxAll, o.inflight)
	o.maxPerDir = max(o.maxPerDir, o.byDir[dir])
	o.mu.Unlock()

	o.waitForOverlapOnce()
	out, err := o.dirGitRunner.Run(dir, args...)

	o.mu.Lock()
	o.inflight--
	o.byDir[dir]--
	o.mu.Unlock()
	return out, err
}

func (o *overlapGitRunner) waitForOverlapOnce() {
	o.mu.Lock()
	if o.gated {
		o.mu.Unlock()
		return
	}
	o.gated = true
	o.mu.Unlock()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		overlapped := o.maxAll >= 2
		o.mu.Unlock()
		if overlapped {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRoadmapMap_ScansWorktreesConcurrently(t *testing.T) {
	server, git := newPhaseTestServer()
	store, _ := server.StoreLoader.LoadStore()
	now := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	store.Contexts = append(store.Contexts, model.Context{Name: "c1", Worktree: "/w/c", Branch: "feat", Status: model.StatusInProgress, CreatedAt: now, LastSeen: now})
	git.results["/w/c"] = git.results["/w/a"]
	overlap := &overlapGitRunner{dirGitRunner: git, byDir: map[string]int{}}
	server.Scanner.Git = overlap

	phases := getRoadmapMapPhases(t, server)

	if phases["c1"] != model.PhasePushed || phases["a4"] != model.PhaseCommitted {
		t.Errorf("phases = %v, want c1=pushed a4=committed", phases)
	}
	if overlap.maxAll < 2 {
		t.Errorf("max concurrent git calls = %d, want >= 2 (worktrees scanned in parallel)", overlap.maxAll)
	}
	// 同じ worktree の別 branch は直列（git status が同じ index を触るため）
	if overlap.maxPerDir != 1 {
		t.Errorf("max concurrent git calls in one worktree = %d, want 1", overlap.maxPerDir)
	}
}

// testClock は handler 内の並列 scan からも読まれる時計。
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestRoadmap_ResponseCacheDoesNotOutliveScannedPhase(t *testing.T) {
	server, git := newPhaseTestServer()
	clock := &testClock{t: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)}
	server.now = clock.now

	getRoadmapMapPhases(t, server)
	clock.advance(phaseCacheTTL - 100*time.Millisecond)
	// map の scan 結果を使って応答を作る。応答キャッシュはその phase の期限までしか使わない
	server.handleAPIRoadmap(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/roadmap", nil))
	clock.advance(100 * time.Millisecond)
	server.handleAPIRoadmap(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/roadmap", nil))

	if got := git.scanCount("/w/a"); got != 4 {
		t.Errorf("scans of /w/a = %d, want 4 (response cache must expire with the phase it used)", got)
	}
}

// clockAdvancingGit は scan のたびに時計を進め、TTL を超える長さの scan を模す。
type clockAdvancingGit struct {
	*dirGitRunner
	clock *testClock
	step  time.Duration
}

func (c *clockAdvancingGit) Run(dir string, args ...string) (string, error) {
	if strings.Join(args, " ") == "rev-parse --git-dir" {
		c.clock.advance(c.step)
	}
	return c.dirGitRunner.Run(dir, args...)
}

func TestRoadmapMap_PhaseTTLStartsWhenScanFinishes(t *testing.T) {
	server, git := newPhaseTestServer()
	clock := &testClock{t: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)}
	server.now = clock.now
	server.Scanner.Git = &clockAdvancingGit{dirGitRunner: git, clock: clock, step: phaseCacheTTL}

	getRoadmapMapPhases(t, server)
	getRoadmapMapPhases(t, server)

	if got := git.scanCount("/w/a"); got != 2 {
		t.Errorf("scans of /w/a = %d, want 2 (a slow scan must not be expired on arrival)", got)
	}
}
