// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inference

import (
	"encoding/json"
	"errors"
	"path/filepath"
)

const (
	codexEnvKey    = "VERDA_INFERENCE_API_KEY"
	codexProfile   = "verda"
	codexAliasName = "codex-verda"

	claudeAliasName = "claude-verda"
)

func codexHome(env *hostEnv) string {
	if dir := env.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(env.Home, ".codex")
}

func codexConfigPath(env *hostEnv) string { return filepath.Join(codexHome(env), "config.toml") }

// Codex reads per-profile overrides from $CODEX_HOME/<profile>.config.toml.
func codexProfilePath(env *hostEnv) string {
	return filepath.Join(codexHome(env), codexProfile+".config.toml")
}

func codexOwns(key []string) bool {
	return len(key) >= 2 && key[0] == "model_providers" && key[1] == providerID
}

func writeCodex(c *agentContext) ([]fileChange, error) {
	path := codexConfigPath(c.env)
	old, exists, err := readFileIfExists(path)
	if err != nil {
		return nil, err
	}
	block := renderTOMLTable(tomlKey("model_providers", providerID), []tomlKV{
		{"name", "Verda"},
		{"base_url", c.openAIBaseURL()},
		{"env_key", codexEnvKey},
		{"wire_api", "responses"},
	})
	out, err := editTOMLFile(path, old, codexOwns, block)
	if err != nil {
		return nil, err
	}
	m := c.primaryModel()
	kvs := []tomlKV{{"model_provider", providerID}, {"model", m.EndpointModel}}
	if m.ContextLength != nil {
		kvs = append(kvs, tomlKV{"model_context_window", *m.ContextLength})
	}
	profile := "# Managed by `verda inference setup`; rewritten on every run.\n" + renderTOMLKVs(kvs)
	pc, err := ownedChange(c, codexProfilePath(c.env), []byte(profile))
	if err != nil {
		return nil, err
	}
	pc.Secret = false
	return []fileChange{
		{Path: path, Kind: fileKindPatched, Old: old, Exists: exists, New: out, Mode: 0o600, Created: c.created(path, exists)},
		pc,
	}, nil
}

func cleanCodex(c *agentContext) ([]fileChange, error) {
	out, err := cleanTOML(c, codexConfigPath(c.env), codexOwns)
	if err != nil {
		return nil, err
	}
	pc, err := deleteOwnedChange(c, codexProfilePath(c.env))
	if err != nil {
		return nil, err
	}
	return append(out, pc), nil
}

func codexHints(_ *agentContext) []string {
	return []string{codexAliasName + "   (open a new shell first)"}
}

func codexAlias(c *agentContext) (string, error) {
	key, err := shellPath(c.env, c.env.keyFile(agentCodex))
	if err != nil {
		return "", err
	}
	return "alias " + codexAliasName + `='` + codexEnvKey + `="$(cat ` + key + `)" codex --profile ` + codexProfile + `'`, nil
}

func claudeCodeSettingsPath(env *hostEnv) string {
	return filepath.Join(env.agentsDir(), agentClaudeCode+".json")
}

type claudeCodeSettings struct {
	Env map[string]string `json:"env"`
}

// writeClaudeCode: a --settings file of our own; ~/.claude/settings.json is never touched.
func writeClaudeCode(c *agentContext) ([]fileChange, error) {
	model := c.primaryModel().EndpointModel
	s := claudeCodeSettings{Env: map[string]string{
		"ANTHROPIC_BASE_URL":                     c.baseURL,
		"ANTHROPIC_AUTH_TOKEN":                   c.key,
		"ANTHROPIC_MODEL":                        model,
		"ANTHROPIC_DEFAULT_OPUS_MODEL":           model,
		"ANTHROPIC_DEFAULT_SONNET_MODEL":         model,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":          model,
		"CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS": "1",
	}}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	ch, err := ownedChange(c, claudeCodeSettingsPath(c.env), append(data, '\n'))
	if err != nil {
		return nil, err
	}
	return []fileChange{ch}, nil
}

func cleanClaudeCode(c *agentContext) ([]fileChange, error) {
	ch, err := deleteOwnedChange(c, claudeCodeSettingsPath(c.env))
	if err != nil {
		return nil, err
	}
	return []fileChange{ch}, nil
}

func claudeCodeKey(c *agentContext) (string, error) {
	data, exists, err := readFileIfExists(claudeCodeSettingsPath(c.env))
	if err != nil || !exists {
		return "", errors.Join(err, errors.New("claude code settings missing"))
	}
	var s claudeCodeSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return "", err
	}
	if s.Env["ANTHROPIC_AUTH_TOKEN"] == "" {
		return "", errors.New("ANTHROPIC_AUTH_TOKEN missing")
	}
	return s.Env["ANTHROPIC_AUTH_TOKEN"], nil
}

func claudeCodeHints(_ *agentContext) []string {
	return []string{claudeAliasName + "   (open a new shell first)"}
}

func claudeCodeAlias(c *agentContext) (string, error) {
	path, err := shellPath(c.env, claudeCodeSettingsPath(c.env))
	if err != nil {
		return "", err
	}
	return "alias " + claudeAliasName + `='claude --settings ` + path + `'`, nil
}
