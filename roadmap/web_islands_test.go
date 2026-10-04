package roadmap

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

// index.html は埋め込み静的ファイルでブラウザ無しでは動作を試せないため、
// island 描画に必要な取得・共通 helper・安定ノード ID が残っていることだけを守る。
func TestHandleIndex_WiresIslandEditing(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).handleIndex(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()

	for _, want := range []string{
		"fetch('/api/islands/ops'",                        // 編集は専用 endpoint に JSON で送る
		"headers: { 'Content-Type': 'application/json' }", // Origin はブラウザが付ける。Content-Type だけ明示
		"function editingActive()",                        // 編集中は refresh で再描画しない
		"pendingData = ",                                  // 編集中に届いた更新は保留して、閉じたら反映する
		"function buildContextMenu(",                      // 右クリックメニュー（DOM は createElement のみ）
		"graphNetwork.on('oncontext'",                     // vis の右クリック
		"function contextMenuItems(",                      // 種別ごとのメニュー項目（純関数）
		"function keyAction(",                             // キーボード操作の対応表（純関数）
		"addEventListener('keydown', onGraphKeydown)",     // ハンドラを定義するだけで配線し忘れない
		"function siblingParentRef(",                      // Enter で兄弟を足すときの親
		"function childrenRefsOf(",                        // 削除確認で見せる子（server に children として送る）
		"keyboard: { enabled: false }",                    // vis の window 束縛キー操作が入力中の矢印・- を奪うため切る
		"graphNetwork.moveTo({ scale: keep.scale",         // 再描画で視点（拡大率・位置）を保つ
		"子が変わりました。もう一度確認してください",                           // children の 409 のトースト
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index.html does not contain %q", want)
		}
	}
	// 編集 UI の DOM は innerHTML で組まない（島の名前は利用者入力）
	if strings.Contains(body, "innerHTML") {
		t.Error("index.html must not use innerHTML")
	}
}

func TestHandleIndex_WiresIslandsIntoMindMap(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).handleIndex(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()

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
			t.Errorf("index.html does not contain %q", want)
		}
	}
	for _, gone := range []string{"'proj:' + (++nid)", "'sess:' + (++nid)"} {
		if strings.Contains(body, gone) {
			t.Errorf("index.html still uses sequential id %q", gone)
		}
	}
}
