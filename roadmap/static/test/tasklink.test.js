'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { loadApi } = require('./load');
const { R, I } = require('./fixtures');

const api = loadApi();

//   hr ─ hiring ─ t1 (未完了) / t2 (完了)
//      └ t3 (未完了)
//   repo:/r/a ─ t4 (未完了)
const tree = {
  islands: [
    { id: 'hr', name: 'HR', parent: '' },
    { id: 'hiring', name: '採用', parent: I('hr') },
    { id: 't1', name: '面接の質問を作る', kind: 'task', parent: I('hiring') },
    { id: 't2', name: '求人票を直す', kind: 'task', done: true, parent: I('hiring') },
    { id: 't3', name: 'A 案件', kind: 'task', parent: I('hr') },
    { id: 't4', name: 'API', kind: 'task', parent: R('/r/a') },
  ],
  repos: [],
};

test('linkedTaskNodeId: 実在するタスクだけ。島・消えたタスク・不正な ref は null', () => {
  assert.equal(api.linkedTaskNodeId({ task_ref: I('t1') }, tree), I('t1'));
  assert.equal(api.linkedTaskNodeId({ task_ref: I('hiring') }, tree), null, 'テーマ island はタスクではない');
  assert.equal(api.linkedTaskNodeId({ task_ref: I('t9') }, tree), null);
  assert.equal(api.linkedTaskNodeId({ task_ref: R('/r/a') }, tree), null);
  assert.equal(api.linkedTaskNodeId({}, tree), null);
  assert.equal(api.linkedTaskNodeId(null, tree), null);
});

test('sessionTaskLine: 単一プロジェクト・インスペクタ用は「▸ タスク名」、All Projects で下に付く場合は出さない', () => {
  const s = { task_ref: I('t1'), task_label: '面接の質問を作る' };
  assert.equal(api.sessionTaskLine(s, false, tree), '▸ 面接の質問を作る');
  assert.equal(api.sessionTaskLine(s, true, tree), '', 'All Projects ではタスクの下に付くので重ねない');
  assert.equal(api.sessionTaskLine({}, false, tree), '');
});

test('sessionTaskLine: 消えたタスクは All Projects でも「削除済み tN」を出す（repo の下に残るため）', () => {
  const s = { task_ref: I('t9'), task_label: '削除済み t9' };
  assert.equal(api.sessionTaskLine(s, true, tree), '▸ 削除済み t9');
  assert.equal(api.sessionTaskLine(s, false, tree), '▸ 削除済み t9');
});

test('sessionTaskName: 紐付け無しは空、label があればそれ、無ければ islands から', () => {
  assert.equal(api.sessionTaskName({}, tree), '');
  assert.equal(api.sessionTaskName({ task_ref: I('t1'), task_label: 'X' }, tree), 'X');
  assert.equal(api.sessionTaskName({ task_ref: I('t2') }, tree), '求人票を直す');
  assert.equal(api.sessionTaskName({ task_ref: I('t9') }, tree), '削除済み t9');
});

test('sessionTaskLine: server が label を付けなかったときは islands から補う', () => {
  assert.equal(api.sessionTaskLine({ task_ref: I('t1') }, false, tree), '▸ 面接の質問を作る');
  assert.equal(api.sessionTaskLine({ task_ref: I('t9') }, true, tree), '▸ 削除済み t9');
});

test('taskOptions: 未完了が先、完了は後ろ。親の道筋（島 / repo 名）を添える', () => {
  const opts = api.taskOptions(tree);
  assert.deepEqual(opts.map((o) => o.ref), [I('t3'), I('t1'), I('t4'), I('t2')], '道筋の昇順（HR, HR / 採用, a）');
  const byRef = Object.fromEntries(opts.map((o) => [o.ref, o]));
  assert.equal(byRef[I('t1')].path, 'HR / 採用');
  assert.equal(byRef[I('t3')].path, 'HR');
  assert.equal(byRef[I('t4')].path, 'a', 'repo は basename');
  assert.equal(byRef[I('t2')].done, true);
});

test('taskOptions: 親が輪になっていても止まる。タスクが無ければ空', () => {
  const loop = { islands: [
    { id: 'a', name: 'A', parent: I('b') }, { id: 'b', name: 'B', parent: I('a') },
    { id: 't', name: 'T', kind: 'task', parent: I('a') },
  ], repos: [] };
  assert.equal(api.taskOptions(loop).length, 1);
  assert.deepEqual(api.taskOptions({ islands: [], repos: [] }), []);
});

test('taskMenuItems: 完了は ✓ を付けて後ろ、path は括弧書き。空なら無効の案内', () => {
  const items = api.taskMenuItems(api.taskOptions(tree));
  assert.deepEqual(items.map((i) => i.label), ['A 案件  (HR)', '面接の質問を作る  (HR / 採用)', 'API  (a)', '✓ 求人票を直す  (HR / 採用)']);
  assert.equal(items[0].action, 'pickTask');
  assert.equal(items[0].ref, I('t3'));
  assert.deepEqual(api.taskMenuItems([]), [{ action: 'none', label: 'タスクがありません', disabled: true }]);
});

