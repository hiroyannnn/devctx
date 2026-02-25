package roadmap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

type mockStoreLoader struct {
	store *model.Store
	err   error
}

func (m *mockStoreLoader) LoadStore() (*model.Store, error) {
	return m.store, m.err
}

func TestHandleAPIRoadmap_EmptyStore(t *testing.T) {
	server := &Server{
		StoreLoader: &mockStoreLoader{store: &model.Store{}},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{}},
			Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var entries []RoadmapEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestHandleAPIRoadmap_WithContexts(t *testing.T) {
	now := time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC)
	store := &model.Store{
		Contexts: []model.Context{
			{
				Name:          "auth",
				Branch:        "feature/auth",
				Worktree:      "/tmp/auth",
				Status:        model.StatusInProgress,
				InitialPrompt: "認証機能を実装して",
				CreatedAt:     now,
				LastSeen:      now,
			},
			{
				Name:     "api-fix",
				Branch:   "fix/api-500",
				Worktree: "/tmp/api-fix",
				Status:   model.StatusReview,
				PRURL:    "https://github.com/example/repo/pull/1",
				CreatedAt: now,
				LastSeen:  now,
			},
		},
	}

	server := &Server{
		StoreLoader: &mockStoreLoader{store: store},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{
				"rev-parse --git-dir": {err: fmt.Errorf("not found")},
			}},
			Gh: &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var entries []RoadmapEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	if entries[0].Name != "auth" {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, "auth")
	}
	if entries[0].InitialPrompt != "認証機能を実装して" {
		t.Errorf("entries[0].InitialPrompt = %q, want %q", entries[0].InitialPrompt, "認証機能を実装して")
	}
	if entries[0].Phase != model.PhaseIdle {
		t.Errorf("entries[0].Phase = %q, want %q", entries[0].Phase, model.PhaseIdle)
	}

	if entries[1].Name != "api-fix" {
		t.Errorf("entries[1].Name = %q, want %q", entries[1].Name, "api-fix")
	}
	if entries[1].PRURL != "https://github.com/example/repo/pull/1" {
		t.Errorf("entries[1].PRURL = %q, want %q", entries[1].PRURL, "https://github.com/example/repo/pull/1")
	}
}

func TestHandleIndex(t *testing.T) {
	server := &Server{
		StoreLoader: &mockStoreLoader{store: &model.Store{}},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{}},
			Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	server.handleIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", contentType, "text/html; charset=utf-8")
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("body is empty")
	}
	if !strings.Contains(body, "Session Roadmap") {
		t.Error("body does not contain 'Session Roadmap'")
	}
}

func TestHandleAPIRoadmap_StoreError(t *testing.T) {
	server := &Server{
		StoreLoader: &mockStoreLoader{err: fmt.Errorf("disk error")},
		Scanner: &Scanner{
			Git: &mockGitRunner{results: map[string]mockResult{}},
			Gh:  &mockGhRunner{available: false, results: map[string]mockResult{}},
		},
	}

	req := httptest.NewRequest("GET", "/api/roadmap", nil)
	w := httptest.NewRecorder()
	server.handleAPIRoadmap(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
