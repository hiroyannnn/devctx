package model

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEffectiveProvider(t *testing.T) {
	if got := (Context{}).EffectiveProvider(); got != ProviderClaude {
		t.Fatalf("empty provider = %q, want %q", got, ProviderClaude)
	}
	if got := (Context{Provider: ProviderCodex}).EffectiveProvider(); got != ProviderCodex {
		t.Fatalf("codex provider = %q, want %q", got, ProviderCodex)
	}
}

func TestParseProvider(t *testing.T) {
	tests := []struct {
		in      string
		want    Provider
		wantErr bool
	}{
		{in: "", want: ProviderClaude},
		{in: "claude", want: ProviderClaude},
		{in: "codex", want: ProviderCodex},
		{in: "Codex", want: ProviderCodex},
		{in: "manual", want: ProviderManual},
		{in: "cursor", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseProvider(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("ParseProvider(%q) error = nil, want error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseProvider(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("ParseProvider(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestContextYAMLKeepsLegacyFormat(t *testing.T) {
	legacy, err := yaml.Marshal(Context{Name: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy), "provider") {
		t.Fatalf("legacy context should not emit provider key:\n%s", legacy)
	}

	var loaded Context
	if err := yaml.Unmarshal([]byte("name: auth\nprovider: codex\n"), &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Provider != ProviderCodex {
		t.Fatalf("loaded provider = %q, want %q", loaded.Provider, ProviderCodex)
	}
}

func TestStoreProviderAwareFinders(t *testing.T) {
	store := &Store{
		Contexts: []Context{
			{Name: "feat-x", SessionID: "s1", Worktree: "/tmp/feat-x"},
			{Name: "feat-x-0926", SessionID: "s1", Worktree: "/tmp/feat-x", Provider: ProviderCodex},
			{Name: "other", SessionID: "s2", Worktree: "/tmp/other"},
		},
	}

	all := store.FindAllByWorktree("/tmp/feat-x")
	if len(all) != 2 || all[0].Name != "feat-x" || all[1].Name != "feat-x-0926" {
		t.Fatalf("FindAllByWorktree = %v, want [feat-x feat-x-0926]", names(all))
	}
	all[1].Note = "mutated"
	if store.Contexts[1].Note != "mutated" {
		t.Fatalf("FindAllByWorktree should return pointers into the store")
	}

	if got := store.FindByWorktreeAndProvider("/tmp/feat-x", ProviderClaude); got == nil || got.Name != "feat-x" {
		t.Fatalf("FindByWorktreeAndProvider(claude) = %v, want feat-x", got)
	}
	if got := store.FindByWorktreeAndProvider("/tmp/feat-x", ProviderCodex); got == nil || got.Name != "feat-x-0926" {
		t.Fatalf("FindByWorktreeAndProvider(codex) = %v, want feat-x-0926", got)
	}
	if got := store.FindByWorktreeAndProvider("/tmp/other", ProviderCodex); got != nil {
		t.Fatalf("FindByWorktreeAndProvider(other, codex) = %v, want nil", got.Name)
	}

	if got := store.FindByProviderSession(ProviderCodex, "s1"); got == nil || got.Name != "feat-x-0926" {
		t.Fatalf("FindByProviderSession(codex, s1) = %v, want feat-x-0926", got)
	}
	if got := store.FindByProviderSession(ProviderClaude, "s1"); got == nil || got.Name != "feat-x" {
		t.Fatalf("FindByProviderSession(claude, s1) = %v, want feat-x", got)
	}
	if got := store.FindByProviderSession(ProviderClaude, ""); got != nil {
		t.Fatalf("FindByProviderSession with empty id = %v, want nil", got.Name)
	}
}

func names(contexts []*Context) []string {
	var result []string
	for _, c := range contexts {
		result = append(result, c.Name)
	}
	return result
}
