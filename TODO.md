# Mind Map セマンティックグラフ — Phase 0: データモデル拡張

仕様書: docs/spec-mindmap-redesign.md

## タスク

### 1. TaskItem モデル拡張
- [ ] `TaskItem` に `ID` (string) フィールド追加
- [ ] `TaskItem` に `DependsOn` ([]string) フィールド追加
- [ ] `TaskItem` に `FlowsTo` (string) フィールド追加
- [ ] `TaskStatus` に `rejected` 定数追加
- [ ] 既存テスト更新 + 新フィールドのテスト

### 2. AI推論プロンプト更新
- [ ] `BuildAnalyzePrompt` の JSON 形式に `id`, `depends_on`, `flows_to` を追加
- [ ] `ParseAnalyzeResponse` で新フィールドをパース
- [ ] analyzer_test.go 更新

### 3. Extractor 対応
- [ ] `ExtractTasks` で新フィールドを保持
- [ ] extractor_test.go 更新

### 4. API レスポンス確認
- [ ] `/api/roadmap-map` の RoadmapEntry が新フィールドを含むことを確認
- [ ] web_test.go 更新
