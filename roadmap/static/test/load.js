'use strict';
// static/*.js をブラウザと同じ形（グローバルを共有する素の script を index.html の順に実行）で読み、
// 各ファイル末尾の module.exports を 1 つの api にまとめて返す。
// Why runInThisContext: ファイルをまたいで var / function を共有する前提で、require で個別に読むと解決できない。
// Why not 別 context（createContext）: 配列・オブジェクトの prototype が違い、assert.deepStrictEqual が構造一致でも落ちる。
// テストは 1 ファイル 1 プロセスなので、global への展開は他のテストファイルに漏れない。
// Why app.js を読まない: 初期化（refresh / setInterval / localStorage）を含み、純関数のテスト対象も持たないため。
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const STATIC_DIR = path.join(__dirname, '..');
// index.html の <script src> の順（app.js 以外）
const FILES = ['theme.js', 'layout.js', 'tree.js', 'graphdata.js', 'edit.js', 'drag.js'];

// edit.js が読み込み時に graph-canvas へ listener を付けるので、DOM の最小の代役を置く
function stubElement() {
  return { addEventListener() {}, style: {} };
}

function loadApi() {
  globalThis.document = { getElementById: stubElement, addEventListener() {} };
  globalThis.window = { addEventListener() {} };
  const api = {};
  for (const file of FILES) {
    globalThis.module = { exports: {} };
    vm.runInThisContext(fs.readFileSync(path.join(STATIC_DIR, file), 'utf8'), { filename: file });
    Object.assign(api, globalThis.module.exports);
  }
  delete globalThis.module;
  // グローバルの var（cachedIslands / graphNodes など）を、テストから差し替える口
  api.setGlobal = (name, value) => {
    globalThis[name] = value;
  };
  return api;
}

module.exports = { loadApi };
