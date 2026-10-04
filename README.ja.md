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
# Claude Code hooks を設定（セッション自動追跡）
devctx hooks --install

# Mind Map ダッシュボードを開く
devctx roadmap serve
```

あとは普段通り Claude Code を使うだけ。セッションは hooks で自動追跡されます。

### Codex Hooks とエージェント状態

```bash
# Codex hooks を $CODEX_HOME/hooks.json（既定 ~/.codex/hooks.json）にインストール
devctx hooks --install --provider codex
```

Codex は新規・変更された hook を trust するまで実行しません。Codex で `/hooks` を実行して devctx のエントリを確認・trust してください（devctx のアップグレードなどでエントリが変わるたびに再 trust が必要です）。`hooks.json` の既存キーや他ツールの hook は保持され、再実行しても差分は出ません。

hook は Mind Map とカードに表示するエージェント状態を記録します。

| 状態 | 契機 |
|------|------|
| running | `UserPromptSubmit`。Codex は `PostToolUse` でも running に戻す（許可を承認したあと） |
| needs input | `Notification`（許可 / 確認ダイアログ / 放置、Claude Code）、`PermissionRequest` と `request_user_input` の `PreToolUse`（Codex） |
| turn done | `Stop`。Codex は `Interrupt` でも |
| ended | `SessionEnd` |

Codex の状態更新の hook は `"async": true` で動き、エージェントを待たせません。Codex の `SessionEnd` は同期で既定 1 秒しか待たないため、devctx は `timeout: 3` を付け、git を使う phase の更新を省いて登録します。

既知の制約（Codex）:
- `PermissionRequest` は Codex が確認を出す直前に発火します。別の hook や自動承認が許可した場合、実際には待ちません。
- ツールを並列に呼んでいると、別のツールの `PostToolUse` で、許可待ちのまま running に戻ることがあります。
- `Stop` は他の Stop hook によって継続されることがあるため、turn done は目安です。

### Agent View からの Claude 状態

Claude については、ダッシュボード（Mind Map / カード）、`devctx list`、`devctx tui`、`devctx status` が `claude agents --json`（Claude Code の agent view）の live 状態を hook 状態に重ねて表示し、`claude · needs input · permission prompt` のように待ちの理由も出します。

- 照合: まずセッション ID。無ければ live セッションの cwd の git toplevel が context の worktree と一致し、かつ 1 件だけのときに採用します。曖昧な場合は hook 状態のままです。
- hook は、agent view が使えない・タイムアウト・未一致のときのフォールバックと、登録・`ended` の信号源として残ります。取得開始後に届いた hook と `ended` は常に live より優先されます。
- agent view に載っていないことを ended とは**みなしません**。制限された環境ではセッションが生きていても空配列が返ることがあるためです。
- `dx` の fzf 選択とシェル補完（`list --fzf` / `--names-only`）は速度のため hook 状態のみを使います（`dxl` / `dxw` は通常の `list` / `list --watch` なので live 状態を表示します）。hook 自体は `claude agents` を呼びません。
- live の値は表示専用で、`contexts.yaml` には書き込みません。

### エージェントが待っているもの

needs input のとき、hook の payload から「何を」待っているかも記録し、汎用の理由の代わりに `claude · needs input · Bash: Run tests` のように表示します。

| 種別 | 記録する hook |
|------|---------------|
| bash / edit / mcp / plan / network / other | `PermissionRequest`（Claude Code・Codex） |
| question | `AskUserQuestion`（Claude Code）/ `request_user_input`（Codex）の `PreToolUse` |
| 解除 | `UserPromptSubmit`、`Stop`、`SessionEnd`、Codex の `Interrupt`、待ち要求と `tool_input` が一致する Codex の `PostToolUse` |

プライバシー: コマンド本文・ファイル内容などの自由記述は保存しません。残すのは短い要約だけです（Bash の `description` かプログラム名（先頭トークンの basename）、ファイルの basename、MCP の `server/tool`、URL のホスト、質問の `header`）。`tool_input` のハッシュは、後続の `PostToolUse` との照合にだけ使います。

既知の制約:
- 手動で拒否しても hook が発火しないため、要求は次のプロンプト・`Stop`・中断まで残ります。
- Claude の sandbox のネットワーク要求（`sandbox request`）などのダイアログは分類しません。Claude では live の待ち理由と整合するとき（ツール要求は `permission prompt`、質問は `input needed`）だけ待ち要求を表示します。
- 保持するのは 1 件です。並列に要求が出た場合は最後の 1 件を表示します。
- hook は async なので、状態に対してラベルがわずかに遅れることがあります。

新しい hook を入れるには `devctx hooks --install`（Claude Code）と `devctx hooks --install --provider codex` を再実行し、Codex では `/hooks` で変更されたエントリを確認・trust してください。

### Islands（島）

**island** は手で作るテーマのノード（例: `人事強化`）で、**All Projects** の Mind Map に表示されます。repo を持たなくてもかまいません。island と repo は**1 本の木**になり、repo を island の下に、island を repo の下に置けます。エージェントのセッションは自動で所属 repo の下に付き、何も接続していない repo は今まで通りトップレベルに並びます。

```bash
devctx island add 人事強化 --id hr            # 日本語名を参照するには --id が必要
devctx island attach devctx --to hr            # repo "devctx" を island "hr" の下へ
devctx island add 採用フロー --id hiring --parent island:hr
devctx island attach island:m3 --to repo:.     # island "m3" をカレントディレクトリの repo の下へ
devctx island detach devctx                    # トップレベルへ戻す
devctx island rm hr --reparent                 # 子を hr の親へ付け替えて削除
devctx island list                             # 木と、repo ごとのアクティブなセッション数
```

ref は `island:<id>` または `repo:<path>` です（`repo:.` はカレントディレクトリの repo）。名前だけを渡すと island の id、次に repo の basename を探します。island と repo の両方に当たる、または同じ basename の repo が複数ある場合は、推測せず候補を型付き ref で表示して何も変更しません。`island rm` は子がいると `--reparent` を付けない限り拒否します。循環（island → repo → island を含む）も拒否します。

木は `~/.config/devctx/islands.yaml` に型付き ref で保存されます（repo のパスは symlink 解決済みで、セッションを repo 単位にまとめるキーと同じです）。

```yaml
islands:
  - id: hr
    name: 人事強化
  - id: m3
    name: M3 UI
    parent: repo:/Users/me/code/devctx
repos:
  - root: /Users/me/code/devctx
    parent: island:hr
```

このバージョンの Mind Map は island について閲覧専用です。編集は CLI で行います。親が存在しなくなった場合はトップレベルに表示され、`island list` が警告します。

### オプション設定

```bash
# Claude Code スラッシュコマンドをインストール（/devctx-review, /devctx-done 等）
devctx commands --install

# シェルショートカットを有効化（.bashrc / .zshrc に追加）
eval "$(devctx shell-init)"
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
| `devctx register <name>` | コンテキストを登録（通常は hook で自動）。`--provider codex` で Claude 以外のエージェントを同じ worktree に別コンテキストとして登録 |
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
| `devctx hooks [--install] [--provider codex]` | Claude Code hooks を設定（`--provider codex` で Codex hooks。Codex の `/hooks` で trust が必要） |
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
| `devctx discover` | 既存の Claude Code / Codex セッションを発見（Codex は直近 14 日の対話セッション） |
| `devctx discover --provider codex` | 対象の provider を絞る（`claude` / `codex`） |
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
# 有効時は `devctx list` の実行ごとに、直近 2 日の対話的な Codex セッションも取り込む
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
