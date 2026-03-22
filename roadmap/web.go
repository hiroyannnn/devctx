package roadmap

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// InsightLoader abstracts insight loading for testing.
type InsightLoader interface {
	LoadInsights() (*model.InsightStore, error)
}

// EventLoader abstracts event loading for testing.
type EventLoader interface {
	LoadEvents() (*model.EventStore, error)
}

// ProjectGroup groups sessions by project (repo root).
type ProjectGroup struct {
	Name     string         `json:"name"`
	RepoRoot string         `json:"repo_root"`
	Sessions []RoadmapEntry `json:"sessions"`
}

// SessionTimeline holds the event timeline for a single session.
type SessionTimeline struct {
	SessionName string               `json:"session_name"`
	Events      []model.SessionEvent `json:"events"`
	Summary     model.MilestoneSummary `json:"summary"`
}

// RoadmapEntry is the JSON response structure for each context.
type RoadmapEntry struct {
	Name           string              `json:"name"`
	Branch         string              `json:"branch"`
	Status         model.Status        `json:"status"`
	Phase          model.Phase         `json:"phase"`
	InitialPrompt  string              `json:"initial_prompt,omitempty"`
	Worktree       string              `json:"worktree"`
	PRURL          string              `json:"pr_url,omitempty"`
	IssueURL       string              `json:"issue_url,omitempty"`
	Note           string              `json:"note,omitempty"`
	SessionName    string              `json:"session_name,omitempty"`
	CreatedAt      string              `json:"created_at"`
	LastSeen       string              `json:"last_seen"`
	Goal           string              `json:"goal,omitempty"`
	CurrentFocus   string              `json:"current_focus,omitempty"`
	NextStep       string              `json:"next_step,omitempty"`
	AttentionState model.AttentionState `json:"attention_state,omitempty"`
	InferredAt     string              `json:"inferred_at,omitempty"`
	RepoRoot       string              `json:"repo_root,omitempty"`
	Milestones     *model.MilestoneSummary `json:"milestones,omitempty"`
	Topics         []model.SemanticTopic  `json:"topics,omitempty"`
	Tasks          []model.TaskItem       `json:"tasks,omitempty"`
}

// Server serves the roadmap web UI.
type Server struct {
	StoreLoader   StoreLoader
	InsightLoader InsightLoader
	EventLoader   EventLoader
	Scanner       *Scanner
	Port          int

	cacheMu      sync.RWMutex
	cachedResult []byte
	cacheExpiry  time.Time
}

const cacheTTL = 5 * time.Second

// NewServer creates a new Server.
func NewServer(loader StoreLoader, insightLoader InsightLoader, eventLoader EventLoader, scanner *Scanner, port int) *Server {
	return &Server{StoreLoader: loader, InsightLoader: insightLoader, EventLoader: eventLoader, Scanner: scanner, Port: port}
}

// ListenAndServe starts the HTTP server on localhost only.
func (s *Server) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/roadmap", s.handleAPIRoadmap)
	mux.HandleFunc("/api/roadmap-map", s.handleAPIRoadmapMap)
	mux.HandleFunc("/api/roadmap-graph", s.handleAPIRoadmapGraph)
	mux.HandleFunc("/api/timeline/", s.handleAPITimeline)
	mux.HandleFunc("/", s.handleIndex)

	addr := fmt.Sprintf("127.0.0.1:%d", s.Port)
	url := fmt.Sprintf("http://%s", addr)
	fmt.Printf("Session Roadmap: %s\n", url)
	fmt.Println("Press Ctrl+C to stop")

	// Auto-open browser after listener is established
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go openBrowser(url)

	return http.Serve(ln, mux)
}

