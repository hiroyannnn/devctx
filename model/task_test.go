package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAddTask(t *testing.T) {
	t.Run("id is t<seq+1> and TaskSeq advances", func(t *testing.T) {
		s := newTree()
		got, err := s.AddTask("求人票を直す", "island:hiring")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "t1" || got.Kind != "task" || got.Parent != "island:hiring" || got.Name != "求人票を直す" || got.Done {
			t.Errorf("got %+v", got)
		}
		if s.TaskSeq != 1 || s.findIsland("t1") == nil {
			t.Errorf("seq=%d stored=%v", s.TaskSeq, s.findIsland("t1"))
		}
	})
	t.Run("repo parent is allowed", func(t *testing.T) {
		s := newTree()
		if _, err := s.AddTask("x", "repo:/r/a"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("skips ids already used by any island", func(t *testing.T) {
		s := newTree()
		s.Islands = append(s.Islands, Island{ID: "t1", Name: "手書き"}, Island{ID: "t2", Name: "手書き2"})
		got, err := s.AddTask("x", "island:hr")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "t3" || s.TaskSeq != 3 {
			t.Errorf("id=%s seq=%d", got.ID, s.TaskSeq)
		}
	})
	t.Run("ids are not reused after deletion", func(t *testing.T) {
		s := newTree()
		a, _ := s.AddTask("a", "island:hr")
		if err := s.RemoveIsland(a.ID, false); err != nil {
			t.Fatal(err)
		}
		b, err := s.AddTask("b", "island:hr")
		if err != nil {
			t.Fatal(err)
		}
		if b.ID != "t2" {
			t.Errorf("id = %s, want t2", b.ID)
		}
	})
	tests := []struct {
		name, taskName, parent, wantErr string
	}{
		{"empty name", " ", "island:hr", "empty"},
		{"parent required", "x", "", "parent"},
		{"missing parent island", "x", "island:nope", "not found"},
		{"malformed parent", "x", "hr", "ref"},
		{"task parent", "x", "island:tk", "tasks cannot have children"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTree()
			s.Islands = append(s.Islands, Island{ID: "tk", Name: "T", Kind: "task", Parent: "island:hr"})
			before := *s
			before.Islands = append([]Island(nil), s.Islands...)
			_, err := s.AddTask(tt.taskName, tt.parent)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
			if !reflect.DeepEqual(*s, before) {
				t.Error("store changed on error")
			}
		})
	}
}

func treeWithTask() *IslandStore {
	s := newTree()
	s.Islands = append(s.Islands, Island{ID: "t1", Name: "T", Kind: "task", Parent: "island:hr"})
	s.TaskSeq = 1
	return s
}

func TestNothingCanBePlacedUnderATask(t *testing.T) {
	t.Run("AddIsland", func(t *testing.T) {
		s := treeWithTask()
		if _, err := s.AddIsland("x", "", "island:t1"); err == nil || !strings.Contains(err.Error(), "tasks cannot have children") {
			t.Errorf("err = %v", err)
		}
	})
	for _, child := range []string{"island:hiring", "repo:/r/new", "repo:/r/a"} {
		t.Run("SetParent "+child, func(t *testing.T) {
			s := treeWithTask()
			if err := s.SetParent(child, "island:t1"); err == nil || !strings.Contains(err.Error(), "tasks cannot have children") {
				t.Errorf("err = %v", err)
			}
		})
	}
	t.Run("a task can be moved under islands and repos", func(t *testing.T) {
		s := treeWithTask()
		if err := s.SetParent("island:t1", "island:hiring"); err != nil {
			t.Fatal(err)
		}
		if err := s.SetParent("island:t1", "repo:/r/a"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a task cannot be moved under another task", func(t *testing.T) {
		s := treeWithTask()
		s.Islands = append(s.Islands, Island{ID: "t2", Name: "T2", Kind: "task", Parent: "island:hr"})
		if err := s.SetParent("island:t2", "island:t1"); err == nil {
			t.Error("want error")
		}
	})
	t.Run("a task cannot be detached to top level", func(t *testing.T) {
		s := treeWithTask()
		if err := s.Detach("island:t1"); err == nil || !strings.Contains(err.Error(), "parent") {
			t.Errorf("err = %v", err)
		}
		if s.ParentOf("island:t1") != "island:hr" {
			t.Error("parent changed")
		}
	})
}

func TestSetTaskDone(t *testing.T) {
	s := treeWithTask()
	if err := s.SetTaskDone("t1", true); err != nil {
		t.Fatal(err)
	}
	if !s.findIsland("t1").Done {
		t.Error("not done")
	}
	if err := s.SetTaskDone("t1", false); err != nil || s.findIsland("t1").Done {
		t.Errorf("undo: err=%v", err)
	}
	if err := s.SetTaskDone("hr", true); err == nil || !strings.Contains(err.Error(), "not a task") {
		t.Errorf("theme island: %v", err)
	}
	if err := s.SetTaskDone("nope", true); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing: %v", err)
	}
}

func TestResolvedCutsParentThatIsATask(t *testing.T) {
	s := treeWithTask()
	s.Islands = append(s.Islands, Island{ID: "x", Name: "X", Parent: "island:t1"}) // 手編集
	s.Repos = append(s.Repos, RepoNode{Root: "/r/z", Parent: "island:t1"})
	r := s.Resolved()
	for _, is := range r.Islands {
		if is.ID == "x" && is.Parent != "" {
			t.Errorf("x parent = %q", is.Parent)
		}
	}
	if r.Repos[len(r.Repos)-1].Parent != "" {
		t.Error("repo parent not cut")
	}
	if s.ParentOf("island:x") != "" {
		t.Error("ParentOf should also cut")
	}
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "invalid parent (task)") {
		t.Errorf("Validate = %v", err)
	}
	if err := treeWithTask().Validate(); err != nil {
		t.Errorf("valid tree: %v", err)
	}
}

func TestTaskYAMLCompatibility(t *testing.T) {
	old := "islands:\n- id: hr\n  name: HR\nrepos: []\n"
	var s IslandStore
	if err := yaml.Unmarshal([]byte(old), &s); err != nil {
		t.Fatal(err)
	}
	if s.Islands[0].Kind != "" || s.Islands[0].Done || s.TaskSeq != 0 {
		t.Errorf("%+v", s)
	}
	out, err := yaml.Marshal(&s)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"kind", "done", "task_seq"} {
		if strings.Contains(string(out), key) {
			t.Errorf("theme-only yaml leaked %q:\n%s", key, out)
		}
	}
	ts := treeWithTask()
	ts.Islands[len(ts.Islands)-1].Done = true
	out, _ = yaml.Marshal(ts)
	var back IslandStore
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&back, ts) || !strings.Contains(string(out), "task_seq: 1") {
		t.Errorf("round trip:\n%s", out)
	}
}