test('sessionMenuItems: 「タスクに付ける…」は常に、「タスクから外す」はリンク済みのときだけ', () => {
  assert.deepEqual(api.sessionMenuItems({}).map((i) => i.label), ['タスクに付ける…']);
  assert.deepEqual(api.sessionMenuItems({ task_ref: I('t1') }).map((i) => i.label), ['タスクに付ける…', 'タスクから外す']);
  assert.deepEqual(api.sessionMenuItems({ task_ref: I('t9') }).map((i) => i.action), ['linkTask', 'unlinkTask'], '消えたタスクへの紐付けも外せる');
});

// islandOverlay: セッションをタスクの下に付け替え、深さを task + 1 から数え直す
function overlayFixture() {
  const sess = (name, ref) => ({ id: 'session:' + name, label: name, level: 1, _type: 'session', _data: { name, task_ref: ref } });
  const nodes = [
    { id: R('/r/a'), label: 'a', level: 0, _type: 'project', _data: { sessions: [] } },
    sess('linked', I('t4')),
    sess('dangling', I('t9')),
    sess('plain', undefined),
    // DAG: linked セッションの下のタスクノード（builder は repo = 0 起点で level を振る）
    { id: 'dag:linked:n1', label: 'n1', level: 2, _type: 'dag' },
    { id: 'dag:linked:n2', label: 'n2', level: 3, _type: 'dag' },
  ];
  const e = (from, to) => ({ from, to });
  const edges = [
    e(R('/r/a'), 'session:linked'), e(R('/r/a'), 'session:dangling'), e(R('/r/a'), 'session:plain'),
    e('session:linked', 'dag:linked:n1'), e('dag:linked:n1', 'dag:linked:n2'),
  ];
  return { nodes, edges };
}

test('islandOverlay: タスクに紐付いたセッションは repo ではなくタスクの下に付き、部分木の深さが task + 1 になる', () => {
  const { nodes, edges } = overlayFixture();
  const data = { islands: [{ id: 't4', name: 'API', kind: 'task', parent: R('/r/a') }], repos: [] };
  api.islandOverlay(nodes, edges, data);
  const level = Object.fromEntries(nodes.map((n) => [n.id, n.level]));
  // repo(1) → task(2) → linked(3) → n1(4) → n2(5)
  assert.equal(level[R('/r/a')], 1);
  assert.equal(level[I('t4')], 2);
  assert.equal(level['session:linked'], 3);
  assert.equal(level['dag:linked:n1'], 4);
  assert.equal(level['dag:linked:n2'], 5);
  // 紐付けの無い・タスクが消えたセッションは repo の下のまま（repo(1) + 1）
  assert.equal(level['session:plain'], 2);
  assert.equal(level['session:dangling'], 2);
  const pairs = edges.map((e) => e.from + '>' + e.to);
  assert.ok(pairs.includes(I('t4') + '>session:linked'));
  assert.ok(!pairs.includes(R('/r/a') + '>session:linked'), 'repo → session の辺は task → session に置き換わる');
  assert.ok(pairs.includes(R('/r/a') + '>session:plain'));
  assert.ok(pairs.includes(R('/r/a') + '>session:dangling'));
});

test('islandOverlay: タスクが島の下にあり深くても、セッションはそのタスクの深さから数える', () => {
  const { nodes, edges } = overlayFixture();
  const data = {
    islands: [{ id: 'hr', name: 'HR', parent: '' }, { id: 't4', name: 'API', kind: 'task', parent: I('hr') }],
    repos: [{ root: '/r/a', parent: I('hr') }],
  };
  api.islandOverlay(nodes, edges, data);
  const level = Object.fromEntries(nodes.map((n) => [n.id, n.level]));
  // hr(1) → task(2) → linked(3) → n1(4) → n2(5)。repo(2) 側の plain は 3
  assert.equal(level[I('t4')], 2);
  assert.equal(level['session:linked'], 3);
  assert.equal(level['dag:linked:n2'], 5);
  assert.equal(level['session:plain'], 3);
});

test('islandOverlay: 紐付いたセッションは root へ直接ぶら下がらない', () => {
  const { nodes, edges } = overlayFixture();
  api.islandOverlay(nodes, edges, { islands: [{ id: 't4', name: 'API', kind: 'task', parent: R('/r/a') }], repos: [] });
  assert.ok(!edges.some((e) => e.from === 'root' && e.to === 'session:linked'));
});

// DAG のタスクノードの _type は、島のタスク（'task'）と別にする。同じだと overlay が DAG ノードを島のタスクとして
// 再配置し、root にぶら下げてしまう。
test('islandOverlay: DAG ノードは島の木に入れず、root にもぶら下げない', () => {
  const { nodes, edges } = overlayFixture();
  api.islandOverlay(nodes, edges, { islands: [], repos: [] });
  assert.ok(!edges.some((e) => e.from === 'root' && e.to.startsWith('dag:')));
  assert.equal(nodes.find((n) => n.id === 'dag:linked:n1').level, 3);
});
