package cmd

import (
	"fmt"
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
