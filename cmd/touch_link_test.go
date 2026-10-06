package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

// linkFixture は t1（タスク）と theme（テーマ island）を持つ islands と、claude の 1 context を用意する。
func linkFixture(t *testing.T, contexts ...model.Context) *storage.Storage {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s, err := storage.New()
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) == 0 {
		contexts = []model.Context{{Name: "c1", SessionID: "s1", Status: model.StatusInProgress}}
	}
	if err := s.SaveStore(&model.Store{Contexts: contexts}); err != nil {
		t.Fatal(err)
	}
	err = s.UpdateIslands(func(is *model.IslandStore) error {
		if _, err := is.AddIsland("テーマ", "theme", ""); err != nil {
			return err
		}
		_, err := is.AddTask("面接の質問を作る", "island:theme")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func promptInput(session, prompt string) hookInput {
	return hookInput{SessionID: session, HookEventName: "UserPromptSubmit", Prompt: prompt}
}

// touchHook は hook 経路（quick + track-state）の touch を 1 回流す。
func touchHook(t *testing.T, s *storage.Storage, provider model.Provider, input hookInput, at time.Time) {
	t.Helper()
	if _, _, err := touchOnce(s, provider, input, nil, at, true, true); err != nil {
		t.Fatal(err)
	}
}

func ctxByName(t *testing.T, s *storage.Storage, name string) model.Context {
	t.Helper()
	store, err := s.LoadStore()
	if err != nil {
		t.Fatal(err)
	}
	c := store.FindByName(name)
	if c == nil {
		t.Fatalf("context %s not found", name)
	}
	return *c
}

func TestParseHookInput_Prompt(t *testing.T) {
	in, err := parseHookInput(strings.NewReader(`{"session_id":"c1","hook_event_name":"UserPromptSubmit","prompt":"hello\n[devctx:task:t1]"}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if in.Prompt != "hello\n[devctx:task:t1]" {
		t.Fatalf("prompt = %q", in.Prompt)
	}
}

func TestTouch_MarkerLinksTask(t *testing.T) {
	s := linkFixture(t)
	at := time.Now()
	touchHook(t, s, model.ProviderClaude, promptInput("s1", "面接の質問を作る\n\n[devctx:task:t1]\n</pasted_content id=\"x\">"), at)
	c := ctxByName(t, s, "c1")
	if c.TaskRef != "island:t1" || c.TaskLinkSession != "s1" || c.TaskLinkSource != "marker" {
		t.Fatalf("link = %+v", c)
	}
	if c.AgentState != model.AgentRunning {
		t.Errorf("state should still be tracked: %q", c.AgentState)
	}
}

func TestTouch_MarkerForMissingTaskIsIgnored(t *testing.T) {
	s := linkFixture(t)
	touchHook(t, s, model.ProviderClaude, promptInput("s1", "x\n[devctx:task:t9]"), time.Now())
	if c := ctxByName(t, s, "c1"); c.TaskRef != "" {
		t.Fatalf("link = %q, want none", c.TaskRef)
	}
}

func TestTouch_MarkerForNonTaskIslandIsIgnored(t *testing.T) {
	s := linkFixture(t)
	// "theme" は task id の形式ではないので marker にならないが、t<n> の id を持つテーマ island が手書きされた場合も弾く
	if err := s.UpdateIslands(func(is *model.IslandStore) error {
		_, err := is.AddIsland("手書き", "t7", "")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	touchHook(t, s, model.ProviderClaude, promptInput("s1", "x\n[devctx:task:t7]"), time.Now())
	if c := ctxByName(t, s, "c1"); c.TaskRef != "" {
		t.Fatalf("link = %q, want none", c.TaskRef)
	}
}

func TestTouch_SecondMarkerInSameSessionIsIgnored(t *testing.T) {
	s := linkFixture(t)
	if err := s.UpdateIslands(func(is *model.IslandStore) error { _, err := is.AddTask("別件", "island:theme"); return err }); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	touchHook(t, s, model.ProviderClaude, promptInput("s1", "a\n[devctx:task:t1]"), now)
	touchHook(t, s, model.ProviderClaude, promptInput("s1", "b\n[devctx:task:t2]"), now.Add(time.Minute))
	if c := ctxByName(t, s, "c1"); c.TaskRef != "island:t1" {
		t.Fatalf("link = %q, want island:t1", c.TaskRef)
	}
}

func TestTouch_NewSessionMarkerRelinks(t *testing.T) {
	s := linkFixture(t)
	if err := s.UpdateIslands(func(is *model.IslandStore) error { _, err := is.AddTask("別件", "island:theme"); return err }); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	touchHook(t, s, model.ProviderClaude, promptInput("s1", "a\n[devctx:task:t1]"), now)
	// register が新しい session id を同じ context に書く（Claude は worktree 単位）
	if err := s.UpdateStore(func(st *model.Store) error { st.FindByName("c1").SessionID = "s2"; return nil }); err != nil {
		t.Fatal(err)
	}
	// 新セッションの最初のプロンプトが marker なしでも、前のリンクは残る
	touchHook(t, s, model.ProviderClaude, promptInput("s2", "marker なし"), now.Add(time.Minute))
	if c := ctxByName(t, s, "c1"); c.TaskRef != "island:t1" {
		t.Fatalf("link should persist across sessions: %q", c.TaskRef)
	}
	touchHook(t, s, model.ProviderClaude, promptInput("s2", "b\n[devctx:task:t2]"), now.Add(2*time.Minute))
	c := ctxByName(t, s, "c1")
	if c.TaskRef != "island:t2" || c.TaskLinkSession != "s2" {
		t.Fatalf("link = %+v", c)
	}
}

func TestTouch_CodexPerSessionContext(t *testing.T) {
	s := linkFixture(t,
		model.Context{Name: "x1", SessionID: "k1", Provider: model.ProviderCodex, Status: model.StatusInProgress},
		model.Context{Name: "x2", SessionID: "k2", Provider: model.ProviderCodex, Status: model.StatusInProgress},
	)
	touchHook(t, s, model.ProviderCodex, promptInput("k2", "a\n[devctx:task:t1]"), time.Now())
	if c := ctxByName(t, s, "x1"); c.TaskRef != "" {
		t.Errorf("x1 linked: %q", c.TaskRef)
	}
	if c := ctxByName(t, s, "x2"); c.TaskRef != "island:t1" {
		t.Errorf("x2 link = %q", c.TaskRef)
	}
}

func TestTouchIsNoop_DoesNotSkipAPromptWithMarker(t *testing.T) {
	s := linkFixture(t)
	now := time.Now()
	// 状態は running・last_seen も新しいので、marker が無ければ no-op
	touchHook(t, s, model.ProviderClaude, promptInput("s1", "plain"), now)
	if !touchIsNoop(s, model.ProviderClaude, promptInput("s1", "plain again"), nil, now.Add(time.Second), true, true) {
		t.Fatal("precondition: a plain prompt should be a no-op here")
	}
	if touchIsNoop(s, model.ProviderClaude, promptInput("s1", "x\n[devctx:task:t1]"), nil, now.Add(time.Second), true, true) {
		t.Fatal("a prompt with a marker must reach the locked update")
	}
	touchHook(t, s, model.ProviderClaude, promptInput("s1", "x\n[devctx:task:t1]"), now.Add(time.Second))
	if c := ctxByName(t, s, "c1"); c.TaskRef != "island:t1" {
		t.Fatalf("link = %q", c.TaskRef)
	}
}

func TestTouch_PromptTextIsNeverPersisted(t *testing.T) {
	s := linkFixture(t)
	secret := "ひみつのプロンプト本文-9f3a"
	touchHook(t, s, model.ProviderClaude, promptInput("s1", secret+"\n[devctx:task:t1]"), time.Now())
	touchHook(t, s, model.ProviderClaude, promptInput("s1", secret+" 2回目"), time.Now().Add(time.Minute))
	home, _ := os.UserHomeDir()
	entries, err := filepath.Glob(filepath.Join(home, ".config", "devctx", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range entries {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "ひみつ") {
			t.Errorf("%s contains prompt text", f)
		}
	}
}

// リンクはタスクの完了状態もコミットメントも動かさない。hook とマイルストーン記録は islands.yaml に触れない。
func TestTouch_LinkLeavesIslandsUntouched(t *testing.T) {
	s := linkFixture(t)
	home, _ := os.UserHomeDir()
	islandsPath := filepath.Join(home, ".config", "devctx", "islands.yaml")
	before, err := os.ReadFile(islandsPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	touchHook(t, s, model.ProviderClaude, promptInput("s1", "a\n[devctx:task:t1]"), now)
	touchHook(t, s, model.ProviderClaude, hookInput{SessionID: "s1", HookEventName: "Stop"}, now.Add(time.Minute))
	// PR 作成・マージのマイルストーンはイベントに記録されるだけで、タスクを動かさない
	for _, mt := range []model.MilestoneType{model.MilestonePRCreated, model.MilestonePRMerged} {
		recordEvent(s, "c1", mt, "https://example.com/pr/1")
	}
	after, err := os.ReadFile(islandsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("islands.yaml changed:\n%s\n---\n%s", before, after)
	}
	is, _ := s.LoadIslands()
	for _, i := range is.Islands {
		if i.Done {
			t.Errorf("task %s became done", i.ID)
		}
	}
}
