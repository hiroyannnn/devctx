# Mind Map セマンティックグラフ リデザイン仕様書

## 1. 概要

devctx Session Roadmap の Mind Map ビューを、ツリー表示からセマンティックグラフに変更する。
各セッション内のタスクフローを DAG（有向非巡回グラフ）として表現し、分岐・合流・棄却を可視化する。

### 1.1 ビジョン

「開発をやっていて自分の現在地点がわからなくならないようにするツール」

- 各セッションが **自分自身の物語** を持つ
- マップ上のノード配置に **意味がある**（分岐・合流・行き止まり）
- 固定パイプライン（Idle→Impl→Commit→Push→PR→Done）は廃止
- 「今このセッションで何をしていて、何が終わり、何が残っていて、何を捨てたか」が一目でわかる

### 1.2 現状の問題

| 問題 | 説明 |
|------|------|
| 固定パイプライン | 全セッションに同じ Phase 枠を強制。PR がない人、コードレビューがある人など、多様なフローに対応できない |
| ツリー構造 | 分岐のみで合流がない。「PRレビューが何をレビューしているか」が見えない |
| ステータスの粒度 | `planned/in_progress/done/blocked` の4値では意味的な進捗を表現できない |
| セッション内構造の欠如 | タスク間の依存・順序がなく、フラットなリスト |

## 2. 目指す姿

### 2.1 セッション内タスクフロー（DAG）

```
                          ┌─ JWTミドルウェア実装 ✓ ─┐
                          │                          │
Goal: JWT認証に移行 ──────┼─ リフレッシュトークン ▶ ──┼─→ PRレビュー ○ → コードレビュー反映 ○
                          │                          │
                          ├─ E2Eテスト ○ ────────────┘
                          │
                          └─ 旧コード削除 ✗（棄却・合流せず終了）
```

**ポイント:**
- **Goal** から複数タスクが **分岐**
- 完了/進行中のタスクは次のフェーズノード（PRレビュー等）に **合流**
  - 「何をレビューしているか」がエッジで明確
- **棄却タスク** は合流せず **行き止まり**
  - PRに含まれないことが視覚的にわかる
- Current Focus のタスクが **強調表示**

### 2.2 ノード種別

| ノード種別 | 説明 | 視覚表現 |
|-----------|------|---------|
| **Goal** | セッションの目的（起点） | オレンジ枠、🎯アイコン |
| **Task (active)** | 進行中のタスク | 青太枠、▶アイコン、グロー効果 |
| **Task (done)** | 完了タスク | 緑細枠、✓アイコン、やや透過 |
| **Task (planned)** | 予定タスク | グレー細枠、○アイコン |
| **Task (blocked)** | ブロック中 | 赤枠、✖アイコン |
| **Task (rejected)** | 棄却（新ステータス） | グレー破線枠、行き止まりマーク |
| **Milestone** | 合流ノード（PRレビュー等） | 丸角ボックス、複数の入力エッジ |
| **Endpoint** | セッション完了点 | Done バッジ |

### 2.3 エッジ種別

| エッジ種別 | 説明 | 視覚表現 |
|-----------|------|---------|
| **fork** | Goal → Task（分岐） | 実線、色はタスクの状態色 |
| **flow** | Task → Milestone（合流） | 実線、完了=緑、進行中=青 |
| **dependency** | Task → Task（依存） | 破線矢印 |
| **rejected** | Goal → rejected Task | 薄いグレー、行き止まり |

## 3. データモデル拡張

### 3.1 TaskItem の拡張

```go
type TaskItem struct {
    Title    string     `json:"title" yaml:"title"`
    Status   TaskStatus `json:"status" yaml:"status"`
    TopicID  string     `json:"topic,omitempty" yaml:"topic_id,omitempty"`
    Evidence []string   `json:"evidence,omitempty" yaml:"evidence,omitempty"`
    Source   string     `json:"source,omitempty" yaml:"source,omitempty"`
    // 新規フィールド
    ID        string   `json:"id,omitempty" yaml:"id,omitempty"`           // タスク識別子（slug形式）
    DependsOn []string `json:"depends_on,omitempty" yaml:"depends_on,omitempty"` // 依存先タスクのID
    FlowsTo   string   `json:"flows_to,omitempty" yaml:"flows_to,omitempty"`     // 合流先タスクのID
}
```

### 3.2 TaskStatus の拡張

```go
const (
    TaskPlanned    TaskStatus = "planned"
    TaskInProgress TaskStatus = "in_progress"
    TaskDone       TaskStatus = "done"
    TaskBlocked    TaskStatus = "blocked"
    TaskRejected   TaskStatus = "rejected"   // 新規: 棄却
)
```

### 3.3 SessionInsight の拡張（将来）

