// 状態・取得・描画・表示切り替え・初期化。他の static/*.js が定義した関数を呼ぶので、最後に読み込む。
// 素の <script> としてグローバルを共有する（ES module / bundler にしない。単一ファイル時代と挙動を変えずに分割するため）。

// All data comes from the local devctx API.
// All dynamic values are inserted via textContent (safe against XSS).
var currentView = localStorage.getItem('devctx-view') || 'project';
var cachedData = null;

const PHASES = ['idle', 'implementation', 'committed', 'pushed', 'pr_open', 'done'];
const PHASE_LABELS = {
  idle: 'Idle', implementation: 'Impl', committed: 'Commit',
  pushed: 'Push', pr_open: 'PR', done: 'Done'
};
const EVENT_LABELS = {
  session_start: 'Session Start', session_end: 'Session End',
  first_commit: 'First Commit', commit: 'Commit',
  first_push: 'First Push', pr_created: 'PR Created',
  pr_merged: 'PR Merged', command: 'Command', status_change: 'Status Change'
};

var graphNetwork = null;
var graphNodes = null;
var graphEdges = null;
var allSessionsFlat = [];

function setView(view) {
  // 編集 UI（特に削除確認）やドラッグの状態を残したまま別の表示へ移ると、描画の保留（holdRender）が解けず自動更新が止まる
  cancelEditing();
  currentView = view;
  localStorage.setItem('devctx-view', view);
  document.getElementById('btn-project').className = 'view-btn' + (view === 'project' ? ' active' : '');
  document.getElementById('btn-flat').className = 'view-btn' + (view === 'flat' ? ' active' : '');
  document.getElementById('btn-graph').className = 'view-btn' + (view === 'graph' ? ' active' : '');
  document.getElementById('content').style.display = (view === 'graph') ? 'none' : '';
  document.getElementById('graph-container').style.display = (view === 'graph') ? 'block' : 'none';
  if (cachedData) render(cachedData);
  // project 表示中は graph / islands を取っていないので、Mind Map に切り替えたらすぐ取り直す
  if (view === 'graph') refresh();
}

function phaseIndex(phase) {
  var idx = PHASES.indexOf(phase);
  return idx >= 0 ? idx : 0;
}

// Agent state comes from the server (agent_state_label / agent_waiting); do not re-derive it here.
// Waiting-for-user amber; keep in sync with .card.agent-waiting in the stylesheet.
var AGENT_WAITING_STYLE = { color: { border: '#d29922', background: '#2d2208' }, borderWidth: 3 };

// "needs input · Bash: Run tests": label plus the waiting detail (all server-provided).
// The server already puts the pending request label ahead of the generic live reason.
function agentStateText(item) {
  if (!item.agent_state_label) return '';
  var detail = item.agent_waiting_for;
  return detail ? item.agent_state_label + ' \u00B7 ' + detail : item.agent_state_label;
}

// "claude · needs input · permission prompt" line for Mind Map session nodes
function agentLine(item) {
  var text = agentStateText(item);
  return text ? item.provider + ' \u00B7 ' + text : item.provider;
}

// Session node style: waiting for the user overrides the attention color
function sessionNodeStyle(session) {
  if (session.agent_waiting) return AGENT_WAITING_STYLE;
  var attnColor = ({blocked: '#f85149', active: '#58a6ff', waiting: '#bc8cff'})[session.attention_state] || '#484f58';
  return { color: { background: '#1a1f27', border: attnColor }, borderWidth: 2 };
}

function createInsightRow(label, value) {
  var row = document.createElement('div');
  row.className = 'insight-row';
  var labelEl = document.createElement('span');
  labelEl.className = 'insight-label';
  labelEl.textContent = label;
  var valueEl = document.createElement('span');
  valueEl.className = 'insight-value';
  valueEl.textContent = value;
  row.appendChild(labelEl);
  row.appendChild(valueEl);
  return row;
}

function createPipeline(currentPhase) {
  var container = document.createElement('div');
  container.className = 'pipeline';
  var currentIdx = phaseIndex(currentPhase);

  PHASES.forEach(function(phase, i) {
    if (i > 0) {
      var conn = document.createElement('div');
      conn.className = 'connector' + (i <= currentIdx ? ' reached' : '');
      container.appendChild(conn);
    }
    var stage = document.createElement('div');
    stage.className = 'stage';
    var dot = document.createElement('div');
    dot.className = 'stage-dot';
    if (i < currentIdx) dot.classList.add('reached');
    else if (i === currentIdx) dot.classList.add('current');
    var label = document.createElement('div');
    label.className = 'stage-label';
    if (i < currentIdx) label.classList.add('reached');
    else if (i === currentIdx) label.classList.add('current');
    label.textContent = PHASE_LABELS[phase];
    stage.appendChild(dot);
    stage.appendChild(label);
    container.appendChild(stage);
  });

  return container;
}

