// island / repo / task の木モデル（ノード ID・親子の辿り・overlay）。

// ── Islands ──
// Why: island はユーザーが手で作るテーマノードで、repo と 1 本の木になる（親は island / repo のどちらでもよい）。
// 2 つの builder（tree / semantic DAG）が同じ描画を共有するよう、構築後に 1 か所で重ねる。
var cachedIslands = { islands: [], repos: [] };

var ROOT_ID = 'root';

function normalizeIslands(d) {
  return { islands: (d && d.islands) || [], repos: (d && d.repos) || [] };
}

// ノード ID は /api/islands の親 ref（island:<id> / repo:<path>）と同じ形にしてある。親をそのままノード ID として辿れる
var REPO_PREFIX = 'repo:';
var ISLAND_PREFIX = 'island:';

function repoNodeId(repoRoot) {
  return REPO_PREFIX + (repoRoot || '__ungrouped__');
}

function islandNodeId(id) {
  return ISLAND_PREFIX + id;
}

function repoBaseName(root) {
  var parts = String(root).split('/').filter(function(p) { return p; });
  return parts.length ? parts[parts.length - 1] : String(root);
}

// builder 共通の後処理。All Projects は island / root を重ね、全モードにテーマを当てる。
// rootId は中央 root があるときだけ入る（左右バランス配置の起点。単一プロジェクト表示では null）。
function postProcess(nodes, edges, isAllProjects) {
  var rootId = isAllProjects ? islandOverlay(nodes, edges, cachedIslands) : null;
  applyMindmapTheme(nodes);
  return { nodes: nodes, edges: edges, rootId: rootId };
}

// タスクの表示名。未完了は ☐、完了は ✓ を前に付ける。
function taskLabel(task) {
  return (task.done ? '✓ ' : '☐ ') + task.name;
}

// 「プロンプトとしてコピー」の marker。形式は model.TaskMarker（Go）と同じで、次の PR でセッションの
// 最初のプロンプトからこれを拾ってタスクの下へ付ける。片方だけ変えると紐づかなくなる。
var TASK_MARKER_PREFIX = '[devctx:task:';
function taskMarker(id) {
  return TASK_MARKER_PREFIX + id + ']';
}

// コピーするプロンプト。タスク名の後ろに空行を挟み、marker を独立した最終行に置く。
function taskPromptText(task) {
  return task.name + '\n\n' + taskMarker(task.id);
}

// ── Islands overlay ──
// nodes / edges を破壊的に更新する。All Projects 専用。戻り値は root の ID（ノードが無ければ null）。
// repo ノードは builder が置いたものを nodes から拾う。親は /api/islands が dangling を "" に解決済み（ここでは再検証しない）。
function islandOverlay(nodes, edges, islandsData) {
  var nodeIds = {};
  nodes.forEach(function(n) { nodeIds[n.id] = true; });

  var parentOf = {};
  islandsData.islands.forEach(function(is) { if (is.parent) parentOf[islandNodeId(is.id)] = is.parent; });
  islandsData.repos.forEach(function(r) { if (r.parent) parentOf[repoNodeId(r.root)] = r.parent; });

  // セッションが無い（またはフィルタで消えた）repo も、island 構造に関わるなら薄いノードで出す。
  // Why: 手で組んだ構造が「アクティブな context がある repo だけ」に依存して消えるのを避ける
  function ensureRepoNode(id, root) {
    if (nodeIds[id]) return;
    nodeIds[id] = true;
    nodes.push({
      id: id,
      label: repoBaseName(root) + '\n(no active sessions)',
      level: 0,
      _type: 'project',
      _data: { name: repoBaseName(root), repo_root: root, sessions: [] }
    });
  }
  function ensureParentRepo(parentRef) {
    if (parentRef.indexOf(REPO_PREFIX) === 0) ensureRepoNode(parentRef, parentRef.slice(REPO_PREFIX.length));
  }
  islandsData.repos.forEach(function(r) {
    if (!r.parent) return;
    ensureRepoNode(repoNodeId(r.root), r.root);
    ensureParentRepo(r.parent);
  });
  islandsData.islands.forEach(function(is) { if (is.parent) ensureParentRepo(is.parent); });

  islandsData.islands.forEach(function(is) {
    nodeIds[islandNodeId(is.id)] = true;
    if (is.kind === 'task') {
      // 親付け替え・メニュー・キーの種別は _type で決まるので、タスクは島と別の種別にする。完了は先頭の記号で示す
      nodes.push({ id: islandNodeId(is.id), label: taskLabel(is), level: 0, _type: 'task', _data: is });
      return;
    }
    nodes.push({ id: islandNodeId(is.id), label: is.name, level: 0, _type: 'island', _data: is });
  });

  // island / repo の木の辺と、根からの深さ。island→repo→island→repo の鎖でも LR に並ぶ。
  // 深さは DAG と同じ computeNodeDepth（根 = 1、循環は打ち切り）。root を level 0 に置くので、そのまま level になる
  var treeEdges = Object.keys(parentOf).map(function(child) { return { from: parentOf[child], to: child }; });
  var memo = {};
  var treeNodes = nodes.filter(function(n) { return n._type === 'island' || n._type === 'task' || n._type === 'project'; });

  // repo 配下（session / more / DAG）は、repo の level 分だけまとめてずらす。builder は repo = 0 起点で level を振っている。
  // 辺を下向きに辿るので、辺で繋がらない DAG ノードは builder が不可視の辺で繋いである（buildSemanticGraph）。
  // 木の辺を足す前の builder の辺だけを辿る（repo→island の辺を辿ると island を二重にずらす）
  var kids = {};
  edges.forEach(function(e) { (kids[e.from] = kids[e.from] || []).push(e.to); });
  var nodeById = {};
  nodes.forEach(function(n) { nodeById[n.id] = n; });
  treeNodes.forEach(function(n) {
    n.level = computeNodeDepth(n.id, treeEdges, nodes, memo);
    if (n._type !== 'project') return;
    var seen = {};
    var stack = (kids[n.id] || []).slice();
    while (stack.length) {
      var id = stack.pop();
      if (seen[id]) continue;
      seen[id] = true;
      nodeById[id].level += n.level;
      (kids[id] || []).forEach(function(c) { stack.push(c); });
    }
  });
  treeEdges.forEach(function(e) { edges.push(treeEdge(e.from, e.to)); });

  // 親の無い island / repo（未登録 repo や Other を含む）を全て root にぶら下げる。
  // Why: 左右バランス配置が root 起点の部分木分割で成り立つため、孤立したトップレベルを作らない
  if (nodes.length === 0) return null;
  nodes.push({ id: ROOT_ID, label: 'devctx', level: 0, _type: 'root' });
  treeNodes.forEach(function(n) {
    if (!parentOf[n.id]) edges.push(treeEdge(ROOT_ID, n.id));
  });
  return ROOT_ID;
}