```go
type SessionInsight struct {
    // 既存フィールド省略...

    // 将来追加候補
    Milestones []MilestoneNode `json:"milestones,omitempty" yaml:"milestones,omitempty"`
}

type MilestoneNode struct {
    ID        string   `json:"id" yaml:"id"`
    Title     string   `json:"title" yaml:"title"`       // e.g. "PRレビュー", "コードレビュー反映"
    InputFrom []string `json:"input_from" yaml:"input_from"` // 合流元タスクのID
    Status    string   `json:"status" yaml:"status"`
}
```

## 4. AI 推論プロンプトの変更

### 4.1 現在のプロンプト（tasks 部分）

```json
"tasks": [
    {"title": "タスク名", "status": "planned|in_progress|done|blocked", "topic": "関連トピック"}
]
```

### 4.2 変更後のプロンプト

```json
"tasks": [
    {
        "id": "jwt-middleware",
        "title": "JWTミドルウェア実装",
        "status": "done",
        "depends_on": [],
        "flows_to": "pr-review"
    },
    {
        "id": "refresh-token",
        "title": "リフレッシュトークン",
        "status": "in_progress",
        "depends_on": ["jwt-middleware"],
        "flows_to": "pr-review"
    },
    {
        "id": "old-code-removal",
        "title": "旧コード削除",
        "status": "rejected"
    },
    {
        "id": "pr-review",
        "title": "PRレビュー",
        "status": "planned",
        "depends_on": ["jwt-middleware", "refresh-token", "e2e-test"]
    }
]
```

**LLM への指示追加:**
- 各タスクに一意の `id`（slug形式）を付与
- タスク間の依存関係を `depends_on` で明示
- 複数タスクの結果が合流するノード（PRレビュー等）は `depends_on` に合流元を列挙
- 不要と判断されたタスクは `status: "rejected"` とし、`flows_to` を空にする

## 5. API 変更

### 5.1 新規 API: `/api/roadmap-graph`

サーバー側でグラフ構造を構築して返す。

```json
{
    "projects": [
        {
            "name": "web-app",
            "repo_root": "/path/to/web-app",
            "sessions": [
                {
                    "name": "auth-refactor",
                    "goal": "JWT認証に移行",
                    "attention_state": "active",
                    "current_focus": "リフレッシュトークンのローテーション実装",
                    "nodes": [
                        {"id": "goal", "type": "goal", "label": "JWT認証に移行"},
                        {"id": "jwt-mw", "type": "task", "label": "JWTミドルウェア実装", "status": "done"},
                        {"id": "refresh", "type": "task", "label": "リフレッシュトークン", "status": "in_progress"},
                        {"id": "e2e", "type": "task", "label": "E2Eテスト", "status": "planned"},
                        {"id": "old-rm", "type": "task", "label": "旧コード削除", "status": "rejected"},
                        {"id": "pr-review", "type": "milestone", "label": "PRレビュー", "status": "planned"}
                    ],
                    "edges": [
                        {"from": "goal", "to": "jwt-mw", "type": "fork"},
                        {"from": "goal", "to": "refresh", "type": "fork"},
                        {"from": "goal", "to": "e2e", "type": "fork"},
                        {"from": "goal", "to": "old-rm", "type": "fork"},
                        {"from": "jwt-mw", "to": "pr-review", "type": "flow"},
                        {"from": "refresh", "to": "pr-review", "type": "flow"},
                        {"from": "e2e", "to": "pr-review", "type": "flow"}
                    ]
                }
            ]
        }
    ]
}
```

### 5.2 既存 API の維持

`/api/roadmap-map` は Project / Flat ビュー用にそのまま維持。

## 6. フロントエンド変更

### 6.1 レイアウト

- vis-network の hierarchical layout を維持（LR direction）
- ただし `sortMethod: 'directed'` により DAG のトポロジカル順でレベル配置
- `levelSeparation: 150`, `nodeSpacing: 24`, `treeSpacing: 32`
- canvas 背景: `radial-gradient(circle at 20% 20%, #161b22 0%, #0d1117 55%)`

### 6.2 All Projects モード

- **ルートノード削除**: Project を最左列に直接配置
- Project ノード: ニュートラル色（`#21262d`）+ attention バッジ
  - バッジ例: `● 1 blocked` (赤), `● 2 active` (青)
- Session ノード: 左アクセントバーで attention state を表現
  - blocked=赤バー, active=青バー, waiting=紫バー, idle=グレーバー
- Session の並び順: attention state の緊急度順
  - blocked → active → waiting → idle
- Session ノードのラベル: `名前 + Current Focus の要約（1行）`

### 6.3 Single Project モード（セッション内 DAG）

新しい `/api/roadmap-graph` を使用。

- **Goal ノード**: 起点、オレンジ枠
- **Task ノード**: Goal から分岐、ステータスに応じた色
- **Milestone ノード**: 合流点、複数の入力エッジ
- **Rejected ノード**: 行き止まり、グレー破線、`×` マーク
- **Current Focus**: 対応するタスクノードにグロー効果 + `NOW` バッジ

### 6.4 エッジスタイル

