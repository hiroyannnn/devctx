package roadmap

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

type mockIslandLoader struct {
	store *model.IslandStore
	err   error
}

func (m *mockIslandLoader) LoadIslands() (*model.IslandStore, error) { return m.store, m.err }

type islandsPayload struct {
	Islands []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Parent string `json:"parent"`
	} `json:"islands"`
	Repos []struct {
		Root   string `json:"root"`
		Parent string `json:"parent"`
	} `json:"repos"`
}

func getIslands(t *testing.T, s *Server) (*httptest.ResponseRecorder, islandsPayload) {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleAPIIslands(w, httptest.NewRequest("GET", "/api/islands", nil))
	var resp islandsPayload
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("parse: %v\n%s", err, w.Body.String())
		}
	}
	return w, resp
}

func TestHandleAPIIslands_NilLoaderReturnsEmptyArrays(t *testing.T) {
	w, _ := getIslands(t, &Server{})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	// フロントが null を気にせず forEach できるよう、空でも [] で返す
	if got := strings.TrimSpace(w.Body.String()); got != `{"islands":[],"repos":[]}` {
		t.Errorf("body = %s", got)
	}
}

func TestHandleAPIIslands_ReturnsTreeWithDanglingParentsResolved(t *testing.T) {
	loader := &mockIslandLoader{store: &model.IslandStore{
		Islands: []model.Island{
			{ID: "hr", Name: "人事強化"},
			{ID: "m3", Name: "M3", Parent: "repo:/r/devctx"},
			{ID: "orphan", Name: "Orphan", Parent: "island:gone"},
		},
		Repos: []model.RepoNode{
			{Root: "/r/devctx", Parent: "island:hr"},
			{Root: "/r/stray", Parent: "island:gone"},
		},
	}}
	_, resp := getIslands(t, &Server{IslandLoader: loader})

	parents := map[string]string{}
	for _, i := range resp.Islands {
		parents["island:"+i.ID] = i.Parent
	}
	for _, r := range resp.Repos {
		parents["repo:"+r.Root] = r.Parent
	}
	want := map[string]string{
		"island:hr":      "",
		"island:m3":      "repo:/r/devctx",
		"island:orphan":  "",
		"repo:/r/devctx": "island:hr",
		"repo:/r/stray":  "",
	}
	for k, v := range want {
		if got, ok := parents[k]; !ok || got != v {
			t.Errorf("parent of %s = %q (present %v), want %q", k, got, ok, v)
		}
	}
	if resp.Islands[0].Name != "人事強化" {
		t.Errorf("name = %q", resp.Islands[0].Name)
	}
}

