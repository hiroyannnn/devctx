package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/hiroyannnn/devctx/model"
	"github.com/hiroyannnn/devctx/storage"
	"github.com/spf13/cobra"
)

type DiscoveredSession struct {
	Provider       model.Provider
	SessionID      string
	SessionName    string // Claude Code's auto-generated name (slug)
	TranscriptPath string
	ProjectPath    string // The actual project directory
	ProjectHash    string // The hash used in ~/.claude/projects/
	LastModified   time.Time
	MessageCount   int
	IsRegistered   bool

	// Branch は Codex では Discover が session_meta から入れ、空のときだけ resolveSessionPlacement が
	// git から埋める。Claude は resolveSessionPlacement が埋める。
	Branch string
	// Worktree / RepoRoot は取り込み先の配置で、git 呼び出しを伴うため contexts.yaml のロック外で
	// resolveSessionPlacement が埋める。空なら mergeDiscoveredSessions が ProjectPath を使う。
	Worktree string
	RepoRoot string
}

var (
	discoverImport   bool
	discoverAll      bool
	discoverProvider string
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover existing Claude Code and Codex sessions",
	Long: `Scan ~/.claude/projects/ and ~/.codex/sessions/ ($CODEX_HOME) to find existing
Claude Code and Codex sessions. Codex sessions are limited to interactive ones
from the last 14 days (codex exec, subagents and other automated sessions are skipped).

This helps you see sessions that weren't registered with devctx hooks.
Use --provider to restrict the scan (claude|codex).
Use --import to automatically register discovered sessions.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := storage.New()
		if err != nil {
			return err
		}
		store, err := s.LoadStore()
		if err != nil {
			return err
		}

		adapters, err := selectAdapters(discoverProvider)
		if err != nil {
			return err
		}
		sessions, err := discoverFromAdapters(adapters, store)
		if err != nil {
			return err
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions found.")
			return nil
		}

		// Filter to unregistered only unless --all
		var displaySessions []DiscoveredSession
		for _, sess := range sessions {
			if discoverAll || !sess.IsRegistered {
				displaySessions = append(displaySessions, sess)
			}
		}

		if len(displaySessions) == 0 {
			fmt.Println("All sessions are already registered.")
			return nil
		}

		// Display
		displayDiscoveredSessions(displaySessions)

		// Import if requested
		if discoverImport {
			return importSessions(s, displaySessions)
		}

		fmt.Println()
		fmt.Println("Run 'devctx discover --import' to register these sessions.")

		return nil
	},
}

func extractProjectPath(transcriptPath string) string {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Look for cwd in the first few lines
	lineCount := 0
	for scanner.Scan() && lineCount < 50 {
		lineCount++
		var msg struct {
			Cwd string `json:"cwd"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil {
			if msg.Cwd != "" {
				return msg.Cwd
			}
		}
	}

	return ""
}

