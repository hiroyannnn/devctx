package model

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestParseTaskMarker(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		id     string
		ok     bool
	}{
		{"plain", "面接の質問を作る\n\n[devctx:task:t4]", "t4", true},
		{"trailing newline", "x\n[devctx:task:t4]\n", "t4", true},
		{"trailing pasted_content closing line", "x\n\n[devctx:task:t4]\n</pasted_content id=\"x\">", "t4", true},
		{"pasted_content without id", "x\n[devctx:task:t4]\n</pasted_content>", "t4", true},
		{"extra spaces trimmed", "x\n   [devctx:task:t12]  \n  ", "t12", true},
		{"marker only", "[devctx:task:t1]", "t1", true},
		{"marker not last line", "[devctx:task:t4]\nそのあと続ける", "", false},
		{"marker inside text", "これは [devctx:task:t4] を含む文", "", false},
		{"wrong prefix", "x\n[devctx:tsk:t4]", "", false},
		{"id not t<n>", "x\n[devctx:task:foo]", "", false},
		{"leading zero id", "x\n[devctx:task:t01]", "", false},
		{"t0 rejected", "x\n[devctx:task:t0]", "", false},
		{"missing bracket", "x\n[devctx:task:t4", "", false},
		{"multiple markers last wins", "[devctx:task:t1]\nx\n[devctx:task:t2]", "t2", true},
		{"multiple markers but last is text", "[devctx:task:t1]\nx", "", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := ParseTaskMarker(tt.prompt)
			if id != tt.id || ok != tt.ok {
				t.Errorf("ParseTaskMarker(%q) = (%q, %v), want (%q, %v)", tt.prompt, id, ok, tt.id, tt.ok)
			}
		})
	}
}

var linkT0 = time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

func TestApplyTaskMarker(t *testing.T) {
	t.Run("ignores another session", func(t *testing.T) {
		ctx := &Context{SessionID: "s1"}
		if ApplyTaskMarker(ctx, "other", "t1", linkT0) || ctx.TaskRef != "" {
			t.Fatalf("link by another session must be ignored: %+v", ctx)
		}
	})
	t.Run("first link", func(t *testing.T) {
		ctx := &Context{SessionID: "s1"}
		if !ApplyTaskMarker(ctx, "s1", "t1", linkT0) {
			t.Fatal("want changed")
		}
		if ctx.TaskRef != "island:t1" || ctx.TaskLinkSession != "s1" || ctx.TaskLinkSource != "marker" || !ctx.TaskLinkAt.Equal(linkT0) {
			t.Fatalf("got %+v", ctx)
		}
	})
	t.Run("later marker in the same session is ignored", func(t *testing.T) {
		ctx := &Context{SessionID: "s1"}
		ApplyTaskMarker(ctx, "s1", "t1", linkT0)
		if ApplyTaskMarker(ctx, "s1", "t2", linkT0.Add(time.Minute)) || ctx.TaskRef != "island:t1" {
			t.Fatalf("first marker wins: %+v", ctx)
		}
	})
	t.Run("same marker again is not a change", func(t *testing.T) {
		ctx := &Context{SessionID: "s1"}
		ApplyTaskMarker(ctx, "s1", "t1", linkT0)
		if ApplyTaskMarker(ctx, "s1", "t1", linkT0.Add(time.Minute)) {
			t.Fatal("no change expected")
		}
	})
	t.Run("out-of-order earlier marker replaces", func(t *testing.T) {
		ctx := &Context{SessionID: "s1"}
		ApplyTaskMarker(ctx, "s1", "t2", linkT0.Add(time.Minute))
		if !ApplyTaskMarker(ctx, "s1", "t1", linkT0) {
			t.Fatal("earlier event should win")
		}
		if ctx.TaskRef != "island:t1" || !ctx.TaskLinkAt.Equal(linkT0) {
			t.Fatalf("got %+v", ctx)
		}
	})
	t.Run("out-of-order earlier marker for the same task is not a change", func(t *testing.T) {
		ctx := &Context{SessionID: "s1"}
		ApplyTaskMarker(ctx, "s1", "t1", linkT0.Add(time.Minute))
		if ApplyTaskMarker(ctx, "s1", "t1", linkT0) {
			t.Fatal("same task, no relink needed")
		}
	})
	t.Run("new session relinks to a different task", func(t *testing.T) {
		ctx := &Context{SessionID: "s2", TaskRef: "island:t1", TaskLinkSession: "s1", TaskLinkSource: "marker", TaskLinkAt: linkT0}
		if !ApplyTaskMarker(ctx, "s2", "t2", linkT0.Add(time.Hour)) {
			t.Fatal("want changed")
		}
		if ctx.TaskRef != "island:t2" || ctx.TaskLinkSession != "s2" {
			t.Fatalf("got %+v", ctx)
		}
	})
	t.Run("new session same task still claims the session", func(t *testing.T) {
		ctx := &Context{SessionID: "s2", TaskRef: "island:t1", TaskLinkSession: "s1", TaskLinkSource: "marker", TaskLinkAt: linkT0}
		if !ApplyTaskMarker(ctx, "s2", "t1", linkT0.Add(time.Hour)) {
			t.Fatal("session/at changed, want changed")
		}
		if ctx.TaskLinkSession != "s2" {
			t.Fatalf("got %+v", ctx)
		}
		if ApplyTaskMarker(ctx, "s2", "t3", linkT0.Add(2*time.Hour)) {
			t.Fatal("later marker in s2 must be ignored")
		}
	})
	t.Run("manual link from the same session is not overridden by a later marker", func(t *testing.T) {
		ctx := &Context{SessionID: "s1"}
		LinkTask(ctx, "island:t1", linkT0)
		if ApplyTaskMarker(ctx, "s1", "t2", linkT0.Add(time.Minute)) {
			t.Fatal("manual link in this session wins")
		}
	})
	t.Run("manual link from a previous session is replaced by a new session marker", func(t *testing.T) {
		ctx := &Context{SessionID: "s1"}
		LinkTask(ctx, "island:t1", linkT0)
		ctx.SessionID = "s2"
		if !ApplyTaskMarker(ctx, "s2", "t2", linkT0.Add(time.Hour)) || ctx.TaskRef != "island:t2" || ctx.TaskLinkSource != "marker" {
			t.Fatalf("got %+v", ctx)
		}
	})
}

