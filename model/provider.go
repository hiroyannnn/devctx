package model

import (
	"fmt"
	"strings"
)

// Provider はコーディングエージェントの種類。
type Provider string

const (
	ProviderClaude Provider = "claude"
	ProviderCodex  Provider = "codex"
	ProviderManual Provider = "manual"
)

// ParseProvider は CLI 入力などから Provider を解釈する。空文字は claude とみなす。
func ParseProvider(s string) (Provider, error) {
	switch p := Provider(strings.ToLower(strings.TrimSpace(s))); p {
	case "":
		return ProviderClaude, nil
	case ProviderClaude, ProviderCodex, ProviderManual:
		return p, nil
	default:
		return "", fmt.Errorf("unknown provider: %q (must be claude/codex/manual)", s)
	}
}

// EffectiveProvider は Provider 未設定の既存データを claude として扱う。
func (c Context) EffectiveProvider() Provider {
	if c.Provider == "" {
		return ProviderClaude
	}
	return c.Provider
}

// FindAllByWorktree は worktree に紐づく全 context を返す（同じ worktree に複数 provider が並びうる）。
func (s *Store) FindAllByWorktree(worktree string) []*Context {
	var result []*Context
	for i := range s.Contexts {
		if s.Contexts[i].Worktree == worktree {
			result = append(result, &s.Contexts[i])
		}
	}
	return result
}

// FindByWorktreeAndProvider は worktree と provider の組で context を探す。
func (s *Store) FindByWorktreeAndProvider(worktree string, provider Provider) *Context {
	for _, c := range s.FindAllByWorktree(worktree) {
		if c.EffectiveProvider() == provider {
			return c
		}
	}
	return nil
}

// FindByProviderSession は provider 側のセッション ID で context を探す。
func (s *Store) FindByProviderSession(provider Provider, sessionID string) *Context {
	if sessionID == "" {
		return nil
	}
	for i := range s.Contexts {
		c := &s.Contexts[i]
		if c.SessionID == sessionID && c.EffectiveProvider() == provider {
			return c
		}
	}
	return nil
}
