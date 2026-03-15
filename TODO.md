# 自動更新チェック機能

## タスク

### 1. バージョン基盤
- [ ] `cmd/version.go` — `var Version = "dev"` + `devctx version` コマンド
- [ ] `.goreleaser.yaml` — ldflags に `-X cmd.Version={{.Tag}}` 追加

### 2. 更新チェックコア
- [ ] `cmd/update_check.go` — GitHub API で最新リリース取得 + semver 比較
- [ ] キャッシュ（`~/.config/devctx/update-check.yaml`）: `last_checked_at` + `latest_version`、成功 TTL 24h / 失敗 1h
- [ ] `golang.org/x/mod/semver` でバージョン比較

### 3. CLI 統合
- [ ] `PersistentPreRun` でキャッシュ読み → stale なら非同期チェック開始
- [ ] `PersistentPostRun` でキャッシュ済み結果を stderr に表示（TTY のみ）
- [ ] skip 対象: hooks, completion, shell-init, version, stdin pipe, 非TTY, CI=true
- [ ] `DEVCTX_NO_UPDATE_CHECK=1` でオプトアウト

### 4. version --check
- [ ] `devctx version --check` で手動更新確認（同期的にAPIコール）

### 5. テスト
- [ ] update_check_test.go — キャッシュ TTL、semver 比較、skip 判定
- [ ] version コマンドのテスト