func TestLinkAndUnlinkTask(t *testing.T) {
	ctx := &Context{SessionID: "s1"}
	LinkTask(ctx, "island:t3", linkT0)
	if ctx.TaskRef != "island:t3" || ctx.TaskLinkSource != "manual" || ctx.TaskLinkSession != "s1" || !ctx.TaskLinkAt.Equal(linkT0) {
		t.Fatalf("link: %+v", ctx)
	}
	UnlinkTask(ctx)
	if ctx.TaskRef != "" || ctx.TaskLinkSource != "" || ctx.TaskLinkSession != "" || !ctx.TaskLinkAt.IsZero() {
		t.Fatalf("unlink: %+v", ctx)
	}
}

func TestContextTaskLinkYAML(t *testing.T) {
	data, err := yaml.Marshal(Context{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"task_ref", "task_link_session", "task_link_source", "task_link_at"} {
		if strings.Contains(string(data), k) {
			t.Errorf("%s should be omitted when empty:\n%s", k, data)
		}
	}
	data, _ = yaml.Marshal(Context{Name: "a", TaskRef: "island:t1", TaskLinkSession: "s", TaskLinkSource: "marker", TaskLinkAt: linkT0})
	var back Context
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.TaskRef != "island:t1" || back.TaskLinkSession != "s" || back.TaskLinkSource != "marker" || !back.TaskLinkAt.Equal(linkT0) {
		t.Fatalf("round trip: %+v", back)
	}
}

// リンクは context の紐付けだけを書き換える。タスクの完了状態（islands.yaml）はそもそも引数に取らない。
func TestApplyTaskMarker_DoesNotTouchOtherContextFields(t *testing.T) {
	ctx := &Context{SessionID: "s1", Status: StatusReview, Phase: PhasePROpen, AgentState: AgentRunning}
	before := *ctx
	ApplyTaskMarker(ctx, "s1", "t1", linkT0)
	ctx.TaskRef, ctx.TaskLinkSession, ctx.TaskLinkSource, ctx.TaskLinkAt = "", "", "", time.Time{}
	if !reflect.DeepEqual(*ctx, before) {
		t.Fatalf("other fields changed: %+v", ctx)
	}
}
