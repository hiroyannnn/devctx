package roadmap

import (
	"fmt"

	"github.com/hiroyannnn/devctx/model"
)

// NodeType はグラフノードの種別。
type NodeType string

const (
	NodeGoal      NodeType = "goal"
	NodeTask      NodeType = "task"
	NodeMilestone NodeType = "milestone"
	NodeRejected  NodeType = "rejected"
)

// EdgeType はグラフエッジの種別。
type EdgeType string

const (
	EdgeFork       EdgeType = "fork"
	EdgeFlow       EdgeType = "flow"
	EdgeDependency EdgeType = "dependency"
	EdgeRejected   EdgeType = "rejected"
)

// GraphNode はグラフ上の1ノード。
type GraphNode struct {
	ID     string           `json:"id"`
	Type   NodeType         `json:"type"`
	Label  string           `json:"label"`
	Status model.TaskStatus `json:"status,omitempty"`
}

// GraphEdge はグラフ上の1エッジ。
type GraphEdge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Type EdgeType `json:"type"`
}

// SessionGraph は1セッション内のタスクフローDAG。
type SessionGraph struct {
	Name           string              `json:"name"`
	Goal           string              `json:"goal,omitempty"`
	AttentionState model.AttentionState `json:"attention_state,omitempty"`
	CurrentFocus   string              `json:"current_focus,omitempty"`
	NextStep       string              `json:"next_step,omitempty"`
	Branch         string              `json:"branch,omitempty"`
	Status         model.Status        `json:"status,omitempty"`
	Phase          model.Phase         `json:"phase,omitempty"`
	PRURL          string              `json:"pr_url,omitempty"`
	InferredAt     string              `json:"inferred_at,omitempty"`
	Nodes          []GraphNode         `json:"nodes"`
	Edges          []GraphEdge         `json:"edges"`
}

// ProjectGraphGroup はプロジェクト単位のグラフグループ。
type ProjectGraphGroup struct {
	Name     string         `json:"name"`
	RepoRoot string         `json:"repo_root"`
	Sessions []SessionGraph `json:"sessions"`
}

// BuildSessionGraph は RoadmapEntry からセッション内タスクフローDAGを構築する。
func BuildSessionGraph(entry RoadmapEntry) SessionGraph {
	sg := SessionGraph{
		Name:           entry.Name,
		Goal:           entry.Goal,
		AttentionState: entry.AttentionState,
		CurrentFocus:   entry.CurrentFocus,
		NextStep:       entry.NextStep,
		Branch:         entry.Branch,
		Status:         entry.Status,
		Phase:          entry.Phase,
		PRURL:          entry.PRURL,
		InferredAt:     entry.InferredAt,
	}

	if entry.Goal == "" && len(entry.Tasks) == 0 {
		return sg
	}

	// Assign IDs to tasks that don't have one
	tasks := assignTaskIDs(entry.Tasks)

	hasGoal := entry.Goal != ""
	taskIDs := collectTaskIDs(tasks)
	nodeTypes := resolveNodeTypes(tasks)

	// Phase 1: ノード追加
	if hasGoal {
		sg.Nodes = append(sg.Nodes, GraphNode{
			ID:    "goal",
			Type:  NodeGoal,
			Label: entry.Goal,
		})
	}
	for _, task := range tasks {
		sg.Nodes = append(sg.Nodes, GraphNode{
			ID:     task.ID,
			Type:   nodeTypes[task.ID],
			Label:  task.Title,
			Status: task.Status,
		})
	}

	// Phase 2: エッジ生成
	// Track edges to avoid duplicates (dependency + flow to same target)
	edgeSet := make(map[string]bool)
	edgeKey := func(from, to string) string { return from + "->" + to }

	// Pass 1: flows_to → flow edges（flow は dependency より優先）
	for _, task := range tasks {
		if task.FlowsTo != "" && task.Status != model.TaskRejected {
			key := edgeKey(task.ID, task.FlowsTo)
			if !edgeSet[key] {
				sg.Edges = append(sg.Edges, GraphEdge{From: task.ID, To: task.FlowsTo, Type: EdgeFlow})
				edgeSet[key] = true
			}

			// flows_to 先がタスクリストに存在しない場合、Milestone ノードを生成
			if !taskIDs[task.FlowsTo] && nodeTypes[task.FlowsTo] == "" {
				nodeTypes[task.FlowsTo] = NodeMilestone
				sg.Nodes = append(sg.Nodes, GraphNode{
					ID:    task.FlowsTo,
					Type:  NodeMilestone,
					Label: task.FlowsTo,
				})
			}
		}
	}

	// Pass 2: Goal → Task, depends_on → dependency
	for _, task := range tasks {
		// Goal → Task（fork / rejected）— Milestone には直接つながない
		if hasGoal && nodeTypes[task.ID] != NodeMilestone {
			sg.Edges = appendGoalEdge(sg.Edges, task)
		}

		// depends_on → dependency（flow と重複しない）
		for _, dep := range task.DependsOn {
			key := edgeKey(dep, task.ID)
			if !edgeSet[key] {
				sg.Edges = append(sg.Edges, GraphEdge{From: dep, To: task.ID, Type: EdgeDependency})
				edgeSet[key] = true
			}
		}
	}

	return sg
}

// assignTaskIDs は ID が空のタスクに自動 ID を付与したコピーを返す。
func assignTaskIDs(tasks []model.TaskItem) []model.TaskItem {
	result := make([]model.TaskItem, len(tasks))
	for i, t := range tasks {
		result[i] = t
		if result[i].ID == "" {
			result[i].ID = fmt.Sprintf("task-%d", i)
		}
	}
	return result
}

// collectTaskIDs はタスクリストからID集合を作成する。
func collectTaskIDs(tasks []model.TaskItem) map[string]bool {
	ids := make(map[string]bool, len(tasks))
	for _, t := range tasks {
		ids[t.ID] = true
	}
	return ids
}

// resolveNodeTypes は各タスクのノード種別を決定する。
// flows_to の合流先は NodeMilestone に昇格する。
func resolveNodeTypes(tasks []model.TaskItem) map[string]NodeType {
	// flows_to の参照カウントを集計
	flowsToCount := make(map[string]int)
	for _, t := range tasks {
		if t.FlowsTo != "" && t.Status != model.TaskRejected {
			flowsToCount[t.FlowsTo]++
		}
	}

	types := make(map[string]NodeType, len(tasks))
	for _, t := range tasks {
		switch {
		case t.Status == model.TaskRejected:
			types[t.ID] = NodeRejected
		case flowsToCount[t.ID] > 0:
			types[t.ID] = NodeMilestone
		default:
			types[t.ID] = NodeTask
		}
	}
	return types
}

// appendGoalEdge は Goal → Task エッジを追加する。
func appendGoalEdge(edges []GraphEdge, task model.TaskItem) []GraphEdge {
	edgeType := EdgeFork
	if task.Status == model.TaskRejected {
		edgeType = EdgeRejected
	}
	return append(edges, GraphEdge{From: "goal", To: task.ID, Type: edgeType})
}
