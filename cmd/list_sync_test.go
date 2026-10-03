package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
)

func TestSyncCodexSessions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := storage.New()
	if err != nil {
		t.Fatal(err)
	}

	codexHome := t.TempDir()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	cwd := t.TempDir() // git 管理外
	writeRollout(t, codexHome, "2026/10/03", "id-new", metaLine("id-new", cwd, "user", `"vscode"`), now.Add(-time.Hour))
	writeRollout(t, codexHome, "2026/10/03", "id-auto", metaLine("id-auto", cwd, "automation", `"vscode"`), now.Add(-time.Hour))
	adapter := codexAdapter{home: codexHome, days: 2, now: func() time.Time { return now }}

	imported, changed, err := syncCodexSessions(s, adapter)
	if err != nil || len(imported) != 1 || !changed {
		t.Fatalf("imported=%v changed=%v err=%v", imported, changed, err)
	}
	saved, _ := s.LoadStore()
	if len(saved.Contexts) != 1 || saved.Contexts[0].Provider != model.ProviderCodex ||
		saved.Contexts[0].SessionID != "id-new" || saved.Contexts[0].Worktree != cwd {
		t.Fatalf("unexpected store: %+v", saved.Contexts)
	}

	// 2 回目は何も変わらない（import なし）
	before, _ := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "devctx", "contexts.yaml"))
	imported, changed, err = syncCodexSessions(s, adapter)
	if err != nil || len(imported) != 0 || changed {
		t.Fatalf("second sync imported=%v changed=%v err=%v", imported, changed, err)
	}
	after, _ := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "devctx", "contexts.yaml"))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("contexts.yaml must not be rewritten when nothing changed")
	}
}

func TestSyncCodexSessions_RefreshesRegisteredFromTranscript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := storage.New()
	if err != nil {
		t.Fatal(err)
	}
	codexHome := t.TempDir()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	// An old session resumed today: its file is in an old day dir outside the sync window
	transcript := writeRollout(t, codexHome, "2026/08/01", "id-old", metaLine("id-old", "/w/x", "user", `"cli"`), now.Add(-10*time.Minute))
	if err := s.SaveStore(&model.Store{Contexts: []model.Context{
		{Name: "x-codex", Provider: model.ProviderCodex, SessionID: "id-old", TranscriptPath: transcript,
			LastSeen: now.Add(-48 * time.Hour), AgentState: model.AgentTurnDone},
	}}); err != nil {
		t.Fatal(err)
	}

	adapter := codexAdapter{home: codexHome, days: 2, now: func() time.Time { return now }, skipResumedSearch: true}
	imported, changed, err := syncCodexSessions(s, adapter)
	if err != nil || len(imported) != 0 || !changed {
		t.Fatalf("imported=%v changed=%v err=%v", imported, changed, err)
	}
	saved, _ := s.LoadStore()
	got := saved.Contexts[0]
	if !got.LastSeen.Equal(now.Add(-10*time.Minute)) || got.AgentState != model.AgentTurnDone {
		t.Fatalf("LastSeen should follow the transcript mtime without touching AgentState: %+v", got)
	}
}

func mustLoadStore(t *testing.T, s *storage.Storage) *model.Store {
	t.Helper()
	store, err := s.LoadStore()
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestFirstRunImport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := storage.New()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	sessions := []DiscoveredSession{
		{Provider: model.ProviderClaude, SessionID: "abc", ProjectPath: dir, LastModified: now.Add(-time.Hour)}, // shorter than 6 chars
		{Provider: model.ProviderClaude, SessionID: "abcdef-2", ProjectPath: dir, LastModified: now.Add(-2 * time.Hour)},
		{Provider: model.ProviderClaude, SessionID: "old-session", ProjectPath: dir, LastModified: now.Add(-72 * time.Hour)},
		{Provider: model.ProviderClaude, SessionID: "registered", ProjectPath: dir, LastModified: now.Add(-time.Hour), IsRegistered: true},
	}
	updated, err := firstRunImport(s, sessions, now)
	if err != nil || updated == nil {
		t.Fatalf("updated=%v err=%v", updated, err)
	}
	saved := mustLoadStore(t, s)
	if len(saved.Contexts) != 2 {
		t.Fatalf("only recent unregistered sessions should be imported: %+v", saved.Contexts)
	}
	base := filepath.Base(dir)
	if saved.Contexts[0].Name != base || saved.Contexts[1].Name != base+"-abcdef" {
		t.Fatalf("names = %q, %q", saved.Contexts[0].Name, saved.Contexts[1].Name)
	}
	if saved.Contexts[0].Provider != "" || saved.Contexts[0].Worktree != dir {
		t.Fatalf("claude import keeps provider empty and cwd as worktree: %+v", saved.Contexts[0])
	}

	if again, err := firstRunImport(s, sessions[:3], now); err != nil || again != nil {
		t.Fatalf("nothing new should not write: %v %v", again, err)
	}
}

func TestAutoImportEnabled(t *testing.T) {
	on, off := true, false
	for name, tc := range map[string]struct {
		cfg  *model.Config
		want bool
	}{
		"nil config":   {nil, true},
		"nil flag":     {&model.Config{}, true},
		"explicit on":  {&model.Config{AutoImport: &on}, true},
		"explicit off": {&model.Config{AutoImport: &off}, false},
	} {
		if got := autoImportEnabled(tc.cfg); got != tc.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
}