func TestHandleAPIIslands_RejectsNonGET(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).handleAPIIslands(w, httptest.NewRequest("POST", "/api/islands", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if got := w.Header().Get("Allow"); got != "GET" {
		t.Errorf("Allow = %q", got)
	}
}

func TestHandleAPIIslands_LoaderError(t *testing.T) {
	w, _ := getIslands(t, &Server{IslandLoader: &mockIslandLoader{err: errors.New("boom")}})
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// index.html と static/*.js は埋め込み静的ファイルでブラウザ無しでは動作を試せないため、
// island 描画に必要な取得・共通 helper・安定ノード ID が残っていることだけを守る。
func TestHandleIndex_WiresIslandEditing(t *testing.T) {
	body := dashboardSource(t)

	for _, want := range []string{
		"postOp('/api/islands/ops', body)",                // 編集は専用 endpoint に JSON で送る
		"await fetch(url, {",                              // island 編集とセッション紐付けが同じ送信経路を通る
		"headers: { 'Content-Type': 'application/json' }", // Origin はブラウザが付ける。Content-Type だけ明示
		"function editingActive()",                        // 編集中は refresh で再描画しない
		"pendingData = ",                                  // 編集中に届いた更新は保留して、閉じたら反映する
		"function buildContextMenu(",                      // 右クリックメニュー（DOM は createElement のみ）
		"graphNetwork.on('oncontext'",                     // vis の右クリック
		"function contextMenuItems(",                      // 種別ごとのメニュー項目（純関数）
		"function keyAction(",                             // キーボード操作の対応表（純関数）
		"addEventListener('keydown', onGraphKeydown)",     // ハンドラを定義するだけで配線し忘れない
		"function selectAfterOp(",                         // 全 op が同じ再描画経路で選択を引き継ぐ
		"var islandSides = {};",                           // 部分木の左右は再描画をまたいで保つ
		"var refreshSeq = 0;",                             // 古い取得が新しい取得を巻き戻さない
		"function closeEditUIOnOutsideMouseDown()",        // 外側クリックで閉じても、更新の反映はクリックが済んでから
		"function deleteConfirmModel(",                    // 削除は子がいなくても必ず確認する
		"function graphFocusEl()",                         // キー操作の受け手は vis の frame
		"function closeEditUI()",                          // 編集 UI を閉じる処理は 1 か所
		"function descendantsOf(",                         // ドラッグの落とし先から自分の子孫を除く（循環を作らせない）
		"function dropCandidateIds(",                      // 落とし先の候補（island / repo / root）
		"function dropOpFor(",                             // 落とした結果の操作（同じ親・既にトップレベルは無操作）
		"graphNetwork.on('dragging'",                      // ドラッグ中に落とし先を強調する
		"graphNetwork.on('dragEnd'",                       // 離したら attach / detach を送る
		"var DROP_BORDER = '#EF6F6F';",                    // 落とし先の強調色
		"/api/islands/known-repos",                        // 「repo を付ける」の一覧（選んだときだけ取る）
		"window.addEventListener('pointercancel', onCancel, { once: true })", // pointercancel でドラッグ状態が残らない（このドラッグの間だけ登録）
		"function pointerInsideGraph(",                                       // グラフの外では落とし先を選ばない
		"borderDashes: false",                                                // 強調の破線は省略ではなく false で消す（vis は深くマージする）
		"function fullColor(",                                                // highlight / hover まで省略なしで元へ戻す
		"graphNetwork.moveNode(st.id, st.start.x, st.start.y)",               // 取り消し・無操作・失敗では元の位置へ戻す
		"function holdRender()",                                              // 描画の保留は編集 UI とドラッグ中
		"function showRepoSubmenu(",                                          // 一覧は「repo を付ける…」を選んだときに取る
		"function attachableRepos(",                                          // 子・祖先（循環）を除いた repo の候補
		"repo を付ける…",                                                         // メニュー項目
		"var refreshInFlight = false;",                                       // ポーリングが重ならない
		"normalizeIslands(res.data.islands)",                                 // 成功後は応答の木で描画する（全体取得しない）
		"function siblingParentRef(",                                         // Enter で兄弟を足すときの親
		"function childrenRefsOf(",                                           // 削除確認で見せる子（server に children として送る）
		"keyboard: { enabled: true, bindToWindow: false",                     // vis の window 束縛キー操作が入力中の矢印・- を奪うため切る
		"graphNetwork.moveTo({ scale: keep.scale",                            // 再描画で視点（拡大率・位置）を保つ
		"子が変わりました。もう一度確認してください",                                              // children の 409 のトースト
		"プロンプトとしてコピー",                                                        // タスクのメニュー（marker 付きの本文をクリップボードへ）
		"navigator.clipboard.writeText(text)",                                // クリックの同期部分で直接呼ぶ
		"function showCopyFallback(",                                         // clipboard が使えない・拒否されたときの textarea
		"var TASK_MARKER_PREFIX = '[devctx:task:';",                          // model.TaskMarkerPrefix と同じ書式
		"function taskPromptText(",                                           // タスク名 + 空行 + marker（最終行）
		"kind: 'task'",                                                       // タスク追加は add の kind で送る
		"op: 'done'",                                                         // 完了の切り替え
		"var draggedIsTask = editableKind(dragged) === 'task';",              // タスクのドラッグでは root を候補から外す
		"コピーできませんでした",                                                        // 別の編集 UI が開いている間の clipboard 拒否は toast だけにする
		"タスクは親が必要です",                                                         // 親のない島のタスク付き削除は、確認ではなく理由を見せる
		"function buildConfirmPanel(",                                        // 削除確認とコピーのフォールバックが共有する確認パネル
		"_type: 'task'",                                                      // タスクは島と別の種別（メニュー・キー・ドラッグの分岐点）
		"/api/sessions/ops",                                                  // セッションのタスク紐付けは専用 endpoint（islands とは別の store を書く）
		"function sessionMenuItems(",                                         // セッションの右クリックメニュー（タスクに付ける / 外す）
		"function taskMenuItems(",                                            // 付ける先のタスク一覧（未完了が先）
		"function taskOptions(",                                              // 候補は描画済みの islands から作る
		"function runSessionOp(",                                             // 紐付け後は全体を取り直して描画する
		"function linkedTaskNodeId(",                                         // タスクに紐付いたセッションの親を決める
		"function sessionTaskLine(",                                          // 単一プロジェクト・消えたタスクのラベル行
		"function sessionTaskName(",                                          // インスペクタの Task 欄
		"linkedSessions.forEach(function(l) { edges.push(treeEdge(l.taskId, l.session.id)); });", // repo → session をタスク → session に置き換える
		"_type: 'dag',",               // DAG ノードは島のタスク（_type 'task'）と別の種別
		"openSessionMenu(node, x, y)", // セッション上の右クリック
		"完了にする",                       // タスクのメニュー
		"未完了に戻す",                      // タスクのメニュー
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard source does not contain %q", want)
		}
	}
	// ドラッグの間引き（rAF）と、ドラッグに依らない document 全体の mouseup 保険は持たない
	for _, gone := range []string{"requestAnimationFrame", "settling", "document.addEventListener('mouseup'"} {
		if strings.Contains(body, gone) {
			t.Errorf("dashboard source still contains %q", gone)
		}
	}
	// 編集 UI の DOM は innerHTML で組まない（島の名前は利用者入力）
	if strings.Contains(body, "innerHTML") {
		t.Error("dashboard source (index.html + static/*.js) must not use innerHTML")
	}
}

func TestHandleIndex_WiresIslandsIntoMindMap(t *testing.T) {
	body := dashboardSource(t)

	for _, want := range []string{
		"/api/islands",                                      // refresh と同じ周期で取得する
		"function islandOverlay(",                           // tree / semantic の両 builder が共有する
		"islandOverlay(nodes, edges, cachedIslands)",        // 両 builder から呼ばれる
		"JSON.stringify(islandsData)",                       // island の編集が再描画の変化検知に入る
		"'session:' + session.name",                         // 連番ではなく安定した ID
		"repoNodeId(",                                       // repo ノードの安定 ID
		"'more:' + ",                                        // more ノードも repo 単位の安定 ID
		"var MINDMAP_THEME = {",                             // 見た目の数値・色は 1 か所
		"function balancedLayout(",                          // 左右バランス配置の純関数
		"function postProcess(nodes, edges, isAllProjects)", // 両 builder が通る後処理の単一入口
		"function islandNodeId(",                            // ノード ID を手組みしない
		"if (n.borderWidth === undefined) n.borderWidth = T.borderWidth;", // session の待ち強調（太い枠）を上書きしない
		"applyMindmapTheme(nodes);",                                       // 両 builder で共通のテーマ適用
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard source does not contain %q", want)
		}
	}
	for _, gone := range []string{"'proj:' + (++nid)", "'sess:' + (++nid)"} {
		if strings.Contains(body, gone) {
			t.Errorf("dashboard source still uses sequential id %q", gone)
		}
	}
}

