// 編集 UI（右クリックメニュー・名前入力・削除確認・キー操作）。

// ── Island editing ──
// 編集は All Projects の Mind Map だけ（island は All Projects にしか描かれない）。変更は POST /api/islands/ops に送り、
// 結果は応答（更新後の木）か refresh() で server の木を取り直して描画に反映する（クライアントで木を書き換えない）。
// Why not 楽観更新: 木の検証（循環・存在）は server の model が正で、クライアントで再実装すると食い違うため。

// 右クリックメニューの項目。「親から外す」は親があるときだけ。
// タスクは親が要る（外せない）葉なので、子や repo は付けられず、「親から外す」も出さない。root の下にはタスクを置かない。
function contextMenuItems(kind, hasParent, done) {
  if (kind === 'root') return [{ action: 'addChild', label: '島を追加' }];
  if (kind === 'task') {
    return [
      { action: 'copyPrompt', label: 'プロンプトとしてコピー' },
      { action: 'toggleDone', label: done ? '未完了に戻す' : '完了にする' },
      { action: 'rename', label: '名前を変更' },
      { action: 'delete', label: '削除', danger: true }
    ];
  }
  var items = [{ action: 'addChild', label: '子の島を追加' }, { action: 'addTask', label: 'タスクを追加' }];
  if (kind === 'island') items.push({ action: 'attachRepo', label: 'repo を付ける…' });
  if (kind === 'island') items.push({ action: 'rename', label: '名前を変更' });
  if (hasParent) items.push({ action: 'detach', label: '親から外す' });
  if (kind === 'island') items.push({ action: 'delete', label: '削除', danger: true });
  return items;
}

// 「repo を付ける」のメニュー項目。同じ basename の repo が複数あるときだけパスを添えて見分ける。
function repoMenuItems(repos) {
  if (repos.length === 0) return [{ action: 'none', label: '付けられる repo がありません', disabled: true }];
  var counts = {};
  repos.forEach(function(r) { counts[r.label] = (counts[r.label] || 0) + 1; });
  return repos.map(function(r) {
    return { action: 'pickRepo', root: r.root, label: counts[r.label] > 1 ? r.label + '  (' + r.root + ')' : r.label, title: r.root };
  });
}

// キー → 編集操作。対応しないキーは null（preventDefault しない）。rename / delete は island とタスクのみ。
// タスクは子を持てないので Tab は何もせず、Enter は同じ親の下にタスクを足す（runEditAction が種別で分ける）。
function keyAction(key, kind) {
  if (!kind) return null;
  if (key === 'Tab') return kind === 'task' ? null : 'addChild';
  if (key === 'Enter') return 'addSibling';
  if (kind !== 'island' && kind !== 'task') return null;
  if (key === 'F2' || key === ' ') return 'rename';
  // Mac の「delete」キーは Backspace だが、打ち間違いで島を消さないよう Delete だけにする
  if (key === 'Delete') return 'delete';
  return null;
}

// 編集 UI（メニュー / 名前入力 / 削除確認）の状態。
// 編集中に届いた refresh は pendingData に預け、閉じてから反映する。
// Why: 5 秒ごとの再描画（graphNetwork の作り直し）で、開いている入力欄やメニューの足元からノードが消えるのを防ぐ。
var editState = { menu: null, input: null, confirm: null };
var pendingData = null;   // 編集中に届いた {mapData, graphData, islandsData}
var pendingSelect = null; // 操作成功後の再描画で選択するノード（add の戻り値）
var graphLaidOut = false; // 直近の描画の左右バランス配置が済んだか（視点の保存はこのとき以外は信用しない）
var carriedKeep = null;   // レイアウト前の描画が引き継ぐ予定の {scale, position, selected}
var graphRootId = null;   // 直近の描画の root ID。null なら編集不可（単一プロジェクト表示）

// 編集 UI（メニュー / 名前入力 / 削除確認）が開いているか。
function editingActive() {
  return !!(editState.menu || editState.input || editState.confirm);
}

// 更新の反映（= network の作り直し）を保留すべきか。編集 UI に加えて、ドラッグ中も保留する
// （作り直されるとドラッグが途切れるため）。ドラッグは UI を持たないので editingActive には含めない。
function holdRender() {
  return editingActive() || !!dragState;
}