func TestTaskJSONShape(t *testing.T) {
	r := treeWithTask().Resolved()
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "task_seq") || strings.Contains(string(b), "TaskSeq") {
		t.Errorf("TaskSeq leaked into JSON: %s", b)
	}
	if !strings.Contains(string(b), `"kind":"task"`) {
		t.Errorf("kind missing: %s", b)
	}
	b, _ = json.Marshal(Island{ID: "hr", Name: "HR"})
	if strings.Contains(string(b), "kind") || strings.Contains(string(b), "done") {
		t.Errorf("theme island leaked: %s", b)
	}
}

func TestTaskMarker(t *testing.T) {
	if TaskMarkerPrefix != "[devctx:task:" {
		t.Errorf("prefix = %q", TaskMarkerPrefix)
	}
	if got := TaskMarker("t3"); got != "[devctx:task:t3]" {
		t.Errorf("marker = %q", got)
	}
}

func TestRemoveIslandRefusesToOrphanTasks(t *testing.T) {
	newS := func(parent string) *IslandStore {
		return &IslandStore{
			Islands: []Island{
				{ID: "top", Name: "Top", Parent: parent},
				{ID: "t1", Name: "T", Kind: KindTask, Parent: "island:top"},
				{ID: "sub", Name: "Sub", Parent: "island:top"},
			},
			TaskSeq: 1,
		}
	}
	t.Run("top-level island with task child: reparent refused", func(t *testing.T) {
		s := newS("")
		before := *newS("")
		err := s.RemoveIsland("top", true)
		if err == nil || !strings.Contains(err.Error(), "tasks need a parent; move them first") {
			t.Fatalf("err = %v", err)
		}
		if !reflect.DeepEqual(*s, before) {
			t.Error("store changed on error")
		}
	})
	t.Run("RemoveIslandExpecting is refused the same way", func(t *testing.T) {
		s := newS("")
		err := s.RemoveIslandExpecting("top", []string{"island:t1", "island:sub"})
		if err == nil || !strings.Contains(err.Error(), "tasks need a parent") {
			t.Fatalf("err = %v", err)
		}
		if !s.HasIsland("top") {
			t.Error("island removed")
		}
	})
	t.Run("island with a parent: tasks move up", func(t *testing.T) {
		s := newS("repo:/r/a")
		if err := s.RemoveIsland("top", true); err != nil {
			t.Fatal(err)
		}
		if got := s.ParentOf("island:t1"); got != "repo:/r/a" {
			t.Errorf("t1 parent = %q", got)
		}
	})
	t.Run("theme-only children may go top level", func(t *testing.T) {
		s := newS("")
		s.Islands = s.Islands[:1:1]
		s.Islands = append(s.Islands, Island{ID: "sub", Name: "Sub", Parent: "island:top"})
		if err := s.RemoveIsland("top", true); err != nil {
			t.Fatal(err)
		}
		if got := s.ParentOf("island:sub"); got != "" {
			t.Errorf("sub parent = %q", got)
		}
	})
}

