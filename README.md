# devctx

Claude Code セッションと git worktree をカンバン形式で管理するCLIツール

## インストール

```bash
# ビルド
cd devctx
go mod tidy
go build -o devctx .

# PATHに追加（例）
sudo mv devctx /usr/local/bin/
# または
mv devctx ~/.local/bin/
```

## セットアップ

### 1. Claude Code hooks の設定

```bash
# 設定内容を確認
devctx hooks

# 自動インストール
devctx hooks --install
```

インストール後、Claude Code で `/hooks` を実行して変更を承認する必要があります。

### 2. シェル統合（オプション）

`.bashrc` または `.zshrc` に追加：

```bash
eval "$(devctx shell-init)"
```

これにより以下のショートカットが使えるようになります：
- `dx` - コンテキスト一覧表示
- `dx <name>` - コンテキストを再開（cd + claude --resume）
- `dxl` - 一覧表示
- `dxm <name> <status>` - ステータス変更

## 使い方

### 基本コマンド

```bash
# カンバン形式で一覧表示
devctx list

# 手動でコンテキストを登録（通常はhookで自動登録）
devctx register my-feature

# コンテキストを再開
devctx resume my-feature
# 出力されたコマンドを実行するか、シェル統合を使用

# ステータスを変更（チェックリスト付き）
devctx move my-feature review

# 完了としてアーカイブ
devctx archive my-feature

# コンテキストを削除
devctx remove my-feature
```

### カンバン表示例

```
🚀 In Progress
╭──────────────────────────────────────────────╮
│ [auth]                                       │
│   ⎇ feature/auth                             │
│   📁 ~/code/project/worktrees/auth           │
│   🤖 abc123...  ⏱ 2h ago                     │
╰──────────────────────────────────────────────╯

👀 Review
╭──────────────────────────────────────────────╮
│ [api-fix]                                    │
│   ⎇ fix/api-error                            │
│   📁 ~/code/project/worktrees/api-fix        │
│   🤖 def456...  ⏱ 30m ago                    │
│   ☑ /compact                                 │
│   ☐ /create-pr                               │
╰──────────────────────────────────────────────╯
```

## 設定

設定ファイル: `~/.config/devctx/config.yaml`

```yaml
statuses:
  - name: in-progress
    next: [review, blocked, done]
  - name: review
    next: [in-progress, done]
    checklist:
      - /compact
  - name: blocked
    next: [in-progress]
  - name: done
    next: []
    archive: true
    checklist:
      - /create-pr
```

### チェックリストのカスタマイズ

ステータス移行時に確認したいスラッシュコマンドを `checklist` に追加：

```yaml
statuses:
  - name: review
    next: [in-progress, done]
    checklist:
      - /compact
      - /code-simplifier
      - "PR下書き作成済み?"  # 自由形式のチェック項目も可
```

## データ

コンテキストデータ: `~/.config/devctx/contexts.yaml`

```yaml
contexts:
  - name: auth
    worktree: /home/user/code/project/worktrees/auth
    branch: feature/auth
    session_id: abc123-def456-...
    transcript_path: ~/.claude/projects/.../abc123.jsonl
    status: in-progress
    created_at: 2025-01-20T10:00:00Z
    last_seen: 2025-01-20T14:30:00Z
    checklist:
      /compact: false
      /create-pr: false
```

## Claude Code カスタムコマンド（オプション）

Claude 側からステータス変更するためのカスタムコマンドを作成できます：

`.claude/commands/devctx-done.md`:
```markdown
このタスクを完了としてマークします。

以下のコマンドを実行してください：
\`\`\`bash
devctx move $(basename $(pwd)) done
\`\`\`
```

## トラブルシューティング

### hooks が動作しない

1. `devctx hooks` で設定内容を確認
2. Claude Code で `/hooks` を実行して承認
3. `devctx` コマンドがPATHにあることを確認

### セッションが自動登録されない

- hooks の `SessionStart` が正しく設定されているか確認
- `devctx register` を手動で実行してテスト

### resume でディレクトリ移動しない

シェルの制約上、サブプロセスから親シェルのディレクトリは変更できません。
シェル統合 (`eval "$(devctx shell-init)"`) を使用するか、
表示されたコマンドを手動で実行してください。
