package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

const (
	codexLineDesktop    = `{"timestamp":"2026-10-03T12:27:38.918Z","type":"session_meta","payload":{"session_id":"019a0000-0000-7000-8000-000000000001","id":"019a0000-0000-7000-8000-000000000001","timestamp":"2026-10-03T12:27:38.918Z","cwd":"/Users/me/proj","git":{"branch":"feat/x","commit_hash":"abc","repository_url":"git@github.com:o/r.git"},"originator":"Codex Desktop","source":"vscode","thread_source":"user","base_instructions":{"text":"large"}}}`
	codexLineExec       = `{"timestamp":"2026-10-03T12:27:38.918Z","type":"session_meta","payload":{"id":"e1","timestamp":"2026-10-03T12:27:38.918Z","cwd":"/w","git":null,"originator":"codex_exec","source":"exec"}}`
	codexLineGuardian   = `{"timestamp":"2026-10-03T12:27:38.918Z","type":"session_meta","payload":{"id":"g1","timestamp":"2026-10-03T12:27:38.918Z","cwd":"/w","git":null,"originator":"Codex Desktop","source":{"subagent":{"other":"guardian"}},"thread_source":"guardian_review"}}`
	codexLineLegacyCLI  = `{"timestamp":"2026-10-03T12:27:38.918Z","type":"session_meta","payload":{"id":"c1","timestamp":"2026-10-03T12:27:38.918Z","cwd":"/w","originator":"codex_cli_rs","source":"cli"}}`
	codexLineLegacyExec = `{"timestamp":"2026-10-03T12:27:38.918Z","type":"session_meta","payload":{"id":"c2","cwd":"/w","originator":"codex_cli_rs","source":"exec"}}`
	codexLineFromClaude = `{"timestamp":"2026-10-03T12:27:38.918Z","type":"session_meta","payload":{"id":"c3","cwd":"/w","originator":"Claude Code","source":"cli"}}`
)

func TestParseCodexSessionMeta(t *testing.T) {
	meta, err := parseCodexSessionMeta([]byte(codexLineDesktop))
	if err != nil {
		t.Fatal(err)
	}
	if meta.SessionID != "019a0000-0000-7000-8000-000000000001" || meta.Cwd != "/Users/me/proj" ||
		meta.GitBranch != "feat/x" || meta.Originator != "Codex Desktop" ||
		meta.Source != "vscode" || meta.ThreadSource != "user" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if meta.Timestamp.IsZero() || meta.Timestamp.UTC().Format("2006-01-02T15:04:05Z") != "2026-10-03T12:27:38Z" {
		t.Fatalf("timestamp = %v", meta.Timestamp)
	}
}

