package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hiroyannnn/devctx/model"
)

// codexSessionMeta は rollout ファイル 1 行目 (session_meta) のうち devctx が使う項目。
type codexSessionMeta struct {
	SessionID    string
	Cwd          string
	GitBranch    string
	Originator   string
	Source       string // source が文字列のときだけ入る（オブジェクトは種別判定に使わないので空）
	ThreadSource string
	Timestamp    time.Time
}

// parseCodexSessionMeta は rollout の 1 行目を解釈する。
// Codex のバージョンで項目が増減するため、必要な項目以外は厳密に検証しない。
func parseCodexSessionMeta(line []byte) (codexSessionMeta, error) {
	var raw struct {
		Type    string `json:"type"`
		Payload struct {
			ID           string          `json:"id"`
			SessionID    string          `json:"session_id"`
			Timestamp    string          `json:"timestamp"`
			Cwd          string          `json:"cwd"`
			Originator   string          `json:"originator"`
			Source       json.RawMessage `json:"source"`
			ThreadSource string          `json:"thread_source"`
			Git          *struct {
				Branch string `json:"branch"`
			} `json:"git"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		return codexSessionMeta{}, err
	}
	if raw.Type != "session_meta" {
		return codexSessionMeta{}, errors.New("first line is not session_meta")
	}
	p := raw.Payload
	id := p.ID
	if id == "" {
		id = p.SessionID
	}
	if id == "" {
		return codexSessionMeta{}, errors.New("session_meta has no id")
	}

	meta := codexSessionMeta{
		SessionID:    id,
		Cwd:          p.Cwd,
		Originator:   p.Originator,
		ThreadSource: p.ThreadSource,
	}
	// source はオブジェクト（subagent 等）のこともあるため、文字列として読めたときだけ採用する
	var src string
	if json.Unmarshal(p.Source, &src) == nil {
		meta.Source = src
	}
	if p.Git != nil {
		meta.GitBranch = p.Git.Branch
	}
	if t, err := time.Parse(time.RFC3339Nano, p.Timestamp); err == nil {
		meta.Timestamp = t
	}
	return meta, nil
}

// isInteractiveCodexSession は人間が対話しているセッションだけを true にする。
// codex exec・guardian review・subagent 等の自動生成セッションは数が多く、
// カンバンに取り込むとノイズになるため除外する。
// thread_source は新しい Codex でのみ出力されるので、無い場合は source / originator で推定する。
func isInteractiveCodexSession(meta codexSessionMeta) bool {
	if meta.ThreadSource != "" {
		return meta.ThreadSource == "user"
	}
	return meta.Source != "exec" &&
		meta.Originator != "Claude Code" &&
		meta.Originator != "codex_exec"
}

const defaultCodexDiscoveryDays = 14

// codexAdapter は $CODEX_HOME/sessions の rollout から Codex セッションを探す。
type codexAdapter struct {
	home string
	days int
	now  func() time.Time
	// skipResumedSearch は「古い日付ディレクトリにあるが最近再開された」セッションの探索を省く。
	// この探索は全日付ディレクトリを走査するため、毎回走る list の同期では省き、
	// 取り込み済みセッションの更新は保存済みの TranscriptPath の mtime で追う。
	skipResumedSearch bool
	// skipRegistered は登録済みセッションの rollout を開かない（1 行目の読み込みも stat もしない）。
	// list の同期用: 登録済みの LastSeen は保存済みの TranscriptPath の mtime で追えるため、
	// 毎回の list で全ファイルを開くコストを省く。discover --all は登録済みも表示するので使わない。
	skipRegistered bool
}

// codexHomeDir は $CODEX_HOME（未設定なら ~/.codex）を返す。
func codexHomeDir() (string, error) {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// newCodexAdapter は codexHomeDir を見る adapter を返す。
// Why not エラーを返す: home が解決できなくても list / discover 全体は動かすべきで、その場合は Codex を空として扱う。
func newCodexAdapter() codexAdapter {
	home, _ := codexHomeDir()
	return codexAdapter{home: home, days: defaultCodexDiscoveryDays, now: time.Now}
}

func (codexAdapter) Provider() model.Provider { return model.ProviderCodex }

func (codexAdapter) AgentCommand(ctx model.Context) (string, error) {
	if ctx.SessionID != "" {
		return "codex resume " + shellQuote(ctx.SessionID), nil
	}
	return "codex", nil
}

// Discover は直近 days 日の対話セッションを返す。
// rollout は十数万ファイルになりうるため、全走査せず日付ディレクトリと
// session_index.jsonl の更新時刻から候補を絞り、各ファイルは 1 行目しか読まない。
func (a codexAdapter) Discover(store *model.Store) ([]DiscoveredSession, error) {
	sessionsDir := filepath.Join(a.home, "sessions")
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return nil, nil
	}

	now := a.now()
	// Why lazy: 同期で新規セッションが無く再開探索も省くときは、index を読む必要がない
	var index map[string]codexIndexEntry
	loadIndex := func() map[string]codexIndexEntry {
		if index == nil {
			index = readCodexIndex(filepath.Join(a.home, "session_index.jsonl"))
		}
		return index
	}
	cutoff := now.AddDate(0, 0, -a.days)

	var paths []string
	haveID := map[string]bool{}

	// ディレクトリ名はローカル日付と UTC 日付のどちらでもありうるので、前後 1 日の余裕を持たせる
	for i := -1; i <= a.days; i++ {
		day := now.AddDate(0, 0, -i)
		matches, _ := filepath.Glob(filepath.Join(sessionsDir, day.Format("2006"), day.Format("01"), day.Format("02"), "rollout-*.jsonl"))
		for _, m := range matches {
			paths = append(paths, m)
			if id, ok := rolloutSessionID(m); ok {
				haveID[id] = true
			}
		}
	}

	// 古い日付ディレクトリにあるが最近も更新されている（resume された）セッション。
	// 全日付ディレクトリの走査は高いので、対象 id をまとめて 1 回だけ glob する
	if !a.skipResumedSearch {
		missing := map[string]bool{}
		for id, entry := range loadIndex() {
			if !entry.UpdatedAt.Before(cutoff) && !haveID[id] {
				missing[id] = true
			}
		}
		if len(missing) > 0 {
			all, _ := filepath.Glob(filepath.Join(sessionsDir, "*", "*", "*", "rollout-*.jsonl"))
			for _, m := range all {
				if id, ok := rolloutSessionID(m); ok && missing[id] {
					paths = append(paths, m)
				}
			}
		}
	}

	var sessions []DiscoveredSession
	seenID := map[string]bool{}
	for _, path := range paths {
		if a.skipRegistered {
			if id, ok := rolloutSessionID(path); ok && store.FindByProviderSession(model.ProviderCodex, id) != nil {
				continue
			}
		}
		meta, err := readCodexSessionMeta(path)
		if err != nil || !isInteractiveCodexSession(meta) || seenID[meta.SessionID] {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		seenID[meta.SessionID] = true
		sessions = append(sessions, DiscoveredSession{
			Provider:       model.ProviderCodex,
			SessionID:      meta.SessionID,
			SessionName:    loadIndex()[meta.SessionID].ThreadName,
			TranscriptPath: path,
			ProjectPath:    meta.Cwd,
			Branch:         meta.GitBranch,
			LastModified:   info.ModTime(),
			IsRegistered:   store.FindByProviderSession(model.ProviderCodex, meta.SessionID) != nil,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastModified.After(sessions[j].LastModified)
	})
	return sessions, nil
}

// readCodexSessionMeta は rollout の 1 行目だけを読む。
// 1 行目は base_instructions で巨大になるため、行長に上限のある Scanner は使わない。
func readCodexSessionMeta(path string) (codexSessionMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return codexSessionMeta{}, err
	}
	defer f.Close()

	line, err := bufio.NewReader(f).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return codexSessionMeta{}, err
	}
	return parseCodexSessionMeta(line)
}

type codexIndexEntry struct {
	ThreadName string
	UpdatedAt  time.Time
}

// readCodexIndex は session_index.jsonl を id 引きできる形にする。
// 無い・壊れた行は無視する（index は表示名と更新時刻の補助情報でしかないため）。
func readCodexIndex(path string) map[string]codexIndexEntry {
	index := map[string]codexIndexEntry{}
	f, err := os.Open(path)
	if err != nil {
		return index
	}
	defer f.Close()

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		var raw struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
			UpdatedAt  string `json:"updated_at"`
		}
		if len(line) > 0 && json.Unmarshal(line, &raw) == nil && raw.ID != "" {
			entry := codexIndexEntry{ThreadName: raw.ThreadName}
			if t, perr := time.Parse(time.RFC3339Nano, raw.UpdatedAt); perr == nil {
				entry.UpdatedAt = t
			}
			// 同じ id が複数行ある場合は後勝ち（追記ログのため最新が後ろ）
			index[raw.ID] = entry
		}
		if err != nil {
			return index
		}
	}
}

// rolloutSessionID は rollout-<YYYY-MM-DDThh-mm-ss>-<id>.jsonl から id を取り出す。
// Why not split on the last "-": id 自体（UUID）が "-" を含む。
func rolloutSessionID(path string) (string, bool) {
	const prefixLen = len("rollout-") + len("2006-01-02T15-04-05") + len("-")
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "rollout-") || !strings.HasSuffix(base, ".jsonl") || len(base) <= prefixLen+len(".jsonl") {
		return "", false
	}
	return strings.TrimSuffix(base[prefixLen:], ".jsonl"), true
}
