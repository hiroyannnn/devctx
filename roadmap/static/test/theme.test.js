'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const { loadApi } = require('./load');

const api = loadApi();

test('themeKindOf: _type から色の種別を決める', () => {
  assert.equal(api.themeKindOf({ _type: 'root' }), 'root');
  assert.equal(api.themeKindOf({ _type: 'island' }), 'island');
  assert.equal(api.themeKindOf({ _type: 'project', _data: { sessions: [{}] } }), 'repo');
  assert.equal(api.themeKindOf({ _type: 'project', _data: { sessions: [] } }), 'repoDim', 'アクティブな session が無い repo は dim');
  assert.equal(api.themeKindOf({ _type: 'task', _data: { done: false } }), 'task');
  assert.equal(api.themeKindOf({ _type: 'task', _data: { done: true } }), 'taskDone');
  assert.equal(api.themeKindOf({ _type: 'session' }), null, 'session は builder の意味色を使う');
});

test('applyMindmapTheme: 共通の形を与えるが、builder が与えた枠の太さ・影は潰さない', () => {
  const nodes = [
    { id: 'a', _type: 'island' },
    { id: 's', _type: 'session', borderWidth: 3, shadow: { enabled: true, color: 'blue' } },
  ];
  api.applyMindmapTheme(nodes);
  assert.equal(nodes[0].shape, 'box');
  assert.equal(nodes[0].borderWidth, 1);
  assert.deepEqual(nodes[0].color, { background: '#E0E0E0', border: '#707070' });
  assert.equal(nodes[1].borderWidth, 3, '待ちの強調は残す');
  assert.equal(nodes[1].shadow.color, 'blue', '選択中のグローは残す');
  assert.equal(nodes[1].color, undefined, 'session の色は builder 任せ');
});
