// ドラッグで親を付け替える。

var dragState = null;     // ドラッグ中の状態（親付け替えの対象なら候補・強調中の落とし先も持つ）。holdRender で描画を保留する

// ── ドラッグで親を付け替える ──
// island / repo ノードを別の island / repo / root にドラッグして落とすと、その下に付く（root は親から外す）。
// ドラッグ中は編集中として扱い（editingActive）、ポーリングの描画を保留する。作り直されるとドラッグが途切れるため。
var DROP_BORDER = '#EF6F6F'; // 落とし先の強調色（MindMup の落とし先と同じ赤の破線）

// 落とし先の強調を戻す。ノードが作り直し等で無ければ何もしない。
// vis の color は、highlight / hover が無い入力に対しては base の色で埋める。強調で上書きした highlight / hover を
// 元へ戻すには、省略せず全部を明示して送る必要があるので、元の color を省略なしの形にしておく。
function fullColor(color) {
  var bg = color && color.background, border = color && color.border;
  function variant(v) {
    return { background: (v && v.background) || bg, border: (v && v.border) || border };
  }
  return { background: bg, border: border, highlight: variant(color && color.highlight), hover: variant(color && color.hover) };
}

// 落とし先の強調を戻す。ノードが作り直し等で無ければ何もしない。
// shapeProperties は vis が深くマージするので、borderDashes は省略ではなく false を明示して消す。
function restoreDropHighlight() {
  var st = dragState;
  if (!st || !st.saved) return;
  var saved = st.saved;
  st.saved = null;
  if (graphNodes && graphNodes.get(saved.id)) {
    graphNodes.update({
      id: saved.id,
      color: saved.color,
      borderWidth: saved.borderWidth,
      shapeProperties: Object.assign({}, saved.shapeProperties, { borderDashes: false })
    });
  }
}

function setDropHighlight(id) {
  var st = dragState;
  if (!st || st.target === id) return;
  restoreDropHighlight();
  st.target = id;
  var n = id ? graphNodes.get(id) : null;
  if (!n) return;
  // 元の見た目を控え、枠だけ赤い破線にする（背景は元のまま。透過させない）
  var orig = fullColor(n.color);
  st.saved = { id: id, color: orig, borderWidth: n.borderWidth, shapeProperties: n.shapeProperties };
  var c = { background: orig.background, border: DROP_BORDER };
  graphNodes.update({
    id: id,
    color: { background: orig.background, border: DROP_BORDER, highlight: c, hover: c },
    borderWidth: 3,
    shapeProperties: Object.assign({}, n.shapeProperties, { borderDashes: [6, 4] })
  });
}

// DOM 座標の点が、幅 w・高さ h のグラフ領域の内側か。Hammer は window の pointermove でも dragging を出すので、
// グラフの外でも pointer.canvas は（画面外の）グラフ座標に写ってしまう。落とし先はグラフ内にあるときだけ選ぶ。
function pointInside(dom, w, h) {
  return !!dom && dom.x >= 0 && dom.y >= 0 && dom.x <= w && dom.y <= h;
}

function pointerInsideGraph(dom) {
  var el = document.getElementById('graph-canvas');
  return !!el && pointInside(dom, el.clientWidth, el.clientHeight);
}

// ドラッグ開始。親付け替えの対象（All Projects の island / repo ノード 1 つ）なら、候補とその矩形を先に求める。
// それ以外（視点のパン・session などのノード）も、描画の作り直しで途切れないよう dragState だけは立てる（holdRender 用）。
function beginDrag(nodeId) {
  var kind = nodeId ? editableKind(graphNodes.get(nodeId)) : null;
  var st;
  if (!graphRootId || !kind || kind === 'root') {
    st = { id: null };
  } else {
    // 矩形は canvas 座標。候補はドラッグ中に動かない（動かすのは掴んだノードだけ）ので、1 回測れば足りる
    var boxes = dropCandidateIds(graphNodes.get(), nodeId, cachedIslands).map(function(id) {
      var b = graphNetwork.getBoundingBox(id);
      return { id: id, left: b.left, right: b.right, top: b.top, bottom: b.bottom };
    });
    // 取り消し・無操作・失敗のときに、掴んだノードを元の位置へ戻すために開始位置を控える
    st = { id: nodeId, boxes: boxes, target: null, saved: null, cancelled: false, finished: false, start: graphNetwork.getPosition(nodeId) };
  }
  dragState = st;
  armDragSafety(st);
}

// dragEnd が来ない終わり方への備え。このドラッグの間だけ window に登録し、終わったら外す。
// pointercancel（タッチの中断等）は vis に dragEnd が届かず、dragState が残ると描画の保留が解けなくなる。
// pointerup は通常 dragEnd が先に処理するので、次の tick でまだこのドラッグが残っていたときだけ終わらせる。
function armDragSafety(st) {
  function disarm() {
    window.removeEventListener('pointerup', onUp);
    window.removeEventListener('pointercancel', onCancel);
  }
  function onUp() {
    disarm();
    setTimeout(function() { if (dragState === st) finishDrag(); }, 0);
  }
  function onCancel() {
    disarm();
    if (dragState !== st) return;
    revertDragPosition(st);
    cancelDrag();
    flushPending();
  }
  st.disarm = disarm;
  window.addEventListener('pointerup', onUp, { once: true });
  window.addEventListener('pointercancel', onCancel, { once: true });
}

