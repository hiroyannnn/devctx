package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"
)

var (
	updateResult  chan *UpdateCache // 非同期チェック結果
	updateChecker *UpdateChecker
)

// UpdateChecker はアップデートの確認とキャッシュ管理を行う。
type UpdateChecker struct {
	CurrentVersion string        // 現在のバージョン（例: "v0.2.0"）
	CachePath      string        // キャッシュファイルパス
	APIURL         string        // GitHub API URL（テスト用に差し替え可能）
	SuccessTTL     time.Duration // 成功時キャッシュ TTL（デフォルト 24h）
	FailureTTL     time.Duration // 失敗時キャッシュ TTL（デフォルト 1h）
}

// UpdateCache はアップデート確認結果のキャッシュ。
type UpdateCache struct {
	LastCheckedAt time.Time `yaml:"last_checked_at"`
	LatestVersion string    `yaml:"latest_version"`
	CheckedOK     bool      `yaml:"checked_ok"` // API成功ならtrue
}

// LoadCache はキャッシュファイルを読み込む。ファイルが存在しない場合は空のキャッシュを返す。
func (uc *UpdateChecker) LoadCache() (*UpdateCache, error) {
	data, err := os.ReadFile(uc.CachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &UpdateCache{}, nil
		}
		return nil, fmt.Errorf("failed to read cache: %w", err)
	}

	var cache UpdateCache
	if err := yaml.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to parse cache: %w", err)
	}
	return &cache, nil
}

// SaveCache はキャッシュをファイルに保存する。
func (uc *UpdateChecker) SaveCache(cache *UpdateCache) error {
	data, err := yaml.Marshal(cache)
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(uc.CachePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache: %w", err)
	}
	return nil
}

// IsStale はキャッシュが期限切れかどうかを判定する。
// 成功時は SuccessTTL、失敗時は FailureTTL で判定する。
func (uc *UpdateChecker) IsStale(cache *UpdateCache) bool {
	if cache.LastCheckedAt.IsZero() {
		return true
	}

	ttl := uc.FailureTTL
	if cache.CheckedOK {
		ttl = uc.SuccessTTL
	}

	return time.Since(cache.LastCheckedAt) > ttl
}

// FetchLatestVersion は APIURL から最新バージョンを取得する。
func (uc *UpdateChecker) FetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(uc.APIURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return release.TagName, nil
}

// ensureVPrefix は文字列に "v" プレフィックスを補完する。
func ensureVPrefix(s string) string {
	if !strings.HasPrefix(s, "v") {
		return "v" + s
	}
	return s
}

// IsNewer は latest が current より新しいかどうかを判定する。
// current が "dev" の場合は false を返す（開発ビルドでは通知しない）。
func (uc *UpdateChecker) IsNewer(latest, current string) bool {
	if current == "dev" {
		return false
	}

	latest = ensureVPrefix(latest)
	current = ensureVPrefix(current)

	if !semver.IsValid(latest) || !semver.IsValid(current) {
		return false
	}

	return semver.Compare(latest, current) > 0
}

// CheckAndCache は最新バージョンを取得してキャッシュに保存する。
func (uc *UpdateChecker) CheckAndCache() (*UpdateCache, error) {
	cache := &UpdateCache{
		LastCheckedAt: time.Now(),
	}

	version, err := uc.FetchLatestVersion()
	if err != nil {
		cache.CheckedOK = false
		if saveErr := uc.SaveCache(cache); saveErr != nil {
			return nil, fmt.Errorf("fetch failed: %w, and save failed: %v", err, saveErr)
		}
		return cache, err
	}

	cache.LatestVersion = version
	cache.CheckedOK = true

	if err := uc.SaveCache(cache); err != nil {
		return nil, fmt.Errorf("failed to save cache: %w", err)
	}

	return cache, nil
}

// skipCmds はアップデートチェックをスキップするコマンド名。
var skipCmds = map[string]bool{
	"hooks":      true,
	"completion": true,
	"shell-init": true,
	"version":    true,
}

// shouldSkipUpdateCheckForTest は TTY チェックを除いたスキップ判定（テスト用）。
func shouldSkipUpdateCheckForTest(cmd *cobra.Command) bool {
	if skipCmds[cmd.Name()] {
		return true
	}
	if os.Getenv("DEVCTX_NO_UPDATE_CHECK") == "1" {
		return true
	}
	if os.Getenv("CI") == "true" {
		return true
	}
	return false
}

// shouldSkipUpdateCheck はコマンド名・環境変数・TTY でスキップ判定する。
func shouldSkipUpdateCheck(cmd *cobra.Command) bool {
	if shouldSkipUpdateCheckForTest(cmd) {
		return true
	}
	if !isStderrTTY() {
		return true
	}
	return false
}

// isStderrTTY は stderr が TTY かどうかを返す（テスト用に分離）。
func isStderrTTY() bool {
	stat, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// startUpdateCheck は非同期でアップデートチェックを開始する。
func startUpdateCheck() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	cachePath := filepath.Join(home, ".config", "devctx", "update-check.yaml")

	updateChecker = &UpdateChecker{
		CurrentVersion: Version,
		CachePath:      cachePath,
		APIURL:         "https://api.github.com/repos/hiroyannnn/devctx/releases/latest",
		SuccessTTL:     24 * time.Hour,
		FailureTTL:     1 * time.Hour,
	}

	cache, err := updateChecker.LoadCache()
	if err != nil {
		return
	}

	if !updateChecker.IsStale(cache) {
		// キャッシュが新鮮なら、そのまま使う
		updateResult = make(chan *UpdateCache, 1)
		updateResult <- cache
		return
	}

	// stale なら非同期チェック
	updateResult = make(chan *UpdateCache, 1)
	go func() {
		result, _ := updateChecker.CheckAndCache()
		if result != nil {
			updateResult <- result
		} else {
			updateResult <- cache // 失敗時はキャッシュを返す
		}
	}()
}

// showUpdateNotification はアップデート通知を表示する。
func showUpdateNotification() {
	if updateResult == nil || updateChecker == nil {
		return
	}

	select {
	case cache := <-updateResult:
		if cache != nil && cache.LatestVersion != "" && updateChecker.IsNewer(cache.LatestVersion, updateChecker.CurrentVersion) {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, updateChecker.NotifyMessage(cache.LatestVersion))
		}
	default:
		// 非同期チェックが間に合わなかった場合は何もしない
	}
}

// NotifyMessage はアップデート通知メッセージを返す。
func (uc *UpdateChecker) NotifyMessage(latest string) string {
	return fmt.Sprintf(
		"Update available: %s → %s\nSee https://github.com/hiroyannnn/devctx/releases/latest",
		uc.CurrentVersion, latest,
	)
}