| エッジ種別 | 色 | 太さ | スタイル |
|-----------|-----|------|---------|
| fork (to active) | `#58a6ff` | 3 | 実線 |
| fork (to done) | `#3fb950` | 1.5 | 実線 |
| fork (to planned) | `#484f58` | 1 | 実線 |
| fork (to rejected) | `#484f58` | 1 | 破線 |
| flow (合流) | `#79c0ff` | 2 | 実線 |
| dependency | `#8b949e` | 1 | 破線矢印 |

## 7. 段階的実装計画

### Phase 0: データモデル拡張 (MVP)

- [ ] `TaskItem` に `ID`, `DependsOn`, `FlowsTo` フィールドを追加
- [ ] `TaskStatus` に `rejected` を追加
- [ ] `devctx insight` で手動編集可能にする
- [ ] AI推論プロンプトを更新（`id`, `depends_on`, `flows_to` を要求）
- [ ] 既存テストの更新

### Phase 1: サーバー側グラフ構築

- [ ] `/api/roadmap-graph` エンドポイント追加
- [ ] `TaskItem` からノード・エッジを生成するロジック
  - Goal → Tasks（fork エッジ）
  - Task → Milestone（flow エッジ、`flows_to` に基づく）
  - Task → Task（dependency エッジ、`depends_on` に基づく）
  - rejected タスクは合流エッジなし
- [ ] テスト

### Phase 2: フロントエンド — All Projects 改善

- [ ] "Sessions" ルートノード削除
- [ ] Project ノードをニュートラル色 + attention バッジに変更
- [ ] Session ノードに左アクセントバー追加
- [ ] Session を attention state 緊急度順にソート
- [ ] 画面密度 UP（levelSeparation, nodeSpacing, font size）
- [ ] エッジ視認性向上
- [ ] canvas radial gradient

### Phase 3: フロントエンド — Single Project DAG

- [ ] `/api/roadmap-graph` からデータ取得
- [ ] `buildMindMapData()` を `buildSemanticGraph()` に置換
- [ ] DAG ノード描画（Goal, Task, Milestone, Rejected）
- [ ] 合流エッジの描画（複数 → 1）
- [ ] Current Focus のグロー効果
- [ ] Inspector パネルの更新

### Phase 4: 拡張（将来）

- [ ] AI による `depends_on` / `flows_to` の自動推論精度向上
- [ ] セッション間の共有トピック表示（薄いリンク）
- [ ] Focus mode（選択セッション中心の 1-hop/2-hop 表示）
- [ ] 303 セッション対応の集約・ドリルダウン

## 8. スケーラビリティ

実データでは 303 セッション / 23 プロジェクトが存在する。「全部出す」ではスケールしないため、各ビューモードで表示制御を行う。

### 8.1 All Projects モード

| 制御 | ルール |
|------|--------|
| **プロジェクト表示** | attention あり（blocked/active/waiting セッションを含む）のプロジェクトを優先表示。全セッションが idle/done のプロジェクトは「+N quiet projects」ノードに折り畳む |
| **プロジェクト上限** | 画面上に表示するプロジェクトは最大 8 件。超過分は折り畳みノード |
| **セッション数** | プロジェクトごとに最大 3 セッション（現行維持）。ただし attention state の緊急度順にソートしてから切る（blocked → active → waiting → idle → done） |
| **超過表示** | `+N more...` ノード（現行維持）。クリックで Single Project に遷移 |

### 8.2 Single Project モード（DAG）

| 制御 | ルール |
|------|--------|
| **セッション表示** | active/recent のセッションのみ DAG を展開。done セッションは折り畳み（`✓ Done (N)` ノード） |
| **recent の定義** | `last_seen` が 7 日以内、または `attention_state` が idle 以外 |
| **タスクノード上限** | 1セッション内のタスクノードは最大 12 件。超過時は priority の低いタスク（planned で依存元なし）を `+N planned tasks` に折り畳む |
| **done タスクの表示** | 合流先（Milestone）への flow エッジがある done タスクは表示。それ以外の done タスクは `✓ Done (N)` にまとめる |

### 8.3 Focus Mode（Phase 4 で導入）

選択したセッションを中心に、関連ノードのみを表示するモード。

- **1-hop**: 選択セッション + そのタスク/マイルストーン
- **2-hop**: 1-hop + 同じトピックを持つ他セッション（薄いリンク）
- **操作**: セッションノードをダブルクリックで Focus Mode に遷移、Escape で全体表示に戻る

### 8.4 フィルター（Phase 4 で導入）

ドロップダウンまたはトグルで表示内容を絞り込む。

| フィルター | 選択肢 |
|-----------|--------|
| **Attention** | All / blocked / active / waiting / idle |
| **期間** | All / 24h / 7d / 30d（`last_seen` ベース） |
| **Insight 有無** | All / With insight / Without insight |

## 9. 非目標（スコープ外）

- セッション間の合流・依存表現
- タイムライン軸の導入
- リアルタイムコラボレーション
- モバイル対応
