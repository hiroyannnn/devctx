# devctx

## ビルド・テスト
- `go build ./...` / `go test ./...` / `go vet ./...`
- コミット前に必ず `go test ./... && go vet ./...`

## Web (roadmap/templates/index.html)
- vis-network で Mind Map 描画。embed.FS で組み込み
- プロジェクト切り替え時は `graphNetwork.destroy()` → 再作成（`setData` だと hierarchical layout が壊れる）
- ノード背景は不透明色を使う（半透明だとエッジが透けて見える）
- `onProjectChange()` は async。ポーリングとの競合を `projectChanging` フラグで防止

## デモデータでの UI テスト
- 実データのバックアップ→デモデータ投入→テスト→復元のフロー
- `~/.config/devctx/contexts.yaml` と `insights.yaml` を操作
- テスト後の復元を忘れないこと
