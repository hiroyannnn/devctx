// Mind Map の見た目（色・数値・テーマ適用）。

// ── Mind Map theme ──
// 見た目の数値・色はここに集約する。値は MindMup（mapjs）の既定テーマの数値・配色を参考にしたもので、コードは写していない。
// Why not 自前の vis 描画をやめて mapjs を使う: セッション状態の色・DAG・live 更新など既存の描画資産を vis-network 上に残すため。
// 背景は全て不透明（半透明だとエッジが透ける）。dim も透明度ではなく不透明の暗い色で表す。
// vis-network の fit() は既定で拡大率を 1 で頭打ちにするため、ノードの少ない Map が大きな画面の中央に小さく残る。
// 上限を上げても、ノードの多い Map は縮小側に fit するので影響しない
var FIT_MAX_ZOOM = 1.8;

var MINDMAP_THEME = {
  fontFace: 'NotoSans, "Helvetica Neue", Roboto, Helvetica, Arial, sans-serif',
  fontSize: 12,
  radius: 10,
  margin: { top: 5, right: 8, bottom: 5, left: 8 },
  borderWidth: 1,
  borderWidthSelected: 3,
  shadow: { enabled: true, color: 'rgba(7,7,7,0.4)', size: 2, x: 2, y: 2 },
  maxWidth: 146,
  edge: '#707070',
  root:    { bg: '#22AAE0', border: '#707070', font: '#EEEEEE' },
  island:  { bg: '#E0E0E0', border: '#707070', font: '#4F4F4F' },
  repo:    { bg: '#C9D3DC', border: '#707070', font: '#2F2F2F' },
  // アクティブな context が無い repo。暗い背景のページに溶けない程度に落とした色
  repoDim: { bg: '#4A525B', border: '#5A626B', font: '#B6BEC6' },
  // タスク。島（灰）・repo（青灰）と見分けるため白地に root と同じ青の枠。太字にしないので島より軽く見える。
  // 完了は透明度ではなく不透明の淡い色で沈める（半透明だとエッジが透ける）
  task:     { bg: '#FFFFFF', border: '#22AAE0', font: '#2F2F2F', plain: true },
  taskDone: { bg: '#E3E7EA', border: '#A9B7C2', font: '#8A8A8A', plain: true }
};

// ツリー辺（root / island / repo / session を繋ぐ辺）。DAG の意味付きの辺には使わない
function treeEdge(from, to) {
  return {
    from: from, to: to,
    color: { color: MINDMAP_THEME.edge, inherit: false },
    width: 1,
    arrows: { to: { enabled: false } }
  };
}

// _type から色の種別を決める。session / タスク等は null で、builder が与えた意味色をそのまま使う。
// アクティブな session が無い repo は dim。
function themeKindOf(n) {
  if (n._type === 'root' || n._type === 'island') return n._type;
  if (n._type === 'project') return n._data && n._data.sessions && n._data.sessions.length === 0 ? 'repoDim' : 'repo';
  if (n._type === 'task') return n._data && n._data.done ? 'taskDone' : 'task';
  return null;
}

// 見た目を決める唯一の場所。全ノードに共通の形（角丸・余白・影・折り返し幅）と、構造ノードの色を与える。
// 構造ノード（root / island / repo / session）だけ文字サイズも揃え、DAG のタスクノードは自前のサイズと色を保つ。
// Why: builder / overlay には構造（id・label・level・_type・_data）だけを持たせ、色を散らさない。
function applyMindmapTheme(nodes) {
  var T = MINDMAP_THEME;
  var uniform = { root: 1, island: 1, task: 1, project: 1, session: 1 };
  nodes.forEach(function(n) {
    n.shape = 'box';
    n.shapeProperties = Object.assign({}, n.shapeProperties, { borderRadius: T.radius });
    n.margin = Object.assign({}, T.margin);
    // builder が与えた太さは残す（session の待ち = 3 / attention = 2 の強調を潰さない）。未指定のノードだけ既定にする
    if (n.borderWidth === undefined) n.borderWidth = T.borderWidth;
    n.borderWidthSelected = T.borderWidthSelected;
    if (!n.shadow) n.shadow = Object.assign({}, T.shadow); // フォーカス中ノードの青いグローは残す
    n.widthConstraint = { maximum: T.maxWidth };
    var f = Object.assign({}, n.font, { face: T.fontFace });
    if (uniform[n._type]) f.size = T.fontSize;
    var kind = themeKindOf(n);
    if (kind) {
      var c = T[kind];
      n.color = { background: c.bg, border: c.border };
      f.color = c.font;
      f.bold = !c.plain;
      f.multi = !c.plain;
    }
    n.font = f;
  });
}

// Node の単体テスト用。ブラウザでは module が無いので何も起きない。
if (typeof module !== 'undefined') module.exports = { themeKindOf, applyMindmapTheme };
