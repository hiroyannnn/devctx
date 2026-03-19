package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestIsNewer(t *testing.T) {
	uc := &UpdateChecker{}

	tests := []struct {
		name    string
		latest  string
		current string
		want    bool
	}{
		{"newer version", "v0.3.0", "v0.2.0", true},
		{"same version", "v0.2.0", "v0.2.0", false},
		{"older version", "v0.1.0", "v0.2.0", false},
		{"current is dev", "v0.3.0", "dev", false},
		{"latest is invalid", "invalid", "v0.2.0", false},
		{"current is invalid", "v0.3.0", "invalid", false},
		{"without v prefix latest", "0.3.0", "v0.2.0", true},
		{"without v prefix current", "v0.3.0", "0.2.0", true},
		{"both without v prefix", "0.3.0", "0.2.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uc.IsNewer(tt.latest, tt.current)
			if got != tt.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func TestIsStale(t *testing.T) {
	uc := &UpdateChecker{
		SuccessTTL: 24 * time.Hour,
		FailureTTL: 1 * time.Hour,
	}

	now := time.Now()

	tests := []struct {
		name  string
		cache *UpdateCache
		want  bool
	}{
		{
			"success cache within TTL",
			&UpdateCache{LastCheckedAt: now.Add(-1 * time.Hour), LatestVersion: "v0.3.0", CheckedOK: true},
			false,
		},
		{
			"success cache expired",
			&UpdateCache{LastCheckedAt: now.Add(-25 * time.Hour), LatestVersion: "v0.3.0", CheckedOK: true},
			true,
		},
		{
			"failure cache within TTL",
			&UpdateCache{LastCheckedAt: now.Add(-30 * time.Minute), CheckedOK: false},
			false,
		},
		{
			"failure cache expired",
			&UpdateCache{LastCheckedAt: now.Add(-2 * time.Hour), CheckedOK: false},
			true,
		},
		{
			"empty cache (zero time)",
			&UpdateCache{},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uc.IsStale(tt.cache)
			if got != tt.want {
				t.Errorf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadSaveCache(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		dir := t.TempDir()
		cachePath := filepath.Join(dir, "update_cache.yaml")

		uc := &UpdateChecker{CachePath: cachePath}

		original := &UpdateCache{
			LastCheckedAt: time.Now().Truncate(time.Second),
			LatestVersion: "v0.3.0",
			CheckedOK:     true,
		}

		if err := uc.SaveCache(original); err != nil {
			t.Fatalf("SaveCache() error = %v", err)
		}

		loaded, err := uc.LoadCache()
		if err != nil {
			t.Fatalf("LoadCache() error = %v", err)
		}

		if !loaded.LastCheckedAt.Equal(original.LastCheckedAt) {
			t.Errorf("LastCheckedAt = %v, want %v", loaded.LastCheckedAt, original.LastCheckedAt)
		}
		if loaded.LatestVersion != original.LatestVersion {
			t.Errorf("LatestVersion = %q, want %q", loaded.LatestVersion, original.LatestVersion)
		}
		if loaded.CheckedOK != original.CheckedOK {
			t.Errorf("CheckedOK = %v, want %v", loaded.CheckedOK, original.CheckedOK)
		}
	})

	t.Run("file not found returns empty cache", func(t *testing.T) {
		dir := t.TempDir()
		cachePath := filepath.Join(dir, "nonexistent.yaml")

		uc := &UpdateChecker{CachePath: cachePath}

		cache, err := uc.LoadCache()
		if err != nil {
			t.Fatalf("LoadCache() error = %v", err)
		}

		if cache.LatestVersion != "" {
			t.Errorf("LatestVersion = %q, want empty", cache.LatestVersion)
		}
		if cache.CheckedOK {
			t.Errorf("CheckedOK = true, want false")
		}
	})
}

func TestFetchLatestVersion(t *testing.T) {
	t.Run("successful fetch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]string{"tag_name": "v0.4.0"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		uc := &UpdateChecker{APIURL: server.URL}

		version, err := uc.FetchLatestVersion()
		if err != nil {
			t.Fatalf("FetchLatestVersion() error = %v", err)
		}
		if version != "v0.4.0" {
			t.Errorf("FetchLatestVersion() = %q, want %q", version, "v0.4.0")
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		uc := &UpdateChecker{APIURL: server.URL}

		_, err := uc.FetchLatestVersion()
		if err == nil {
			t.Fatal("FetchLatestVersion() expected error, got nil")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("not json"))
		}))
		defer server.Close()

		uc := &UpdateChecker{APIURL: server.URL}

		_, err := uc.FetchLatestVersion()
		if err == nil {
			t.Fatal("FetchLatestVersion() expected error, got nil")
		}
	})
}

func TestCheckAndCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{"tag_name": "v0.5.0"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update_cache.yaml")

	uc := &UpdateChecker{
		CurrentVersion: "v0.2.0",
		CachePath:      cachePath,
		APIURL:         server.URL,
		SuccessTTL:     24 * time.Hour,
		FailureTTL:     1 * time.Hour,
	}

	cache, err := uc.CheckAndCache()
	if err != nil {
		t.Fatalf("CheckAndCache() error = %v", err)
	}

	if cache.LatestVersion != "v0.5.0" {
		t.Errorf("LatestVersion = %q, want %q", cache.LatestVersion, "v0.5.0")
	}
	if !cache.CheckedOK {
		t.Error("CheckedOK = false, want true")
	}
	if cache.LastCheckedAt.IsZero() {
		t.Error("LastCheckedAt should not be zero")
	}

	// キャッシュファイルが存在することを確認
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Error("cache file should exist after CheckAndCache")
	}
}

func TestNotifyMessage(t *testing.T) {
	uc := &UpdateChecker{CurrentVersion: "v0.2.0"}

	msg := uc.NotifyMessage("v0.3.0")

	expected := "Update available: v0.2.0 → v0.3.0\nSee https://github.com/hiroyannnn/devctx/releases/latest"
	if msg != expected {
		t.Errorf("NotifyMessage() = %q, want %q", msg, expected)
	}
}

func TestNotifiedVersionPreventsRepeat(t *testing.T) {
	uc := &UpdateChecker{CurrentVersion: "v0.2.0"}

	// NotifiedVersion が latest と一致していれば通知不要
	cache := &UpdateCache{
		LatestVersion:   "v0.3.0",
		NotifiedVersion: "v0.3.0",
		CheckedOK:       true,
	}
	if uc.ShouldNotify(cache) {
		t.Error("ShouldNotify() = true, want false (already notified for this version)")
	}

	// NotifiedVersion が古い（別バージョン）なら通知すべき
	cache.NotifiedVersion = "v0.2.5"
	if !uc.ShouldNotify(cache) {
		t.Error("ShouldNotify() = false, want true (notified for different version)")
	}

	// NotifiedVersion が空なら通知すべき
	cache.NotifiedVersion = ""
	if !uc.ShouldNotify(cache) {
		t.Error("ShouldNotify() = false, want true (never notified)")
	}

	// LatestVersion が current と同じなら通知不要
	cache.LatestVersion = "v0.2.0"
	cache.NotifiedVersion = ""
	if uc.ShouldNotify(cache) {
		t.Error("ShouldNotify() = true, want false (already on latest)")
	}
}

func TestCheckAndCachePreservesVersionOnFailure(t *testing.T) {
	// API が失敗しても既知の LatestVersion を保持する
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update_cache.yaml")

	uc := &UpdateChecker{
		CurrentVersion: "v0.2.0",
		CachePath:      cachePath,
		APIURL:         server.URL,
		SuccessTTL:     24 * time.Hour,
		FailureTTL:     1 * time.Hour,
	}

	// 事前に既知のバージョンをキャッシュに書き込み
	uc.SaveCache(&UpdateCache{
		LastCheckedAt: time.Now().Add(-25 * time.Hour),
		LatestVersion: "v0.3.0",
		CheckedOK:     true,
	})

	cache, err := uc.CheckAndCache()
	if err == nil {
		t.Fatal("CheckAndCache() expected error, got nil")
	}

	// 失敗しても LatestVersion が保持されていることを確認
	if cache.LatestVersion != "v0.3.0" {
		t.Errorf("LatestVersion = %q, want %q (should be preserved from previous cache)", cache.LatestVersion, "v0.3.0")
	}
	if cache.CheckedOK {
		t.Error("CheckedOK should be false after failure")
	}
}

func TestStartAndShowUpdateNotification(t *testing.T) {
	// startUpdateCheckWithChecker + showUpdateNotification の統合テスト
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{"tag_name": "v0.5.0"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "update_cache.yaml")

	// 事前にキャッシュを作成（stale、新バージョンあり、未通知）
	checker := &UpdateChecker{
		CurrentVersion: "v0.2.0",
		CachePath:      cachePath,
		APIURL:         server.URL,
		SuccessTTL:     24 * time.Hour,
		FailureTTL:     1 * time.Hour,
	}
	checker.SaveCache(&UpdateCache{
		LastCheckedAt: time.Now().Add(-25 * time.Hour),
		LatestVersion: "v0.3.0",
		CheckedOK:     true,
	})

	// グローバル変数をリセット
	updateChecker = nil
	pendingNotification = ""

	startUpdateCheckWithChecker(checker)

	// pendingNotification はキャッシュ済みの v0.3.0 に基づいて設定されるべき
	if pendingNotification == "" {
		t.Error("pendingNotification should be set from cached version")
	}

	// バックグラウンドの goroutine が完了するのを少し待つ
	time.Sleep(100 * time.Millisecond)

	// キャッシュが更新されていることを確認
	updated, _ := checker.LoadCache()
	if updated.LatestVersion != "v0.5.0" {
		t.Errorf("background update should have cached v0.5.0, got %q", updated.LatestVersion)
	}
}

func TestLoadSaveCacheWithNotifiedVersion(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "cache.yaml")
	uc := &UpdateChecker{CachePath: cachePath}

	original := &UpdateCache{
		LastCheckedAt:   time.Now().Truncate(time.Second),
		LatestVersion:   "v0.3.0",
		CheckedOK:       true,
		NotifiedVersion: "v0.3.0",
	}
	if err := uc.SaveCache(original); err != nil {
		t.Fatalf("SaveCache() error = %v", err)
	}

	loaded, err := uc.LoadCache()
	if err != nil {
		t.Fatalf("LoadCache() error = %v", err)
	}
	if loaded.NotifiedVersion != "v0.3.0" {
		t.Errorf("NotifiedVersion = %q, want %q", loaded.NotifiedVersion, "v0.3.0")
	}
}

func TestShouldSkipUpdateCheck(t *testing.T) {
	tests := []struct {
		name    string
		cmdName string
		envKey  string
		envVal  string
		want    bool
	}{
		{"hooks command", "hooks", "", "", true},
		{"completion command", "completion", "", "", true},
		{"shell-init command", "shell-init", "", "", true},
		{"version command", "version", "", "", true},
		{"list command", "list", "", "", false},
		{"DEVCTX_NO_UPDATE_CHECK", "list", "DEVCTX_NO_UPDATE_CHECK", "1", true},
		{"CI=true", "list", "CI", "true", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envVal)
			}
			cmd := &cobra.Command{Use: tt.cmdName}
			if got := shouldSkipUpdateCheckForTest(cmd); got != tt.want {
				t.Errorf("shouldSkipUpdateCheckForTest() = %v, want %v", got, tt.want)
			}
		})
	}
}
