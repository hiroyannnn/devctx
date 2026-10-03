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