function removeMenu() {
  if (editState.menu) { editState.menu.remove(); editState.menu = null; }
}
function removeConfirm() {
  if (editState.confirm) { editState.confirm.remove(); editState.confirm = null; }
}
function removeInput() {
  if (editState.input) {
    var el = editState.input;
    editState.input = null; // remove() が blur を起こすので、先に外しておく
    el.remove();
  }
}

// 編集 UI（メニュー / 名前入力 / 削除確認）の DOM を全部外す。保留分の反映やフォーカス移動はしない。
function closeEditUI() {
  removeMenu(); removeInput(); removeConfirm();
}

// 保留していた更新があり、編集 UI がすべて閉じていれば反映する（変化の有無は applyRefreshed が再判定する）。
function flushPending() {
  if (holdRender() || !pendingData) return;
  var p = pendingData;
  pendingData = null;
  applyRefreshed(p);
}

// ユーザー操作で編集 UI を閉じる。保留分を反映する。
// 閉じたあとはキー操作を続けられるようグラフにフォーカスを戻す。blur（別の要素をクリック）で閉じる場合は、
// クリックされた側のフォーカスを奪わないよう noFocus を渡す。
function endEditing(noFocus) {
  closeEditUI();
  var focusEl = graphFocusEl();
  if (!noFocus && focusEl && graphRootId) focusEl.focus({ preventScroll: true });
  flushPending();
}

// キー操作を受ける要素は vis の frame（tabindex=0 で、vis の keyboard もここに束縛される）。再描画のたびに作り直される。
function graphFocusEl() {
  var canvasEl = document.getElementById('graph-canvas');
  return canvasEl && (canvasEl.querySelector('.vis-network') || canvasEl);
}

// 編集 UI の外を押したときの閉じ方。UI はすぐ閉じるが、保留していた更新の反映（= network の作り直し）は
// そのクリックが済んでから行う。押下の直後に作り直すと、続くクリック / 右クリックが
// レイアウト前の network に当たり、描画完了後に古い選択へ戻されるため。
var deferredFlushTimer = null;
function closeEditUIOnOutsideMouseDown() {
  closeEditUI();
  function flushLater() {
    document.removeEventListener('click', flushLater, true);
    document.removeEventListener('contextmenu', flushLater, true);
    clearTimeout(deferredFlushTimer);
    deferredFlushTimer = null;
    setTimeout(flushPending, 0); // vis のクリック処理（選択）が先に済むようにする
  }
  document.addEventListener('click', flushLater, true);
  document.addEventListener('contextmenu', flushLater, true);
  // クリックにならなかった（ドラッグ等）場合の保険
  deferredFlushTimer = setTimeout(flushLater, 1000);
}

// 描画の作り直し（フィルタ・プロジェクト切り替え）で消える UI を捨てる。保留分も捨てる（次の refresh が取り直す）。
function cancelEditing() {
  cancelDrag();
  closeEditUI();
  pendingData = null;
  pendingSelect = null; // 古い操作の選択が、次の無関係な描画で効かないように
}

var toastTimer = null;
function showToast(message) {
  var el = document.getElementById('devctx-toast');
  if (!el) {
    el = document.createElement('div');
    el.id = 'devctx-toast';
    el.className = 'devctx-toast';
    el.setAttribute('role', 'alert');
    document.body.appendChild(el);
  }
  el.textContent = message;
  el.style.display = 'block';
  clearTimeout(toastTimer);
  toastTimer = setTimeout(function() { el.style.display = 'none'; }, 4000);
}

// POST /api/islands/ops。ネットワーク失敗も {ok:false} で返し、呼び出し側は 1 経路で扱う。
async function postIslandOp(body) {
  try {
    var res = await fetch('/api/islands/ops', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    });
    var data = null;
    try { data = await res.json(); } catch (e) { data = null; }
    return { ok: res.ok, status: res.status, data: data };
  } catch (e) {
    return { ok: false, status: 0, data: { error: 'network error' } };
  }
}

