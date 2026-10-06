// 左右バランス配置。

// ── 左右バランス配置（All Projects） ──
// root の子の部分木を左右に振り分け、左は root の x で鏡写しにし、各側で縦に詰め直す。
// 入力は vis の初期（hierarchical LR）配置後の座標つきノード {id, label, x, y, h} と辺。DOM / vis に依存しない純関数。
// Why not vis の hierarchical だけで左右に割る: LR は全ノードを root の右に並べるので、後処理で座標を作り直す。
// 戻り値の positions は部分木に属する全ノードの新しい座標、side は各ノードの 'left' / 'right'。
var LAYOUT_GAP = 24;
// root の子ごとの左右（直前の描画）。再描画をまたいで部分木の側を保つ。プロジェクト切り替えで捨てる
var islandSides = {};

function balancedLayout(nodes, edges, rootId, prevSides) {
  prevSides = prevSides || {};
  var byId = {};
  nodes.forEach(function(n) { byId[n.id] = n; });
  var rootNode = byId[rootId];
  if (!rootNode) return null;

  var kids = {};
  edges.forEach(function(e) { (kids[e.from] = kids[e.from] || []).push(e.to); });
  function byLabel(a, b) {
    var la = String(byId[a].label), lb = String(byId[b].label);
    return la < lb ? -1 : la > lb ? 1 : (a < b ? -1 : a > b ? 1 : 0);
  }

  // 部分木。複数の子から届くノードは先に辿った方に属させる（ツリー辺のみなので通常は起きない）
  var owner = {};
  owner[rootId] = rootId;
  var seen = {};
  var heads = (kids[rootId] || []).filter(function(id) {
    if (!byId[id] || seen[id]) return false;
    seen[id] = true;
    return true;
  }).sort(byLabel);
  var subtrees = heads.map(function(head) {
    var members = [];
    var stack = [head];
    owner[head] = head;
    while (stack.length) {
      var id = stack.pop();
      members.push(id);
      (kids[id] || []).forEach(function(c) {
        if (byId[c] && !owner[c]) { owner[c] = head; stack.push(c); }
      });
    }
    return { head: head, members: members };
  });

  // 既存の部分木は前回の側に留め、新しい部分木だけを軽い側へ（同数なら右）割り振る。
  // Why: 島を 1 つ足すたびに全体を貪欲法で振り直すと、無関係な部分木が左右を行き来して位置を見失う。
  // 新規の振り分けは大きい順・ラベル順で決め、再描画で並びが揺れないようにする。各側の積み順もラベル順。
  var left = [], right = [], leftCount = 0, rightCount = 0;
  var headSides = {};
  function assign(st, sideName) {
    headSides[st.head] = sideName;
    if (sideName === 'left') { left.push(st); leftCount += st.members.length; }
    else { right.push(st); rightCount += st.members.length; }
  }
  var fresh = [];
  subtrees.forEach(function(st) {
    if (prevSides[st.head] === 'left' || prevSides[st.head] === 'right') assign(st, prevSides[st.head]);
    else fresh.push(st);
  });
  fresh.sort(function(a, b) {
    return b.members.length - a.members.length || byLabel(a.head, b.head);
  }).forEach(function(st) {
    assign(st, leftCount < rightCount ? 'left' : 'right');
  });

  var positions = {};
  var side = {};
  positions[rootId] = { x: rootNode.x, y: rootNode.y };
  function place(list, mirror) {
    list.sort(function(a, b) { return byLabel(a.head, b.head); });
    var extents = list.map(function(st) {
      var top = Infinity, bottom = -Infinity;
      st.members.forEach(function(id) {
        var n = byId[id], h = n.h || 30;
        top = Math.min(top, n.y - h / 2);
        bottom = Math.max(bottom, n.y + h / 2);
      });
      return { top: top, height: bottom - top };
    });
    var total = extents.reduce(function(sum, e) { return sum + e.height; }, 0) + LAYOUT_GAP * Math.max(0, list.length - 1);
    var cursor = rootNode.y - total / 2;
    list.forEach(function(st, i) {
      var dy = cursor - extents[i].top;
      st.members.forEach(function(id) {
        var n = byId[id];
        positions[id] = { x: mirror ? 2 * rootNode.x - n.x : n.x, y: n.y + dy };
        side[id] = mirror ? 'left' : 'right';
      });
      cursor += extents[i].height + LAYOUT_GAP;
    });
  }
  place(right, false);
  place(left, true);
  return { positions: positions, side: side, headSides: headSides };
}

// Node の単体テスト用。ブラウザでは module が無いので何も起きない。
if (typeof module !== 'undefined') module.exports = { balancedLayout };
