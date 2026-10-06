# devctx

## ビルド・テスト
- `go build ./...` / `go test ./...` / `go vet ./...`
- コミット前に必ず `go test ./... && go vet ./...`

## Web (roadmap/templates/index.html + roadmap/static/)
- `templates/index.html` はマークアップと `<link>` / `<script src>` だけ。CSS は `static/app.css`、JS は `static/*.js`（素の script でグローバルを共有。bundler / ES module なし）
- 読み込み順は index.html の `<script src>` の通り: theme → layout → tree → graphdata → edit → drag → app（app.js は初期化を含むので最後）。新しい js を足したら index.html に追記する（Go のテストが未参照の js を検出する）
- `/static/*` は embed.FS から配信（`Cache-Control: no-cache`）。css / js を足しても embed の glob（`static/*.css static/*.js`）に入る
- 純関数は各 js 末尾の `module.exports`（ブラウザでは無視される）経由で Node から単体テストする: `node --test 'roadmap/static/test/*.test.js'`（ディレクトリ指定の `node --test dir/` は Node 21+ で動かないので glob で渡す）。DOM / vis に触る関数はテスト対象外
- Go 側の文字列アサーションは `dashboardSource(t)`（index.html + static/*.js の連結）を見る
- vis-network で Mind Map 描画。embed.FS で組み込み
- プロジェクト切り替え時は `graphNetwork.destroy()` → 再作成（`setData` だと hierarchical layout が壊れる）
- ノード背景は不透明色を使う（半透明だとエッジが透けて見える）
- `onProjectChange()` は async。ポーリングとの競合を `projectChanging` フラグで防止

## デモデータでの UI テスト
- 実データのバックアップ→デモデータ投入→テスト→復元のフロー
- `~/.config/devctx/contexts.yaml` と `insights.yaml` を操作
- テスト後の復元を忘れないこと
