package roadmap

import "github.com/hiroyannnn/devctx/model"

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
	}

	if entry.Goal == "" && len(entry.Tasks) == 0 {
		return sg
	}

	hasGoal := entry.Goal != ""
	taskIDs := collectTaskIDs(entry.Tasks)
	nodeTypes := resolveNodeTypes(entry.Tasks)

	// Phase 1: ノード追加
	if hasGoal {
		sg.Nodes = append(sg.Nodes, GraphNode{
			ID:    "goal",
			Type:  NodeGoal,
			Label: entry.Goal,
		})
	}
	for _, task := range entry.Tasks {
		sg.Nodes = append(sg.Nodes, GraphNode{
			ID:     task.ID,
			Type:   nodeTypes[task.ID],
			Label:  task.Title,
			Status: task.Status,
		})
	}

	// Phase 2: エッジ生成
	for _, task := range entry.Tasks {
		// Goal → Task（fork / rejected）
		if hasGoal {
			sg.Edges = appendGoalEdge(sg.Edges, task)
		}

		// depends_on → dependency
		for _, dep := range task.DependsOn {
			sg.Edges = append(sg.Edges, GraphEdge{From: dep, To: task.ID, Type: EdgeDependency})
		}

		// flows_to → flow（rejected タスクは除外）
		if task.FlowsTo != "" && task.Status != model.TaskRejected {
			sg.Edges = append(sg.Edges, GraphEdge{From: task.ID, To: task.FlowsTo, Type: EdgeFlow})

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

	return sg
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
