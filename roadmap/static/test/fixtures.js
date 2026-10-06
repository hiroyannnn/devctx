'use strict';
// island / repo の木（/api/islands の形）。テスト間で共有する。
//   hr ─ hiring ─ repo:/r/x ─ deep
//      └ repo:/r/a ─ m3
//   lone（単独）/ repo:/r/free（単独）/ l1 ⇄ l2（手編集で輪になった想定）
const R = (root) => 'repo:' + root;
const I = (id) => 'island:' + id;

const islands = {
  islands: [
    { id: 'hr', name: 'HR', parent: '' },
    { id: 'hiring', name: 'H', parent: I('hr') },
    { id: 'deep', name: 'D', parent: R('/r/x') },
    { id: 'm3', name: 'M3', parent: R('/r/a') },
    { id: 'lone', name: 'L', parent: '' },
    { id: 'l1', name: 'L1', parent: I('l2') },
    { id: 'l2', name: 'L2', parent: I('l1') },
  ],
  repos: [
    { root: '/r/x', parent: I('hiring') },
    { root: '/r/a', parent: I('hr') },
    { root: '/r/free', parent: '' },
  ],
};

module.exports = { R, I, islands };