func extractSessionName(transcriptPath string) string {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Look for slug in the first few lines
	lineCount := 0
	for scanner.Scan() && lineCount < 100 {
		lineCount++
		var msg struct {
			Slug string `json:"slug"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &msg); err == nil {
			if msg.Slug != "" {
				return msg.Slug
			}
		}
	}

	return ""
}

func countMessages(transcriptPath string) int {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return 0
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		count++
	}

	return count
}

func displayDiscoveredSessions(sessions []DiscoveredSession) {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	registeredStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	newStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	fmt.Printf("Found %d session(s):\n\n", len(sessions))

	for i, sess := range sessions {
		// Limit display
		if i >= 20 {
			fmt.Printf("... and %d more sessions\n", len(sessions)-20)
			break
		}

		statusTag := newStyle.Render("[NEW]")
		if sess.IsRegistered {
			statusTag = registeredStyle.Render("[registered]")
		}

		sessionShort := sess.SessionID
		if len(sessionShort) > 12 {
			sessionShort = sessionShort[:12] + "..."
		}

		fmt.Printf("%s %s %s\n", titleStyle.Render(sessionShort), pathStyle.Render("("+string(sess.Provider)+")"), statusTag)

		if sess.SessionName != "" {
			nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Italic(true)
			fmt.Printf("  💬 %s\n", nameStyle.Render(sess.SessionName))
		}

		if sess.ProjectPath != "" {
			fmt.Printf("  📁 %s\n", pathStyle.Render(shortenPath(sess.ProjectPath)))
		}

		if sess.MessageCount > 0 {
			fmt.Printf("  📝 %d messages  ⏱ %s\n",
				sess.MessageCount,
				pathStyle.Render(formatRelativeTime(sess.LastModified)))
		} else {
			// Codex は行数を数えない（遅いため）ので件数は出さない
			fmt.Printf("  ⏱ %s\n", pathStyle.Render(formatRelativeTime(sess.LastModified)))
		}
		fmt.Println()
	}
}

func importSessions(s *storage.Storage, sessions []DiscoveredSession) error {
	imported, _, err := importDiscovered(s, sessions)
	if err != nil {
		return err
	}

	for _, name := range imported {
		fmt.Printf("✓ Imported [%s]\n", name)
	}
	if len(imported) > 0 {
		fmt.Printf("\n✓ Imported %d session(s)\n", len(imported))
	}
	return nil
}

// importDiscovered は探索結果を store に取り込んで保存する。保存した store を返し、変更がなければ nil。
func importDiscovered(s *storage.Storage, sessions []DiscoveredSession) (imported []string, updated *model.Store, err error) {
	// git 呼び出しはロック外で済ませる（ロック保持中に外部コマンドを走らせない）
	resolved := resolvePlacements(sessions)
	err = s.UpdateStore(func(store *model.Store) error {
		var changed bool
		imported, changed = mergeDiscoveredSessions(store, resolved, time.Now())
		if !changed {
			return storage.ErrSkipSave
		}
		updated = store
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return imported, updated, nil
}

// selectAdapters は --provider の値から探索対象の adapter を選ぶ。空なら全 adapter。
func selectAdapters(provider string) ([]ProviderAdapter, error) {
	if provider == "" {
		return providerAdapters(), nil
	}
	p, err := model.ParseProvider(provider)
	if err != nil {
		return nil, err
	}
	adapter, ok := adapterFor(p)
	if !ok {
		return nil, fmt.Errorf("provider %q has no session discovery", p)
	}
	return []ProviderAdapter{adapter}, nil
}

// discoverFromAdapters は adapter ごとの探索結果を LastModified の新しい順に束ねる。
func discoverFromAdapters(adapters []ProviderAdapter, store *model.Store) ([]DiscoveredSession, error) {
	var all []DiscoveredSession
	for _, a := range adapters {
		sessions, err := a.Discover(store)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.Provider(), err)
		}
		all = append(all, sessions...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].LastModified.After(all[j].LastModified)
	})
	return all, nil
}

// resolvePlacements は各セッションの取り込み先を解決する（git を呼ぶのでロック外で使う）。
func resolvePlacements(sessions []DiscoveredSession) []DiscoveredSession {
	resolved := make([]DiscoveredSession, len(sessions))
	for i, sess := range sessions {
		resolved[i] = resolveSessionPlacement(sess)
	}
	return resolved
}

// resolveSessionPlacement は取り込み先の worktree / repo / branch を git から解決する。
// git 管理外のときの Worktree は ProjectPath へのフォールバックを mergeDiscoveredSessions に一本化しているので、ここでは埋めない。
func resolveSessionPlacement(sess DiscoveredSession) DiscoveredSession {
	// 登録済みは merge で LastSeen しか使わないので、git を呼ばない
	if sess.ProjectPath == "" || sess.IsRegistered {
		return sess
	}
	switch sess.Provider {
	case model.ProviderCodex:
		// Codex の cwd は worktree 配下のサブディレクトリのことがあるので、worktree ルートに寄せる
		sess.Worktree = getWorktreeRoot(sess.ProjectPath)
		if sess.Worktree == "" {
			return sess
		}
		sess.RepoRoot = detectRepoRoot(sess.Worktree)
		// Why not always read the checkout: the repo may have moved to another branch since the
		// session ran; session_meta.git.branch is the branch the session actually worked on
		if sess.Branch == "" {
			sess.Branch = getGitBranch(sess.Worktree)
		}
	default:
		// Claude は従来どおり cwd をそのまま worktree として扱う（merge 側のフォールバックに任せる）
		sess.Branch = getGitBranch(sess.ProjectPath)
	}
	return sess
}

// mergeDiscoveredSessions は探索結果を store に反映する純粋なマージ。
// 登録済みセッションは LastSeen を mtime が新しい場合のみ進め、AgentState など他の項目には触れない
// （hook が書いた状態を探索結果で上書きしないため）。
// 戻り値は新規取り込みした context 名と、store が変わったか（保存要否）。
func mergeDiscoveredSessions(store *model.Store, sessions []DiscoveredSession, now time.Time) ([]string, bool) {
	var imported []string
	changed := false

	for _, sess := range sessions {
		if sess.SessionID == "" {
			continue
		}
		if existing := store.FindByProviderSession(sess.Provider, sess.SessionID); existing != nil {
			if sess.LastModified.After(existing.LastSeen) {
				existing.LastSeen = sess.LastModified
				changed = true
			}
			continue
		}

		worktree := sess.Worktree
		if worktree == "" {
			worktree = sess.ProjectPath
		}

		ctx := model.Context{
			Worktree:       worktree,
			Branch:         sess.Branch,
			RepoRoot:       sess.RepoRoot,
			SessionID:      sess.SessionID,
			SessionName:    sess.SessionName,
			TranscriptPath: sess.TranscriptPath,
			Status:         model.StatusInProgress,
			CreatedAt:      sess.LastModified,
			LastSeen:       sess.LastModified,
			Checklist:      make(map[string]bool),
		}
		if tracksPerSession(sess.Provider) {
			ctx.Provider = sess.Provider
			ctx.Name = uniqueContextName(store, generateName(sess.Branch, worktree), sess.Provider, now)
		} else {
			// claude は既存データと同じく provider を空のまま保存する
			ctx.Name = uniqueClaudeImportName(store, worktree, sess.SessionID)
		}

		store.Add(ctx)
		imported = append(imported, ctx.Name)
		changed = true
	}
	return imported, changed
}

// uniqueClaudeImportName は従来どおりディレクトリ名を使い、衝突したら session ID 先頭 6 文字を足す。
// それでも衝突する場合に備えて連番で一意にする。
func uniqueClaudeImportName(store *model.Store, projectPath, sessionID string) string {
	short := sessionID
	if len(short) > 6 {
		short = short[:6]
	}
	return uniqueNameWithSuffix(store, generateNameFromPath(projectPath, sessionID), short)
}

func generateNameFromPath(projectPath, sessionID string) string {
	if projectPath != "" {
		// Use directory name
		name := filepath.Base(projectPath)
		if name != "" && name != "." && name != "/" {
			return name
		}
	}

	// Fallback to session ID prefix
	if len(sessionID) > 8 {
		return "session-" + sessionID[:8]
	}
	return "session-" + sessionID
}

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().BoolVar(&discoverImport, "import", false, "Import discovered sessions")
	discoverCmd.Flags().StringVar(&discoverProvider, "provider", "", "Limit discovery to a provider (claude|codex); default: all")
	discoverCmd.Flags().BoolVar(&discoverAll, "all", false, "Show all sessions including registered ones")
}
