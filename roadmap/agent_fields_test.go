package roadmap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

func TestAPIsExposeProviderAndAgentState(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	store := &model.Store{Contexts: []model.Context{
		{
			Name: "feat-x", Worktree: "/w/feat-x", Status: model.StatusInProgress, Phase: model.PhaseIdle,
			RepoRoot: "/repo", CreatedAt: now, LastSeen: now,
			AgentState: model.AgentTurnDone, AgentStateAt: now,
		},
		{
			Name: "feat-x-codex", Worktree: "/w/feat-x", Status: model.StatusInProgress, Phase: model.PhaseIdle,
			RepoRoot: "/repo", CreatedAt: now, LastSeen: now, Provider: model.ProviderCodex,
		},
	}}
	server := &Server{StoreLoader: &mockStoreLoader{store: store}}

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
				`"provider":"claude"`,
				`"provider":"codex"`,
				`"agent_state":"turn_done"`,
				`"agent_state_at":"2026-09-30T09:00:00Z"`,
				`"agent_state_label":"turn done"`,
				`"agent_waiting":true`,
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s response lacks %s:\n%s", path, want, body)
				}
			}
		})
	}
}
