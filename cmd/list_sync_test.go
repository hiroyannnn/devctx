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

	store, err := s.LoadStore()
	if err != nil {
		t.Fatal(err)
	}
	imported, err := syncCodexSessions(s, adapter, store)
	if err != nil || len(imported) != 1 {
		t.Fatalf("imported=%v err=%v", imported, err)
	}
	saved, _ := s.LoadStore()
	if len(saved.Contexts) != 1 || saved.Contexts[0].Provider != model.ProviderCodex ||
		saved.Contexts[0].SessionID != "id-new" || saved.Contexts[0].Worktree != cwd {
		t.Fatalf("unexpected store: %+v", saved.Contexts)
	}

	// 2 回目は何も変わらない（import なし）
	store, _ = s.LoadStore()
	before, _ := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "devctx", "contexts.yaml"))
	imported, err = syncCodexSessions(s, adapter, store)
	if err != nil || len(imported) != 0 {
		t.Fatalf("second sync imported=%v err=%v", imported, err)
	}
	after, _ := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "devctx", "contexts.yaml"))
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("contexts.yaml must not be rewritten when nothing changed")
	}
}
