package cmd

import (
	"bytes"
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
