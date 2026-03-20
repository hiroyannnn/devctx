# Mind Map セマンティックグラフ — Phase 1: サーバー側グラフ構築

仕様書: docs/spec-mindmap-redesign.md

## タスク

### 1. グラフモデル定義
- [ ] `roadmap/graph.go` に `GraphNode`, `GraphEdge`, `SessionGraph`, `ProjectGraphGroup` を定義
- [ ] ノード種別: goal, task, milestone, rejected
- [ ] エッジ種別: fork, flow, dependency, rejected

### 2. グラフ構築ロジック
- [ ] `BuildSessionGraph(session RoadmapEntry) *SessionGraph` — 1セッションのタスクフローDAGを構築
  - Goal → Task（fork エッジ）
  - Task → flows_to 先（flow エッジ）
  - depends_on によるTask間依存（dependency エッジ）
  - rejected タスクは合流エッジなし、行き止まり
  - insight なし（Goal/Tasks空）の場合は空グラフ

### 3. API エンドポイント
- [ ] `/api/roadmap-graph` ハンドラ追加（web.go）
- [ ] レスポンス形式: `[]ProjectGraphGroup`

### 4. テスト
- [ ] graph_test.go — BuildSessionGraph の各パターン
- [ ] web_test.go — `/api/roadmap-graph` のレスポンス確認
