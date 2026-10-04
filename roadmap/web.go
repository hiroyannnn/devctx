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

	"github.com/hiroyannnn/devctx/agentview"
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
	Provider       model.Provider         `json:"provider"`
	AgentState     model.AgentState       `json:"agent_state,omitempty"`
	AgentStateAt   string                 `json:"agent_state_at,omitempty"`
	// 表示用。待ち判定とラベルは model 側を正とし、Web で判定を再実装しない
	AgentStateLabel string                `json:"agent_state_label,omitempty"`
	AgentWaiting    bool                  `json:"agent_waiting,omitempty"`
	// 待ちの詳細（待ち要求のラベル、なければ live の待ち理由 permission prompt 等）と状態の出どころ（live / hook）。live は agent view 由来
	AgentWaitingFor  string                `json:"agent_waiting_for,omitempty"`
	AgentStateSource string                `json:"agent_state_source,omitempty"`
}

// applyAgentFields は provider（空なら claude）と、view（live と hook を突き合わせた状態）を entry に写す。
// AgentStateAt は hook の観測時刻のまま（live には観測時刻がない）。
func applyAgentFields(entry *RoadmapEntry, ctx model.Context, view agentview.View) {
	entry.Provider = ctx.EffectiveProvider()
	entry.AgentState = view.State
	entry.AgentStateLabel = view.State.Label()
	entry.AgentWaiting = view.State.WaitsForUser()
	entry.AgentWaitingFor = view.Detail()
	entry.AgentStateSource = view.Source
	if !ctx.AgentStateAt.IsZero() {
		entry.AgentStateAt = ctx.AgentStateAt.Format(time.RFC3339)
	}
}

// LiveSource は agent view の最新 snapshot を返す。ハンドラはこれを読むだけで claude を待たない。
type LiveSource interface {
	Snapshot() agentview.Snapshot
}

// Server serves the roadmap web UI.
type Server struct {
	StoreLoader   StoreLoader
	InsightLoader InsightLoader
	EventLoader   EventLoader
	// IslandLoader は /api/islands の供給元。nil なら空の木を返す（NewServer の引数を増やさず既存の呼び出しを保つ）
	IslandLoader IslandLoader
	// IslandUpdater は POST /api/islands/ops の書き込み先。nil なら 503（編集は有効化した呼び出しだけに限る）
	IslandUpdater IslandUpdater
	Scanner       *Scanner
	Port          int
	// Live は agent view の snapshot 供給元。nil なら無効（hook 状態のみ。テストで claude を実行しない）
	Live LiveSource

	cacheMu      sync.RWMutex
	cachedResult []byte
	cacheExpiry  time.Time

	// phaseMu は phaseCache と、その miss 時の scan を守る（scanPhases）
	phaseMu    sync.Mutex
	phaseCache map[phaseKey]cachedPhase
	// now は phase キャッシュと /api/roadmap の応答キャッシュの時計。nil なら time.Now（テストで TTL を待たずに進めるため）
	now func() time.Time
}

const cacheTTL = 5 * time.Second

// agentViews は agent view を重ねた表示状態を context 名で返す。store には書き戻さない。
// all には表示対象に絞らず store の全 context を渡す。Done / 保持期間切れの context が
// 所有する live セッションを、同じ worktree の別 context に誤って当てないため。
func (s *Server) agentViews(all []model.Context) map[string]agentview.View {
	var snap agentview.Snapshot
	if s.Live != nil {
		snap = s.Live.Snapshot()
	}
	return agentview.Overlay(all, snap)
}

// NewServer creates a new Server.
func NewServer(loader StoreLoader, insightLoader InsightLoader, eventLoader EventLoader, scanner *Scanner, port int) *Server {
	return &Server{StoreLoader: loader, InsightLoader: insightLoader, EventLoader: eventLoader, Scanner: scanner, Port: port}
}

// Handler は全 endpoint をまとめ、リクエストガード（Host / Origin 検査）で包んだ http.Handler を返す。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/roadmap", s.handleAPIRoadmap)
	mux.HandleFunc("/api/roadmap-map", s.handleAPIRoadmapMap)
	mux.HandleFunc("/api/roadmap-graph", s.handleAPIRoadmapGraph)
	mux.HandleFunc("/api/islands", s.handleAPIIslands)
	mux.HandleFunc("/api/islands/ops", s.handleAPIIslandOps)
	mux.HandleFunc("/api/islands/known-repos", s.handleAPIKnownRepos)
	mux.HandleFunc("/api/timeline/", s.handleAPITimeline)
	mux.HandleFunc("/", s.handleIndex)
	return s.guard(mux)
}