// 掴んだノードを開始位置へ戻す。親が変わらなかった（取り消し・無操作・失敗）のに、落とした場所に
// 残ると付け替わったように見えるため。成功時は再描画が配置し直すので戻さない。
function revertDragPosition(st) {
  if (st && st.id && st.start && graphNodes.get(st.id)) graphNetwork.moveNode(st.id, st.start.x, st.start.y);
}

function updateDragTarget(canvasPoint, inside) {
  var st = dragState;
  if (!st || !st.id || st.cancelled) return;
  // グラフの外では落とし先を持たない（戻ってくるまで null のまま）
  setDropHighlight(inside === false ? null : hitTarget(st.boxes, canvasPoint));
}

// ドラッグ終了。強調中の落とし先があり、結果が無操作でなければ runIslandOp で送る。
// 落とし先は、グラフの内側にいる間に選ばれた強調（dragging で更新した target）だけを見る。
// 送る間は dragState を残す（= 描画を保留する）ので、応答の木は保留に入り、完了後に flushPending で反映される。
// Why: ポーリング由来の古い保留が、操作の結果を上書きして描画しないようにする（応答反映時に refreshSeq も進む）。
function finishDrag() {
  var st = dragState;
  if (!st || st.finished) return;
  st.finished = true;
  if (st.disarm) st.disarm();
  var target = st.id && !st.cancelled ? st.target : null;
  restoreDropHighlight();
  var op = target ? dropOpFor(st.id, target, cachedIslands) : null;
  if (!op) {
    revertDragPosition(st);
    dragState = null;
    flushPending();
    return;
  }
  runIslandOp(op).then(function(ok) {
    // 待っている間に次のドラッグが始まっていたら、その状態は触らない
    if (dragState !== st) return;
    if (ok === false) revertDragPosition(st); // 失敗時は再描画が起きないことがあるので、位置を戻す
    dragState = null;
    flushPending();
  });
}

// Esc: 掴んだまま取り消す。ボタンを離すまでは dragState を残し（描画を保留）、離したら何もせず終わる。
function softCancelDrag() {
  var st = dragState;
  if (!st || !st.id || st.cancelled) return;
  st.cancelled = true;
  restoreDropHighlight();
  st.target = null;
}

// 表示切り替え・プロジェクト切り替え・再描画で、ドラッグ状態を捨てる。
function cancelDrag() {
  if (dragState && dragState.disarm) dragState.disarm();
  restoreDropHighlight();
  dragState = null;
}

// ドラッグの落とし先の候補（ノード ID の配列）。island / repo / root のうち、ドラッグ中のノードとその子孫を除く。
// タスクは落とし先にならない（タスクの下には何も置けない）。ドラッグ中のノードがタスクなら root も外す。
function dropCandidateIds(nodes, draggedId, islandsData) {
  var banned = descendantsOf(draggedId, islandsData);
  banned[draggedId] = true;
  var dragged = nodes.filter(function(n) { return n.id === draggedId; })[0];
  var draggedIsTask = editableKind(dragged) === 'task';
  return nodes.filter(function(n) {
    var kind = editableKind(n);
    // タスクのドロップは root では無操作（dropOpFor）。強調だけ出ると操作できそうに見えるので候補から外す
    return kind && kind !== 'task' && !(draggedIsTask && kind === 'root') && !banned[n.id];
  }).map(function(n) { return n.id; });
}

// 落とした結果の操作。root へは親から外す（既にトップレベルなら何もしない。タスクは親が要るので常に何もしない）、
// island / repo へは付ける（既にそこが親なら何もしない）。無操作は null。
function dropOpFor(draggedId, targetId, islandsData) {
  var parent = parentRefOf(draggedId, islandsData);
  if (targetId === ROOT_ID) return parent === '' || editableKind(graphNodes.get(draggedId)) === 'task' ? null : { op: 'detach', child: draggedId };
  if (parent === targetId) return null;
  return { op: 'attach', child: draggedId, parent: targetId };
}

// ポインタ（canvas 座標）を含む矩形の id。重なっていれば面積の小さい方（内側のノード）。boxes: [{id,left,right,top,bottom}]
function hitTarget(boxes, p) {
  var best = null, bestArea = Infinity;
  boxes.forEach(function(b) {
    if (p.x < b.left || p.x > b.right || p.y < b.top || p.y > b.bottom) return;
    var area = (b.right - b.left) * (b.bottom - b.top);
    if (area < bestArea) { best = b.id; bestArea = area; }
  });
  return best;
}

// 「repo を付ける」に出す repo。既にこの island の子のものと、付けると循環になるもの
// （この island が repo の子孫、つまり repo がこの island の祖先）を除く。
function attachableRepos(repos, islandRef, islandsData) {
  var children = {};
  childrenRefsOf(islandRef, islandsData).forEach(function(c) { children[c] = true; });
  return repos.filter(function(r) {
    var ref = repoNodeId(r.root);
    return !children[ref] && !descendantsOf(ref, islandsData)[islandRef];
  });
}
