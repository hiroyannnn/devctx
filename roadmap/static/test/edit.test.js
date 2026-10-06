'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { loadApi } = require('./load');
const { R, I, islands } = require('./fixtures');

const api = loadApi();

const labels = (kind, hasParent, done) => api.contextMenuItems(kind, hasParent, done).map((i) => i.label);

test('contextMenuItems: root は島の追加だけ', () => {
  assert.deepEqual(labels('root', false), ['島を追加']);
  assert.deepEqual(labels('root', true), ['島を追加']);
});

test('contextMenuItems: island。「親から外す」は親があるときだけ', () => {
  assert.deepEqual(labels('island', true), ['子の島を追加', 'タスクを追加', 'repo を付ける…', '名前を変更', '親から外す', '削除']);
  assert.deepEqual(labels('island', false), ['子の島を追加', 'タスクを追加', 'repo を付ける…', '名前を変更', '削除']);
  assert.equal(api.contextMenuItems('island', true).find((i) => i.action === 'delete').danger, true);
});

test('contextMenuItems: repo は削除・名前変更・repo を付けるが出ない', () => {
  assert.deepEqual(labels('repo', true), ['子の島を追加', 'タスクを追加', '親から外す']);
  assert.deepEqual(labels('repo', false), ['子の島を追加', 'タスクを追加']);
});

test('contextMenuItems: タスクは葉。子・repo・「親から外す」は無く、完了の切り替えが状態で変わる', () => {
  assert.deepEqual(labels('task', true, false), ['プロンプトとしてコピー', '完了にする', '名前を変更', '削除']);
  assert.deepEqual(labels('task', true, true), ['プロンプトとしてコピー', '未完了に戻す', '名前を変更', '削除']);
  assert.deepEqual(labels('task', false, false), labels('task', true, false), 'タスクは親の有無で変わらない');
});

test('repoMenuItems: 空なら無効な案内。同名 repo だけパスを添える', () => {
  assert.deepEqual(api.repoMenuItems([]), [{ action: 'none', label: '付けられる repo がありません', disabled: true }]);
  const items = api.repoMenuItems([
    { root: '/p/app', label: 'app' }, { root: '/q/app', label: 'app' }, { root: '/p/web', label: 'web' },
  ]);
  assert.deepEqual(items.map((i) => i.label), ['app  (/p/app)', 'app  (/q/app)', 'web']);
  assert.equal(items[0].root, '/p/app');
  assert.equal(items[0].action, 'pickRepo');
});

test('keyAction: Tab は子、Enter は兄弟。タスクは子を持てないので Tab は何もしない', () => {
  for (const kind of ['root', 'island', 'repo']) {
    assert.equal(api.keyAction('Tab', kind), 'addChild', kind);
    assert.equal(api.keyAction('Enter', kind), 'addSibling', kind);
  }
  assert.equal(api.keyAction('Tab', 'task'), null);
  assert.equal(api.keyAction('Enter', 'task'), 'addSibling');
});

test('keyAction: 名前変更・削除は island とタスクだけ。Backspace では消さない', () => {
  for (const kind of ['island', 'task']) {
    assert.equal(api.keyAction('F2', kind), 'rename', kind);
    assert.equal(api.keyAction(' ', kind), 'rename', kind);
    assert.equal(api.keyAction('Delete', kind), 'delete', kind);
    assert.equal(api.keyAction('Backspace', kind), null, kind);
  }
  for (const kind of ['root', 'repo']) {
    for (const key of ['F2', ' ', 'Delete', 'Backspace']) assert.equal(api.keyAction(key, kind), null, kind + ' ' + key);
  }
});

test('keyAction: 種別が無い・知らないキーは null', () => {
  assert.equal(api.keyAction('Tab', null), null);
  assert.equal(api.keyAction('a', 'island'), null);
  assert.equal(api.keyAction('ArrowLeft', 'island'), null);
});

test('selectAfterOp: 操作ごとに再描画後に選択するノード', () => {
  api.setGlobal('cachedIslands', islands);
  assert.equal(api.selectAfterOp({ op: 'add' }), null, 'add は応答の ref で決まる');
  assert.equal(api.selectAfterOp({ op: 'rename', ref: I('hr') }), I('hr'));
  assert.equal(api.selectAfterOp({ op: 'done', ref: I('t') }), I('t'));
  assert.equal(api.selectAfterOp({ op: 'attach', child: I('hiring'), parent: I('m3') }), I('hiring'));
  assert.equal(api.selectAfterOp({ op: 'detach', child: I('hiring') }), I('hiring'));
});

test('selectAfterOp: remove は親を選ぶ。トップレベルなら無し', () => {
  api.setGlobal('cachedIslands', islands);
  assert.equal(api.selectAfterOp({ op: 'remove', ref: I('hiring') }), I('hr'));
  assert.equal(api.selectAfterOp({ op: 'remove', ref: I('hr') }), null);
});

test('deleteConfirmModel: 子がいなければ単純な確認、いれば付け替えの説明と一覧', () => {
  assert.deepEqual(api.deleteConfirmModel('Lone', []), {
    title: '「Lone」を削除', lead: 'この島を削除します。', items: [], okLabel: '削除',
  });
  assert.equal(api.deleteConfirmModel('T', [], 'タスク').lead, 'この' + 'タスク' + 'を削除します。');
  const m = api.deleteConfirmModel('HR', ['A', 'B']);
  assert.deepEqual(m.items, ['A', 'B']);
  assert.equal(m.okLabel, '子を親へ付け替えて削除');
  assert.ok(m.lead.includes('2'));
});
