// /api/roadmap-map・/api/roadmap-graph から vis 用の nodes / edges を組み立てる。

function buildMindMapData(groups) {
  var nodes = [];
  var edges = [];

  var filteredGroups = groups;
  selectedProject = document.getElementById('project-select').value;
  if (selectedProject) {
    filteredGroups = groups.filter(function(g) { return g.repo_root === selectedProject; });
  }

  // Apply period and done filters
  filteredGroups = filteredGroups.map(function(g) {
    return Object.assign({}, g, { sessions: filterSessions(g.sessions) });
  }).filter(function(g) { return g.sessions.length > 0; });

  var isAllProjects = !selectedProject;
  var isSingleProject = filteredGroups.length === 1;
  // 単一セッションで project ノードを省くのは単一プロジェクト表示だけ。All Projects では island の親子が繋がらなくなる
  var isSingleSession = !isAllProjects && isSingleProject && filteredGroups[0] && filteredGroups[0].sessions.length === 1;

  // Sort groups by priority (most urgent first) in All Projects view
  if (isAllProjects) {
    filteredGroups = filteredGroups.slice().sort(function(a, b) {
      return groupPriority(b) - groupPriority(a);
    });
  }

  // Determine level offsets — root node removed, projects are top-level
  var projectLevel = 0;
  var sessionLevel = 1;
  var detailLevel = 2;
  var showProject = !isSingleSession;

  if (isSingleSession) {
    sessionLevel = 0;
    detailLevel = 1;
  }

  filteredGroups.forEach(function(group) {
    var projectId = repoNodeId(group.repo_root);

    // Project node (skip when single session)
    if (showProject) {
      var projLabel = group.name;
      if (isAllProjects) {
        projLabel += '\n' + summarizeGroup(group);
      } else {
        projLabel += ' (' + group.sessions.length + ')';
      }

      // Project nodes are always neutral — attention shown via badges in label
      nodes.push({ id: projectId, label: projLabel, level: projectLevel, _type: 'project', _data: group });
    }

    var sessionParent = showProject ? projectId : null;

    // In All Projects, limit sessions per project to keep the graph readable
    var maxSessions = isAllProjects ? 5 : Infinity;
    var visibleSessions = group.sessions.slice(0, maxSessions);
    var truncated = group.sessions.length > maxSessions;

    visibleSessions.forEach(function(session) {
      var sessionId = 'session:' + session.name;
      var label = session.name + '\n' + agentLine(session);
      if (session.current_focus) {
        var f = session.current_focus;
        if (f.length > 30) f = f.substring(0, 28) + '..';
        label += '\n' + f;
      }

      nodes.push({
        id: sessionId, label: label, shape: 'box',
        color: sessionNodeStyle(session).color,
        font: { color: '#f0f6fc', size: 13, face: '-apple-system, sans-serif', bold: true, multi: true },
        margin: { top: 6, bottom: 6, left: 10, right: 10 },
        borderWidth: sessionNodeStyle(session).borderWidth, level: sessionLevel, _type: 'session', _data: session
      });
      if (sessionParent) {
        edges.push(treeEdge(sessionParent, sessionId));
      }

      // No roadmap warning (skip in All Projects to reduce noise)
      if (!isAllProjects && !session.goal && (!session.tasks || session.tasks.length === 0)) {
        var warnId = 'warn:' + session.name;
        nodes.push({
          id: warnId, label: '\u26A0 No roadmap', shape: 'box',
          color: { background: '#2a1215', border: '#f85149' },
          font: { color: '#f85149', size: 10 },
          margin: { top: 3, bottom: 3, left: 8, right: 8 },
          borderWidth: 1, borderDashes: [4, 4], level: detailLevel, _type: 'warning'
        });
        edges.push({ from: sessionId, to: warnId, color: { color: '#f8514944' }, width: 1, dashes: true });
      }
    });

    if (truncated && sessionParent) {
      var moreId = 'more:' + projectId;
      var remaining = group.sessions.length - maxSessions;
      nodes.push({
        id: moreId,
        label: '+' + remaining + ' more...',
        shape: 'box',
        color: { background: '#161b22', border: '#30363d' },
        font: { color: '#8b949e', size: 11, face: '-apple-system, sans-serif' },
        margin: { top: 3, bottom: 3, left: 6, right: 6 },
        borderWidth: 1,
        borderDashes: [4, 4],
        level: sessionLevel,
        _type: 'more',
        _data: group
      });
      edges.push({ from: sessionParent, to: moreId, color: { color: '#30363d44' }, width: 1, dashes: true });
    }
  });

  return postProcess(nodes, edges, isAllProjects);
}