// 操作後に選択するノード（add 以外）。rename / attach / detach は対象、remove は親（トップレベルなら無し）。
// add は作ったノードが応答の ref で決まる。
function selectAfterOp(body) {
  if (body.op === 'rename' || body.op === 'done') return body.ref;
  if (body.op === 'attach' || body.op === 'detach') return body.child;
  if (body.op === 'remove') return parentRefOf(body.ref, cachedIslands) || null;
  return null;
}

// 操作を送り、成功なら応答の木（GET /api/islands と同形）で描画する。
// Why not 成功後に refresh(): 応答に更新後の木が入っているので、全体取得（roadmap-map / graph / islands の 3 本）を省ける。
// 失敗（409 等）は server の現状と食い違っているので、全体を取り直す。
async function runIslandOp(body) {
  var select = selectAfterOp(body); // remove の親は、消える前の木から引く
  var res = await postIslandOp(body);
  if (!res.ok || !res.data || !res.data.islands) {
    var err = res.data && res.data.error;
    if (res.status === 409 && err === 'children changed') {
      showToast('子が変わりました。もう一度確認してください');
    } else {
      showToast(err || ('HTTP ' + res.status));
    }
    refresh();
    return false;
  }
  pendingSelect = body.op === 'add' ? (res.data.ref || null) : select;
  // 進行中の取得（ポーリング）は、この操作より前の木を持っているので捨てさせる
  refreshSeq++;
  applyRefreshed({ mapData: cachedData, graphData: cachedGraphData, islandsData: normalizeIslands(res.data.islands) });
  // 編集 UI が開いていて保留になった場合だけ、選択を保留の反映まで残す
  if (!pendingData) pendingSelect = null;
  return true;
}

// ノードの DOM 座標（graph-canvas 内）の矩形
function nodeDomBox(nodeId) {
  var b = graphNetwork.getBoundingBox(nodeId);
  var tl = graphNetwork.canvasToDOM({ x: b.left, y: b.top });
  var br = graphNetwork.canvasToDOM({ x: b.right, y: b.bottom });
  return { left: tl.x, top: tl.y, right: br.x, bottom: br.y };
}

// ノード上（rename）または親ノードの横（add）に名前入力を出す。Enter で確定、Esc / blur でキャンセル。
// mode 'add' は root より右にある親なら右側、左なら左側に出す（子が広がる側と合わせる）。
function openNameInput(o) {
  closeEditUI();
  var container = document.getElementById('graph-container');
  var canvasEl = document.getElementById('graph-canvas');
  var box = nodeDomBox(o.anchorId);
  var width = 160;
  var left, top = (box.top + box.bottom) / 2 - 14;
  if (o.mode === 'rename') {
    width = Math.max(140, box.right - box.left);
    left = box.left;
  } else {
    var onRight = graphNetwork.getPosition(o.anchorId).x >= graphNetwork.getPosition(ROOT_ID).x;
    left = onRight ? box.right + 24 : box.left - 24 - width;
  }
  var input = document.createElement('input');
  input.type = 'text';
  input.className = 'island-name-input';
  input.maxLength = 80;
  input.value = o.initial || '';
  input.placeholder = o.placeholder || '島の名前';
  input.setAttribute('aria-label', o.placeholder || '島の名前');
  input.style.left = (canvasEl.offsetLeft + left) + 'px';
  input.style.top = (canvasEl.offsetTop + top) + 'px';
  input.style.width = width + 'px';

  var done = false;
  function finish(commit, fromBlur) {
    if (done) return;
    done = true;
    var name = input.value.trim();
    endEditing(fromBlur);
    if (commit && name) o.onCommit(name);
  }
  input.addEventListener('keydown', function(e) {
    // 日本語入力の変換確定の Enter は確定扱いにしない
    if (e.key === 'Enter' && !e.isComposing && e.keyCode !== 229) { e.preventDefault(); finish(true); }
    else if (e.key === 'Escape') { e.preventDefault(); finish(false); }
    e.stopPropagation();
  });
  input.addEventListener('blur', function() { finish(false, true); });
  container.appendChild(input);
  editState.input = input;
  input.focus();
  input.select();
}

function startAddIsland(parentRef, anchorId) {
  openNameInput({
    mode: 'add', anchorId: anchorId, initial: '',
    onCommit: function(name) { runIslandOp({ op: 'add', name: name, parent: parentRef }); }
  });
}

