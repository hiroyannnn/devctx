package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClassifyPending(t *testing.T) {
	tests := []struct {
		name        string
		tool        string
		input       string
		wantKind    PendingKind
		wantSummary string
	}{
		{"bash with description", "Bash", `{"command":"go test ./...","description":"Run tests"}`, PendingBash, "Run tests"},
		{"bash secret-ish command keeps program name only", "Bash", `{"command":"curl -H \"Authorization: Bearer xxx\" https://x"}`, PendingBash, "curl"},
		{"bash path program uses basename", "Bash", `{"command":"/usr/local/bin/npm install"}`, PendingBash, "npm"},
		{"bash multi-line uses first line", "Bash", `{"command":"\n  make build\nrm -rf /"}`, PendingBash, "make"},
		{"codex command array", "Bash", `{"command":["git","push","origin"]}`, PendingBash, "git"},
		{"codex command string", "Bash", `{"command":"git push origin"}`, PendingBash, "git"},
		{"bash missing fields", "Bash", `{}`, PendingBash, ""},
		{"bash empty input", "Bash", ``, PendingBash, ""},
		{"bash invalid json", "Bash", `{`, PendingBash, ""},
		{"description truncated by runes", "Bash", `{"description":"` + strings.Repeat("あ", 100) + `"}`, PendingBash, strings.Repeat("あ", 79) + "…"},
		{"network approval", "Bash", `{"command":"curl x","description":"network-access example.com"}`, PendingNetwork, "example.com"},
		{"edit", "Edit", `{"file_path":"/a/b/main.go","old_string":"x","new_string":"y"}`, PendingEdit, "main.go"},
		{"write", "Write", `{"file_path":"/a/b/c.txt","content":"secret"}`, PendingEdit, "c.txt"},
		{"multiedit", "MultiEdit", `{"file_path":"/a/b/m.go"}`, PendingEdit, "m.go"},
		{"notebook", "NotebookEdit", `{"notebook_path":"/a/n.ipynb"}`, PendingEdit, "n.ipynb"},
		{"apply_patch update", "apply_patch", `{"command":"*** Begin Patch\n*** Update File: src/app/x.go\n@@\n-a\n+b\n*** End Patch"}`, PendingEdit, "x.go"},
		{"apply_patch add", "apply_patch", `{"command":"*** Begin Patch\n*** Add File: new.txt\n+hi"}`, PendingEdit, "new.txt"},
		{"apply_patch delete", "apply_patch", `{"command":"*** Delete File: d/old.txt"}`, PendingEdit, "old.txt"},
		{"apply_patch none", "apply_patch", `{"command":"garbage"}`, PendingEdit, ""},
		{"apply_patch array command", "apply_patch", `{"command":["apply_patch","*** Update File: z.go\n"]}`, PendingEdit, "z.go"},
		{"mcp", "mcp__github__create_issue", `{"title":"secret"}`, PendingMCP, "github/create_issue"},
		{"unknown tool", "write_stdin", `{"chars":"x"}`, PendingOther, ""},
		{"ask user question", "AskUserQuestion", `{"questions":[{"question":"どれ？","header":"方式","options":[]}]}`, PendingQuestion, "方式"},
		{"ask user question no header", "AskUserQuestion", `{"questions":[{"question":"q"}]}`, PendingQuestion, ""},
		{"ask header truncated", "AskUserQuestion", `{"questions":[{"header":"` + strings.Repeat("あ", 40) + `"}]}`, PendingQuestion, strings.Repeat("あ", 29) + "…"},
		{"request_user_input", "request_user_input", `{"questions":[{"header":"Pick"}]}`, PendingQuestion, "Pick"},
		{"plan", "ExitPlanMode", `{"plan":"# big markdown"}`, PendingPlan, ""},
		{"webfetch host only", "WebFetch", `{"url":"https://user:pw@example.com:8080/path?token=abc"}`, PendingNetwork, "example.com:8080"},
		{"webfetch invalid url", "WebFetch", `{"url":"::"}`, PendingNetwork, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyPending(tt.tool, json.RawMessage(tt.input))
			if got.Tool != tt.tool || got.Kind != tt.wantKind || got.Summary != tt.wantSummary {
				t.Fatalf("got tool=%q kind=%q summary=%q, want kind=%q summary=%q", got.Tool, got.Kind, got.Summary, tt.wantKind, tt.wantSummary)
			}
		})
	}
}

func TestClassifyPending_InputHash(t *testing.T) {
	a := ClassifyPending("Bash", json.RawMessage(`{"command":"ls"}`))
	b := ClassifyPending("Bash", json.RawMessage(`{"command":"ls"}`))
	c := ClassifyPending("Bash", json.RawMessage(`{"command":"pwd"}`))
	if len(a.InputHash) != 16 || a.InputHash != b.InputHash || a.InputHash == c.InputHash {
		t.Fatalf("hashes: %q %q %q", a.InputHash, b.InputHash, c.InputHash)
	}
	if got := ClassifyPending("Bash", nil).InputHash; got != "" {
		t.Errorf("empty input should have empty hash, got %q", got)
	}
}

// PreToolUse / PermissionRequest と PostToolUse で tool_input の直列化（キー順・空白）が違っても同じ要求とみなす
func TestClassifyPending_InputHashIgnoresJSONFormatting(t *testing.T) {
	a := ClassifyPending("request_user_input", json.RawMessage(`{"questions":[{"header":"H","question":"Q"}],"id":1}`))
	b := ClassifyPending("request_user_input", json.RawMessage("{ \"id\": 1,\n \"questions\": [ {\"question\":\"Q\", \"header\":\"H\"} ] }"))
	if a.InputHash != b.InputHash {
		t.Errorf("hash differs by formatting: %q vs %q", a.InputHash, b.InputHash)
	}
	if !a.matches("request_user_input", json.RawMessage(`{"id":1,"questions":[{"question":"Q","header":"H"}]}`)) {
		t.Error("reordered PostToolUse input should match the pending request")
	}
}

func TestPendingRequestLabel(t *testing.T) {
	tests := []struct {
		p    PendingRequest
		want string
	}{
		{PendingRequest{Tool: "Bash", Kind: PendingBash, Summary: "Run tests"}, "Bash: Run tests"},
		{PendingRequest{Tool: "Bash", Kind: PendingBash}, "Bash"},
		{PendingRequest{Tool: "mcp__github__x", Kind: PendingMCP, Summary: "github/x"}, "github/x"},
		{PendingRequest{Tool: "mcp__github__x", Kind: PendingMCP}, "mcp__github__x"},
	}
	for _, tt := range tests {
		if got := tt.p.Label(); got != tt.want {
			t.Errorf("Label() = %q, want %q", got, tt.want)
		}
	}
}
