package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

func TestNewUsesHomeDirAndCreatesConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	s, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	wantBase := filepath.Join(home, ".config", "devctx")
	if s.basePath != wantBase {
		t.Fatalf("basePath = %q, want %q", s.basePath, wantBase)
	}

	info, err := os.Stat(wantBase)
	if err != nil {
		t.Fatalf("config directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("config path is not a directory: %q", wantBase)
	}
}

func TestLoadStoreReturnsEmptyStoreWhenMissing(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}

	store, err := s.LoadStore()
	if err != nil {
		t.Fatalf("LoadStore() error = %v", err)
	}
	if len(store.Contexts) != 0 {
		t.Fatalf("LoadStore() contexts len = %d, want 0", len(store.Contexts))
	}
}

func TestSaveStoreAndLoadStoreRoundTrip(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}

	store := &model.Store{
		Contexts: []model.Context{
			{
				Name:           "auth",
				Worktree:       "/tmp/auth",
				Branch:         "feature/auth",
				SessionID:      "session-1",
				SessionName:    "calm-coding-fox",
				TranscriptPath: "/tmp/transcript.jsonl",
				Status:         model.StatusInProgress,
				CreatedAt:      time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
				LastSeen:       time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
				Checklist:      map[string]bool{"/compact": true},
				Note:           "working on OAuth flow",
				TotalTime:      90 * time.Minute,
				IssueURL:       "https://github.com/example/repo/issues/1",
				PRURL:          "https://github.com/example/repo/pull/2",
			},
		},
	}

	if err := s.SaveStore(store); err != nil {
		t.Fatalf("SaveStore() error = %v", err)
	}

	loaded, err := s.LoadStore()
	if err != nil {
		t.Fatalf("LoadStore() error = %v", err)
	}

	if !reflect.DeepEqual(store, loaded) {
		t.Fatalf("loaded store mismatch\nwant: %#v\ngot:  %#v", store, loaded)
	}
}

func TestLoadStoreReturnsErrorOnInvalidYAML(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}
	if err := os.WriteFile(s.contextsPath(), []byte("contexts:\n  - name: [\n"), 0644); err != nil {
		t.Fatalf("write invalid yaml: %v", err)
	}

	if _, err := s.LoadStore(); err == nil {
		t.Fatalf("LoadStore() error = nil, want yaml parse error")
	}
}

func TestLoadConfigCreatesDefaultWhenMissing(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}

	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	want := defaultConfig()
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("default config mismatch\nwant: %#v\ngot:  %#v", want, cfg)
	}

	if _, err := os.Stat(s.configPath()); err != nil {
		t.Fatalf("default config file should be written, stat error: %v", err)
	}
}

func TestSaveConfigAndLoadConfigRoundTrip(t *testing.T) {
	s := &Storage{basePath: t.TempDir()}
	autoImport := false
	config := &model.Config{
		DoneRetentionDays: 7,
		AutoImport:        &autoImport,
		Statuses: []model.StatusConfig{
			{
				Name: model.StatusInProgress,
				Next: []model.Status{model.StatusReview},
			},
			{
				Name:      model.StatusReview,
				Next:      []model.Status{model.StatusDone},
				Checklist: []string{"/compact", "PR draft created?"},
			},
			{
				Name:    model.StatusDone,
				Next:    []model.Status{},
				Archive: true,
			},
		},
	}

	if err := s.SaveConfig(config); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	loaded, err := s.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if !reflect.DeepEqual(config, loaded) {
		t.Fatalf("loaded config mismatch\nwant: %#v\ngot:  %#v", config, loaded)
	}
}
