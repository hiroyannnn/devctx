package agentview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

func TestParse(t *testing.T) {
	raw := []byte(`[
	 {"pid":1,"cwd":"/w/a","kind":"interactive","startedAt":1791032973049,"sessionId":"s1","name":"A","status":"busy","future":{"x":1}},
	 {"pid":2,"cwd":"/w/b","sessionId":"s2","status":"waiting","waitingFor":"permission prompt"},
	 {"pid":3,"cwd":"/w/c","status":"idle"},
	 {"pid":4,"cwd":"/w/d","sessionId":"s4"},
	 {"pid":5,"cwd":"/w/e","sessionId":"s5","status":"idle","state":"done"}
	]`)
	got, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("sessionId/status 欠落行は無視される想定: %+v", got)
	}
	if got[0].SessionID != "s1" || got[0].Status != "busy" || got[0].PID != 1 || got[0].Name != "A" {
		t.Errorf("row0: %+v", got[0])
	}
	if want := time.UnixMilli(1791032973049); !got[0].StartedAt.Equal(want) {
		t.Errorf("StartedAt = %v", got[0].StartedAt)
	}
	if got[1].WaitingFor != "permission prompt" {
		t.Errorf("row1: %+v", got[1])
	}
	if got[2].State != "done" {
		t.Errorf("row2: %+v", got[2])
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte(`not json`)); err == nil {
		t.Error("不正 JSON はエラー")
	}
	got, err := Parse([]byte(`[]`))
	if err != nil || len(got) != 0 {
		t.Errorf("空配列は正常: %v %v", got, err)
	}
}

func TestFetch(t *testing.T) {
	t0 := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	now := func() time.Time { return t0 }

	t.Run("ok", func(t *testing.T) {
		snap := fetchAt(context.Background(), func(context.Context) ([]byte, error) {
			return []byte(`[{"sessionId":"s1","status":"busy"}]`), nil
		}, now)
		if !snap.OK || len(snap.Sessions) != 1 || !snap.FetchedAt.Equal(t0) {
			t.Errorf("%+v", snap)
		}
	})
	t.Run("empty array is still OK", func(t *testing.T) {
		snap := fetchAt(context.Background(), func(context.Context) ([]byte, error) { return []byte(`[]`), nil }, now)
		if !snap.OK || len(snap.Sessions) != 0 {
			t.Errorf("%+v", snap)
		}
	})
	t.Run("runner error", func(t *testing.T) {
		snap := fetchAt(context.Background(), func(context.Context) ([]byte, error) { return nil, errors.New("boom") }, now)
		if snap.OK {
			t.Errorf("%+v", snap)
		}
	})
	t.Run("parse error", func(t *testing.T) {
		snap := fetchAt(context.Background(), func(context.Context) ([]byte, error) { return []byte(`xx`), nil }, now)
		if snap.OK {
			t.Errorf("%+v", snap)
		}
	})
}

func TestMapStatus(t *testing.T) {
	cases := []struct {
		in     Session
		state  model.AgentState
		reason string
		ok     bool
	}{
		{Session{Status: "busy"}, model.AgentRunning, "", true},
		{Session{Status: "waiting", WaitingFor: "input needed"}, model.AgentNeedsInput, "input needed", true},
		{Session{Status: "idle"}, model.AgentTurnDone, "", true},
		{Session{Status: "weird"}, "", "", false},
		{Session{}, "", "", false},
	}
	for _, c := range cases {
		st, reason, ok := c.in.AgentState()
		if st != c.state || reason != c.reason || ok != c.ok {
			t.Errorf("%+v => %v %q %v", c.in, st, reason, ok)
		}
	}
}

// 実 claude は絶対に呼ばない。PATH を差し替えた fake で引数と出力上限だけを確認する。
func TestDefaultRunnerWithFakeClaude(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n[ \"$1 $2\" = \"agents --json\" ] || exit 3\necho '[{\"sessionId\":\"s\",\"status\":\"busy\"}]'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin:/bin")

	snap := Fetch(context.Background(), nil)
	if !snap.OK || len(snap.Sessions) != 1 {
		t.Fatalf("%+v", snap)
	}

	big := "#!/bin/sh\nyes '[' | head -c 2000000\n"
	if err := os.WriteFile(fake, []byte(big), 0o755); err != nil {
		t.Fatal(err)
	}
	if snap := Fetch(context.Background(), nil); snap.OK {
		t.Errorf("出力上限超過は失敗扱い: %+v", snap)
	}
}

// claude の子孫プロセスが stdout を握り続けても、タイムアウトで Fetch が戻ること。
// WaitDelay 未設定だと cmd.Run は stdout のコピー完了を待ち続ける。
func TestFetchReturnsAfterTimeoutEvenIfDescendantHoldsStdout(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n(sleep 10) &\nsleep 10\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	start := time.Now()
	snap := Fetch(context.Background(), nil)
	elapsed := time.Since(start)

	if snap.OK {
		t.Error("タイムアウトした取得は OK=false")
	}
	if elapsed > 3*time.Second {
		t.Errorf("Fetch が %v かかった（子孫が stdout を握っていても 3s 以内に戻る想定）", elapsed)
	}
}
