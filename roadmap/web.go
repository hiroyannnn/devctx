package roadmap

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

//go:embed templates/*
var templateFS embed.FS

// StoreLoader abstracts store loading for testing.
type StoreLoader interface {
	LoadStore() (*model.Store, error)
}

// Compile-time check that storage.Storage satisfies StoreLoader.
var _ StoreLoader = (*storage.Storage)(nil)

// RoadmapEntry is the JSON response structure for each context.
type RoadmapEntry struct {
	Name          string       `json:"name"`
	Branch        string       `json:"branch"`
	Status        model.Status `json:"status"`
	Phase         model.Phase  `json:"phase"`
	InitialPrompt string       `json:"initial_prompt,omitempty"`
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

	cacheMu      sync.RWMutex
	cachedResult []byte
	cacheExpiry  time.Time
}

const cacheTTL = 5 * time.Second

// NewServer creates a new Server.
func NewServer(loader StoreLoader, scanner *Scanner, port int) *Server {
	return &Server{StoreLoader: loader, Scanner: scanner, Port: port}
}

// ListenAndServe starts the HTTP server on localhost only.
func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/roadmap", s.handleAPIRoadmap)
	mux.HandleFunc("/", s.handleIndex)

	addr := fmt.Sprintf("127.0.0.1:%d", s.Port)
	fmt.Printf("Session Roadmap: http://%s\n", addr)
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
	// Return cached result if still valid
	s.cacheMu.RLock()
	if s.cachedResult != nil && time.Now().Before(s.cacheExpiry) {
		data := s.cachedResult
		s.cacheMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}
	s.cacheMu.RUnlock()

	store, err := s.StoreLoader.LoadStore()
	if err != nil {
		log.Printf("roadmap: failed to load store: %v", err)
		http.Error(w, "failed to load session data", http.StatusInternalServerError)
		return
	}

	active := store.Active()

	entries := make([]RoadmapEntry, 0, len(active))
	for _, ctx := range active {
		phase := ctx.Phase
		// If no cached phase, do a fast scan for this context
		if phase == "" && ctx.Worktree != "" {
			phase = s.Scanner.scanWithMode(&ctx, ScanModeFast)
		}
		entries = append(entries, RoadmapEntry{
			Name:          ctx.Name,
			Branch:        ctx.Branch,
			Status:        ctx.Status,
			Phase:         phase,
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

	data, err := json.Marshal(entries)
	if err != nil {
		log.Printf("roadmap: failed to marshal entries: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Cache the result
	s.cacheMu.Lock()
	s.cachedResult = data
	s.cacheExpiry = time.Now().Add(cacheTTL)
	s.cacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
