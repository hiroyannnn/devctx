# devctx

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Release](https://img.shields.io/github/v/release/hiroyannnn/devctx?include_prereleases)](https://github.com/hiroyannnn/devctx/releases)

[English](README.md)

Claude Code セッションと git worktree を**マインドマップ**で管理する CLI ツール

全セッションの全体像を一目で把握 — 何がアクティブで、何がブロックされていて、各タスクがどこに向かっているか。

![Mind Map - 全体表示](assets/screenshot-mindmap-all.png)

プロジェクトにドリルインすると、**セマンティックタスクフロー**が見える — 分岐・合流・棄却を DAG として可視化。

![Mind Map - DAG 詳細](assets/screenshot-mindmap-detail.png)

![devctx list](assets/list.gif)

## 機能

- **マインドマップ** - セマンティック DAG によるタスクフロー可視化（分岐・合流・棄却）
- **カンバンビュー** - セッションの状態を一覧で把握
- **AI インサイト** - Claude がセッションの目標・フォーカス・次のステップ・状態を推定
- **自動セッション追跡** - Claude Code hooks による自動登録
- **ステータス管理** - in-progress / review / blocked / done
- **シェル統合** - ワンコマンドでコンテキスト切り替え
- **fzf 連携** - 対話的なコンテキスト選択
- **TUI ダッシュボード** - Bubble Tea による対話的 UI
- **作業時間記録** - セッションごとの累計作業時間
- **GitHub 連携** - Issue/PR の自動検出・リンク
- **worktree 自動作成** - ブランチ作成から Claude 起動まで一発
- **セッションロードマップ** - 開発フェーズの自動検出

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
│   💬 zesty-hopping-falcon                    │
│   ⎇ feature/auth                             │
│   ⏱ 2h ago  ⌛ 4h32m                         │
│   📝 OAuth2 実装中、refresh token の処理が残  │
╰──────────────────────────────────────────────╯

👀 Review
╭──────────────────────────────────────────────╮
│ [api-fix]                                    │
│   💬 playful-coding-knuth                    │
│   ⎇ fix/api-error                            │
│   ⏱ 30m ago                                  │
│   🔀 https://github.com/user/repo/pull/123   │
│   ☑ /compact                                 │
│   ☐ /create-pr                               │
╰──────────────────────────────────────────────╯
```

💬 はClaude Codeが自動生成したセッション名（slug）を表示します。

## コマンド

### 基本操作

| コマンド | 説明 |
|---------|------|
| `devctx list` | カンバン形式で一覧表示 |
| `devctx tui` | 対話的 TUI ダッシュボード |
| `devctx show <name>` | コンテキストの詳細表示 |
| `devctx register <name>` | コンテキストを登録（通常は hook で自動） |
| `devctx resume <name>` | コンテキストを再開 |
| `devctx move <name> <status>` | ステータスを変更 |
| `devctx touch <name>` | コンテキストの最終アクティブ時刻を更新 |
| `devctx archive <name>` | 完了としてアーカイブ |
| `devctx remove <name>` | コンテキストの追跡を解除 |

### 新規作成・設定

| コマンド | 説明 |
|---------|------|
| `devctx new <branch>` | worktree 作成 + cd + claude を一発で |
| `devctx note <name> [msg]` | メモを追加/表示 |
| `devctx link <name> <url>` | GitHub Issue/PR をリンク |
| `devctx hooks [--install]` | Claude Code hooks を設定 |
| `devctx commands [--install]` | Claude スラッシュコマンドを設定 |

### GitHub 連携

| コマンド | 説明 |
|---------|------|
| `devctx sync [name]` | PR/Issue を自動検出してリンク |
| `devctx sync --all` | 全コンテキストのセッション名を更新 |
| `devctx pr <name>` | PR を作成 |

### セッションロードマップ

| コマンド | 説明 |
|---------|------|
| `devctx roadmap scan` | git ベースのフェーズを一覧表示 |
| `devctx roadmap status` | 開発フェーズの進捗をビジュアル表示 |
| `devctx roadmap serve` | Web ダッシュボードを起動（localhost:3333） |
| `devctx roadmap refresh` | PR 検出含むフルスキャン（gh CLI 使用） |
| `devctx roadmap analyze [name]` | Claude CLI で AI インサイトを生成 |
| `devctx roadmap analyze --all` | 全アクティブセッションのインサイトを生成 |
| `devctx roadmap init --prompt "..."` | セッションの初期プロンプトを設定 |
| `devctx insight [name]` | セッションインサイトの表示/手動設定 |

### 監視・検索

| コマンド | 説明 |
|---------|------|
| `devctx discover` | 既存の Claude Code セッションを発見 |
| `devctx discover --import` | 発見したセッションをインポート |
| `devctx status` | 全コンテキストのライブ状態を表示 |
| `devctx status --watch` | 監視モード（継続的に更新） |
| `devctx search <query>` | セッション履歴を検索 |

### メンテナンス

| コマンド | 説明 |
|---------|------|
| `devctx stats` | 統計情報を表示 |
| `devctx clean` | 古いコンテキストを削除（デフォルト: 30日以上前のdone） |
| `devctx clean --days=7` | 7日以上前のコンテキストを削除 |
| `devctx clean --done=false` | ステータス問わず古いコンテキストを削除 |
| `devctx clean --dry-run` | 削除対象をプレビュー |

## シェル統合

`.bashrc` または `.zshrc` に追加:

```bash
eval "$(devctx shell-init)"
```

ショートカット:
- `dx` - fzf でコンテキストを選択して再開（fzf がない場合は一覧表示）
- `dx <name>` - コンテキストを再開（cd + claude --resume）
- `dx -` - 最後に触ったコンテキストを再開
- `dxl` - 一覧表示
- `dxw` - ウォッチモード（インタラクティブカンバン）
- `dxm <name> <status>` - ステータス変更
- `dxn <branch>` - 新規 worktree 作成
- `dxs` - GitHub 情報を同期
- `dxt` - TUI ダッシュボード
- `dxp` - ライブステータス表示
- `dxf <query>` - 履歴検索
- `dxd` - 既存セッションを発見

## ウォッチモード

キーボードで操作できるインタラクティブなカンバンビュー:

```bash
devctx list -w   # または dxw
```

![devctx watch mode](assets/watch.gif)

**ナビゲーション:**
- `↑`/`↓` または `j`/`k` - カーソル移動
- `g`/`G` - 先頭/末尾へジャンプ

**アクション:**
- `Enter` または `c` - 起動コマンドをクリップボードにコピー
- `o` - 新しいターミナルで開く

**ステータス変更:**
- `r` - Review へ移動
- `p` - In Progress へ移動
- `b` - Blocked へ移動
- `D` - Done へ移動
- `x` - コンテキストを削除
- `q` - 終了

## 設定

設定ファイル: `~/.config/devctx/config.yaml`

```yaml
# 完了したアイテムを N 日間表示（デフォルト: 1）
done_retention_days: 1

# セッションの自動インポートを無効化（デフォルト: true）
auto_import: false

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

## Claude Code カスタムコマンド

Claude Code 用のスラッシュコマンドをインストール:

```bash
devctx commands --install
```

以下のコマンドが使えるようになります:
- `/devctx-review` - review ステータスに移動
- `/devctx-done` - 完了としてマーク
- `/devctx-blocked` - blocked としてマーク
- `/devctx-note` - メモを追加
- `/devctx-link` - Issue/PR をリンク
- `/devctx-status` - コンテキストの状態を表示
- `/devctx-insight` - セッションインサイトを保存（目標・フォーカス・次のステップ・状態）

ルールファイル（`~/.claude/rules/devctx-insight-auto.md`）も同時にインストールされ、実装計画の作成後に Claude が自動で `/devctx-insight` を実行します。

## セッションロードマップ

セッションの開発ライフサイクルを自動追跡します。

### フェーズ検出

`register` / `touch` 時に git の状態からフェーズを自動検出:

| フェーズ | 条件 |
|---------|------|
| Idle | 変更なし、コミットなし |
| Implementation | 未コミットの変更あり |
| Committed | ベースブランチより先のコミットあり |
| Pushed | リモートブランチが最新 |
| PR Open | オープンな PR が検出された |
| Done | マージ済みの PR |

### マイルストーン追跡

開発マイルストーンがイベントとして自動記録されます:

| マイルストーン | ソース |
|-------------|--------|
| 初回コミット / コミット | `register` / `touch` 時の git log |
| 初回プッシュ | git remote チェック |
| PR 作成 / マージ | `roadmap refresh` の `gh` CLI |
| セッション開始 / 終了 | Claude Code hooks |
| ステータス変更 | `devctx move` コマンド |

イベントは `~/.config/devctx/events.yaml` に append-only ログとして保存されます。

### AI インサイト

Claude がセッションの文脈を分析してインサイトを保存します:

```bash
# カスタムコマンドと自動実行ルールをインストール
devctx commands --install

# Claude Code でのセッション中:
# 実装計画の作成後、Claude が自動的に /devctx-insight を実行

# または Claude CLI で手動分析:
devctx roadmap analyze
```

インサイトの内容:
- **Goal** - このセッションが達成しようとしていること
- **Current Focus** - 今取り組んでいるサブタスク
- **Next Step** - 次にやるべきこと
- **Attention State** - active（作業中）/ waiting（入力待ち）/ idle（一段落）/ blocked（詰まっている）
- **Topics** - git と LLM から抽出されたセマンティックトピック（例: 「認証」「エラーハンドリング」）
- **Tasks** - ステータス付きの具体的な作業項目（planned / in_progress / done / blocked）

トピック・タスク抽出はハイブリッド方式: git からの機械抽出（ブランチ名、コミットメッセージ、変更ディレクトリ）と LLM によるクラスタリング・正規化を組み合わせています。

### Web ダッシュボード

```bash
devctx roadmap serve
```

`http://localhost:3333` でダッシュボードが起動します:
- **プロジェクトグルーピング** - リポジトリ別にセッションを表示
- **フェーズパイプライン** - 開発フェーズの進捗をビジュアル表示
- **マイルストーンチップ** - Sessions/Commits/Pushed/PR の状態を一目で確認
- **トピック＆タスク** - セッション毎のセマンティックトピックとタスクリスト
- **イベントタイムライン** - カードクリックでイベント履歴を展開
- **Project / Flat / Mind Map 表示** - グループ化・フラット・マインドマップ表示を切り替え
- **マインドマップビュー** - セマンティック DAG でタスクの分岐・合流・棄却を可視化。ノードをドラッグで配置調整可能

![Project View](assets/screenshot-project.png)

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