function startAddTask(parentRef, anchorId) {
  openNameInput({
    mode: 'add', anchorId: anchorId, initial: '', placeholder: 'タスクの名前',
    onCommit: function(name) { runIslandOp({ op: 'add', kind: 'task', name: name, parent: parentRef }); }
  });
}

// 「プロンプトとしてコピー」。clipboard.writeText はクリックの同期部分で呼ぶ（await や timer を挟むとユーザー操作の扱いから外れる）。
// 使えない・拒否されたときは、選択済みの textarea を出して手でコピーしてもらう。
function copyTaskPrompt(ref) {
  var node = graphNodes.get(ref);
  var task = node && node._data;
  if (!task) return;
  var text = taskPromptText(task);
  var pending = null;
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) pending = navigator.clipboard.writeText(text);
  } catch (e) {
    pending = null;
  }
  endEditing(); // メニューを閉じ、保留分を反映する
  if (!pending) { showCopyFallback(text); return; }
  pending.then(function() { showToast('コピーしました'); }, function() {
    // 待っている間に別の編集 UI が開いていたら、フォールバックのパネルでそれを閉じてしまわない
    if (editingActive()) showToast('コピーできませんでした');
    else showCopyFallback(text);
  });
}

function showCopyFallback(text) {
  var area = document.createElement('textarea');
  area.readOnly = true;
  area.value = text;
  area.setAttribute('aria-label', 'プロンプト');
  buildConfirmPanel('自動でコピーできませんでした。手でコピーしてください', [area], [{ label: '閉じる' }]);
  area.focus();
  area.select();
}

function startRenameIsland(ref) {
  var node = graphNodes.get(ref);
  var current = node && node._data ? node._data.name : '';
  openNameInput({
    mode: 'rename', anchorId: ref, initial: current,
    placeholder: node && node._type === 'task' ? 'タスクの名前' : undefined,
    onCommit: function(name) {
      if (name !== current) runIslandOp({ op: 'rename', ref: ref, name: name });
    }
  });
}

function refLabel(ref) {
  var node = graphNodes.get(ref);
  if (node && node._data && node._data.name) return node._data.name;
  return ref.indexOf(REPO_PREFIX) === 0 ? repoBaseName(ref.slice(REPO_PREFIX.length)) : ref;
}

// 削除確認の文言。子がいれば付け替え先の説明と一覧、いなければ単純な確認。純関数。
function deleteConfirmModel(name, childLabels, noun) {
  if (childLabels.length === 0) {
    return { title: '「' + name + '」を削除', lead: 'この' + (noun || '島') + 'を削除します。', items: [], okLabel: '削除' };
  }
  return {
    title: '「' + name + '」を削除',
    lead: '次の ' + childLabels.length + ' 件は親へ付け替えられます。',
    items: childLabels,
    okLabel: '子を親へ付け替えて削除'
  };
}

// 確認・通知用のパネル（削除確認とコピーのフォールバックで共有）。editState.confirm として持つので、
// 開いている間は再描画が保留され、外側クリック・Esc で閉じる。buttons: [{label, className, onClick}]（onClick 省略は閉じるだけ）。
// 押されたボタンはまずパネルを閉じてから onClick を呼ぶ。戻り値はボタン要素の配列（フォーカス先の選択用）。
function buildConfirmPanel(titleText, bodyEls, buttons) {
  closeEditUI();
  var panel = document.createElement('div');
  panel.className = 'island-confirm';
  panel.setAttribute('role', 'dialog');
  var title = document.createElement('div');
  title.className = 'confirm-title';
  title.textContent = titleText;
  panel.appendChild(title);
  bodyEls.forEach(function(el) { panel.appendChild(el); });
  var actions = document.createElement('div');
  actions.className = 'confirm-actions';
  var els = buttons.map(function(b) {
    var el = document.createElement('button');
    el.type = 'button';
    if (b.className) el.className = b.className;
    el.textContent = b.label;
    el.addEventListener('click', function() {
      endEditing();
      if (b.onClick) b.onClick();
    });
    actions.appendChild(el);
    return el;
  });
  panel.appendChild(actions);
  document.getElementById('graph-container').appendChild(panel);
  editState.confirm = panel;
  return els;
}