// ListenAndServe starts the HTTP server on localhost only.
func (s *Server) ListenAndServe() error {
	// 先に listen する。--port 0 では、実際の port が分かってから Host の許可表を作る必要がある
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.Port))
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve は listener で配信する。Port には実際の待ち受け port を入れる。
func (s *Server) Serve(ln net.Listener) error {
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		s.Port = addr.Port
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", s.Port)
	fmt.Printf("Session Roadmap: %s\n", url)
	fmt.Println("Press Ctrl+C to stop")

	// Auto-open browser after listener is established
	go openBrowser(url)

	return http.Serve(ln, s.Handler())
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

// loadRoadmapEntries は /api/roadmap と /api/roadmap-map が共有する entry 組み立て。
// 返す entries は active と同じ順・同じ長さ（呼び出し側が index で ctx と突き合わせる）。
// Why not /api/roadmap-graph も寄せる: graph は Phase を scan せず ctx.Phase をそのまま使い、載せる項目も違う。
// phaseValidUntil は entries に載せた scan 結果の期限（scanPhases の validUntil）。
func (s *Server) loadRoadmapEntries() (active []model.Context, entries []RoadmapEntry, phaseValidUntil time.Time, err error) {
	store, err := s.StoreLoader.LoadStore()
	if err != nil {
		return nil, nil, time.Time{}, err
	}

	active = store.Active()
	views := s.agentViews(store.Contexts)

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

	scanned, phaseValidUntil := s.scanPhases(active)
	entries = make([]RoadmapEntry, 0, len(active))
	for _, ctx := range active {
		phase := ctx.Phase
		// If no cached phase, use the fast scan result for this context
		if s.needsPhaseScan(ctx) {
			phase = scanned[phaseKey{ctx.Worktree, ctx.Branch}]
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
			CreatedAt:     ctx.CreatedAt.Format(time.RFC3339),
			LastSeen:      ctx.LastSeen.Format(time.RFC3339),
			RepoRoot:      ctx.RepoRoot,
		}
		applyAgentFields(&entry, ctx, views[ctx.Name])

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
	return active, entries, phaseValidUntil, nil
}

func (s *Server) handleAPIRoadmap(w http.ResponseWriter, r *http.Request) {
	// Return cached result if still valid
	s.cacheMu.RLock()
	if s.cachedResult != nil && s.clock().Before(s.cacheExpiry) {
		data := s.cachedResult
		s.cacheMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}
	s.cacheMu.RUnlock()

	_, entries, phaseValidUntil, err := s.loadRoadmapEntries()
	if err != nil {
		log.Printf("roadmap: failed to load store: %v", err)
		http.Error(w, "failed to load session data", http.StatusInternalServerError)
		return
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
	s.cacheExpiry = s.clock().Add(cacheTTL)
	// 使った phase の期限より長く応答を使い回さない（phase の古さを TTL 以内に保つ）
	if !phaseValidUntil.IsZero() && phaseValidUntil.Before(s.cacheExpiry) {
		s.cacheExpiry = phaseValidUntil
	}
	s.cacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *Server) handleAPIRoadmapMap(w http.ResponseWriter, r *http.Request) {
	active, entries, _, err := s.loadRoadmapEntries()
	if err != nil {
		log.Printf("roadmap-map: failed to load store: %v", err)
		http.Error(w, "failed to load session data", http.StatusInternalServerError)
		return
	}

	// Group by project (repo root)
	projectMap := make(map[string]*ProjectGroup)
	var projectOrder []string

	for i, ctx := range active {
		entry := entries[i]

		// Why: symlink 経由と実パスで同じ repo が別グループに割れないよう、islands と共通の RepoKey で束ねる
		projectKey := model.RepoKey(ctx)
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
	views := s.agentViews(store.Contexts)

	var insights *model.InsightStore
	if s.InsightLoader != nil {
		insights, _ = s.InsightLoader.LoadInsights()
	}

	// Group by project (repo root)
	projectMap := make(map[string]*ProjectGraphGroup)
	var projectOrder []string

	for _, ctx := range active {
		repoRoot := model.RepoKey(ctx)
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
			LastSeen: ctx.LastSeen.Format(time.RFC3339),
		}
		applyAgentFields(&entry, ctx, views[ctx.Name])

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