// Compute topological depth of a node in the DAG (longest path from root)
function computeNodeDepth(nodeId, edges, nodes, memo, visiting) {
  if (!memo) memo = {};
  if (!visiting) visiting = {};
  if (memo[nodeId] !== undefined) return memo[nodeId];
  if (visiting[nodeId]) return 1; // cycle guard
  visiting[nodeId] = true;
  var incomingEdges = edges.filter(function(e) { return e.to === nodeId; });
  if (incomingEdges.length === 0) {
    memo[nodeId] = 1;
    delete visiting[nodeId];
    return 1;
  }
  var maxDepth = 0;
  incomingEdges.forEach(function(e) {
    var d = computeNodeDepth(e.from, edges, nodes, memo, visiting);
    if (d > maxDepth) maxDepth = d;
  });
  memo[nodeId] = maxDepth + 1;
  delete visiting[nodeId];
  return memo[nodeId];
}

// sessionId から辺（edges[fromIndex..]）で辿れないノードを、不可視の辺で sessionId に繋ぐ。
// Why: goal の無い session のタスクは goal→task の辺が無く、辺を辿る後処理（repo 配下の level ずらし・左右の振り分け）から漏れる。
// Why not 全ノードに所属タグを付ける: タグを builder が持つと overlay との結合が増えるので、辺という既存の構造だけで繋ぐ。
function connectOrphanNodes(edges, fromIndex, sessionId, nodeIds) {
  var kids = {};
  edges.slice(fromIndex).forEach(function(e) { (kids[e.from] = kids[e.from] || []).push(e.to); });
  var reached = {};
  function walk(id) {
    var stack = [id];
    while (stack.length) {
      var cur = stack.pop();
      if (reached[cur]) continue;
      reached[cur] = true;
      (kids[cur] || []).forEach(function(c) { stack.push(c); });
    }
  }
  walk(sessionId);
  nodeIds.forEach(function(id) {
    if (reached[id]) return;
    edges.push({ from: sessionId, to: id, hidden: true });
    walk(id);
  });
}

