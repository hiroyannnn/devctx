package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hiroyannnn/devctx/model"
)

// resolveContext は対象 context を決める。名前の指定を最優先し、
// 省略時は worktree から推測する。worktree に複数の context がある場合は推測せずエラーにする。
func resolveContext(store *model.Store, args []string, worktree string) (*model.Context, error) {
	if len(args) > 0 {
		ctx := store.FindByName(args[0])
		if ctx == nil {
			return nil, fmt.Errorf("context [%s] not found", args[0])
		}
		return ctx, nil
	}

	candidates := store.FindAllByWorktree(worktree)
	switch len(candidates) {
	case 0:
		return nil, fmt.Errorf("no context found for %s\nSpecify a name as argument or register with 'devctx register'", worktree)
	case 1:
		return candidates[0], nil
	default:
		labels := make([]string, len(candidates))
		for i, c := range candidates {
			labels[i] = fmt.Sprintf("%s (%s)", c.Name, c.EffectiveProvider())
		}
		return nil, fmt.Errorf("multiple contexts in this worktree: %s\nSpecify one as argument", strings.Join(labels, ", "))
	}
}

// resolveHookContext は Claude Code hook から呼ばれたときの対象 context を決める。
// 名前の指定がなく hook の session_id が Claude の context と一致すれば、worktree が曖昧でもそれを使う。
func resolveHookContext(store *model.Store, args []string, sessionID, worktree string) (*model.Context, error) {
	if len(args) == 0 {
		if ctx := store.FindByProviderSession(model.ProviderClaude, sessionID); ctx != nil {
			return ctx, nil
		}
	}
	return resolveContext(store, args, worktree)
}

// readHookSessionID は hook の stdin JSON から session_id を読む。読めなければ空文字を返す。
func readHookSessionID(r io.Reader) string {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return ""
	}
	var input struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
		return ""
	}
	return input.SessionID
}

// stdinIsPipe は stdin がパイプ（hook からの呼び出し）かを返す。
func stdinIsPipe() bool {
	stat, err := os.Stdin.Stat()
	return err == nil && (stat.Mode()&os.ModeCharDevice) == 0
}