// openBrowser opens the given URL in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	go cmd.Wait()
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

	// Load insights (non-fatal if fails)
	var insights *model.InsightStore
	if s.InsightLoader != nil {
		insights, _ = s.InsightLoader.LoadInsights()
	}

	// Load events (non-fatal if fails)
	var events *model.EventStore
	if s.EventLoader != nil {
		events, _ = s.EventLoader.LoadEvents()
	}

	entries := make([]RoadmapEntry, 0, len(active))
	for _, ctx := range active {
		phase := ctx.Phase
		// If no cached phase, do a fast scan for this context
		if phase == "" && ctx.Worktree != "" && s.Scanner != nil {
			phase = s.Scanner.scanWithMode(&ctx, ScanModeFast)
		}

		entry := RoadmapEntry{
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
			RepoRoot:      ctx.RepoRoot,
		}

		// Merge milestone data
		if events != nil {
			summary := events.Summarize(ctx.Name)
			if summary.CommitCount > 0 || summary.SessionCount > 0 {
				entry.Milestones = &summary
			}
		}

		// Merge insight data
		if insights != nil {
			if insight := insights.Get(ctx.Name); insight != nil {
				entry.Goal = insight.Goal
				entry.CurrentFocus = insight.CurrentFocus
				entry.NextStep = insight.NextStep
				entry.AttentionState = insight.AttentionState
				entry.Topics = insight.Topics
				entry.Tasks = insight.Tasks
				if !insight.InferredAt.IsZero() {
					entry.InferredAt = insight.InferredAt.Format("2006-01-02 15:04")
				}
			}
		}

		entries = append(entries, entry)
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

func (s *Server) handleAPIRoadmapMap(w http.ResponseWriter, r *http.Request) {
	store, err := s.StoreLoader.LoadStore()
	if err != nil {
		log.Printf("roadmap-map: failed to load store: %v", err)
		http.Error(w, "failed to load session data", http.StatusInternalServerError)
		return
	}

	active := store.Active()

	var insights *model.InsightStore
	if s.InsightLoader != nil {
		insights, _ = s.InsightLoader.LoadInsights()
	}

	var events *model.EventStore
	if s.EventLoader != nil {
		events, _ = s.EventLoader.LoadEvents()
	}

	// Group by project (repo root)
	projectMap := make(map[string]*ProjectGroup)
	var projectOrder []string

	for _, ctx := range active {
		phase := ctx.Phase
		if phase == "" && ctx.Worktree != "" && s.Scanner != nil {
			phase = s.Scanner.scanWithMode(&ctx, ScanModeFast)
		}

		entry := RoadmapEntry{
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
			RepoRoot:      ctx.RepoRoot,
		}

		if events != nil {
			summary := events.Summarize(ctx.Name)
			if summary.CommitCount > 0 || summary.SessionCount > 0 {
				entry.Milestones = &summary
			}
		}

		if insights != nil {
			if insight := insights.Get(ctx.Name); insight != nil {
				entry.Goal = insight.Goal
				entry.CurrentFocus = insight.CurrentFocus
				entry.NextStep = insight.NextStep
				entry.AttentionState = insight.AttentionState
				entry.Topics = insight.Topics
				entry.Tasks = insight.Tasks
				if !insight.InferredAt.IsZero() {
					entry.InferredAt = insight.InferredAt.Format("2006-01-02 15:04")
				}
			}
		}

		projectKey := ctx.RepoRoot
		if projectKey == "" {
			projectKey = ctx.Worktree
		}
		projectName := filepath.Base(projectKey)

		if _, exists := projectMap[projectKey]; !exists {
			projectMap[projectKey] = &ProjectGroup{
				Name:     projectName,
				RepoRoot: projectKey,
			}
			projectOrder = append(projectOrder, projectKey)
		}
		projectMap[projectKey].Sessions = append(projectMap[projectKey].Sessions, entry)
	}

	// Build ordered result
	groups := make([]ProjectGroup, 0, len(projectOrder))
	for _, key := range projectOrder {
		groups = append(groups, *projectMap[key])
	}

	data, err := json.Marshal(groups)
	if err != nil {
		log.Printf("roadmap-map: failed to marshal: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *Server) handleAPIRoadmapGraph(w http.ResponseWriter, r *http.Request) {
	store, err := s.StoreLoader.LoadStore()
	if err != nil {
		log.Printf("roadmap-graph: failed to load store: %v", err)
		http.Error(w, "failed to load session data", http.StatusInternalServerError)
		return
	}

	active := store.Active()

	var insights *model.InsightStore
	if s.InsightLoader != nil {
		insights, _ = s.InsightLoader.LoadInsights()
	}

	// Group by project (repo root)
	projectMap := make(map[string]*ProjectGraphGroup)
	var projectOrder []string

	for _, ctx := range active {
		repoRoot := ctx.RepoRoot
		if repoRoot == "" {
			repoRoot = ctx.Worktree
		}
		if repoRoot == "" {
			repoRoot = "__ungrouped__"
		}

		group, exists := projectMap[repoRoot]
		if !exists {
			name := filepath.Base(repoRoot)
			if repoRoot == "__ungrouped__" {
				name = "Other"
			}
			group = &ProjectGraphGroup{
				Name:     name,
				RepoRoot: repoRoot,
			}
			projectMap[repoRoot] = group
			projectOrder = append(projectOrder, repoRoot)
		}

		entry := RoadmapEntry{
			Name:     ctx.Name,
			Branch:   ctx.Branch,
			Status:   ctx.Status,
			Phase:    ctx.Phase,
			PRURL:    ctx.PRURL,
			IssueURL: ctx.IssueURL,
		}

		if insights != nil {
			if insight := insights.Get(ctx.Name); insight != nil {
				entry.Goal = insight.Goal
				entry.CurrentFocus = insight.CurrentFocus
				entry.NextStep = insight.NextStep
				entry.AttentionState = insight.AttentionState
				entry.Tasks = insight.Tasks
				entry.Topics = insight.Topics
				if !insight.InferredAt.IsZero() {
					entry.InferredAt = insight.InferredAt.Format("2006-01-02 15:04")
				}
			}
		}

		graph := BuildSessionGraph(entry)
		group.Sessions = append(group.Sessions, graph)
	}

	result := make([]ProjectGraphGroup, 0, len(projectOrder))
	for _, root := range projectOrder {
		result = append(result, *projectMap[root])
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleAPITimeline(w http.ResponseWriter, r *http.Request) {
	// Extract session name from URL: /api/timeline/{name}
	sessionName := strings.TrimPrefix(r.URL.Path, "/api/timeline/")
	if sessionName == "" {
		http.Error(w, "session name required", http.StatusBadRequest)
		return
	}

	var events *model.EventStore
	if s.EventLoader != nil {
		var err error
		events, err = s.EventLoader.LoadEvents()
		if err != nil {
			log.Printf("timeline: failed to load events: %v", err)
			http.Error(w, "failed to load events", http.StatusInternalServerError)
			return
		}
	}

	if events == nil {
		events = &model.EventStore{}
	}

	timeline := SessionTimeline{
		SessionName: sessionName,
		Events:      events.ForSession(sessionName),
		Summary:     events.Summarize(sessionName),
	}

	// Ensure Events is non-nil for JSON
	if timeline.Events == nil {
		timeline.Events = []model.SessionEvent{}
	}

	data, err := json.Marshal(timeline)
	if err != nil {
		log.Printf("timeline: failed to marshal: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
