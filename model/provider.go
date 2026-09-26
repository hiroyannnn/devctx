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