func TestParseCodexSessionMeta_ObjectSourceAndNullGit(t *testing.T) {
	meta, err := parseCodexSessionMeta([]byte(codexLineGuardian))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Source != "" || meta.GitBranch != "" || meta.ThreadSource != "guardian_review" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestParseCodexSessionMeta_Errors(t *testing.T) {
	for name, line := range map[string]string{
		"not json":   "oops",
		"wrong type": `{"type":"event_msg","payload":{"id":"x"}}`,
		"missing id": `{"type":"session_meta","payload":{"cwd":"/w"}}`,
	} {
		if _, err := parseCodexSessionMeta([]byte(line)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestIsInteractiveCodexSession(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"desktop user thread", codexLineDesktop, true},
		{"exec", codexLineExec, false},
		{"guardian review with object source", codexLineGuardian, false},
		{"legacy cli without thread_source", codexLineLegacyCLI, true},
		{"legacy exec without thread_source", codexLineLegacyExec, false},
		{"spawned by Claude Code", codexLineFromClaude, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := parseCodexSessionMeta([]byte(tt.line))
			if err != nil {
				t.Fatal(err)
			}
			if got := isInteractiveCodexSession(meta); got != tt.want {
				t.Fatalf("isInteractiveCodexSession = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsInteractiveCodexSession_OtherThreadSources(t *testing.T) {
	for _, ts := range []string{"automation", "subagent", "realtime_voice", "unknown"} {
		if isInteractiveCodexSession(codexSessionMeta{ThreadSource: ts, Source: "vscode", Originator: "Codex Desktop"}) {
			t.Errorf("thread_source %q should not be interactive", ts)
		}
	}
}

// --- codexAdapter ---

func writeRollout(t *testing.T, home, day, id, metaLine string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", day)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-"+strings.ReplaceAll(day, "/", "-")+"T00-00-00-"+id+".jsonl")
	body := metaLine + "\n" + `{"type":"event_msg","payload":{}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func metaLine(id, cwd, threadSource, source string) string {
	return `{"type":"session_meta","payload":{"id":"` + id + `","timestamp":"2026-10-03T00:00:00Z","cwd":"` + cwd +
		`","git":null,"originator":"Codex Desktop","source":` + source + `,"thread_source":"` + threadSource + `"}}`
}

func TestCodexAdapter_Discover(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Hour)

	pRecent := writeRollout(t, home, "2026/10/03", "id-recent", metaLine("id-recent", "/w/a", "user", `"vscode"`), recent)
	writeRollout(t, home, "2026/10/03", "id-auto", metaLine("id-auto", "/w/a", "automation", `"vscode"`), recent)
	writeRollout(t, home, "2026/10/02", "id-guard", metaLine("id-guard", "/w/a", "guardian_review", `{"subagent":{"other":"guardian"}}`), recent)
	pOld := writeRollout(t, home, "2026/08/01", "id-old-active", metaLine("id-old-active", "/w/b", "user", `"cli"`), now.Add(-30*time.Minute))
	writeRollout(t, home, "2026/08/01", "id-old-stale", metaLine("id-old-stale", "/w/c", "user", `"cli"`), now.Add(-1000*time.Hour))
	writeRollout(t, home, "2026/09/01", "id-old-registered", metaLine("id-old-registered", "/w/d", "user", `"cli"`), now.Add(-900*time.Hour))

	index := strings.Join([]string{
		`{"id":"id-recent","thread_name":"Recent work","updated_at":"2026-10-03T11:00:00Z"}`,
		`{"id":"id-old-active","thread_name":"Old but active","updated_at":"2026-10-03T11:30:00Z"}`,
		`{"id":"id-old-stale","thread_name":"Stale","updated_at":"2026-08-01T11:30:00Z"}`,
		`not json`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	store := &model.Store{}
	store.Add(model.Context{Name: "reg", Provider: model.ProviderCodex, SessionID: "id-recent"})

	a := codexAdapter{home: home, days: 14, now: func() time.Time { return now }}
	got, err := a.Discover(store)
	if err != nil {
		t.Fatal(err)
	}

	var ids []string
	for _, s := range got {
		ids = append(ids, s.SessionID)
	}
	want := []string{"id-old-active", "id-recent"} // LastModified desc
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", ids, want)
	}

	old := got[0]
	if old.Provider != model.ProviderCodex || old.SessionName != "Old but active" || old.TranscriptPath != pOld ||
		old.ProjectPath != "/w/b" || old.MessageCount != 0 || old.IsRegistered || !old.LastModified.Equal(now.Add(-30*time.Minute)) {
		t.Fatalf("unexpected: %+v", old)
	}
	rec := got[1]
	if rec.TranscriptPath != pRecent || rec.SessionName != "Recent work" || !rec.IsRegistered {
		t.Fatalf("unexpected: %+v", rec)
	}
}

func TestCodexAdapter_Discover_NoHome(t *testing.T) {
	a := codexAdapter{home: filepath.Join(t.TempDir(), "missing"), days: 14, now: time.Now}
	got, err := a.Discover(&model.Store{})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestCodexAdapter_AgentCommand(t *testing.T) {
	a := codexAdapter{}
	if got, _ := a.AgentCommand(model.Context{SessionID: "id'1"}); got != `codex resume 'id'\''1'` {
		t.Fatalf("got %q", got)
	}
	if got, _ := a.AgentCommand(model.Context{}); got != "codex" {
		t.Fatalf("got %q", got)
	}
}

func TestNewCodexAdapter_HomeFromEnv(t *testing.T) {
	t.Setenv("CODEX_HOME", "/custom/codex")
	if a := newCodexAdapter(); a.home != "/custom/codex" || a.days != 14 {
		t.Fatalf("unexpected: %+v", a)
	}
}

func TestCodexAdapter_Discover_UsesSessionBranch(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	line := `{"type":"session_meta","payload":{"id":"id-br","timestamp":"2026-10-03T00:00:00Z","cwd":"/w/a",` +
		`"git":{"branch":"feat/start","commit_hash":"abc"},"originator":"Codex Desktop","source":"vscode","thread_source":"user"}}`
	writeRollout(t, home, "2026/10/03", "id-br", line, now.Add(-time.Hour))

	sessions, err := codexAdapter{home: home, days: 14, now: func() time.Time { return now }}.Discover(&model.Store{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Branch != "feat/start" {
		t.Fatalf("Branch should come from session_meta git.branch (the branch the session ran on), got %+v", sessions)
	}
}

func TestCodexAdapter_Discover_SkipResumedSearch(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	writeRollout(t, home, "2026/08/01", "id-old-active", metaLine("id-old-active", "/w/b", "user", `"cli"`), now.Add(-30*time.Minute))
	idx := `{"id":"id-old-active","thread_name":"old","updated_at":"2026-10-03T11:30:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), []byte(idx), 0o644); err != nil {
		t.Fatal(err)
	}

	full, _ := codexAdapter{home: home, days: 2, now: func() time.Time { return now }}.Discover(&model.Store{})
	quick, _ := codexAdapter{home: home, days: 2, now: func() time.Time { return now }, skipResumedSearch: true}.Discover(&model.Store{})
	if len(full) != 1 || len(quick) != 0 {
		t.Fatalf("full=%d quick=%d; the resumed-session search (all day dirs) should be skipped only when requested", len(full), len(quick))
	}
}

func TestRolloutSessionID(t *testing.T) {
	id, ok := rolloutSessionID("/x/sessions/2026/10/03/rollout-2026-10-03T21-27-38-01a101bb-fa25-77c1-9dd5-5246825f6982.jsonl")
	if !ok || id != "01a101bb-fa25-77c1-9dd5-5246825f6982" {
		t.Fatalf("id=%q ok=%v", id, ok)
	}
	if _, ok := rolloutSessionID("/x/notes.jsonl"); ok {
		t.Fatalf("non-rollout file should not match")
	}
}