// 島の削除は必ず確認する（元に戻せないため）。子がいれば子を親へ付け替えて消す。
function startDeleteIsland(ref) {
  var children = childrenRefsOf(ref, cachedIslands);
  var isTask = editableKind(graphNodes.get(ref)) === 'task';
  // 親のない島は、消すと子が最上位へ上がる。タスクの子がいれば server が拒否するので、確認を出さず理由だけ見せる
  var hasTaskChild = children.some(function(c) { return editableKind(graphNodes.get(c)) === 'task'; });
  if (!isTask && hasTaskChild && !parentRefOf(ref, cachedIslands)) {
    var reason = document.createElement('div');
    reason.textContent = 'タスクは親が必要です。先にタスクを別の島か repo へ移してください';
    buildConfirmPanel('「' + refLabel(ref) + '」は削除できません', [reason], [{ label: '閉じる' }])[0].focus();
    return;
  }
  var model = deleteConfirmModel(refLabel(ref), children.map(refLabel), isTask ? 'タスク' : '島');
  var lead = document.createElement('div');
  lead.textContent = model.lead;
  var list = document.createElement('ul');
  model.items.forEach(function(label) {
    var li = document.createElement('li');
    li.textContent = label;
    list.appendChild(li);
  });
  var els = buildConfirmPanel(model.title, [lead, list], [
    { label: 'キャンセル' },
    { label: model.okLabel, className: 'danger', onClick: function() { runIslandOp({ op: 'remove', ref: ref, children: children }); } }
  ]);
  els[0].focus(); // 既定は安全側（キャンセル）
}

// 操作の実行。kind は editableKind の結果、id はノード ID（= ref。root は ROOT_ID）。
function runEditAction(action, id, kind) {
  if (action === 'addChild') {
    startAddIsland(kind === 'root' ? '' : id, id);
  } else if (action === 'addTask') {
    startAddTask(id, id);
  } else if (action === 'addSibling') {
    var parent = siblingParentRef(id, kind, cachedIslands);
    if (kind !== 'task') {
      startAddIsland(parent, parent || ROOT_ID);
    } else if (parent) {
      startAddTask(parent, parent);
    } else {
      showToast('親のないタスクの隣にはタスクを追加できません');
    }
  } else if (action === 'copyPrompt') {
    copyTaskPrompt(id);
  } else if (action === 'toggleDone') {
    var task = graphNodes.get(id);
    endEditing();
    runIslandOp({ op: 'done', ref: id, done: !(task && task._data && task._data.done) });
  } else if (action === 'rename') {
    startRenameIsland(id);
  } else if (action === 'detach') {
    endEditing(); // メニューを閉じ、保留分を反映してから送る
    runIslandOp({ op: 'detach', child: id });
  } else if (action === 'delete') {
    startDeleteIsland(id);
  }
}

// 右クリックメニュー。DOM は createElement / textContent だけで組む。
function buildContextMenu(items, x, y, onPick) {
  var menu = document.createElement('div');
  menu.className = 'ctx-menu';
  menu.setAttribute('role', 'menu');
  items.forEach(function(item) {
    var b = document.createElement('button');
    b.type = 'button';
    b.className = 'ctx-item' + (item.danger ? ' danger' : '');
    b.setAttribute('role', 'menuitem');
    b.textContent = item.label;
    if (item.title) b.title = item.title;
    if (item.disabled) b.disabled = true;
    else b.addEventListener('click', function() { onPick(item.action, item); });
    menu.appendChild(b);
  });
  menu.addEventListener('keydown', function(e) {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
    e.preventDefault();
    var btns = Array.prototype.slice.call(menu.querySelectorAll('.ctx-item'));
    var i = btns.indexOf(document.activeElement);
    i = e.key === 'ArrowDown' ? (i + 1) % btns.length : (i - 1 + btns.length) % btns.length;
    btns[i].focus();
  });
  document.body.appendChild(menu);
  // 画面外にはみ出さないよう、実寸を測ってから置く
  menu.style.left = Math.max(4, Math.min(x, window.innerWidth - menu.offsetWidth - 4)) + 'px';
  menu.style.top = Math.max(4, Math.min(y, window.innerHeight - menu.offsetHeight - 4)) + 'px';
  return menu;
}

