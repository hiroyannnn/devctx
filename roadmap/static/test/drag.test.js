'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { loadApi } = require('./load');
const { R, I, islands } = require('./fixtures');

const api = loadApi();

const nodes = [
  { id: 'root', _type: 'root' },
  ...islands.islands.map((i) => ({ id: I(i.id), _type: 'island' })),
  ...['/r/x', '/r/a', '/r/free'].map((r) => ({ id: R(r), _type: 'project' })),
  { id: I('task1'), _type: 'task' },
  { id: 'session:s', _type: 'session' },
  { id: 'more:x', _type: 'more' },
  { id: 'repo:__ungrouped__', _type: 'project' },
];

test('dropCandidateIds: ドラッグ中のノードと子孫を除き、root を含む', () => {
  const c = api.dropCandidateIds(nodes, I('hiring'), islands);
  for (const want of ['root', I('hr'), I('lone'), R('/r/free'), R('/r/a')]) assert.ok(c.includes(want), want);
  for (const gone of [I('hiring'), R('/r/x'), I('deep')]) assert.ok(!c.includes(gone), gone + ' は自身か子孫');
});

test('dropCandidateIds: session / more / ungrouped / タスクは落とし先にならない', () => {
  const c = api.dropCandidateIds(nodes, I('hiring'), islands);
  for (const gone of ['session:s', 'more:x', 'repo:__ungrouped__', I('task1')]) assert.ok(!c.includes(gone), gone);
});

test('dropCandidateIds: repo をドラッグしても、その子の島は候補から外れる', () => {
  assert.ok(!api.dropCandidateIds(nodes, R('/r/a'), islands).includes(I('m3')));
});

test('dropCandidateIds: タスクをドラッグすると root も候補から外れる（root へは無操作のため）', () => {
  const c = api.dropCandidateIds(nodes, I('task1'), islands);
  assert.ok(!c.includes('root'));
  assert.ok(c.includes(I('lone')));
});

test('dropOpFor: root へは親から外す。既にトップレベルなら無操作', () => {
  const byId = Object.fromEntries(nodes.map((n) => [n.id, n]));
  api.setGlobal('graphNodes', { get: (id) => byId[id] });
  assert.deepEqual(api.dropOpFor(I('hiring'), 'root', islands), { op: 'detach', child: I('hiring') });
  assert.equal(api.dropOpFor(I('lone'), 'root', islands), null, '既にトップレベル');
});

test('dropOpFor: タスクを root へ落としても無操作（親が要る）', () => {
  const task = { id: I('t'), _type: 'task' };
  api.setGlobal('graphNodes', { get: (id) => (id === I('t') ? task : undefined) });
  const data = { islands: [{ id: 't', name: 'T', kind: 'task', parent: I('hr') }], repos: [] };
  assert.equal(api.dropOpFor(I('t'), 'root', data), null);
});

test('dropOpFor: island / repo へは付ける。既にそこが親なら無操作', () => {
  api.setGlobal('graphNodes', { get: () => undefined });
  assert.equal(api.dropOpFor(I('hiring'), I('hr'), islands), null, '既に親');
  assert.equal(api.dropOpFor(R('/r/a'), I('hr'), islands), null, 'repo も同様');
  assert.deepEqual(api.dropOpFor(I('hiring'), I('lone'), islands), { op: 'attach', child: I('hiring'), parent: I('lone') });
  assert.deepEqual(api.dropOpFor(R('/r/free'), I('lone'), islands), { op: 'attach', child: R('/r/free'), parent: I('lone') });
});

test('hitTarget: 重なっていれば面積の小さい（内側の）矩形。境界は含み、外れは null', () => {
  const boxes = [
    { id: 'big', left: 0, right: 100, top: 0, bottom: 100 },
    { id: 'small', left: 10, right: 30, top: 10, bottom: 30 },
  ];
  assert.equal(api.hitTarget(boxes, { x: 15, y: 15 }), 'small');
  assert.equal(api.hitTarget(boxes, { x: 50, y: 50 }), 'big');
  assert.equal(api.hitTarget(boxes, { x: 100, y: 100 }), 'big', '境界を含む');
  assert.equal(api.hitTarget(boxes, { x: 200, y: 5 }), null);
  assert.equal(api.hitTarget([], { x: 0, y: 0 }), null);
});

test('attachableRepos: この島の子の repo と、付けると循環になる祖先の repo を除く', () => {
  const known = [
    { root: '/r/x', label: 'x' }, { root: '/r/a', label: 'a' },
    { root: '/r/free', label: 'free' }, { root: '/r/zzz', label: 'zzz' },
  ];
  const roots = (island) => api.attachableRepos(known, I(island), islands).map((r) => r.root);
  assert.deepEqual(roots('hiring'), ['/r/a', '/r/free', '/r/zzz'], 'x は hiring の子');
  assert.deepEqual(roots('deep'), ['/r/a', '/r/free', '/r/zzz'], 'x は deep の祖先');
  assert.deepEqual(roots('m3'), ['/r/x', '/r/free', '/r/zzz'], 'a は m3 の祖先');
  assert.deepEqual(roots('lone'), ['/r/x', '/r/a', '/r/free', '/r/zzz']);
  assert.equal(roots('l1').length, 4, '輪があっても止まる');
});
