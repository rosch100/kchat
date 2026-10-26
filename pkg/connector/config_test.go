// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	"testing"

	up "go.mau.fi/util/configupgrade"
	"gopkg.in/yaml.v3"
)

func TestConfigUnmarshalYAML(t *testing.T) {
	t.Parallel()
	input := `
server_url: http://mm.local:8065
displayname_template: "{{.Nickname}} (MM)"
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("UnmarshalYAML: %v", err)
	}
	if cfg.ServerURL != "http://mm.local:8065" {
		t.Errorf("ServerURL: got %q, want %q", cfg.ServerURL, "http://mm.local:8065")
	}
	if cfg.DisplaynameTemplate != "{{.Nickname}} (MM)" {
		t.Errorf("DisplaynameTemplate: got %q", cfg.DisplaynameTemplate)
	}
}

func TestConfigPostProcess(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		DisplaynameTemplate: "{{.FirstName}} {{.LastName}}",
	}
	if err := cfg.PostProcess(); err != nil {
		t.Fatalf("PostProcess: %v", err)
	}
	if cfg.displaynameTemplate == nil {
		t.Error("displaynameTemplate should not be nil after PostProcess")
	}
}

func TestConfigPostProcessInvalidTemplate(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		DisplaynameTemplate: "{{.Bad",
	}
	if err := cfg.PostProcess(); err == nil {
		t.Error("PostProcess should return error for invalid template")
	}
}

func TestFormatDisplayname(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		tmpl   string
		params DisplaynameParams
		want   string
	}{
		{
			name:   "nickname only",
			tmpl:   "{{.Nickname}} (MM)",
			params: DisplaynameParams{Nickname: "JohnD"},
			want:   "JohnD (MM)",
		},
		{
			name:   "full name",
			tmpl:   "{{.FirstName}} {{.LastName}}",
			params: DisplaynameParams{FirstName: "John", LastName: "Doe"},
			want:   "John Doe",
		},
		{
			name:   "username fallback in template",
			tmpl:   "{{.Username}}",
			params: DisplaynameParams{Username: "johnd"},
			want:   "johnd",
		},
		{
			name:   "empty params use zero values",
			tmpl:   "[{{.Nickname}}]",
			params: DisplaynameParams{},
			want:   "[]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{DisplaynameTemplate: tt.tmpl}
			if err := cfg.PostProcess(); err != nil {
				t.Fatalf("PostProcess: %v", err)
			}
			got := cfg.FormatDisplayname(tt.params)
			if got != tt.want {
				t.Errorf("FormatDisplayname: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDisplayname_NilTemplate(t *testing.T) {
	t.Parallel()
	cfg := &Config{} // PostProcess not called — template is nil
	got := cfg.FormatDisplayname(DisplaynameParams{Username: "fallback_user"})
	if got != "fallback_user" {
		t.Errorf("nil template should fall back to Username: got %q, want %q", got, "fallback_user")
	}
}

func TestUpgradeConfig(t *testing.T) {
	t.Parallel()
	// Parse the example config as the base.
	var baseNode yaml.Node
	if err := yaml.Unmarshal([]byte(ExampleConfig), &baseNode); err != nil {
		t.Fatalf("failed to parse base config: %v", err)
	}

	// Parse a user config with overridden values.
	userCfg := `
server_url: http://custom:8065
displayname_template: "{{.Nickname}}"
bot_prefix: "bridge_"
admin_api_addr: ":9999"
`
	var cfgNode yaml.Node
	if err := yaml.Unmarshal([]byte(userCfg), &cfgNode); err != nil {
		t.Fatalf("failed to parse user config: %v", err)
	}

	helper := up.NewHelper(&baseNode, &cfgNode)
	upgradeConfig(helper)

	// Verify the base was updated with user config values.
	if val, ok := helper.Get(up.Str, "server_url"); !ok || val != "http://custom:8065" {
		t.Errorf("server_url after upgrade: got %q, ok=%v", val, ok)
	}
	if val, ok := helper.Get(up.Str, "displayname_template"); !ok || val != "{{.Nickname}}" {
		t.Errorf("displayname_template after upgrade: got %q, ok=%v", val, ok)
	}
}

func TestConfigUnmarshalYAML_NewFields(t *testing.T) {
	t.Parallel()
	input := `
server_url: http://mm.local:8065
displayname_template: "{{.Nickname}} (MM)"
backfill_enabled: true
backfill_max_count: 250
typing_timeout: 10
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("UnmarshalYAML: %v", err)
	}
	if !cfg.BackfillEnabled {
		t.Error("BackfillEnabled: got false, want true")
	}
	if cfg.BackfillMaxCount != 250 {
		t.Errorf("BackfillMaxCount: got %d, want 250", cfg.BackfillMaxCount)
	}
	if cfg.TypingTimeout != 10 {
		t.Errorf("TypingTimeout: got %d, want 10", cfg.TypingTimeout)
	}
}

func TestConfigDefaults(t *testing.T) {
	t.Parallel()
	input := `
server_url: http://mm.local:8065
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatalf("UnmarshalYAML: %v", err)
	}
	if cfg.BackfillEnabled {
		t.Error("BackfillEnabled should default to false")
	}
	if cfg.BackfillMaxCount != 0 {
		t.Errorf("BackfillMaxCount should default to 0, got %d", cfg.BackfillMaxCount)
	}
	if cfg.TypingTimeout != 0 {
		t.Errorf("TypingTimeout should default to 0, got %d", cfg.TypingTimeout)
	}
}

func TestExampleConfigNotEmpty(t *testing.T) {
	t.Parallel()
	if ExampleConfig == "" {
		t.Error("ExampleConfig should not be empty (embedded from example-config.yaml)")
	}
}

// TestFormatDisplayname_SpecialCharacters verifies that template rendering
// handles special characters (unicode, HTML, template syntax) without panicking.
func TestFormatDisplayname_SpecialCharacters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		params DisplaynameParams
	}{
		{"unicode", DisplaynameParams{Username: "user\U0001f600emoji"}},
		{"html entities", DisplaynameParams{Username: "<script>alert(1)</script>"}},
		{"null bytes", DisplaynameParams{Username: "user\x00name"}},
		{"very long", DisplaynameParams{Username: string(make([]byte, 1000))}},
	}

	cfg := &Config{DisplaynameTemplate: "{{.Username}}"}
	if err := cfg.PostProcess(); err != nil {
		t.Fatalf("PostProcess: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Should not panic for any input.
			got := cfg.FormatDisplayname(tt.params)
			if got == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

// Note: FuzzFormatDisplayname is defined in fuzz_test.go with a more
// comprehensive corpus including arbitrary template strings.