// 純関数（DOM / vis に依存しない）。

// ノードを編集の種別に分類する。session / more / DAG ノード / ungrouped の repo は null（編集対象外）。
function editableKind(node) {
  if (!node) return null;
  if (node._type === 'root') return 'root';
  if (node._type === 'island') return 'island';
  if (node._type === 'task') return 'task';
  if (node._type === 'project' && node.id !== repoNodeId('')) return 'repo';
  return null;
}

// ノード ID（= ref）の親 ref。トップレベルは ''。islandsData は /api/islands（dangling 解決済み）。
function parentRefOf(id, islandsData) {
  var i;
  if (id.indexOf(ISLAND_PREFIX) === 0) {
    for (i = 0; i < islandsData.islands.length; i++) {
      if (islandNodeId(islandsData.islands[i].id) === id) return islandsData.islands[i].parent || '';
    }
  } else if (id.indexOf(REPO_PREFIX) === 0) {
    for (i = 0; i < islandsData.repos.length; i++) {
      if (repoNodeId(islandsData.repos[i].root) === id) return islandsData.repos[i].parent || '';
    }
  }
  return '';
}

// Enter で兄弟を足すときの親 ref。root は兄弟を持たないので "root の下"（'' = トップレベル）に足す。
function siblingParentRef(id, kind, islandsData) {
  return kind === 'root' ? '' : parentRefOf(id, islandsData);
}

// 削除確認で見せる子の ref（island → repo の順）。server は同じ集合を children として照合する。
function childrenRefsOf(ref, islandsData) {
  var out = [];
  islandsData.islands.forEach(function(is) { if (is.parent === ref) out.push(islandNodeId(is.id)); });
  islandsData.repos.forEach(function(r) { if (r.parent === ref) out.push(repoNodeId(r.root)); });
  return out;
}

// ref の子孫（island / repo を混ぜて辿る）の集合 {ref: true}。ref 自身は含まない。手編集の輪でも止まるよう visited で守る。
// Why: ドラッグで親を付け替えるとき、自分の子孫の下へは付けられない（循環）ので候補から外す。server も検証するが、
// 候補に出さないことで、落とせない場所へ落とす操作自体を無くす。
function descendantsOf(ref, islandsData) {
  var out = {};
  var stack = [ref];
  while (stack.length) {
    var cur = stack.pop();
    childrenRefsOf(cur, islandsData).forEach(function(c) {
      if (c !== ref && !out[c]) { out[c] = true; stack.push(c); }
    });
  }
  return out;
}
