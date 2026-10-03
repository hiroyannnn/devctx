package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/hiroyannnn/devctx/model"
)

// buildDevctx は hook と同じく stdin をパイプで渡して実行するため、実バイナリをビルドする。
// Why not cobra の Execute をテスト内で呼ぶ: stdinIsPipe は os.Stdin を見るので、プロセスを分けないと hook 経路を再現できない。
func buildDevctx(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "devctx")
	if out, err := exec.Command("go", "build", "-o", bin, "..").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// runHook は隔離した HOME で devctx を実行し、stdin に hook の JSON を渡す。
func runHook(t *testing.T, bin, home, stdin string, args ...string) (stdout string, err error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "HOME="+home, "CODEX_HOME="+filepath.Join(home, ".codex"))
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	return out.String(), err
}

func loadStoreFromHome(t *testing.T, home string) model.Store {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".config", "devctx", "contexts.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var store model.Store
	if err := yaml.Unmarshal(data, &store); err != nil {
		t.Fatal(err)
	}
	return store
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "feature-x"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v\n%s", err, out)
		}
	}
	return dir
}

func TestRegisterCodexHook_RegistersSilently(t *testing.T) {
	bin := buildDevctx(t)
	home := t.TempDir()
	repo := initGitRepo(t)
	sub := filepath.Join(repo, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(home, "rollout.jsonl")

	stdout, err := runHook(t, bin, home,
		`{"session_id":"019a-codex","transcript_path":"`+transcript+`","cwd":"`+sub+`","hook_event_name":"SessionStart","source":"startup"}`,
		"register", "--provider", "codex")
	if err != nil {
		t.Fatal(err)
	}
	// Why: hook の stdout はモデルの context に追加される
	if stdout != "" {
		t.Fatalf("hook mode must not print to stdout, got %q", stdout)
	}

	store := loadStoreFromHome(t, home)
	if len(store.Contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(store.Contexts))
	}
	c := store.Contexts[0]
	if c.EffectiveProvider() != model.ProviderCodex || c.SessionID != "019a-codex" || c.TranscriptPath != transcript {
		t.Fatalf("unexpected context: %+v", c)
	}
	if c.Worktree != repo || c.Branch != "feature-x" {
		t.Fatalf("worktree=%q branch=%q, want %q feature-x", c.Worktree, c.Branch, repo)
	}
	if c.SessionName != "" {
		t.Fatalf("codex must not extract a Claude session name, got %q", c.SessionName)
	}

	// 同じセッションの再 register は更新であり、新規作成しない
	if _, err := runHook(t, bin, home,
		`{"session_id":"019a-codex","cwd":"`+sub+`","source":"resume"}`,
		"register", "--provider", "codex"); err != nil {
		t.Fatal(err)
	}
	if got := len(loadStoreFromHome(t, home).Contexts); got != 1 {
		t.Fatalf("contexts after resume = %d, want 1", got)
	}

	// touch --provider codex が同じ context を解決して状態を記録する
	if _, err := runHook(t, bin, home,
		`{"session_id":"019a-codex","hook_event_name":"PermissionRequest"}`,
		"touch", "--quick", "--track-state", "--provider", "codex"); err != nil {
		t.Fatal(err)
	}
	if got := loadStoreFromHome(t, home).Contexts[0].AgentState; got != model.AgentNeedsInput {
		t.Fatalf("agent state = %q, want needs_input", got)
	}
}

func TestRegisterClaudeHook_RegistersSilently(t *testing.T) {
	bin := buildDevctx(t)
	home := t.TempDir()
	repo := initGitRepo(t)

	stdout, err := runHook(t, bin, home,
		`{"session_id":"claude-1","cwd":"`+repo+`","source":"startup"}`, "register")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "" {
		t.Fatalf("hook mode must not print to stdout, got %q", stdout)
	}
	if got := len(loadStoreFromHome(t, home).Contexts); got != 1 {
		t.Fatalf("contexts = %d, want 1", got)
	}
}
