package cmd

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hiroyannnn/devctx/agentview"
	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// newConcurrencyStorage は隔離した HOME に context a / b を保存した Storage を返す。
func newConcurrencyStorage(t *testing.T) *storage.Storage {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, err := storage.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	store := &model.Store{Contexts: []model.Context{
		{Name: "a", Status: model.StatusInProgress, SessionID: "s1", LastSeen: now, AgentState: model.AgentRunning, AgentStateAt: now},
		{Name: "b", Status: model.StatusInProgress, SessionID: "s2", LastSeen: now, AgentState: model.AgentRunning, AgentStateAt: now},
	}}
	if err := s.SaveStore(store); err != nil {
		t.Fatal(err)
	}
	return s
}

// concurrentAgentState は画面や CLI が store を読んだ後に hook（touch 等）が書く状況を再現する。
func concurrentAgentState(t *testing.T, s *storage.Storage, name string, state model.AgentState) {
	t.Helper()
	err := s.UpdateStore(func(store *model.Store) error {
		store.FindByName(name).AgentState = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func loadContext(t *testing.T, s *storage.Storage, name string) *model.Context {
	t.Helper()
	store, err := s.LoadStore()
	if err != nil {
		t.Fatal(err)
	}
	return store.FindByName(name)
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestTuiMoveKeepsConcurrentAgentState(t *testing.T) {
	s := newConcurrencyStorage(t)
	stubSnapshot(t, agentview.Snapshot{})
	store, err := s.LoadStore()
	if err != nil {
		t.Fatal(err)
	}
	m := newTuiModel(store, s)
	name := m.list.SelectedItem().(contextItem).ctx.Name

	concurrentAgentState(t, s, name, model.AgentNeedsInput)
	next, _ := m.moveSelected(model.StatusReview)

	c := loadContext(t, s, name)
	if c.Status != model.StatusReview {
		t.Errorf("status = %s, want review", c.Status)
	}
	if c.AgentState != model.AgentNeedsInput {
		t.Errorf("並行して書かれた agent_state が消えた: %s", c.AgentState)
	}
	// 画面の model も保存後の store に追従する
	got := next.(tuiModel).store.FindByName(name)
	if got.AgentState != model.AgentNeedsInput || got.Status != model.StatusReview {
		t.Errorf("in-memory store が更新されていない: %+v", got)
	}
}

func TestTuiDeleteKeepsConcurrentChangeOnOtherContext(t *testing.T) {
	s := newConcurrencyStorage(t)
	stubSnapshot(t, agentview.Snapshot{})
	store, err := s.LoadStore()
	if err != nil {
		t.Fatal(err)
	}
	m := newTuiModel(store, s)
	selected := m.list.SelectedItem().(contextItem).ctx.Name
	other := "b"
	if selected == "b" {
		other = "a"
	}

	concurrentAgentState(t, s, other, model.AgentNeedsInput)
	next, _ := m.Update(keyRunes("d"))

	if loadContext(t, s, selected) != nil {
		t.Errorf("[%s] が削除されていない", selected)
	}
	if c := loadContext(t, s, other); c == nil || c.AgentState != model.AgentNeedsInput {
		t.Errorf("並行して書かれた [%s] の agent_state が消えた: %+v", other, c)
	}
	if got := next.(tuiModel).store.FindByName(other); got == nil || got.AgentState != model.AgentNeedsInput {
		t.Errorf("in-memory store が更新されていない: %+v", got)
	}
}

func TestKanbanMoveKeepsConcurrentAgentState(t *testing.T) {
	s := newConcurrencyStorage(t)
	stubSnapshot(t, agentview.Snapshot{})
	m := newKanbanModel(s)
	name := m.selectedContext().Name

	concurrentAgentState(t, s, name, model.AgentNeedsInput)
	next, _ := m.moveSelectedTo(model.StatusBlocked, "moved")

	c := loadContext(t, s, name)
	if c.Status != model.StatusBlocked {
		t.Errorf("status = %s, want blocked", c.Status)
	}
	if c.AgentState != model.AgentNeedsInput {
		t.Errorf("並行して書かれた agent_state が消えた: %s", c.AgentState)
	}
	km := next.(*kanbanModel)
	if km.message != "moved" {
		t.Errorf("message = %q", km.message)
	}
	if got := km.store.FindByName(name); got.AgentState != model.AgentNeedsInput {
		t.Errorf("in-memory store が更新されていない: %+v", got)
	}
}

func TestKanbanDeleteKeepsConcurrentChangeOnOtherContext(t *testing.T) {
	s := newConcurrencyStorage(t)
	stubSnapshot(t, agentview.Snapshot{})
	m := newKanbanModel(s)
	selected := m.selectedContext().Name
	other := "b"
	if selected == "b" {
		other = "a"
	}

	concurrentAgentState(t, s, other, model.AgentNeedsInput)
	next, _ := m.Update(keyRunes("x"))

	if loadContext(t, s, selected) != nil {
		t.Errorf("[%s] が削除されていない", selected)
	}
	if c := loadContext(t, s, other); c == nil || c.AgentState != model.AgentNeedsInput {
		t.Errorf("並行して書かれた [%s] の agent_state が消えた: %+v", other, c)
	}
	km := next.(kanbanModel)
	if len(km.contexts) != 1 {
		t.Errorf("表示中の context 数 = %d, want 1", len(km.contexts))
	}
	if got := km.store.FindByName(other); got == nil || got.AgentState != model.AgentNeedsInput {
		t.Errorf("in-memory store が更新されていない: %+v", got)
	}
}

func TestApplyPhaseResultsKeepsConcurrentAgentState(t *testing.T) {
	s := newConcurrencyStorage(t)

	// gh scan の最中に hook が書く
	concurrentAgentState(t, s, "a", model.AgentNeedsInput)
	changes, err := applyPhaseResults(s, map[string]model.Phase{"a": model.PhasePROpen, "gone": model.PhaseDone}, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	c := loadContext(t, s, "a")
	if c.Phase != model.PhasePROpen || c.PhaseCheckedAt.IsZero() {
		t.Errorf("phase が反映されていない: %+v", c)
	}
	if c.AgentState != model.AgentNeedsInput {
		t.Errorf("並行して書かれた agent_state が消えた: %s", c.AgentState)
	}
	if len(changes) != 1 || changes[0].Name != "a" || changes[0].Old != "" || changes[0].New != model.PhasePROpen {
		t.Errorf("changes = %+v", changes)
	}
}

func TestApplyPhaseResultsKeepsKnownPhaseWhenScanIsIdle(t *testing.T) {
	s := newConcurrencyStorage(t)
	err := s.UpdateStore(func(store *model.Store) error {
		store.FindByName("a").Phase = model.PhasePushed
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := applyPhaseResults(s, map[string]model.Phase{"a": model.PhaseIdle}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if c := loadContext(t, s, "a"); c.Phase != model.PhasePushed {
		t.Errorf("idle の scan 結果で既知の phase が上書きされた: %s", c.Phase)
	}
}

func TestApplyMoveKeepsConcurrentAgentState(t *testing.T) {
	s := newConcurrencyStorage(t)

	// checklist の対話中に hook が書く
	concurrentAgentState(t, s, "a", model.AgentNeedsInput)
	now := time.Now().Add(time.Minute)
	if err := applyMove(s, "a", model.StatusReview, map[string]bool{"/compact": true}, now); err != nil {
		t.Fatal(err)
	}

	c := loadContext(t, s, "a")
	if c.Status != model.StatusReview || !c.LastSeen.Equal(now) || !c.Checklist["/compact"] {
		t.Errorf("move が反映されていない: %+v", c)
	}
	if c.AgentState != model.AgentNeedsInput {
		t.Errorf("並行して書かれた agent_state が消えた: %s", c.AgentState)
	}
}

func TestApplyLinkUpdatesKeepsConcurrentAgentState(t *testing.T) {
	s := newConcurrencyStorage(t)

	// gh pr view の最中に hook が書く
	concurrentAgentState(t, s, "a", model.AgentNeedsInput)
	err := applyLinkUpdates(s, map[string]linkUpdate{
		"a": {PRURL: "https://github.com/o/r/pull/1", SessionName: "slug"},
	})
	if err != nil {
		t.Fatal(err)
	}

	c := loadContext(t, s, "a")
	if c.PRURL != "https://github.com/o/r/pull/1" || c.SessionName != "slug" || c.IssueURL != "" {
		t.Errorf("link が反映されていない: %+v", c)
	}
	if c.AgentState != model.AgentNeedsInput {
		t.Errorf("並行して書かれた agent_state が消えた: %s", c.AgentState)
	}
}

func TestRemoveContextsKeepsConcurrentChangeOnOtherContext(t *testing.T) {
	s := newConcurrencyStorage(t)

	// clean の確認プロンプト中に hook が書く
	concurrentAgentState(t, s, "b", model.AgentNeedsInput)
	_, removed, err := removeContexts(s, "a", "gone")
	if err != nil {
		t.Fatal(err)
	}

	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if loadContext(t, s, "a") != nil {
		t.Error("a が削除されていない")
	}
	if c := loadContext(t, s, "b"); c == nil || c.AgentState != model.AgentNeedsInput {
		t.Errorf("並行して書かれた b の agent_state が消えた: %+v", c)
	}
}

func TestUpdateContextReturnsNotFound(t *testing.T) {
	s := newConcurrencyStorage(t)
	_, err := updateContext(s, "gone", func(*model.Context) error { return nil })
	if err == nil {
		t.Fatal("存在しない context で error にならない")
	}
}
