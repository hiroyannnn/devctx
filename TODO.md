# Mind Map セマンティックグラフ — Phase 3: Single Project DAG

仕様書: docs/spec-mindmap-redesign.md (セクション 6.3)

## タスク（roadmap/templates/index.html）

### 1. データ取得切り替え
- [ ] Single Project モード時に `/api/roadmap-graph` から取得
- [ ] refresh() で currentView === 'graph' かつ selectedProject 時に graph API を使用

### 2. DAG 描画関数
- [ ] `buildSemanticGraph(graphData)` 関数を新規作成
- [ ] ノード種別ごとの視覚表現:
  - goal: オレンジ枠、🎯 アイコン
  - task (in_progress): 青太枠、▶ アイコン、グロー効果
  - task (done): 緑細枠、✓ アイコン、やや透過
  - task (planned): グレー細枠、○ アイコン
  - task (blocked): 赤枠、✖ アイコン
  - milestone: 丸角ボックス、合流ノード
  - rejected: グレー破線枠、行き止まりマーク
- [ ] エッジ種別ごとの視覚表現:
  - fork: 実線、タスク状態色
  - flow: 水色実線（合流）
  - dependency: グレー破線矢印
  - rejected: 薄グレー破線

### 3. Current Focus 強調
- [ ] current_focus に一致するタスクノードにグロー効果

### 4. Inspector 対応
- [ ] DAG ノードクリック時にセッション情報を Inspector に表示
