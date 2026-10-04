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
