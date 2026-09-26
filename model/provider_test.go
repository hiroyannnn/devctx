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
