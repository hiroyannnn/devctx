package roadmap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/agentview"
	"github.com/hiroyannnn/devctx/model"
)

type fakeLive struct{ snap agentview.Snapshot }

func (f fakeLive) Snapshot() agentview.Snapshot { return f.snap }

func TestAPIsOverlayLiveState(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	store := &model.Store{Contexts: []model.Context{
		{
			Name: "feat-x", Worktree: "/w/feat-x", Status: model.StatusInProgress, Phase: model.PhaseIdle,
			RepoRoot: "/repo", CreatedAt: now, LastSeen: now, SessionID: "sess-1",
			AgentState: model.AgentTurnDone, AgentStateAt: now,
		},
	}}
	snap := agentview.Snapshot{OK: true, FetchedAt: now.Add(time.Hour), Sessions: []agentview.Session{
		{SessionID: "sess-1", Status: "waiting", WaitingFor: "permission prompt"},
	}}
	server := &Server{StoreLoader: &mockStoreLoader{store: store}, Live: fakeLive{snap}}

	handlers := map[string]func(http.ResponseWriter, *http.Request){
		"/api/roadmap":       server.handleAPIRoadmap,
		"/api/roadmap-map":   server.handleAPIRoadmapMap,
		"/api/roadmap-graph": server.handleAPIRoadmapGraph,
	}
	for path, handler := range handlers {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest("GET", path, nil))
			body := w.Body.String()
			for _, want := range []string{
				`"agent_state":"needs_input"`,
				`"agent_state_label":"needs input"`,
				`"agent_waiting":true`,
				`"agent_waiting_for":"permission prompt"`,
				`"agent_state_source":"live"`,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s response lacks %s:\n%s", path, want, body)
				}
			}
		})
	}
	// store の値は書き換わらない
	if store.Contexts[0].AgentState != model.AgentTurnDone {
		t.Errorf("overlay が store を汚染: %v", store.Contexts[0].AgentState)
	}
}

func TestAPIsWithoutLiveReportHookSource(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	store := &model.Store{Contexts: []model.Context{{
		Name: "feat-x", Worktree: "/w/feat-x", Status: model.StatusInProgress, Phase: model.PhaseIdle,
		RepoRoot: "/repo", CreatedAt: now, LastSeen: now, AgentState: model.AgentTurnDone, AgentStateAt: now,
	}}}
	server := &Server{StoreLoader: &mockStoreLoader{store: store}}
	w := httptest.NewRecorder()
	server.handleAPIRoadmapMap(w, httptest.NewRequest("GET", "/api/roadmap-map", nil))
	body := w.Body.String()
	if !strings.Contains(body, `"agent_state_source":"hook"`) || strings.Contains(body, "agent_waiting_for") {
		t.Errorf("%s", body)
	}
}

// UI は待ちの理由をサーバー提供フィールドから表示する（JS 側で状態を再導出しない）
func TestIndexRendersWaitingReason(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).handleIndex(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Body.String(), "agent_waiting_for") {
		t.Error("index.html が agent_waiting_for を参照していない")
	}
}

func TestAPIsDoNotStealLiveSessionOfDoneContext(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	store := &model.Store{Contexts: []model.Context{
		{Name: "a", Worktree: "/w/x", Status: model.StatusDone, RepoRoot: "/repo", CreatedAt: now, LastSeen: now, SessionID: "live-a"},
		{Name: "b", Worktree: "/w/x", Status: model.StatusInProgress, RepoRoot: "/repo", CreatedAt: now, LastSeen: now,
			SessionID: "old-b", AgentState: model.AgentTurnDone, AgentStateAt: now},
	}}
	snap := agentview.Snapshot{OK: true, FetchedAt: now.Add(time.Hour), Sessions: []agentview.Session{
		{SessionID: "live-a", Cwd: "/w/x", Toplevel: "/w/x", Status: "busy"},
	}}
	server := &Server{StoreLoader: &mockStoreLoader{store: store}, Live: fakeLive{snap}}
	w := httptest.NewRecorder()
	server.handleAPIRoadmap(w, httptest.NewRequest("GET", "/api/roadmap", nil))
	if body := w.Body.String(); strings.Contains(body, `"agent_state_source":"live"`) {
		t.Fatalf("b が Done の a の live セッションを奪った:\n%s", body)
	}
}

func TestAPIsExposePendingLabel(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	store := &model.Store{Contexts: []model.Context{{
		Name: "feat-x", Worktree: "/w/feat-x", Status: model.StatusInProgress, Phase: model.PhaseIdle,
		RepoRoot: "/repo", CreatedAt: now, LastSeen: now,
		Provider: model.ProviderCodex, AgentState: model.AgentNeedsInput, AgentStateAt: now,
		PendingRequest: &model.PendingRequest{Tool: "Bash", Kind: model.PendingBash, Summary: "Run tests", At: now},
	}}}
	server := &Server{StoreLoader: &mockStoreLoader{store: store}}
	for path, handler := range map[string]func(http.ResponseWriter, *http.Request){
		"/api/roadmap":       server.handleAPIRoadmap,
		"/api/roadmap-map":   server.handleAPIRoadmapMap,
		"/api/roadmap-graph": server.handleAPIRoadmapGraph,
	} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", path, nil))
		body := w.Body.String()
		for _, want := range []string{`"agent_pending_kind":"bash"`, `"agent_pending_label":"Bash: Run tests"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %s:\n%s", path, want, body)
			}
		}
	}
}

func TestIndexPrefersPendingLabel(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).handleIndex(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Body.String(), "agent_pending_label") {
		t.Error("index.html が agent_pending_label を参照していない")
	}
}
