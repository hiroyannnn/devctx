# devctx ユースケースと価値

## 基本的なユースケース

### 1. 複数機能の並行開発

**シナリオ**: 認証機能（auth）、API修正（api-fix）、UI改善（ui-update）を並行で進めている。

```bash
# 各 worktree で Claude Code セッションを開始すると自動登録
cd ~/project/worktrees/auth
claude  # → 自動で devctx に登録される

# 別の機能に切り替え
dx api-fix  # cd + claude --resume を一発で実行

# 現状を確認
dx
```

```
🚀 In Progress
╭──────────────────────────────────────────────╮
│ [auth]                                       │
│   ⎇ feature/auth                             │
│   📁 ~/project/worktrees/auth                │
│   🤖 abc123...  ⏱ 2h ago                     │
╰──────────────────────────────────────────────╯
╭──────────────────────────────────────────────╮
│ [api-fix]                                    │
│   ⎇ fix/api-error                            │
│   📁 ~/project/worktrees/api-fix             │
│   🤖 def456...  ⏱ just now                   │
╰──────────────────────────────────────────────╯
```

### 2. セッションの中断と再開

**シナリオ**: 金曜日に作業途中で退勤、月曜日に続きから再開したい。

```bash
# 月曜日の朝
dx                    # 作業中だったコンテキストを確認
dx auth               # 前回のセッションをそのまま再開
                      # Claude は前回の会話を覚えている
```

### 3. レビュー前のチェックリスト

**シナリオ**: 実装完了、レビュー依頼前に忘れがちな作業を確認したい。

```bash
devctx move auth review

# 出力:
# Moving [auth] to review
# Please confirm checklist items:
#
#   /compact executed? (y/n/skip): y
#   /code-simplifier executed? (y/n/skip): y
#   PR下書き作成済み? (y/n/skip): n
#
# ⚠ Warning: 1 checklist item(s) not completed.
# Continue anyway? (y/n/skip): n
# Aborted.
```

### 4. ブロック状態の可視化

**シナリオ**: 外部チームの回答待ちで auth の作業がブロックされている。

```bash
devctx move auth blocked
dx

# 🚧 Blocked に auth が表示される
# → 何がブロックされているか一目で把握
```

### 5. 完了タスクの整理

**シナリオ**: api-fix が完了、PR もマージされた。

```bash
devctx archive api-fix   # done に移動してアーカイブ
# または
devctx move api-fix done
```

---

## devctx がない場合との比較

### コンテキスト切り替え

| 操作 | devctx なし | devctx あり |
|------|-------------|-------------|
| 別機能に切替 | `cd ~/project/worktrees/auth`<br>`claude --resume <session-id>` | `dx auth` |
| セッションID確認 | `ls ~/.claude/projects/.../*.jsonl`<br>ファイル名からID特定 | 不要（自動管理） |
| 作業状態の確認 | 頭の中 or メモ帳 | `dx` でカンバン表示 |

### セッション再開

**devctx なし**:
```bash
# 1. どのディレクトリだっけ？
ls ~/project/worktrees/

# 2. セッションIDは？
ls ~/.claude/projects/*/
# → 複数の .jsonl ファイルから正しいものを探す
# → ファイルの更新日時やサイズから推測

# 3. やっと再開
cd ~/project/worktrees/auth
claude --resume abc123-def456-...
```

**devctx あり**:
```bash
dx auth
```

### 作業状態の把握

**devctx なし**:
- 「あれ、auth の作業どこまでやったっけ？」
- 「api-fix は誰かにレビュー依頼した？」
- 「ブロックされてるタスクなんだっけ？」
- → 付箋、Notion、頭の中で管理

**devctx あり**:
- ターミナルで `dx` を打てば全体像が見える
- ステータスで作業段階が明確
- チェックリストで抜け漏れ防止

### 定量的な改善

| 指標 | devctx なし | devctx あり | 改善 |
|------|-------------|-------------|------|
| コンテキスト切替 | 30秒〜2分 | 3秒 | 10〜40倍高速化 |
| セッションID検索 | 必要 | 不要 | 認知負荷削減 |
| 状態管理 | 外部ツール | CLI内完結 | ツール統合 |
| 忘れがちな作業 | 忘れる | チェックリスト | ミス防止 |

---

## さらに体験を良くできる部分

### 高優先度

#### 1. fzf 連携
現状: `dx auth` で名前を手入力
改善案: `dx` だけで fzf によるインタラクティブ選択

```bash
dx  # → fzf で選択 → 選んだコンテキストを resume
```

#### 2. 最後に触ったコンテキストの自動再開
```bash
dx -   # 最後に touch されたコンテキストを再開
dx --last
```

#### 3. プロジェクトごとのコンテキスト分離
現状: 全プロジェクトのコンテキストが混在
改善案: `devctx list --project myapp` でフィルタ、または自動検出

#### 4. worktree 自動作成
```bash
devctx new feature/payment
# → git worktree add + cd + claude を一発で
```

### 中優先度

#### 5. ステータス変更の Claude 連携
Claude Code 内から直接ステータス変更:
```
/devctx-review   # 現在のコンテキストを review に移動
/devctx-done     # 完了としてマーク
```

#### 6. 経過時間・作業時間の記録
```bash
dx
# 🚀 In Progress
# [auth] ⏱ 作業時間: 4h32m（今日: 1h15m）
```

#### 7. 複数ターミナル間の同期
tmux/Ghostty の複数ペインで同じコンテキストを開いている場合の検出・警告

#### 8. コンテキストのメモ/説明
```bash
devctx note auth "OAuth2 実装中、refresh token の処理が残っている"
dx
# [auth]
#   📝 OAuth2 実装中、refresh token の処理が残っている
```

### 低優先度（将来）

#### 9. Web UI / TUI ダッシュボード
Bubble Tea を使った対話的 TUI、またはローカル Web UI

#### 10. GitHub Issue/PR 連携
```bash
dx
# [auth]
#   🔗 #123 Add OAuth2 authentication
#   🔗 PR #125 (draft)
```

#### 11. チーム共有（オプショナル）
複数人で同じプロジェクトを開発している場合の状態共有

---

## 実装の推奨順序

1. **fzf 連携** - 即効性が高い
2. **dx -（最後のコンテキスト）** - よく使うパターン
3. **worktree 自動作成** - 新機能開始のフロー改善
4. **コンテキストメモ** - 中断時の記憶補助
5. **Claude スラッシュコマンド** - Claude 内での操作性向上
