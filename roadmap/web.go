package roadmap

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hiroyannnn/devctx/model"
)

//go:embed templates/*
var templateFS embed.FS

// StoreLoader abstracts store loading for testing.
type StoreLoader interface {
	LoadStore() (*model.Store, error)
}

// RoadmapEntry is the JSON response structure for each context.
type RoadmapEntry struct {
	Name          string       `json:"name"`
	Branch        string       `json:"branch"`
	Status        model.Status `json:"status"`
	Phase         model.Phase  `json:"phase"`
	InitialPrompt string      `json:"initial_prompt,omitempty"`
	Worktree      string       `json:"worktree"`
	PRURL         string       `json:"pr_url,omitempty"`
	IssueURL      string       `json:"issue_url,omitempty"`
	Note          string       `json:"note,omitempty"`
	SessionName   string       `json:"session_name,omitempty"`
	CreatedAt     string       `json:"created_at"`
	LastSeen      string       `json:"last_seen"`
}

// Server serves the roadmap web UI.
type Server struct {
	StoreLoader StoreLoader
	Scanner     *Scanner
	Port        int
}

// NewServer creates a new Server.
func NewServer(loader StoreLoader, scanner *Scanner, port int) *Server {
	return &Server{StoreLoader: loader, Scanner: scanner, Port: port}
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/roadmap", s.handleAPIRoadmap)
	mux.HandleFunc("/", s.handleIndex)

	addr := fmt.Sprintf(":%d", s.Port)
	fmt.Printf("Session Roadmap: http://localhost%s\n", addr)
	fmt.Println("Press Ctrl+C to stop")
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		http.Error(w, "dashboard not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) handleAPIRoadmap(w http.ResponseWriter, r *http.Request) {
	store, err := s.StoreLoader.LoadStore()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	phases := s.Scanner.ScanAll(store.Contexts)

	entries := make([]RoadmapEntry, 0, len(store.Contexts))
	for _, ctx := range store.Contexts {
		entries = append(entries, RoadmapEntry{
			Name:          ctx.Name,
			Branch:        ctx.Branch,
			Status:        ctx.Status,
			Phase:         phases[ctx.Name],
			InitialPrompt: ctx.InitialPrompt,
			Worktree:      ctx.Worktree,
			PRURL:         ctx.PRURL,
			IssueURL:      ctx.IssueURL,
			Note:          ctx.Note,
			SessionName:   ctx.SessionName,
			CreatedAt:     ctx.CreatedAt.Format("2006-01-02 15:04"),
			LastSeen:      ctx.LastSeen.Format("2006-01-02 15:04"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}