// JS 側の marker 書式が model.TaskMarkerPrefix とずれると、セッションの紐づけが黙って外れる。
func TestHandleIndex_TaskMarkerMatchesModel(t *testing.T) {
	want := "var TASK_MARKER_PREFIX = '" + model.TaskMarkerPrefix + "';"
	if !strings.Contains(dashboardSource(t), want) {
		t.Errorf("dashboard source does not contain %q", want)
	}
}

// dashboardSource は index.html と static/*.js（読み込み順）を連結した文字列を返す。
// Why: ダッシュボードの JS は複数ファイルに分かれているので、文字列で配線を守るテストは全体を対象にする。
// 読み込み順は index.html の <script src> から取り、配信できないファイルを参照していればここで落ちる。
func dashboardSource(t *testing.T) string {
	t.Helper()
	index, err := templateFS.ReadFile("templates/index.html")
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.Write(index)
	srcs := scriptSrcPattern.FindAllStringSubmatch(string(index), -1)
	if len(srcs) == 0 {
		t.Fatal("index.html に <script src=\"/static/...\"> が無い")
	}
	for _, m := range srcs {
		js, err := staticFS.ReadFile("static/" + m[1])
		if err != nil {
			t.Fatalf("index.html が参照する static/%s を読めない: %v", m[1], err)
		}
		sb.WriteString("\n")
		sb.Write(js)
	}
	return sb.String()
}

var scriptSrcPattern = regexp.MustCompile(`<script src="/static/([^"]+\.js)"></script>`)

func TestStaticScripts(t *testing.T) {
	handler := (&Server{Port: 3333}).Handler()
	entries, err := staticFS.ReadDir("static")
	if err != nil {
		t.Fatal(err)
	}
	referenced := map[string]bool{}
	for _, m := range scriptSrcPattern.FindAllStringSubmatch(dashboardSource(t), -1) {
		referenced[m[1]] = true
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		req := httptest.NewRequest("GET", "/static/"+e.Name(), nil)
		req.Host = "127.0.0.1:3333"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", e.Name(), w.Code)
		}
		if got := w.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
			t.Errorf("%s: Content-Type = %q", e.Name(), got)
		}
		// 置いただけで読み込まれない js は、配線し忘れの兆候
		if !referenced[e.Name()] {
			t.Errorf("%s は index.html から読み込まれていない", e.Name())
		}
	}
	// app.js は他ファイルの関数を呼ぶので最後に読む
	order := scriptSrcPattern.FindAllStringSubmatch(dashboardSource(t), -1)
	if last := order[len(order)-1][1]; last != "app.js" {
		t.Errorf("最後に読み込む script = %s, want app.js", last)
	}
}
