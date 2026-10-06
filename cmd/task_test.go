package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiroyannnn/devctx/model"
)

func TestTaskAddDoneAndList(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer
	if err := islandAdd(s, &out, base, "採用フロー", "hiring", ""); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := taskAdd(s, &out, base, "求人票を直す", "hiring"); err != nil { // 素の island id でも解決できる
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "island:t1") {
		t.Errorf("add output = %q", out.String())
	}
	out.Reset()
	if err := taskAdd(s, &out, base, "API を見直す", "devctx"); err != nil { // repo basename
		t.Fatal(err)
	}
	is, _ := s.LoadIslands()
	if got := is.ParentOf("island:t2"); got != "repo:/r/devctx" {
		t.Errorf("t2 parent = %q", got)
	}

	out.Reset()
	if err := taskDone(s, &out, "t1", false); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := islandList(s, &out); err != nil {
		t.Fatal(err)
	}
	want := `採用フロー [island:hiring]
└── 求人票を直す [task t1] ✓
devctx (/r/devctx, 2 active)
└── API を見直す [task t2]
web (/r/web, 1 active)
`
	if out.String() != want {
		t.Errorf("list:\n%s\nwant:\n%s", out.String(), want)
	}

	if err := taskDone(s, &out, "island:t1", true); err != nil { // --undo。型付き ref も受ける
		t.Fatal(err)
	}
	is, _ = s.LoadIslands()
	if is.Islands[1].Done {
		t.Error("undo did not clear done")
	}
}

func TestTaskCommandsRejectInvalidInput(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer
	if err := islandAdd(s, &out, base, "HR", "hr", ""); err != nil {
		t.Fatal(err)
	}
	if err := taskAdd(s, &out, base, "t", "island:nope"); err == nil {
		t.Error("missing parent")
	}
	if err := taskAdd(s, &out, base, "t", ""); err == nil {
		t.Error("empty --to")
	}
	if err := taskDone(s, &out, "hr", false); err == nil || !strings.Contains(err.Error(), "not a task") {
		t.Errorf("done on theme island: %v", err)
	}
	if err := taskDone(s, &out, "repo:/r/devctx", false); err == nil {
		t.Error("done on repo ref")
	}
	if err := taskAdd(s, &out, base, "t", "hr"); err != nil {
		t.Fatal(err)
	}
	// 既存コマンド経由でもタスクの下には置けない
	if err := islandAdd(s, &out, base, "x", "x", "island:t1"); err == nil {
		t.Error("island add under task")
	}
	if err := islandAttach(s, &out, base, "devctx", "island:t1"); err == nil {
		t.Error("attach repo under task")
	}
	if err := taskAdd(s, &out, base, "nested", "island:t1"); err == nil {
		t.Error("task under task")
	}
	// rename / rm は既存コマンドがそのまま使える
	if err := islandRename(s, &out, "t1", "改名"); err != nil {
		t.Fatal(err)
	}
	if err := islandRm(s, &out, "t1", false); err != nil {
		t.Fatal(err)
	}
}

func TestIslandRmReparentRefusesToOrphanTasks(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer
	if err := islandAdd(s, &out, base, "Top", "top", ""); err != nil {
		t.Fatal(err)
	}
	if err := taskAdd(s, &out, base, "t", "top"); err != nil {
		t.Fatal(err)
	}
	if err := islandRm(s, &out, "top", true); err == nil || !strings.Contains(err.Error(), "tasks need a parent") {
		t.Errorf("err = %v", err)
	}
	is, _ := s.LoadIslands()
	if !is.HasIsland("top") || !is.HasIsland("t1") {
		t.Error("store changed")
	}
}

