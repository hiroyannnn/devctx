package cmd

import "testing"

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
