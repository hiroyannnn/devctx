# devctx

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/v/release/hiroyannnn/devctx?include_prereleases)](https://github.com/hiroyannnn/devctx/releases)

Claude Code セッションと git worktree をカンバン形式で管理する CLI ツール

## 機能

- **カンバンビュー** - セッションの状態を一覧で把握
- **自動セッション追跡** - Claude Code hooks による自動登録
- **ステータス管理** - in-progress / review / blocked / done
- **チェックリスト** - ステータス移行時の確認項目
- **シェル統合** - ワンコマンドでコンテキスト切り替え

## インストール

```bash
go install github.com/hiroyannnn/devctx@latest
```

または、ソースからビルド:

```bash
git clone https://github.com/hiroyannnn/devctx.git
cd devctx
go build -o devctx .
mv devctx ~/.local/bin/  # または /usr/local/bin/
```

## クイックスタート

```bash
# Claude Code hooks を設定
devctx hooks --install

# シェル統合を有効化（.bashrc / .zshrc に追加）
eval "$(devctx shell-init)"

# カンバン表示
devctx list
```

## カンバン表示例

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

## コマンド

| コマンド | 説明 |
|---------|------|
| `devctx list` | カンバン形式で一覧表示 |
| `devctx register <name>` | コンテキストを登録（通常は hook で自動） |
| `devctx resume <name>` | コンテキストを再開 |
| `devctx move <name> <status>` | ステータスを変更 |
| `devctx archive <name>` | 完了としてアーカイブ |
| `devctx touch <name>` | 最終アクセス時刻を更新 |
| `devctx hooks [--install]` | Claude Code hooks を設定 |
| `devctx shell-init` | シェル統合スクリプトを出力 |

## シェル統合

`.bashrc` または `.zshrc` に追加:

```bash
eval "$(devctx shell-init)"
```

ショートカット:
- `dx` - コンテキスト一覧表示
- `dx <name>` - コンテキストを再開（cd + claude --resume）
- `dxl` - 一覧表示
- `dxm <name> <status>` - ステータス変更

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

ステータス移行時に確認したい項目を `checklist` に追加:

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

Claude 側からステータス変更するためのカスタムコマンドを作成できます:

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
3. `devctx` コマンドが PATH にあることを確認

### セッションが自動登録されない

- hooks の `SessionStart` が正しく設定されているか確認
- `devctx register` を手動で実行してテスト

### resume でディレクトリ移動しない

シェルの制約上、サブプロセスから親シェルのディレクトリは変更できません。
シェル統合 (`eval "$(devctx shell-init)"`) を使用するか、
表示されたコマンドを手動で実行してください。

## License

MIT
