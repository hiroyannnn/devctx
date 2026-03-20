package roadmap

import (
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func TestBuildSessionGraph_FullDAG(t *testing.T) {
	entry := RoadmapEntry{
		Name:           "jwt-session",
		Goal:           "JWT認証に移行",
		AttentionState: model.AttentionActive,
		CurrentFocus:   "refresh token implementation",
		Tasks: []model.TaskItem{
			{ID: "jwt-mw", Title: "JWT middleware", Status: model.TaskDone, FlowsTo: "pr-review"},
			{ID: "refresh", Title: "Refresh token", Status: model.TaskInProgress, DependsOn: []string{"jwt-mw"}, FlowsTo: "pr-review"},
			{ID: "e2e", Title: "E2E tests", Status: model.TaskPlanned, FlowsTo: "pr-review"},
			{ID: "cleanup", Title: "Dead code cleanup", Status: model.TaskRejected},
			{ID: "pr-review", Title: "PR review", Status: model.TaskPlanned, DependsOn: []string{"jwt-mw", "refresh", "e2e"}},
		},
	}

	sg := BuildSessionGraph(entry)

	// Basic fields
	if sg.Name != "jwt-session" {
		t.Errorf("Name = %q, want %q", sg.Name, "jwt-session")
	}
	if sg.Goal != "JWT認証に移行" {
		t.Errorf("Goal = %q, want %q", sg.Goal, "JWT認証に移行")
	}
	if sg.AttentionState != model.AttentionActive {
		t.Errorf("AttentionState = %q, want %q", sg.AttentionState, model.AttentionActive)
	}
	if sg.CurrentFocus != "refresh token implementation" {
		t.Errorf("CurrentFocus = %q, want %q", sg.CurrentFocus, "refresh token implementation")
	}

	// Nodes: goal, jwt-mw, refresh, e2e, cleanup, pr-review = 6
	if len(sg.Nodes) != 6 {
		t.Fatalf("len(Nodes) = %d, want 6", len(sg.Nodes))
	}

	nodeMap := make(map[string]GraphNode)
	for _, n := range sg.Nodes {
		nodeMap[n.ID] = n
	}

	// goal node
	if n, ok := nodeMap["goal"]; !ok {
		t.Error("missing goal node")
	} else if n.Type != NodeGoal {
		t.Errorf("goal node Type = %q, want %q", n.Type, NodeGoal)
	}

	// pr-review should be milestone (multiple inputs)
	if n, ok := nodeMap["pr-review"]; !ok {
		t.Error("missing pr-review node")
	} else if n.Type != NodeMilestone {
		t.Errorf("pr-review node Type = %q, want %q", n.Type, NodeMilestone)
	}

	// cleanup should be rejected
	if n, ok := nodeMap["cleanup"]; !ok {
		t.Error("missing cleanup node")
	} else if n.Type != NodeRejected {
		t.Errorf("cleanup node Type = %q, want %q", n.Type, NodeRejected)
	}

	// jwt-mw, refresh, e2e should be task
	for _, id := range []string{"jwt-mw", "refresh", "e2e"} {
		if n, ok := nodeMap[id]; !ok {
			t.Errorf("missing %s node", id)
		} else if n.Type != NodeTask {
			t.Errorf("%s node Type = %q, want %q", id, n.Type, NodeTask)
		}
	}

	// Edges
	edgeSet := make(map[string]EdgeType)
	for _, e := range sg.Edges {
		key := e.From + "->" + e.To
		edgeSet[key] = e.Type
	}

	// goal -> fork edges (jwt-mw, refresh, e2e)
	for _, id := range []string{"jwt-mw", "refresh", "e2e"} {
		key := "goal->" + id
		if typ, ok := edgeSet[key]; !ok {
			t.Errorf("missing edge %s", key)
		} else if typ != EdgeFork {
			t.Errorf("edge %s Type = %q, want %q", key, typ, EdgeFork)
		}
	}

	// goal -> cleanup (rejected edge)
	if typ, ok := edgeSet["goal->cleanup"]; !ok {
		t.Error("missing edge goal->cleanup")
	} else if typ != EdgeRejected {
		t.Errorf("edge goal->cleanup Type = %q, want %q", typ, EdgeRejected)
	}

	// flow edges: jwt-mw -> pr-review, refresh -> pr-review, e2e -> pr-review
	for _, id := range []string{"jwt-mw", "refresh", "e2e"} {
		found := false
		for _, e := range sg.Edges {
			if e.From == id && e.To == "pr-review" && e.Type == EdgeFlow {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing flow edge %s->pr-review", id)
		}
	}

	// dependency edge: jwt-mw -> refresh
	if typ, ok := edgeSet["jwt-mw->refresh"]; !ok {
		t.Error("missing edge jwt-mw->refresh")
	} else if typ != EdgeDependency {
		t.Errorf("edge jwt-mw->refresh Type = %q, want %q", typ, EdgeDependency)
	}

	// dependency edges: jwt-mw -> pr-review, refresh -> pr-review, e2e -> pr-review (from depends_on)
	// Note: pr-review depends_on jwt-mw, refresh, e2e
	for _, id := range []string{"jwt-mw", "refresh", "e2e"} {
		// These edges already exist as flow edges; dependency edges from depends_on
		// are separate — check they exist (may overlap with flow)
		found := false
		for _, e := range sg.Edges {
			if e.From == id && e.To == "pr-review" && e.Type == EdgeDependency {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing dependency edge %s->pr-review", id)
		}
	}
}

func TestBuildSessionGraph_NoGoal(t *testing.T) {
	entry := RoadmapEntry{
		Name: "no-goal-session",
		Tasks: []model.TaskItem{
			{ID: "task-a", Title: "Task A", Status: model.TaskPlanned},
			{ID: "task-b", Title: "Task B", Status: model.TaskInProgress},
		},
	}

	sg := BuildSessionGraph(entry)

	// No goal node
	for _, n := range sg.Nodes {
		if n.Type == NodeGoal {
			t.Error("should not have goal node when Goal is empty")
		}
	}

	// No fork edges
	for _, e := range sg.Edges {
		if e.Type == EdgeFork {
			t.Error("should not have fork edges when Goal is empty")
		}
	}

	// Should have 2 task nodes
	if len(sg.Nodes) != 2 {
		t.Errorf("len(Nodes) = %d, want 2", len(sg.Nodes))
	}
}

func TestBuildSessionGraph_Empty(t *testing.T) {
	entry := RoadmapEntry{
		Name: "empty-session",
	}

	sg := BuildSessionGraph(entry)

	if len(sg.Nodes) != 0 {
		t.Errorf("len(Nodes) = %d, want 0", len(sg.Nodes))
	}
	if len(sg.Edges) != 0 {
		t.Errorf("len(Edges) = %d, want 0", len(sg.Edges))
	}
	if sg.Name != "empty-session" {
		t.Errorf("Name = %q, want %q", sg.Name, "empty-session")
	}
}

func TestBuildSessionGraph_TasksOnly_NoFlowsTo(t *testing.T) {
	entry := RoadmapEntry{
		Name: "no-flow-session",
		Goal: "Simple goal",
		Tasks: []model.TaskItem{
			{ID: "a", Title: "Task A", Status: model.TaskPlanned},
			{ID: "b", Title: "Task B", Status: model.TaskDone},
		},
	}

	sg := BuildSessionGraph(entry)

	// Should have goal + 2 tasks = 3 nodes
	if len(sg.Nodes) != 3 {
		t.Errorf("len(Nodes) = %d, want 3", len(sg.Nodes))
	}

	// No milestone nodes
	for _, n := range sg.Nodes {
		if n.Type == NodeMilestone {
			t.Error("should not have milestone node when no flows_to")
		}
	}

	// No flow edges
	for _, e := range sg.Edges {
		if e.Type == EdgeFlow {
			t.Error("should not have flow edges when no flows_to")
		}
	}
}

func TestBuildSessionGraph_FlowsToNonExistentTask(t *testing.T) {
	entry := RoadmapEntry{
		Name: "phantom-milestone",
		Tasks: []model.TaskItem{
			{ID: "impl", Title: "Implementation", Status: model.TaskDone, FlowsTo: "deploy"},
		},
	}

	sg := BuildSessionGraph(entry)

	// Should have impl + deploy = 2 nodes
	if len(sg.Nodes) != 2 {
		t.Fatalf("len(Nodes) = %d, want 2", len(sg.Nodes))
	}

	nodeMap := make(map[string]GraphNode)
	for _, n := range sg.Nodes {
		nodeMap[n.ID] = n
	}

	// deploy should be created as milestone
	if n, ok := nodeMap["deploy"]; !ok {
		t.Error("missing deploy milestone node")
	} else {
		if n.Type != NodeMilestone {
			t.Errorf("deploy node Type = %q, want %q", n.Type, NodeMilestone)
		}
		if n.Label != "deploy" {
			t.Errorf("deploy node Label = %q, want %q", n.Label, "deploy")
		}
	}

	// flow edge: impl -> deploy
	found := false
	for _, e := range sg.Edges {
		if e.From == "impl" && e.To == "deploy" && e.Type == EdgeFlow {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing flow edge impl->deploy")
	}
}

func TestBuildSessionGraph_RejectedTaskNoFlowEdge(t *testing.T) {
	entry := RoadmapEntry{
		Name: "rejected-flow",
		Goal: "Test goal",
		Tasks: []model.TaskItem{
			{ID: "bad-idea", Title: "Bad idea", Status: model.TaskRejected, FlowsTo: "milestone"},
			{ID: "milestone", Title: "Milestone", Status: model.TaskPlanned},
		},
	}

	sg := BuildSessionGraph(entry)

	// rejected task should NOT have flow edge
	for _, e := range sg.Edges {
		if e.From == "bad-idea" && e.Type == EdgeFlow {
			t.Error("rejected task should not have flow edge")
		}
	}

	// but should have rejected edge from goal
	found := false
	for _, e := range sg.Edges {
		if e.From == "goal" && e.To == "bad-idea" && e.Type == EdgeRejected {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing rejected edge goal->bad-idea")
	}
}
