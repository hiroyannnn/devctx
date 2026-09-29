package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/roadmap"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

type SessionStartInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Source         string `json:"source"` // "startup", "resume", "clear"
}

var registerCmd = &cobra.Command{
	Use:   "register [name]",
	Short: "Register current worktree with an agent session",
	Long: `Register the current directory as a development context.
If called from a Claude Code hook, reads session info from stdin.
If called manually, uses current directory and prompts for name.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}

		var input SessionStartInput
		var name string

		// Check if stdin has data (called from hook)
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			// Reading from pipe (hook mode)
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
					return fmt.Errorf("failed to parse hook input: %w", err)
				}
			}
		}

		// Get working directory
		cwd := input.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}

		// Detect git info
		branch := getGitBranch(cwd)
		worktreeRoot := getWorktreeRoot(cwd)
		if worktreeRoot != "" {
			cwd = worktreeRoot
		}

		// Detect repo root for project grouping
		repoRoot := detectRepoRoot(cwd)

		provider, err := model.ParseProvider(registerProvider)
		if err != nil {
			return err
		}

		// Determine name hint (used only when a new context is created)
		if len(args) > 0 {
			name = args[0]
		} else {
			name = generateName(branch, cwd)
		}

		// Session name is extracted from Claude Code transcripts only
		sessionName := ""
		if provider == model.ProviderClaude && input.TranscriptPath != "" {
			sessionName = extractSessionName(input.TranscriptPath)
		}

		var ctx model.Context
		var created bool
		err = s.UpdateStore(func(store *model.Store) error {
			registered, isNew := upsertRegistration(store, registration{
				Name:           name,
				Worktree:       cwd,
				Branch:         branch,
				RepoRoot:       repoRoot,
				Provider:       provider,
				SessionID:      input.SessionID,
				SessionName:    sessionName,
				TranscriptPath: input.TranscriptPath,
			}, time.Now())
			ctx, created = *registered, isNew
			return nil
		})
		if err != nil {
			return err
		}

		if err := refreshPhaseOutsideLock(s, ctx); err != nil {
			return err
		}
		if !created {
			// Collect git milestones
			collectAndSaveMilestones(s, &ctx)
		}

		// Record session_start event
		recordEvent(s, ctx.Name, model.MilestoneSessionStart, "")

		if !created {
			fmt.Printf("Updated context [%s]\n", ctx.Name)
			return nil
		}

		fmt.Printf("Registered new context [%s] (%s)\n", ctx.Name, provider)
		fmt.Printf("  Worktree: %s\n", cwd)
		fmt.Printf("  Branch: %s\n", branch)
		if input.SessionID != "" {
			fmt.Printf("  Session: %s\n", input.SessionID[:min(8, len(input.SessionID))])
		}

		return nil
	},
}

var registerProvider string

func init() {
	registerCmd.Flags().StringVar(&registerProvider, "provider", "claude", "Agent provider (claude/codex/manual)")
}

// registration は register コマンドが context に反映する入力。
type registration struct {
	Name           string // 新規作成時の名前の候補
	Worktree       string
	Branch         string
	RepoRoot       string
	Provider       model.Provider
	SessionID      string
	SessionName    string
	TranscriptPath string
}

// upsertRegistration は (worktree, provider) が一致する context を更新し、なければ作成する。
// 同じ worktree でも provider が違えば別の context として扱う。
func upsertRegistration(store *model.Store, reg registration, now time.Time) (*model.Context, bool) {
	if existing := store.FindByWorktreeAndProvider(reg.Worktree, reg.Provider); existing != nil {
		if reg.SessionID != "" {
			existing.SessionID = reg.SessionID
			existing.TranscriptPath = reg.TranscriptPath
			if reg.SessionName != "" {
				existing.SessionName = reg.SessionName
			}
			// A (re)started session has not been observed yet; the previous session's
			// state (e.g. ended / turn_done) would otherwise linger until the next prompt.
			existing.SetAgentState("", time.Time{})
		}
		existing.LastSeen = now
		if reg.Branch != "" {
			existing.Branch = reg.Branch
		}
		if reg.RepoRoot != "" {
			existing.RepoRoot = reg.RepoRoot
		}
		return existing, false
	}

	ctx := model.Context{
		Name:           uniqueContextName(store, reg.Name, reg.Provider, now),
		Worktree:       reg.Worktree,
		Branch:         reg.Branch,
		SessionID:      reg.SessionID,
		SessionName:    reg.SessionName,
		TranscriptPath: reg.TranscriptPath,
		Status:         model.StatusInProgress,
		CreatedAt:      now,
		LastSeen:       now,
		Checklist:      make(map[string]bool),
		RepoRoot:       reg.RepoRoot,
	}
	// claude は既存データと同じく provider を空のまま保存する
	if reg.Provider != model.ProviderClaude {
		ctx.Provider = reg.Provider
	}
	store.Add(ctx)
	return &store.Contexts[len(store.Contexts)-1], true
}

// uniqueContextName は既存の context と衝突しない名前を返す。
// 衝突時は claude なら日付、それ以外は provider 名を付け、それでも衝突すれば連番を足す。
func uniqueContextName(store *model.Store, base string, provider model.Provider, now time.Time) string {
	if store.FindByName(base) == nil {
		return base
	}
	suffix := now.Format("0102")
	if provider != model.ProviderClaude {
		suffix = string(provider)
	}
	candidate := base + "-" + suffix
	for i := 2; store.FindByName(candidate) != nil; i++ {
		candidate = fmt.Sprintf("%s-%s-%d", base, suffix, i)
	}
	return candidate
}

func getGitBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getWorktreeRoot(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func generateName(branch, dir string) string {
	if branch != "" && branch != "main" && branch != "master" {
		// Use last part of branch name
		parts := strings.Split(branch, "/")
		name := parts[len(parts)-1]
		// Shorten if too long
		if len(name) > 20 {
			name = name[:20]
		}
		return name
	}
	// Use directory name
	return filepath.Base(dir)
}

func detectRepoRoot(dir string) string {
	// For worktrees, find the main repository root
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	commonDir := strings.TrimSpace(string(out))
	// commonDir is like /path/to/repo/.git - get parent
	if strings.HasSuffix(commonDir, "/.git") {
		return strings.TrimSuffix(commonDir, "/.git")
	}
	// Fallback: use toplevel
	return getWorktreeRoot(dir)
}

// refreshPhaseOutsideLock は git を使う phase 判定を contexts.yaml のロック外で行い、
// 結果の Phase / PhaseCheckedAt だけを短いロックで書き戻す。
// Why not UpdateStore の中で判定する: git の起動（最大数回）の間、高頻度の hook（touch）が待たされる。
func refreshPhaseOutsideLock(s *storage.Storage, ctx model.Context) error {
	roadmap.NewScanner().RefreshPhase(&ctx, roadmap.ScanModeFast)
	return s.UpdateStore(func(store *model.Store) error {
		stored := store.FindByName(ctx.Name)
		if stored == nil {
			return storage.ErrSkipSave
		}
		stored.Phase, stored.PhaseCheckedAt = ctx.Phase, ctx.PhaseCheckedAt
		return nil
	})
}

func collectAndSaveMilestones(s *storage.Storage, ctx *model.Context) {
	events, err := s.LoadEvents()
	if err != nil {
		return
	}
	collector := roadmap.NewMilestoneCollector()
	newEvents := collector.CollectGitMilestones(ctx, events)
	for _, e := range newEvents {
		_ = s.AppendEvent(e)
	}
}

func recordEvent(s *storage.Storage, sessionName string, mtype model.MilestoneType, detail string) {
	now := time.Now()
	_ = s.AppendEvent(model.SessionEvent{
		SessionName: sessionName,
		Type:        mtype,
		Detail:      detail,
		OccurredAt:  now,
		ObservedAt:  now,
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