function createMilestoneChips(ms) {
  var container = document.createElement('div');
  container.className = 'card-milestones';

  function addChip(label, achieved) {
    var chip = document.createElement('span');
    chip.className = 'milestone-chip' + (achieved ? ' achieved' : '');
    chip.textContent = label;
    container.appendChild(chip);
  }

  addChip('Sessions: ' + (ms.session_count || 0), ms.session_count > 0);
  addChip('Commits: ' + (ms.commit_count || 0), ms.commit_count > 0);
  if (ms.first_push_at && ms.first_push_at !== '0001-01-01T00:00:00Z') addChip('Pushed', true);
  if (ms.pr_created_at && ms.pr_created_at !== '0001-01-01T00:00:00Z') addChip('PR Created', true);
  if (ms.pr_merged_at && ms.pr_merged_at !== '0001-01-01T00:00:00Z') addChip('Merged', true);
  addChip('Commands: ' + (ms.command_count || 0), ms.command_count > 0);

  return container;
}

function formatTime(ts) {
  if (!ts) return '';
  var d = new Date(ts);
  if (isNaN(d.getTime())) return '';
  return d.toLocaleString('ja-JP', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function createTimelineEvent(evt) {
  var row = document.createElement('div');
  row.className = 'timeline-event';

  var dotCol = document.createElement('div');
  dotCol.className = 'timeline-dot-col';
  var dot = document.createElement('div');
  dot.className = 'timeline-dot ' + (evt.type || '');
  dotCol.appendChild(dot);
  var line = document.createElement('div');
  line.className = 'timeline-line';
  dotCol.appendChild(line);
  row.appendChild(dotCol);

  var content = document.createElement('div');
  content.className = 'timeline-content';
  var typeSpan = document.createElement('span');
  typeSpan.className = 'timeline-type';
  typeSpan.textContent = EVENT_LABELS[evt.type] || evt.type;
  content.appendChild(typeSpan);
  if (evt.detail) {
    var detail = document.createElement('span');
    detail.className = 'timeline-detail';
    detail.textContent = evt.detail;
    content.appendChild(detail);
  }
  row.appendChild(content);

  var time = document.createElement('span');
  time.className = 'timeline-time';
  time.textContent = formatTime(evt.occurred_at);
  row.appendChild(time);

  return row;
}

async function toggleTimeline(card, sessionName) {
  var existing = card.querySelector('.timeline-panel');
  if (existing) {
    existing.remove();
    card.classList.remove('expanded');
    return;
  }

  card.classList.add('expanded');
  var panel = document.createElement('div');
  panel.className = 'timeline-panel';
  var title = document.createElement('div');
  title.className = 'timeline-title';
  title.textContent = 'Event Timeline';
  panel.appendChild(title);

  var loading = document.createElement('div');
  loading.className = 'timeline-loading';
  loading.textContent = 'Loading...';
  panel.appendChild(loading);
  card.appendChild(panel);

  try {
    var res = await fetch('/api/timeline/' + encodeURIComponent(sessionName));
    var data = await res.json();
    loading.remove();

    if (data.events && data.events.length > 0) {
      // Show newest first
      var events = data.events.slice().reverse();
      events.forEach(function(evt) {
        panel.appendChild(createTimelineEvent(evt));
      });
    } else {
      var empty = document.createElement('div');
      empty.className = 'timeline-loading';
      empty.textContent = 'No events recorded yet.';
      panel.appendChild(empty);
    }
  } catch (e) {
    loading.textContent = 'Error loading timeline: ' + e.message;
  }
}

function createCard(entry) {
  var card = document.createElement('div');
  card.className = 'card';
  card.onclick = function() { toggleTimeline(card, entry.name); };

  var header = document.createElement('div');
  header.className = 'card-header';
  var name = document.createElement('span');
  name.className = 'card-name';
  name.textContent = entry.name;
  var status = document.createElement('span');
  status.className = 'card-status status-' + (entry.status || '').replace(/[^a-z-]/g, '');
  status.textContent = entry.status;
  header.appendChild(name);
  var provider = document.createElement('span');
  provider.className = 'badge provider-' + entry.provider.replace(/[^a-z]/g, '');
  provider.textContent = entry.provider;
  header.appendChild(provider);
  if (entry.agent_state_label) {
    var agentState = document.createElement('span');
    agentState.className = 'badge agent-state' + (entry.agent_waiting ? ' waiting' : '');
    agentState.textContent = agentStateText(entry);
    // Subtle hint that the state comes from the live agent view rather than hooks
    if (entry.agent_state_source === 'live') agentState.title = 'live (claude agents)';
    header.appendChild(agentState);
  }
  if (entry.agent_waiting) card.classList.add('agent-waiting');
  header.appendChild(status);
  card.appendChild(header);

  var branch = document.createElement('div');
  branch.className = 'card-branch';
  branch.textContent = entry.branch || 'no branch';
  card.appendChild(branch);

  if (entry.goal || entry.current_focus || entry.next_step) {
    var insights = document.createElement('div');
    insights.className = 'card-insights';
    if (entry.goal) insights.appendChild(createInsightRow('Goal', entry.goal));
    if (entry.current_focus) insights.appendChild(createInsightRow('Focus', entry.current_focus));
    if (entry.next_step) insights.appendChild(createInsightRow('Next', entry.next_step));
    if (entry.attention_state) {
      var badge = document.createElement('span');
      badge.className = 'attention-badge attention-' + entry.attention_state;
      badge.textContent = entry.attention_state;
      insights.appendChild(badge);
    }
    if (entry.inferred_at) {
      var fresh = document.createElement('div');
      fresh.className = 'insight-freshness';
      fresh.textContent = 'Inferred: ' + entry.inferred_at;
      insights.appendChild(fresh);
    }
    card.appendChild(insights);
  } else if (entry.initial_prompt) {
    var prompt = document.createElement('div');
    prompt.className = 'card-prompt';
    prompt.textContent = entry.initial_prompt;
    card.appendChild(prompt);
  }

  card.appendChild(createPipeline(entry.phase));

  if (entry.milestones) {
    card.appendChild(createMilestoneChips(entry.milestones));
  }

  // Topics & Tasks
  if ((entry.topics && entry.topics.length > 0) || (entry.tasks && entry.tasks.length > 0)) {
    var ttContainer = document.createElement('div');
    ttContainer.className = 'card-topics-tasks';

    if (entry.topics && entry.topics.length > 0) {
      var topicsDiv = document.createElement('div');
      topicsDiv.className = 'card-topics';
      var topicsTitle = document.createElement('div');
      topicsTitle.className = 'section-title';
      topicsTitle.textContent = 'Topics';
      topicsDiv.appendChild(topicsTitle);
      entry.topics.forEach(function(topic) {
        var tag = document.createElement('span');
        tag.className = 'topic-tag' + (topic.source === 'git' ? ' git' : topic.source === 'manual' ? ' manual' : '');
        tag.textContent = topic.name;
        topicsDiv.appendChild(tag);
      });
      ttContainer.appendChild(topicsDiv);
    }

    if (entry.tasks && entry.tasks.length > 0) {
      var tasksDiv = document.createElement('div');
      tasksDiv.className = 'card-tasks';
      var tasksTitle = document.createElement('div');
      tasksTitle.className = 'section-title';
      tasksTitle.textContent = 'Tasks';
      tasksDiv.appendChild(tasksTitle);
      var TASK_ICONS = {done: '\u2713', in_progress: '\u25B6', planned: '\u25CB', blocked: '\u2716'};
      entry.tasks.forEach(function(task) {
        var item = document.createElement('div');
        item.className = 'task-item';
        var icon = document.createElement('span');
        icon.className = 'task-icon task-' + task.status;
        icon.textContent = TASK_ICONS[task.status] || '\u25CB';
        item.appendChild(icon);
        var title = document.createElement('span');
        title.textContent = task.title;
        item.appendChild(title);
        tasksDiv.appendChild(item);
      });
      ttContainer.appendChild(tasksDiv);
    }

    card.appendChild(ttContainer);
  }

  var footer = document.createElement('div');
  footer.className = 'card-footer';
  var timeEl = document.createElement('span');
  timeEl.textContent = formatDateTime(entry.last_seen);
  footer.appendChild(timeEl);

  var links = document.createElement('span');
  links.className = 'card-links';
  if (entry.pr_url) {
    var a = document.createElement('a');
    a.href = entry.pr_url;
    a.target = '_blank';
    a.rel = 'noopener noreferrer';
    a.textContent = 'PR';
    a.onclick = function(e) { e.stopPropagation(); };
    links.appendChild(a);
  }
  if (entry.issue_url) {
    var a2 = document.createElement('a');
    a2.href = entry.issue_url;
    a2.target = '_blank';
    a2.rel = 'noopener noreferrer';
    a2.textContent = 'Issue';
    a2.onclick = function(e) { e.stopPropagation(); };
    links.appendChild(a2);
  }
  footer.appendChild(links);
  card.appendChild(footer);

  return card;
}

function renderProjectView(groups) {
  var container = document.getElementById('content');
  container.replaceChildren();

  if (groups.length === 0) {
    var empty = document.createElement('div');
    empty.className = 'empty';
    empty.textContent = 'No sessions registered.';
    container.appendChild(empty);
    return;
  }

  var totalSessions = 0;
  groups.forEach(function(group) {
    var section = document.createElement('div');
    section.className = 'project-group';

    var header = document.createElement('div');
    header.className = 'project-header';
    var nameEl = document.createElement('span');
    nameEl.className = 'project-name';
    nameEl.textContent = group.name;
    header.appendChild(nameEl);
    var countEl = document.createElement('span');
    countEl.className = 'project-count';
    countEl.textContent = group.sessions.length + ' session(s)';
    header.appendChild(countEl);
    section.appendChild(header);

    var cards = document.createElement('div');
    cards.className = 'cards';
    group.sessions.forEach(function(entry) {
      cards.appendChild(createCard(entry));
    });
    section.appendChild(cards);

    container.appendChild(section);
    totalSessions += group.sessions.length;
  });

  var now = new Date();
  document.getElementById('meta').textContent =
    groups.length + ' project(s), ' + totalSessions + ' session(s) | Updated: ' + now.toLocaleTimeString();
}

function renderFlatView(groups) {
  var container = document.getElementById('content');
  container.replaceChildren();

  var allSessions = [];
  groups.forEach(function(g) {
    g.sessions.forEach(function(s) { allSessions.push(s); });
  });

  if (allSessions.length === 0) {
    var empty = document.createElement('div');
    empty.className = 'empty';
    empty.textContent = 'No sessions registered.';
    container.appendChild(empty);
    return;
  }

  var cards = document.createElement('div');
  cards.className = 'cards';
  allSessions.forEach(function(entry) {
    cards.appendChild(createCard(entry));
  });
  container.appendChild(cards);

  var now = new Date();
  document.getElementById('meta').textContent =
    allSessions.length + ' session(s) | Updated: ' + now.toLocaleTimeString();
}

function render(data) {
  // Flatten sessions for flat view (use copies to avoid mutating cached data)
  allSessionsFlat = [];
  data.forEach(function(g) {
    g.sessions.forEach(function(s) {
      var copy = Object.assign({}, s);
      copy._projectName = g.name;
      copy._projectKey = g.repo_root;
      allSessionsFlat.push(copy);
    });
  });

  if (currentView === 'project') {
    renderProjectView(data);
  } else if (currentView === 'graph') {
    renderGraphView(data, cachedGraphData);
  } else {
    renderFlatView(data);
  }
}

// ── Mind Map View ──

var PHASE_COLORS = {
  idle: '#484f58', implementation: '#f0883e', committed: '#d2a8ff',
  pushed: '#58a6ff', pr_open: '#bc8cff', done: '#3fb950'
};
var TASK_COLORS = {
  done: '#3fb950', in_progress: '#58a6ff', planned: '#484f58', blocked: '#f85149'
};
var TASK_ICONS_MAP = {done: '\u2713', in_progress: '\u25B6', planned: '\u25CB', blocked: '\u2716'};

var graphGroups = [];
var selectedProject = '';

function updateProjectSelect(groups) {
  var sel = document.getElementById('project-select');
  var prev = sel.value;
  sel.replaceChildren();
  var allOpt = document.createElement('option');
  allOpt.value = '';
  allOpt.textContent = 'All Projects (' + groups.length + ')';
  sel.appendChild(allOpt);
  groups.forEach(function(g) {
    var opt = document.createElement('option');
    opt.value = g.repo_root;
    opt.textContent = g.name + ' (' + g.sessions.length + ')';
    sel.appendChild(opt);
  });
  if (prev) sel.value = prev;
  selectedProject = sel.value;
}

// Priority order for sorting groups: higher = more urgent
var STATUS_PRIORITY = {'in-progress': 5, blocked: 4, review: 3, done: 0};
function groupPriority(group) {
  var max = 0;
  group.sessions.forEach(function(s) {
    var p = STATUS_PRIORITY[s.status] || 1;
    if (p > max) max = p;
  });
  return max;
}

function formatDateTime(s) {
  if (!s) return '';
  var d = new Date(s);
  if (isNaN(d.getTime())) return s;
  var pad = function(n) { return n < 10 ? '0' + n : '' + n; };
  return d.getFullYear() + '-' + pad(d.getMonth()+1) + '-' + pad(d.getDate()) + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
}

function summarizeGroup(group) {
  // Summarize by attention state (semantic) rather than status (mechanical)
  var counts = {};
  group.sessions.forEach(function(s) {
    var attn = s.attention_state || s.status || 'unknown';
    counts[attn] = (counts[attn] || 0) + 1;
  });
  var parts = [];
  if (counts['blocked']) parts.push('\u25CF ' + counts['blocked'] + ' blocked');
  if (counts['active'] || counts['in-progress']) {
    var n = (counts['active'] || 0) + (counts['in-progress'] || 0);
    parts.push('\u25CF ' + n + ' active');
  }
  if (counts['waiting'] || counts['review']) {
    var n = (counts['waiting'] || 0) + (counts['review'] || 0);
    parts.push('\u25CF ' + n + ' waiting');
  }
  if (parts.length === 0) parts.push(group.sessions.length + ' sessions');
  return parts.join('  ');
}

var cachedGraphData = null; // cached /api/roadmap-graph data

// opts.preserveView: 再描画（refresh / 編集後）で拡大率・視点・選択を引き継ぐ。初回・プロジェクト切り替え・フィルタ変更は fit し直す。
// opts.select: 描画後に選択するノード ID（add で作ったノード）。
function renderGraphView(groups, graphData, opts) {
  opts = opts || {};
  // 作り直すと vis の frame（フォーカス先）も新しくなるので、フォーカスを持っていたかを先に控える
  var focusEl = graphFocusEl();
  var hadFocus = !!focusEl && document.activeElement === focusEl;
  var userPicked = false; // 作り直しの後、レイアウト完了前に利用者がノードを選んだか
  cancelEditing(); // ノードごと作り直すので、開いている編集 UI は残せない
  var keep = null;
  if (opts.preserveView && graphNetwork && graphRootId) {
    // 直前の描画がまだレイアウト前（afterDrawing 前）なら、作りたての network の scale=1・選択なしを拾わず、
    // その描画が引き継ごうとしていた値をそのまま持ち越す。refresh が立て続けに描画するとき視点・選択が失われるため
    keep = graphLaidOut
      ? { scale: graphNetwork.getScale(), position: graphNetwork.getViewPosition(), selected: graphNetwork.getSelectedNodes()[0] || null }
      : carriedKeep;
  }
  if (keep && opts.select) keep = { scale: keep.scale, position: keep.position, selected: opts.select };
  carriedKeep = keep;
  graphLaidOut = false;
  graphGroups = groups;
  updateProjectSelect(groups);

  // Use semantic DAG when graph data is available
  var useDAG = !!graphData;
  var filteredGraph = null;
  if (useDAG) {
    filteredGraph = selectedProject
      ? graphData.filter(function(g) { return g.repo_root === selectedProject; })
      : graphData;
    if (filteredGraph.length === 0) useDAG = false; // fallback to tree if no match
  }
  var data = useDAG ? buildSemanticGraph(filteredGraph) : buildMindMapData(groups);

  // Destroy and recreate network for clean hierarchical layout
  if (graphNetwork) {
    graphNetwork.destroy();
    graphNetwork = null;
  }

  graphRootId = data.rootId;
  graphNodes = new vis.DataSet(data.nodes);
  graphEdges = new vis.DataSet(data.edges);
  var container = document.getElementById('graph-canvas');
  graphNetwork = new vis.Network(container, { nodes: graphNodes, edges: graphEdges }, {
    layout: {
      hierarchical: {
        enabled: true,
        direction: 'LR',
        sortMethod: 'directed',
        levelSeparation: 200,
        nodeSpacing: 45,
        treeSpacing: 50,
        parentCentralization: true,
        blockShifting: true,
        edgeMinimization: true
      }
    },
    physics: { enabled: false },
    interaction: {
      hover: true,
      tooltipDelay: 200,
      dragNodes: true,
      dragView: true,
      zoomView: true,
      zoomSpeed: 0.3,
      multiselect: false,
      navigationButtons: false,
      // 矢印・+/- による移動・拡大は frame に束縛する。window 束縛だと名前入力中の矢印や '-' を奪うため
      keyboard: { enabled: true, bindToWindow: false, speed: { x: 10, y: 10, zoom: 0.03 } }
    },
    edges: {
      arrows: { to: false },
      color: { color: MINDMAP_THEME.edge, hover: '#8b949e', inherit: false },
      width: 1,
      smooth: { type: 'cubicBezier', forceDirection: 'horizontal', roundness: 0.8 }
    }
  });

  // After initial hierarchical layout, disable it so nodes can be dragged freely
  var rootId = data.rootId;
  graphNetwork.once('afterDrawing', function() {
    var layout = null;
    if (rootId) {
      // hierarchical を切る前に、初期配置の座標と実寸の高さを読む
      var pos = graphNetwork.getPositions();
      var measured = graphNodes.getIds().map(function(id) {
        var box = graphNetwork.getBoundingBox(id);
        return { id: id, label: graphNodes.get(id).label, x: pos[id].x, y: pos[id].y, h: box.bottom - box.top };
      });
      layout = balancedLayout(measured, graphEdges.get(), rootId, islandSides);
      if (layout) islandSides = layout.headSides;
    }
    graphNetwork.setOptions({ layout: { hierarchical: { enabled: false } } });
    graphLaidOut = true;
    carriedKeep = null;
    if (layout) {
      graphNodes.update(Object.keys(layout.positions).map(function(id) {
        return { id: id, x: layout.positions[id].x, y: layout.positions[id].y };
      }));
      if (keep) {
        graphNetwork.moveTo({ scale: keep.scale, position: keep.position, animation: false });
      } else {
        graphNetwork.fit({ maxZoomLevel: FIT_MAX_ZOOM });
      }
      var reselect = opts.select && graphNodes.get(opts.select) ? opts.select
        : keep && keep.selected && graphNodes.get(keep.selected) ? keep.selected : null;
      if (reselect && !userPicked) graphNetwork.selectNodes([reselect]);
    }
    if (hadFocus) {
      var fe = graphFocusEl();
      if (fe) fe.focus({ preventScroll: true });
    }
  });

  graphNetwork.on('oncontext', function(params) {
    var id = graphNetwork.getNodeAt(params.pointer.DOM);
    if (id === undefined) return;
    // セッションのメニュー（タスクに付ける）は単一プロジェクト表示でも出す。island の編集は All Projects だけ
    var picked = graphNodes.get(id);
    var isSession = !!picked && picked._type === 'session';
    if (!isSession && (!graphRootId || !editableKind(picked))) return;
    graphNetwork.selectNodes([id]);
    var src = params.event && params.event.srcEvent;
    // ブラウザ標準のメニューを出さない（編集可能なノード上のときだけ）
    if (params.event && params.event.preventDefault) params.event.preventDefault();
    if (src && src.preventDefault) src.preventDefault();
    var rect = document.getElementById('graph-canvas').getBoundingClientRect();
    openContextMenu(id, src ? src.clientX : rect.left + params.pointer.DOM.x, src ? src.clientY : rect.top + params.pointer.DOM.y);
  });
  // ズーム・パンで入力欄などが指すノードとずれるので閉じる
  graphNetwork.on('zoom', function() { if (editingActive()) endEditing(true); });
  // ドラッグ開始では編集 UI を閉じるだけで、保留した更新は反映しない（反映は作り直しなのでドラッグが途切れる）
  graphNetwork.on('dragStart', function(params) {
    closeEditUI();
    beginDrag(params.nodes.length === 1 ? params.nodes[0] : null);
  });
  graphNetwork.on('dragging', function(params) {
    if (dragState && dragState.id) updateDragTarget(params.pointer.canvas, pointerInsideGraph(params.pointer.DOM));
  });
  graphNetwork.on('dragEnd', function(params) {
    // 外で離した場合、最後の dragging が内側のものでも落とし先を無効にする
    if (dragState && dragState.id && params && params.pointer && !pointerInsideGraph(params.pointer.DOM)) setDropHighlight(null);
    finishDrag();
  });

  graphNetwork.on('click', function(params) {
    // キー操作を受けるため、クリックでグラフにフォーカスを移す
    var fe = graphFocusEl();
    if (fe) fe.focus({ preventScroll: true });
    // 描画の作り直しの直後（レイアウト前）に押された場合、完了後に古い選択へ戻さない
    userPicked = true;
    if (keep) keep.selected = params.nodes[0] || null;
    if (params.nodes.length > 0) {
      var nodeData = graphNodes.get(params.nodes[0]);
      if (nodeData._type === 'session') {
        showInspector(nodeData._data);
      } else if (nodeData._type === 'island') {
        // island に見せる詳細は無い。編集は右クリック / キー操作
        closeInspector();
      } else if (nodeData._type === 'more') {
        // Click "+N more" to select that project
        var sel = document.getElementById('project-select');
        sel.value = nodeData._data.repo_root;
        onProjectChange();
      } else {
        closeInspector();
      }
    } else {
      closeInspector();
    }
  });

  var sessionCount = 0;
  groups.forEach(function(g) { sessionCount += g.sessions.length; });
  var now = new Date();
  document.getElementById('meta').textContent =
    sessionCount + ' session(s) | Mind Map | Updated: ' + now.toLocaleTimeString();
}

function showInspector(session) {
  var inspector = document.getElementById('graph-inspector');
  inspector.style.display = 'block';
  document.getElementById('inspector-name').textContent = session.name;

  var body = document.getElementById('inspector-body');
  body.replaceChildren();

  function addSection(title, content) {
    if (!content) return;
    var sec = document.createElement('div');
    sec.className = 'inspector-section';
    var t = document.createElement('div');
    t.className = 'inspector-section-title';
    t.textContent = title;
    sec.appendChild(t);
    var v = document.createElement('div');
    v.className = 'inspector-value';
    if (typeof content === 'string') {
      v.textContent = content;
    } else {
      v.appendChild(content);
    }
    sec.appendChild(v);
    body.appendChild(sec);
  }

  // Status & Phase
  var statusPhase = document.createElement('div');
  statusPhase.style.display = 'flex';
  statusPhase.style.gap = '8px';
  statusPhase.style.marginBottom = '4px';
  var statusBadge = document.createElement('span');
  statusBadge.className = 'card-status status-' + (session.status || '').replace(/[^a-z-]/g, '');
  statusBadge.textContent = session.status;
  statusPhase.appendChild(statusBadge);
  var phaseBadge = document.createElement('span');
  phaseBadge.className = 'milestone-chip achieved';
  phaseBadge.textContent = PHASE_LABELS[session.phase] || session.phase;
  statusPhase.appendChild(phaseBadge);
  addSection('Status', statusPhase);

  addSection('Task', sessionTaskName(session, cachedIslands));
  addSection('Branch', session.branch);
  if (session.goal) addSection('Goal', session.goal);
  if (session.current_focus) addSection('Focus', session.current_focus);
  if (session.next_step) addSection('Next Step', session.next_step);

  if (session.attention_state) {
    var badge = document.createElement('span');
    badge.className = 'attention-badge attention-' + session.attention_state;
    badge.textContent = session.attention_state;
    addSection('Attention', badge);
  }

  // Pipeline
  addSection('Pipeline', createPipeline(session.phase));

  // Milestones
  if (session.milestones) {
    addSection('Milestones', createMilestoneChips(session.milestones));
  }

  // Topics
  if (session.topics && session.topics.length > 0) {
    var topicsEl = document.createElement('div');
    session.topics.forEach(function(topic) {
      var tag = document.createElement('span');
      tag.className = 'topic-tag' + (topic.source === 'git' ? ' git' : topic.source === 'manual' ? ' manual' : '');
      tag.textContent = topic.name;
      topicsEl.appendChild(tag);
    });
    addSection('Topics', topicsEl);
  }

  // Tasks
  if (session.tasks && session.tasks.length > 0) {
    var tasksEl = document.createElement('div');
    session.tasks.forEach(function(task) {
      var item = document.createElement('div');
      item.className = 'task-item';
      var icon = document.createElement('span');
      icon.className = 'task-icon task-' + task.status;
      icon.textContent = TASK_ICONS_MAP[task.status] || '\u25CB';
      item.appendChild(icon);
      var title = document.createElement('span');
      title.textContent = task.title;
      item.appendChild(title);
      tasksEl.appendChild(item);
    });
    addSection('Tasks', tasksEl);
  }

  // Links
  if (session.pr_url || session.issue_url) {
    var linksEl = document.createElement('div');
    linksEl.className = 'card-links';
    if (session.pr_url) {
      var a = document.createElement('a');
      a.href = session.pr_url;
      a.target = '_blank';
      a.rel = 'noopener noreferrer';
      a.textContent = 'Pull Request';
      linksEl.appendChild(a);
    }
    if (session.issue_url) {
      var a2 = document.createElement('a');
      a2.href = session.issue_url;
      a2.target = '_blank';
      a2.rel = 'noopener noreferrer';
      a2.textContent = 'Issue';
      a2.style.marginLeft = '8px';
      linksEl.appendChild(a2);
    }
    addSection('Links', linksEl);
  }

  // Timestamps
  addSection('Last Seen', formatDateTime(session.last_seen));
  if (session.inferred_at) addSection('Inferred', session.inferred_at);
}

function closeInspector() {
  document.getElementById('graph-inspector').style.display = 'none';
}

function onFilterChange() {
  if (currentView === 'graph' && graphGroups) {
    renderGraphView(graphGroups, cachedGraphData);
  }
}

// Filter sessions by period and done status
function filterSessions(sessions) {
  var periodDays = parseInt(document.getElementById('period-filter').value, 10);
  var hideDone = document.getElementById('hide-done').checked;
  var now = Date.now();

  return sessions.filter(function(s) {
    // Hide done
    if (hideDone && s.status === 'done') return false;

    // Period filter (0 = all)
    if (periodDays > 0 && s.last_seen) {
      var lastSeen = new Date(s.last_seen).getTime();
      if (now - lastSeen > periodDays * 86400000) return false;
    }

    return true;
  });
}

var projectChanging = false;
async function onProjectChange() {
  cancelEditing();
  islandSides = {};
  projectChanging = true;
  selectedProject = document.getElementById('project-select').value;
  // Fetch graph data for DAG view
  cachedGraphData = await fetchOptionalJSON('/api/roadmap-graph'); // refresh と同じ失敗時の扱い（null → tree に fallback）
  var islandsData = await fetchOptionalJSON('/api/islands');
  if (islandsData) cachedIslands = normalizeIslands(islandsData);
  lastJson = dataFingerprint(cachedData, cachedGraphData, cachedIslands);
  renderGraphView(graphGroups, cachedGraphData);
  projectChanging = false;
}

function graphFit() {
  if (graphNetwork) graphNetwork.fit({ animation: true, maxZoomLevel: FIT_MAX_ZOOM });
}

// Reset は視点を全体表示に戻し、部分木の左右も振り直す（islandSides は新しい島ほど片側に偏りうるので、ここで手放す）。
function graphReset() {
  islandSides = {};
  if (currentView === 'graph' && graphGroups && graphNetwork) {
    renderGraphView(graphGroups, cachedGraphData); // preserveView なし = fit し直す
  }
}

// 再描画の要否判定用。map / graph / islands のどれが変わっても差が出る
var lastJson = '';
function dataFingerprint(mapData, graphData, islandsData) {
  return JSON.stringify(mapData) + JSON.stringify(graphData) + JSON.stringify(islandsData);
}

// 任意の API: 失敗・非 2xx は null（呼び出し側が前回値や fallback に戻す）
async function fetchOptionalJSON(url) {
  try {
    var res = await fetch(url);
    return res.ok ? await res.json() : null;
  } catch (e) { return null; }
}

// refresh は同時に 1 本だけ走らせる。ポーリングは走行中なら見送り、操作失敗後などの明示的な要求は
// 走行中なら終了後に 1 回だけ再実行する。
// Why: 取得が 5 秒より長引いたときにポーリングが重なって、古い取得が新しい取得を追い越すのを防ぐ。
// refreshSeq は、取得の途中で操作の応答（より新しい木）が反映されたとき、その取得を捨てるための世代。
var refreshSeq = 0;
var refreshInFlight = false;
var refreshRerun = false;
async function refresh(opts) {
  if (projectChanging) return; // skip refresh during project change
  if (refreshInFlight) {
    if (!(opts && opts.poll)) refreshRerun = true;
    return;
  }
  refreshInFlight = true;
  try {
    await fetchAndApply();
  } finally {
    refreshInFlight = false;
  }
  if (refreshRerun) {
    refreshRerun = false;
    await refresh();
  }
}

async function fetchAndApply() {
  var seq = refreshSeq;
  try {
    var res = await fetch('/api/roadmap-map');
    if (!res.ok) throw new Error('HTTP ' + res.status);
    var newData = await res.json();

    // Also fetch graph data for DAG view and the islands tree (both optional: failures fall back)
    var graphData = null;
    var islandsData = cachedIslands;
    if (currentView === 'graph') {
      var extras = await Promise.all([
        fetchOptionalJSON('/api/roadmap-graph'),
        fetchOptionalJSON('/api/islands')
      ]);
      graphData = extras[0];
      // 取得失敗時は直前の木を保つ（一時的な失敗で island が消えて見えないように）
      if (extras[1]) islandsData = normalizeIslands(extras[1]);
    }
    if (seq !== refreshSeq) return;
    applyRefreshed({ mapData: newData, graphData: graphData, islandsData: islandsData });
  } catch (e) {
    document.getElementById('meta').textContent = 'Error: ' + e.message;
  }
}

// 取得した最新データを画面に反映する。編集 UI が開いている間は反映せず pendingData に預ける
// （閉じたときに flushPending が再度ここへ通し、そのとき変化があれば描画する）。
function applyRefreshed(r) {
  if (holdRender()) {
    pendingData = r;
    return;
  }
  pendingData = null; // より新しい取得が反映されるので、古い保留は捨てる
  var newData = r.mapData, graphData = r.graphData, islandsData = r.islandsData;
  // 比較用の文字列は前回分を持ち回り、新しいデータだけ stringify する
  var newJson = dataFingerprint(newData, graphData, islandsData);
  // For mind map view, skip re-render if data hasn't changed (preserves zoom/pan)
  if (currentView === 'graph' && cachedData && graphNetwork) {
    if (newJson === lastJson) {
      var sessionCount = 0;
      newData.forEach(function(g) { sessionCount += g.sessions.length; });
      var now = new Date();
      document.getElementById('meta').textContent =
        sessionCount + ' session(s) | Mind Map | Updated: ' + now.toLocaleTimeString();
      return;
    }
  }
  cachedData = newData;
  cachedGraphData = graphData;
  cachedIslands = islandsData;
  lastJson = newJson;
  if (currentView === 'graph') {
    renderGraphView(cachedData, cachedGraphData, { preserveView: true, select: pendingSelect });
    pendingSelect = null;
  } else {
    render(cachedData);
  }
}

// Restore saved view on load
if (currentView !== 'project') {
  setView(currentView);
} else {
  refresh();
}
setInterval(function() { refresh({ poll: true }); }, 5000);
