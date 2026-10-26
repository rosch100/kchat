// Copyright 2024-2026 Remi Philippe
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package connector

import (
	_ "embed"
	"text/template"

	up "go.mau.fi/util/configupgrade"
	"gopkg.in/yaml.v3"
)

//go:embed example-config.yaml
var ExampleConfig string

// Config holds the kChat connector configuration.
type Config struct {
	ServerURL           string `yaml:"server_url"`
	DisplaynameTemplate string `yaml:"displayname_template"`
	// BotPrefix is a username prefix for echo prevention. Any kChat
	// username starting with this prefix is treated as a bridge-managed bot
	// and its posts are not relayed back to Matrix. Leave empty to disable
	// prefix-based filtering.
	BotPrefix string `yaml:"bot_prefix"`
	// AdminAPIAddr is the listen address for the admin HTTP API that serves
	// the /api/reload-puppets endpoint. Defaults to ":29320".
	AdminAPIAddr string `yaml:"admin_api_addr"`

	BackfillEnabled  bool `yaml:"backfill_enabled"`
	BackfillMaxCount int  `yaml:"backfill_max_count"`
	TypingTimeout    int  `yaml:"typing_timeout"`

	displaynameTemplate *template.Template `yaml:"-"`
}

// DisplaynameParams holds the parameters for rendering the displayname template.
type DisplaynameParams struct {
	Username  string
	Nickname  string
	FirstName string
	LastName  string
}

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	type rawConfig Config
	return node.Decode((*rawConfig)(c))
}

func (c *Config) PostProcess() error {
	var err error
	c.displaynameTemplate, err = template.New("displayname").Parse(c.DisplaynameTemplate)
	return err
}

func upgradeConfig(helper up.Helper) {
	helper.Copy(up.Str, "server_url")
	helper.Copy(up.Str, "displayname_template")
	helper.Copy(up.Str, "bot_prefix")
	helper.Copy(up.Str, "admin_api_addr")
	helper.Copy(up.Bool, "backfill_enabled")
	helper.Copy(up.Int, "backfill_max_count")
	helper.Copy(up.Int, "typing_timeout")
}

func (mc *KChatConnector) GetConfig() (example string, data any, upgrader up.Upgrader) {
	return ExampleConfig, &mc.Config, &up.StructUpgrader{
		SimpleUpgrader: up.SimpleUpgrader(upgradeConfig),
		Blocks:         nil,
		Base:           ExampleConfig,
	}
}

// FormatDisplayname generates a display name from the template and params.
func (c *Config) FormatDisplayname(params DisplaynameParams) string {
	if c.displaynameTemplate == nil {
		return params.Username
	}
	var buf []byte
	err := c.displaynameTemplate.Execute(
		(*templateBuffer)(&buf),
		params,
	)
	if err != nil {
		return params.Username
	}
	return string(buf)
}

// templateBuffer is a simple io.Writer that appends to a byte slice.
type templateBuffer []byte

func (b *templateBuffer) Write(p []byte) (int, error) {
	*b = append(*b, p...)
	return len(p), nil
}
