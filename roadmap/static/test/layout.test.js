'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { loadApi } = require('./load');

const api = loadApi();

// vis の hierarchical LR を模す: x = depth * 200、y は DFS 順に 50 刻み
function simulate(edges, rootId, labels) {
  const kids = {};
  edges.forEach((e) => (kids[e.from] = kids[e.from] || []).push(e.to));
  const out = [];
  let row = 0;
  (function walk(id, depth) {
    out.push({ id, label: labels[id] || id, x: depth * 200, y: row++ * 50, h: 40 });
    (kids[id] || []).forEach((c) => walk(c, depth + 1));
  })(rootId, 0);
  return out;
}

// root の下に、名前順 a, b, c, d の部分木（大きさ 3, 1, 1, 2）を置く
const edges = [
  ['root', 'a'], ['a', 'a1'], ['a', 'a2'],
  ['root', 'b'],
  ['root', 'c'],
  ['root', 'd'], ['d', 'd1'],
].map(([from, to]) => ({ from, to }));

function subtreeExtent(layout, head, members) {
  const ys = members.map((id) => layout.positions[id].y);
  return [Math.min(...ys) - 20, Math.max(...ys) + 20];
}

test('balancedLayout: 根が無ければ null', () => {
  assert.equal(api.balancedLayout([{ id: 'x', label: 'x', x: 0, y: 0 }], [], 'root'), null);
});

test('balancedLayout: 左は root の x で鏡写し、右は x を保つ', () => {
  const nodes = simulate(edges, 'root', {});
  const L = api.balancedLayout(nodes, edges, 'root');
  const rootX = L.positions.root.x;
  for (const n of nodes) {
    if (n.id === 'root') continue;
    if (L.side[n.id] === 'left') assert.equal(L.positions[n.id].x, 2 * rootX - n.x, n.id);
    else assert.equal(L.positions[n.id].x, n.x, n.id);
  }
});

test('balancedLayout: 新規の部分木は大きい順に軽い側（同数なら右）へ振り分ける', () => {
  const L = api.balancedLayout(simulate(edges, 'root', {}), edges, 'root');
  // 大きい順: a(3) → 右、d(2) → 左、b(1) → 左(2 < 3)、c(1) → 右(2 == 2 は右)
  assert.deepEqual(L.headSides, { a: 'right', d: 'left', b: 'left', c: 'right' });
});

test('balancedLayout: 各側の部分木は縦に重ならず間隔 LAYOUT_GAP 以上で、root の高さを中心に積む', () => {
  const L = api.balancedLayout(simulate(edges, 'root', {}), edges, 'root');
  const members = { a: ['a', 'a1', 'a2'], b: ['b'], c: ['c'], d: ['d', 'd1'] };
  const rootY = L.positions.root.y;
  for (const side of ['left', 'right']) {
    const ext = Object.keys(members)
      .filter((h) => L.headSides[h] === side)
      .map((h) => subtreeExtent(L, h, members[h]))
      .sort((p, q) => p[0] - q[0]);
    for (let i = 1; i < ext.length; i++) assert.ok(ext[i][0] >= ext[i - 1][1] + 24 - 1e-6, side + ' が重なる');
    const mid = (ext[0][0] + ext[ext.length - 1][1]) / 2;
    assert.ok(Math.abs(mid - rootY) < 1e-6, side + ' が root の高さに中心を持たない');
  }
});

test('balancedLayout: 既存の部分木は前回の側に留まり（粘着）、新しい部分木だけが軽い側へ入る', () => {
  const nodes = simulate(edges, 'root', {});
  // a を左へ、b・c・d を右へ固定。グリーディなら a は右に行くはず
  const prev = { a: 'left', b: 'right', c: 'right', d: 'right' };
  const L = api.balancedLayout(nodes, edges, 'root', prev);
  assert.deepEqual(L.headSides, prev);

  // 新しい島 e が増えたら、軽い側（左 3 vs 右 4）へ入り、他は動かない
  const edges2 = edges.concat([{ from: 'root', to: 'e' }]);
  const L2 = api.balancedLayout(simulate(edges2, 'root', {}), edges2, 'root', prev);
  assert.deepEqual(L2.headSides, Object.assign({}, prev, { e: 'left' }));
});

test('balancedLayout: 入力の並びを変えても結果は同じ（決定性）', () => {
  const nodes = simulate(edges, 'root', {});
  const shuffledEdges = edges.slice().reverse();
  const shuffledNodes = nodes.slice().reverse();
  assert.deepEqual(
    api.balancedLayout(shuffledNodes, shuffledEdges, 'root'),
    api.balancedLayout(nodes, edges, 'root'),
  );
});

test('balancedLayout: 同じラベルでも id で順序が決まる（揺れない）', () => {
  const same = [{ from: 'root', to: 'p' }, { from: 'root', to: 'q' }];
  const nodes = simulate(same, 'root', { p: 'same', q: 'same' });
  const L1 = api.balancedLayout(nodes, same, 'root');
  const L2 = api.balancedLayout(nodes.slice().reverse(), same.slice().reverse(), 'root');
  assert.deepEqual(L1, L2);
});
