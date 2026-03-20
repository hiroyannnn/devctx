# Mind Map セマンティックグラフ — Phase 2: All Projects 改善

仕様書: docs/spec-mindmap-redesign.md (セクション 6.2)

## タスク（roadmap/templates/index.html）

### 1. レイアウト改善
- [ ] "Sessions" ルートノード削除 — Project を最左列に直接配置
- [ ] 画面密度UP: levelSeparation 220→150, nodeSpacing 40→24, treeSpacing 60→32
- [ ] canvas 背景: radial-gradient

### 2. ノードスタイル改善
- [ ] Project ノード: ニュートラル色 (#21262d) + attention バッジ (● N blocked/active 等)
- [ ] Session ノード: 左アクセントバーで attention state 表現
  - blocked=赤, active=青, waiting=紫, idle=グレー
- [ ] Session ラベル: name + Current Focus 要約 (1行)
- [ ] フォントサイズUP: session 10→13, project 12→15

### 3. エッジ改善
- [ ] デフォルト色: #30363d → #484f58
- [ ] デフォルト幅: 1 → 1.5
- [ ] active エッジ色: #79c0ff, 幅 3

### 4. ソート改善
- [ ] All Projects のセッションを attention state 緊急度順にソート
  - blocked → active → waiting → idle → done
- [ ] プロジェクトも attention 優先度順にソート