// Build DAG from /api/roadmap-graph data for Single Project view
function buildSemanticGraph(graphData) {
  var nodes = [];
  var edges = [];

  // Opaque backgrounds — edges are hidden behind nodes
  var NODE_STYLES = {
    goal:      { bg: '#1e1610', border: '#f0883e', fontColor: '#f0883e', icon: '\uD83C\uDFAF ', fontSize: 14, borderWidth: 2 },
    task:      { bg: '#161b22', border: '#484f58', fontColor: '#8b949e', icon: '\u25CB ', fontSize: 12, borderWidth: 1 },
    milestone: { bg: '#111d2e', border: '#79c0ff', fontColor: '#79c0ff', icon: '\u25C6 ', fontSize: 13, borderWidth: 2 },
    rejected:  { bg: '#131618', border: '#484f58', fontColor: '#484f58', icon: '\u2717 ', fontSize: 11, borderWidth: 1 }
  };
  var STATUS_STYLES = {
    in_progress: { bg: '#0f2744', border: '#58a6ff', fontColor: '#f0f6fc', icon: '\u25B6 ', fontSize: 13, borderWidth: 3 },
    done:        { bg: '#0f2018', border: '#3fb950', fontColor: '#3fb950', icon: '\u2713 ', fontSize: 12, borderWidth: 1 },
    blocked:     { bg: '#2a1215', border: '#f85149', fontColor: '#f85149', icon: '\u2716 ', fontSize: 12, borderWidth: 2 },
    planned:     { bg: '#161b22', border: '#484f58', fontColor: '#8b949e', icon: '\u25CB ', fontSize: 12, borderWidth: 1 },
    rejected:    { bg: '#131618', border: '#484f58', fontColor: '#484f58', icon: '\u2717 ', fontSize: 11, borderWidth: 1 }
  };

  var EDGE_STYLES = {
    fork:       { color: '#484f58', width: 1.5, dashes: false },
    flow:       { color: '#79c0ff', width: 2, dashes: false },
    dependency: { color: '#8b949e', width: 1, dashes: [5, 5] },
    rejected:   { color: '#484f5866', width: 1, dashes: [3, 3] }
  };

  var isAllProjects = !selectedProject;
  var maxSessions = isAllProjects ? 5 : Infinity;

  graphData.forEach(function(projectGroup) {
    // Apply period and done filters to graph sessions
    var filteredSessions = filterSessions(projectGroup.sessions);
    if (filteredSessions.length === 0) return;

    // "+N more" は表示切り詰めの前に、フィルタ後の件数で数える
    var filteredCount = filteredSessions.length;
    var truncated = filteredCount > maxSessions;
    filteredSessions = filteredSessions.slice(0, maxSessions);

    // All Projects では常に repo ノードを置く（island の親子が繋がるのは repo ノード経由）
    var showProjectNode = isAllProjects || graphData.length > 1 || filteredSessions.length > 1;
    var projectNodeId = null;

    if (showProjectNode) {
      projectNodeId = repoNodeId(projectGroup.repo_root);
      nodes.push({ id: projectNodeId, label: projectGroup.name, level: 0, _type: 'project', _data: projectGroup });
    }

    filteredSessions.forEach(function(session) {
      var baseLevel = showProjectNode ? 1 : 0;

      // Session header node — Goal is integrated as subtitle, not a separate node
      var sessionNodeId = 'session:' + session.name;
      var sessionLabel = session.name + '\n' + agentLine(session);
      if (session.goal) {
        var goalText = session.goal;
        if (goalText.length > 30) goalText = goalText.substring(0, 28) + '..';
        sessionLabel += '\n\uD83C\uDFAF ' + goalText;
      } else if (session.current_focus) {
        var cf = session.current_focus;
        if (cf.length > 30) cf = cf.substring(0, 28) + '..';
        sessionLabel += '\n' + cf;
      }
      nodes.push({
        id: sessionNodeId, label: sessionLabel, shape: 'box',
        color: sessionNodeStyle(session).color,
        font: { color: '#f0f6fc', size: 14, bold: true, multi: true },
        margin: { top: 8, bottom: 8, left: 12, right: 12 },
        borderWidth: sessionNodeStyle(session).borderWidth, level: baseLevel, _type: 'session', _data: session
      });
      if (projectNodeId) {
        edges.push(treeEdge(projectNodeId, sessionNodeId));
      }

      if (!session.nodes || session.nodes.length === 0) {
        // No DAG data — All Projects ではノイズになるので warning を出さずに打ち切る
        if (isAllProjects) return;
        var warnId = 'warn:' + session.name;
        nodes.push({
          id: warnId, label: '\u26A0 No roadmap', shape: 'box',
          color: { background: '#2a1215', border: '#f85149' },
          font: { color: '#f85149', size: 10 },
          margin: { top: 3, bottom: 3, left: 8, right: 8 },
          borderWidth: 1, borderDashes: [4, 4], level: baseLevel + 1, _type: 'warning'
        });
        edges.push({ from: sessionNodeId, to: warnId, color: { color: '#f8514944' }, width: 1, dashes: true });
        return;
      }

      // Map graph node IDs to vis-network IDs — skip Goal nodes (integrated into session)
      var idMap = {};
      // Filter out edges from/to goal for level computation
      var sessionEdges = session.edges || [];
      var nonGoalEdges = sessionEdges.filter(function(e) { return e.from !== 'goal' && e.to !== 'goal'; });

      session.nodes.forEach(function(gn) {
        if (gn.type === 'goal') return; // Goal is merged into session node

        // session 名で名前空間を切り、session 間で DAG ノード ID が衝突しないようにする
        var visId = 'dag:' + session.name + ':' + gn.id;
        idMap[gn.id] = visId;

        var style;
        if (gn.type === 'milestone') {
          style = NODE_STYLES.milestone;
        } else if (gn.type === 'rejected' || gn.status === 'rejected') {
          style = STATUS_STYLES.rejected;
        } else {
          style = STATUS_STYLES[gn.status] || NODE_STYLES.task;
        }

        // Current focus glow effect
        var isFocus = session.current_focus && gn.label &&
          gn.label.indexOf(session.current_focus) !== -1;
        var shadow = isFocus ? { enabled: true, color: '#58a6ff', size: 12, x: 0, y: 0 } : undefined;

        // Dynamic level based on topological depth (excluding goal edges)
        var level = baseLevel + computeNodeDepth(gn.id, nonGoalEdges, session.nodes);

        nodes.push({
          id: visId,
          label: style.icon + (gn.label.length > 20 ? gn.label.substring(0, 18) + '..' : gn.label),
          shape: 'box',
          color: { background: style.bg, border: style.border },
          font: { color: style.fontColor, size: style.fontSize, bold: gn.status === 'in_progress' },
          margin: { top: 5, bottom: 5, left: 10, right: 10 },
          borderWidth: style.borderWidth,
          borderDashes: (gn.type === 'rejected' || gn.status === 'rejected') ? [4, 4] : false,
          shadow: shadow,
          level: level,
          _type: gn.type,
          _data: gn
        });
      });

      var sessionEdgeStart = edges.length;
      // Connect fork/rejected edges from session node instead of goal node
      sessionEdges.forEach(function(ge) {
        if (ge.from === 'goal') {
          // Remap: goal → task becomes session → task
          var toVis = idMap[ge.to];
          if (!toVis) return;
          var toNode = session.nodes.find(function(n) { return n.id === ge.to; });
          var edgeColor = '#484f58';
          if (ge.type === 'rejected') edgeColor = '#484f5866';
          else if (toNode && toNode.status === 'in_progress') edgeColor = '#58a6ff';
          edges.push({
            from: sessionNodeId, to: toVis,
            color: { color: edgeColor },
            width: (toNode && toNode.status === 'in_progress') ? 2.5 : 1.5,
            dashes: ge.type === 'rejected' ? [3, 3] : false
          });
          return;
        }
        var fromVis = idMap[ge.from];
        var toVis = idMap[ge.to];
        if (!fromVis || !toVis) return;

        var es = EDGE_STYLES[ge.type] || EDGE_STYLES.fork;

        // Override edge color for active tasks
        var toNode = session.nodes.find(function(n) { return n.id === ge.to; });
        var edgeColor = es.color;
        if (toNode && toNode.status === 'in_progress' && ge.type !== 'rejected') {
          edgeColor = '#58a6ff';
        } else if (toNode && toNode.status === 'done' && ge.type === 'flow') {
          edgeColor = '#3fb95088';
        }

        edges.push({
          from: fromVis, to: toVis,
          color: { color: edgeColor },
          width: (toNode && toNode.status === 'in_progress') ? 3 : es.width,
          dashes: es.dashes,
          arrows: ge.type === 'dependency' ? { to: { enabled: true, scaleFactor: 0.5 } } : undefined
        });
      });
      // All Projects の level ずらしと左右の振り分けは辺を辿るので、辺で繋がらないタスクを不可視の辺で session に繋ぐ。
      // 単一プロジェクトの配置は変えない
      if (isAllProjects) connectOrphanNodes(edges, sessionEdgeStart, sessionNodeId, Object.keys(idMap).map(function(k) { return idMap[k]; }));
    });

    if (truncated && projectNodeId) {
      var moreId = 'more:' + projectNodeId;
      var remaining = filteredCount - maxSessions;
      nodes.push({
        id: moreId,
        label: '+' + remaining + ' more...',
        shape: 'box',
        color: { background: '#161b22', border: '#30363d' },
        font: { color: '#8b949e', size: 11, face: '-apple-system, sans-serif' },
        margin: { top: 3, bottom: 3, left: 6, right: 6 },
        borderWidth: 1, borderDashes: [4, 4], level: 1,
        _type: 'more', _data: projectGroup
      });
      edges.push({ from: projectNodeId, to: moreId, color: { color: '#30363d44' }, width: 1, dashes: true });
    }
  });

  return postProcess(nodes, edges, isAllProjects);
}

// Node の単体テスト用。ブラウザでは module が無いので何も起きない。
if (typeof module !== 'undefined') module.exports = { computeNodeDepth, connectOrphanNodes };