func TestIslandListWarnsOnTaskWithoutParent(t *testing.T) {
	s, _ := islandFixture(t, fixtureContexts()...)
	if err := s.UpdateIslands(func(is *model.IslandStore) error {
		is.Islands = append(is.Islands, model.Island{ID: "t1", Name: "孤児", Kind: model.KindTask})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := islandList(s, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "warning: task without parent: island:t1") {
		t.Errorf("list output:\n%s", out.String())
	}
}

// 手編集でタスクの下に置かれたノードは、解決後の木（トップレベル）にだけ出し、タスクの下には重ねて出さない。
func TestIslandListShowsNodeUnderTaskOnce(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer
	if err := islandAdd(s, &out, base, "HR", "hr", ""); err != nil {
		t.Fatal(err)
	}
	if err := taskAdd(s, &out, base, "T", "hr"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateIslands(func(is *model.IslandStore) error {
		is.Islands = append(is.Islands, model.Island{ID: "x", Name: "手編集", Parent: "island:t1"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := islandList(s, &out); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "手編集 [island:x]"); n != 1 {
		t.Errorf("shown %d times:\n%s", n, out.String())
	}
	if strings.Contains(out.String(), "│   └── 手編集") || strings.Contains(out.String(), "    └── 手編集") {
		t.Errorf("shown under the task:\n%s", out.String())
	}
}

func TestTaskLinkAndUnlink(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer
	if err := islandAdd(s, &out, base, "採用フロー", "hiring", ""); err != nil {
		t.Fatal(err)
	}
	if err := taskAdd(s, &out, base, "求人票を直す", "hiring"); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := taskLink(s, &out, "a1", "island:t1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "a1") || !strings.Contains(out.String(), "island:t1") {
		t.Errorf("link output = %q", out.String())
	}
	c := ctxByName(t, s, "a1")
	if c.TaskRef != "island:t1" || c.TaskLinkSource != "manual" {
		t.Fatalf("link = %+v", c)
	}
	// 素の id でも付けられる
	if err := taskLink(s, &out, "a2", "t1"); err != nil {
		t.Fatal(err)
	}
	if c := ctxByName(t, s, "a2"); c.TaskRef != "island:t1" {
		t.Fatalf("a2 link = %q", c.TaskRef)
	}

	out.Reset()
	if err := islandList(s, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "求人票を直す [task t1] (2 sessions)") {
		t.Errorf("list:\n%s", out.String())
	}

	// リンクは完了状態に触れない
	is, _ := s.LoadIslands()
	if is.Islands[1].Done {
		t.Errorf("link must not mark the task done")
	}

	if err := taskUnlink(s, &out, "a1"); err != nil {
		t.Fatal(err)
	}
	if c := ctxByName(t, s, "a1"); c.TaskRef != "" || c.TaskLinkSource != "" {
		t.Fatalf("unlink: %+v", c)
	}
	out.Reset()
	_ = islandList(s, &out)
	if !strings.Contains(out.String(), "求人票を直す [task t1] (1 session)") {
		t.Errorf("list after unlink:\n%s", out.String())
	}
}

func TestTaskLink_Errors(t *testing.T) {
	s, base := islandFixture(t, fixtureContexts()...)
	var out bytes.Buffer
	if err := islandAdd(s, &out, base, "採用フロー", "hiring", ""); err != nil {
		t.Fatal(err)
	}
	if err := taskAdd(s, &out, base, "求人票を直す", "hiring"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, ctx, task string }{
		{"unknown context", "nope", "t1"},
		{"missing task", "a1", "t9"},
		{"theme island is not a task", "a1", "island:hiring"},
		{"repo ref", "a1", "repo:/r/devctx"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := taskLink(s, &out, tt.ctx, tt.task); err == nil {
				t.Fatal("want error")
			}
			if c := ctxByName(t, s, "a1"); c.TaskRef != "" {
				t.Errorf("a1 linked: %q", c.TaskRef)
			}
		})
	}
	if err := taskUnlink(s, &out, "nope"); err == nil {
		t.Error("unlink unknown context should fail")
	}
}

// 完了済み context は Mind Map に出ないので、island list の件数にも数えない。
func TestLinkedSessionCounts_OnlyActive(t *testing.T) {
	store := &model.Store{Contexts: []model.Context{
		{Name: "a1", Status: model.StatusInProgress, TaskRef: "island:t1"},
		{Name: "a2", Status: model.StatusDone, TaskRef: "island:t1"},
		{Name: "a3", Status: model.StatusReview},
	}}
	got := linkedSessionCounts(store)
	if got["island:t1"] != 1 || len(got) != 1 {
		t.Fatalf("counts = %v, want {island:t1: 1}", got)
	}
}

// islands.yaml が壊れているときに「task not found」と誤報せず、読み込みエラーを返す。hook 経路は従来どおり黙って無視する。
func TestTaskLink_ReportsIslandsLoadError(t *testing.T) {
	s, _ := islandFixture(t, fixtureContexts()...)
	// islandFixture は HOME を temp にしており、設定ディレクトリは ~/.config/devctx
	path := filepath.Join(os.Getenv("HOME"), ".config", "devctx", "islands.yaml")
	if err := os.WriteFile(path, []byte("islands: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := taskLink(s, &bytes.Buffer{}, "a1", "t1")
	if err == nil || strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want the islands load error", err)
	}
	if taskExists(s, "t1") {
		t.Fatal("hook path must keep treating a load failure as 'no such task'")
	}
}