// 既知 repo の一覧（GET /api/islands/known-repos）。メニューを開いたときだけ取りに行く（ポーリングには含めない）。失敗は null。
async function fetchKnownRepos() {
  var data = await fetchOptionalJSON('/api/islands/known-repos');
  return data && Array.isArray(data.repos) ? data.repos : null;
}

// 「repo を付ける…」。メニューを repo の一覧に差し替える（同じ位置）。一覧はここで初めて取りに行く
// （メニューを開くたびには取らない）。取得中は無効の項目を出し、済む前に閉じられていたら何もしない。
async function showRepoSubmenu(islandRef, x, y) {
  removeMenu();
  var loadingMenu = buildContextMenu([{ action: 'none', label: '読み込み中…', disabled: true }], x, y, function() {});
  editState.menu = loadingMenu;
  var repos = await fetchKnownRepos();
  if (editState.menu !== loadingMenu) return;
  if (repos === null) {
    endEditing();
    showToast('repo の一覧を取得できませんでした');
    return;
  }
  var items = repoMenuItems(attachableRepos(repos, islandRef, cachedIslands));
  removeMenu();
  editState.menu = buildContextMenu(items, x, y, function(action, item) {
    if (action !== 'pickRepo') return;
    endEditing();
    runIslandOp({ op: 'attach', child: repoNodeId(item.root), parent: islandRef });
  });
  var first = editState.menu.querySelector('.ctx-item:not([disabled])');
  if (first) first.focus();
}

function openContextMenu(nodeId, x, y) {
  var node = graphNodes.get(nodeId);
  var kind = editableKind(node);
  if (!kind) return false;
  closeEditUI();
  var items = contextMenuItems(kind, !!parentRefOf(nodeId, cachedIslands), !!(node._data && node._data.done));
  editState.menu = buildContextMenu(items, x, y, function(action) {
    if (action === 'attachRepo') {
      showRepoSubmenu(nodeId, x, y);
      return;
    }
    runEditAction(action, nodeId, kind);
  });
  var first = editState.menu.querySelector('.ctx-item');
  if (first) first.focus();
  return true;
}

// 編集 UI を閉じる共通のきっかけ（メニュー外クリック / Esc / スクロール）。入力欄はスクロールに追従するので残す。
document.addEventListener('mousedown', function(e) {
  var outside = (editState.menu && !editState.menu.contains(e.target)) ||
    (editState.confirm && !editState.confirm.contains(e.target));
  if (outside) closeEditUIOnOutsideMouseDown();
});
document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape' && dragState) softCancelDrag();
  if (e.key === 'Escape' && (editState.menu || editState.confirm)) endEditing();
});
// ポインタがグラフの外へ出たら、落とし先の強調を外す（離しても何も起きない）
document.getElementById('graph-canvas').addEventListener('mouseleave', function() {
  if (dragState && dragState.id && !dragState.cancelled) setDropHighlight(null);
});
window.addEventListener('scroll', function() { if (editState.menu) endEditing(true); }, true);

// グラフにフォーカスがあり、編集可能なノードが 1 つ選ばれ、編集 UI が開いていないときだけキー操作を受ける。
function onGraphKeydown(e) {
  if (!graphRootId || !graphNetwork || holdRender()) return;
  // 入力欄などではなく、グラフ自身（vis の frame）にフォーカスがあるときだけ
  if (e.target !== graphFocusEl()) return;
  if (e.isComposing || e.ctrlKey || e.metaKey || e.altKey) return;
  if (e.key === 'Escape') {
    // 編集 UI が無いときの Esc は、選択を外してグラフからフォーカスを離す
    graphNetwork.unselectAll();
    e.target.blur();
    return;
  }
  if (e.shiftKey) return; // Shift+Tab（フォーカスを戻す）などを奪わない
  var sel = graphNetwork.getSelectedNodes();
  if (sel.length !== 1) return;
  var kind = editableKind(graphNodes.get(sel[0]));
  var action = keyAction(e.key, kind);
  if (!action) return;
  e.preventDefault();
  runEditAction(action, sel[0], kind);
}
document.getElementById('graph-canvas').addEventListener('keydown', onGraphKeydown);