func TestValidateReportsTaskWithoutParent(t *testing.T) {
	s := newTree()
	s.Islands = append(s.Islands, Island{ID: "t1", Name: "T", Kind: KindTask})
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "task without parent: island:t1") {
		t.Errorf("Validate = %v", err)
	}
	if err := treeWithTask().Validate(); err != nil {
		t.Errorf("valid: %v", err)
	}
}

// 手編集でタスクの下に置かれた子は UI に見えない（Resolved が切る）。そのタスクを消すとき、
// 見えない子の中にタスクがいれば、親なしにしてしまうので拒否する。
func TestRemoveIslandExpectingRefusesHiddenTaskChild(t *testing.T) {
	newS := func() *IslandStore {
		return &IslandStore{
			Islands: []Island{
				{ID: "hr", Name: "HR"},
				{ID: "t1", Name: "T1", Kind: KindTask, Parent: "island:hr"},
				{ID: "t2", Name: "T2", Kind: KindTask, Parent: "island:t1"}, // 手編集
				{ID: "x", Name: "X", Parent: "island:t1"},                   // 手編集（テーマ島）
			},
			TaskSeq: 2,
		}
	}
	s := newS()
	err := s.RemoveIslandExpecting("t1", nil)
	if err == nil || !strings.Contains(err.Error(), "tasks need a parent") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(*s, *newS()) {
		t.Error("store changed on error")
	}
	// タスクでない見えない子だけなら、従来どおり最上位へ落として消せる
	s = newS()
	s.Islands = append(s.Islands[:2:2], s.Islands[3])
	if err := s.RemoveIslandExpecting("t1", nil); err != nil {
		t.Fatal(err)
	}
	if s.HasIsland("t1") || s.findIsland("x").Parent != "" {
		t.Errorf("got %+v", s.Islands)
	}
}

// 消したタスクの id を、後からテーマ島が名乗ると、古いプロンプトの marker がテーマ島を指してしまう。
func TestAddIslandDoesNotTakeTaskIDs(t *testing.T) {
	newS := func() *IslandStore {
		s := newTree()
		a, _ := s.AddTask("a", "island:hr") // t1
		b, _ := s.AddTask("b", "island:hr") // t2
		_ = s.RemoveIsland(a.ID, false)
		_ = s.RemoveIsland(b.ID, false)
		return s // TaskSeq = 2、t1 / t2 は空き
	}
	t.Run("derived id gets a suffix", func(t *testing.T) {
		s := newS()
		got, err := s.AddIsland("T1", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "t1-2" {
			t.Errorf("id = %s, want t1-2", got.ID)
		}
	})
	t.Run("explicit id is refused", func(t *testing.T) {
		s := newS()
		before := len(s.Islands)
		for _, id := range []string{"t1", "t2"} {
			if _, err := s.AddIsland("x", id, ""); err == nil || !strings.Contains(err.Error(), "reserved for tasks") {
				t.Errorf("%s: err = %v", id, err)
			}
		}
		if len(s.Islands) != before {
			t.Error("store changed")
		}
	})
	t.Run("ids beyond TaskSeq and non-task-shaped ids are free", func(t *testing.T) {
		s := newS()
		for _, id := range []string{"t3", "t10", "t1x", "t", "tt1"} {
			if _, err := s.AddIsland("x"+id, id, ""); err != nil {
				t.Errorf("%s: %v", id, err)
			}
		}
	})
	t.Run("AddTask still skips islands that already hold the id", func(t *testing.T) {
		s := newS()
		s.Islands = append(s.Islands, Island{ID: "t3", Name: "手書き"})
		got, err := s.AddTask("c", "island:hr")
		if err != nil || got.ID != "t4" {
			t.Errorf("got %+v, err %v", got, err)
		}
	})
}

func TestValidateReportsUnknownKindAndDoneOnNonTask(t *testing.T) {
	s := newTree()
	s.Islands = append(s.Islands,
		Island{ID: "e", Name: "E", Kind: "epic", Parent: "island:hr"},
		Island{ID: "d", Name: "D", Done: true, Parent: "island:hr"},
	)
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), `unknown kind "epic": island:e`) || !strings.Contains(err.Error(), "done on non-task: island:d") {
		t.Errorf("Validate = %v", err)
	}
}
