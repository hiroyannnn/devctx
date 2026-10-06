'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { loadApi } = require('./load');
const { R, I, islands } = require('./fixtures');

const api = loadApi();

const keys = (obj) => Object.keys(obj).sort();

test('ノード ID は /api/islands の ref と同じ形', () => {
  assert.equal(api.repoNodeId('/r/x'), 'repo:/r/x');
  assert.equal(api.repoNodeId(''), 'repo:__ungrouped__');
  assert.equal(api.islandNodeId('hr'), 'island:hr');
  assert.equal(api.repoBaseName('/a/b/c/'), 'c');
  assert.equal(api.repoBaseName(''), '');
});

test('normalizeIslands: 欠けていれば空配列', () => {
  assert.deepEqual(api.normalizeIslands(null), { islands: [], repos: [] });
  assert.deepEqual(api.normalizeIslands({ islands: [{ id: 'a' }] }), { islands: [{ id: 'a' }], repos: [] });
});

test('taskLabel / taskMarker / taskPromptText', () => {
  assert.equal(api.taskLabel({ name: 'T', done: false }), '☐ T');
  assert.equal(api.taskLabel({ name: 'T', done: true }), '✓ T');
  assert.equal(api.taskMarker('abc'), '[devctx:task:abc]');
  // marker は独立した最終行（model.TaskMarker と同じ書式）
  assert.equal(api.taskPromptText({ id: 'abc', name: 'T' }), 'T\n\n[devctx:task:abc]');
});

test('editableKind: 編集対象の種別。session / more / ungrouped は対象外', () => {
  assert.equal(api.editableKind({ _type: 'root', id: 'root' }), 'root');
  assert.equal(api.editableKind({ _type: 'island', id: I('hr') }), 'island');
  assert.equal(api.editableKind({ _type: 'task', id: I('t') }), 'task');
  assert.equal(api.editableKind({ _type: 'project', id: R('/r/dev') }), 'repo');
  assert.equal(api.editableKind({ _type: 'project', id: 'repo:__ungrouped__' }), null);
  assert.equal(api.editableKind({ _type: 'session', id: 'session:a' }), null);
  assert.equal(api.editableKind({ _type: 'more' }), null);
  assert.equal(api.editableKind(undefined), null);
});

test('parentRefOf / siblingParentRef / childrenRefsOf', () => {
  assert.equal(api.parentRefOf(I('hiring'), islands), I('hr'));
  assert.equal(api.parentRefOf(I('hr'), islands), '');
  assert.equal(api.parentRefOf(R('/r/a'), islands), I('hr'));
  assert.equal(api.parentRefOf(R('/r/free'), islands), '');
  assert.equal(api.parentRefOf(R('/r/none'), islands), '', '知らない ref はトップレベル扱い');

  assert.equal(api.siblingParentRef('root', 'root', islands), '', 'root の兄弟は root の下（トップレベル）');
  assert.equal(api.siblingParentRef(I('hiring'), 'island', islands), I('hr'));

  // island → repo の順
  assert.deepEqual(api.childrenRefsOf(I('hr'), islands), [I('hiring'), R('/r/a')]);
  assert.deepEqual(api.childrenRefsOf(R('/r/x'), islands), [I('deep')]);
  assert.deepEqual(api.childrenRefsOf(I('lone'), islands), []);
});

test('descendantsOf: island と repo の混在した鎖を辿り、自身は含まない', () => {
  const d = (ref) => keys(api.descendantsOf(ref, islands));
  assert.deepEqual(d(I('hr')), [I('deep'), I('hiring'), I('m3'), R('/r/a'), R('/r/x')].sort());
  assert.deepEqual(d(R('/r/x')), [I('deep')]);
  assert.deepEqual(d(I('lone')), []);
  assert.deepEqual(d(R('/r/free')), []);
});

test('descendantsOf: 手編集で輪になっていても止まり、自身を含めない', () => {
  assert.deepEqual(keys(api.descendantsOf(I('l1'), islands)), [I('l2')]);
});

test('islandOverlay: island / repo を root の下の木にし、親の無いものは root にぶら下げる', () => {
  const nodes = [{ id: R('/r/a'), label: 'a', level: 0, _type: 'project', _data: { sessions: [{}] } }];
  const edges = [];
  const data = {
    islands: [{ id: 'hr', name: 'HR', parent: '' }, { id: 'm3', name: 'M3', parent: R('/r/a') }],
    repos: [{ root: '/r/a', parent: I('hr') }],
  };
  const rootId = api.islandOverlay(nodes, edges, data);
  assert.equal(rootId, 'root');
  const level = Object.fromEntries(nodes.map((n) => [n.id, n.level]));
  assert.deepEqual(level, { [R('/r/a')]: 2, [I('hr')]: 1, [I('m3')]: 3, root: 0 });
  const pairs = edges.map((e) => e.from + '>' + e.to).sort();
  assert.deepEqual(pairs, ['island:hr>repo:/r/a', 'repo:/r/a>island:m3', 'root>island:hr'].sort());
});

test('islandOverlay: タスクは task 種別、セッション無しの親 repo は薄いノードで補う', () => {
  const nodes = [];
  const edges = [];
  const data = {
    islands: [{ id: 't1', name: 'T', kind: 'task', done: true, parent: R('/r/ghost') }],
    repos: [],
  };
  api.islandOverlay(nodes, edges, data);
  const task = nodes.find((n) => n.id === I('t1'));
  assert.equal(task._type, 'task');
  assert.equal(task.label, '✓ T');
  const ghost = nodes.find((n) => n.id === R('/r/ghost'));
  assert.equal(ghost._type, 'project');
  assert.deepEqual(ghost._data.sessions, []);
});

test('islandOverlay: ノードが無ければ null', () => {
  assert.equal(api.islandOverlay([], [], { islands: [], repos: [] }), null);
});

test('computeNodeDepth: root を 1 とする最長経路。循環は打ち切る', () => {
  const nodes = [{ id: 'a' }, { id: 'b' }, { id: 'c' }];
  const dag = [{ from: 'a', to: 'b' }, { from: 'b', to: 'c' }, { from: 'a', to: 'c' }];
  assert.equal(api.computeNodeDepth('a', dag, nodes), 1);
  assert.equal(api.computeNodeDepth('c', dag, nodes), 3);
  const loop = [{ from: 'a', to: 'b' }, { from: 'b', to: 'a' }];
  assert.ok(Number.isFinite(api.computeNodeDepth('a', loop, nodes)));
});

test('connectOrphanNodes: session から辺で辿れないノードを不可視の辺で繋ぐ', () => {
  const edges = [{ from: 's', to: 'goal' }, { from: 'goal', to: 't1' }];
  const before = edges.length;
  api.connectOrphanNodes(edges, 0, 's', ['s', 'goal', 't1', 'orphan']);
  const added = edges.slice(before);
  assert.deepEqual(added.map((e) => e.to), ['orphan']);
  assert.equal(added[0].from, 's');
});
