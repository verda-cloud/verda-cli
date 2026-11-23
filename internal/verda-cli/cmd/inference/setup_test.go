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
	"os"
	"path/filepath"
	"strings"
	"testing"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", path, got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s should not exist (err=%v)", path, err)
	}
}

const userOpenCode = `{
  // personal config
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-4", // my default
  "provider": {
    "ollama": {
      "npm": "@ai-sdk/openai-compatible",
      "options": { "baseURL": "http://localhost:11434/v1" },
    },
  },
}
`

const userKimi = `# Kimi config
default_model = "kimi-for-coding"

[providers.moonshot]
type = "kimi"
base_url = "https://api.moonshot.ai/v1"
api_key = "sk-user"

# my models
[models.kimi-for-coding]
provider = "moonshot"
model = "kimi-for-coding"
max_context_size = 262144
`

func TestSetupFreshNonInteractiveJSON(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)

	out, _, err := runInference(t, api, h, runOpts{format: "json"}, "setup", "opencode", "kimi", "--model", "qwen3-coder", "--model", "kimi-k2", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	var res setupResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if res.DryRun || res.BaseURL != defaultInferenceBaseURL || len(res.Agents) != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Agents[0].KeyAction != keyActionCreate || res.Agents[0].KeyID == "" || res.Agents[0].KeyLabel != "verda-cli opencode (testhost)" {
		t.Errorf("opencode agent result: %+v", res.Agents[0])
	}
	if strings.Contains(out, "dcs_plaintext") {
		t.Errorf("setup JSON must not contain plaintext keys:\n%s", out)
	}
	if len(api.subscribed) != 1 || api.subscribed[0] != "qwen3-coder/standard" {
		t.Errorf("subscribed = %v, want only the missing qwen3-coder/standard", api.subscribed)
	}
	if len(api.created) != 2 {
		t.Fatalf("created keys = %v", api.created)
	}

	keyFile := filepath.Join(h.env.VerdaDir, "agents", "opencode.key")
	if got := readFile(t, keyFile); got != "dcs_plaintext0001" {
		t.Errorf("opencode key file = %q", got)
	}
	assertMode(t, keyFile, 0o600)
	assertMode(t, h.env.statePath(), 0o600)

	oc := readFile(t, h.path(".config/opencode/opencode.json"))
	for _, want := range []string{`"provider"`, `"npm": "@ai-sdk/openai-compatible"`, `"baseURL": "https://inference.verda.com/v1"`,
		`"apiKey": "{file:~/.verda/agents/opencode.key}"`, `"Qwen/Qwen3-Coder"`, `"context": 262144`} {
		if !strings.Contains(oc, want) {
			t.Errorf("opencode.json missing %s:\n%s", want, oc)
		}
	}
	if strings.Contains(oc, "dcs_") {
		t.Errorf("opencode.json must reference the key file, not inline the key:\n%s", oc)
	}

	kimi := readFile(t, h.path(".kimi/config.toml"))
	for _, want := range []string{"[providers.verda]", `type = "openai_legacy"`, `api_key = "dcs_plaintext0002"`,
		`[models."verda/qwen3-coder"]`, `model = "Qwen/Qwen3-Coder"`, "max_context_size = 262144", `[models."verda/kimi-k2"]`} {
		if !strings.Contains(kimi, want) {
			t.Errorf("kimi config missing %s:\n%s", want, kimi)
		}
	}
	assertMode(t, h.path(".kimi/config.toml"), 0o600)
	assertMissing(t, h.path(".zshrc"))
}

func TestSetupPreservesUserConfigAndRemoveRestoresIt(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	ocPath := h.path(".config/opencode/opencode.jsonc")
	kimiPath := h.path(".kimi/config.toml")
	writeFile(t, ocPath, userOpenCode)
	writeFile(t, kimiPath, userKimi)

	if _, _, err := runInference(t, api, h, runOpts{}, "setup", "opencode", "kimi", "--model", "kimi-k2", "--yes"); err != nil {
		t.Fatal(err)
	}
	oc := readFile(t, ocPath)
	for _, keep := range []string{"// personal config", `"model": "anthropic/claude-sonnet-4", // my default`, `"ollama"`, `"verda"`} {
		if !strings.Contains(oc, keep) {
			t.Errorf("opencode.jsonc lost %q:\n%s", keep, oc)
		}
	}
	kimi := readFile(t, kimiPath)
	if !strings.HasPrefix(kimi, userKimi) {
		t.Errorf("kimi config changed outside the verda tables:\n%s", kimi)
	}
	if got := readFile(t, ocPath+backupSuffix); got != userOpenCode {
		t.Errorf("opencode backup = %q", got)
	}
	if got := readFile(t, kimiPath+backupSuffix); got != userKimi {
		t.Errorf("kimi backup = %q", got)
	}

	out, _, err := runInference(t, api, h, runOpts{}, "remove", "opencode", "kimi", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, ocPath); got != userOpenCode {
		t.Errorf("remove did not restore opencode.jsonc\n--- want\n%s\n--- got\n%s", userOpenCode, got)
	}
	if got := readFile(t, kimiPath); got != userKimi {
		t.Errorf("remove did not restore kimi config\n--- want\n%s\n--- got\n%s", userKimi, got)
	}
	if api.keyCount() != 0 || len(api.deleted) != 2 {
		t.Errorf("keys left = %d, deleted = %v", api.keyCount(), api.deleted)
	}
	assertMissing(t, filepath.Join(h.env.VerdaDir, "agents", "opencode.key"))
	st, err := loadState(h.env.statePath())
	if err != nil || len(st.allAgents()) != 0 {
		t.Errorf("state still lists agents: %v (err %v)", st.allAgents(), err)
	}
	if !strings.Contains(out, "deleted API key") {
		t.Errorf("remove output: %s", out)
	}
}

func TestSetupRerunIsIdempotent(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	writeFile(t, h.path(".kimi/config.toml"), userKimi)
	args := []string{"setup", "opencode", "kimi", "--model", "kimi-k2", "--yes"}
	if _, _, err := runInference(t, api, h, runOpts{}, args...); err != nil {
		t.Fatal(err)
	}
	oc1, kimi1 := readFile(t, h.path(".config/opencode/opencode.json")), readFile(t, h.path(".kimi/config.toml"))

	out, _, err := runInference(t, api, h, runOpts{format: "json"}, "setup", "opencode", "kimi", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if len(api.created) != 2 {
		t.Errorf("re-run created keys: %v", api.created)
	}
	if oc2 := readFile(t, h.path(".config/opencode/opencode.json")); oc2 != oc1 {
		t.Errorf("opencode.json changed on re-run\n%s\n---\n%s", oc1, oc2)
	}
	if kimi2 := readFile(t, h.path(".kimi/config.toml")); kimi2 != kimi1 {
		t.Errorf("kimi config changed on re-run\n%s\n---\n%s", kimi1, kimi2)
	}
	var res setupResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	for _, a := range res.Agents {
		if a.KeyAction != keyActionReuse {
			t.Errorf("%s key action = %s, want reuse", a.Agent, a.KeyAction)
		}
		for _, f := range a.Files {
			if f.Action != actionUnchanged {
				t.Errorf("%s: %s %s on re-run", a.Agent, f.Action, f.Path)
			}
		}
	}
}

func TestSetupRotateReplacesKey(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	if _, _, err := runInference(t, api, h, runOpts{}, "setup", "opencode", "kimi", "--model", "kimi-k2", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runInference(t, api, h, runOpts{}, "setup", "opencode", "kimi", "--rotate", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(api.created) != 4 || api.keyCount() != 2 {
		t.Fatalf("created %v, live keys %d", api.created, api.keyCount())
	}
	if strings.Join(api.deleted, ",") != "key-1,key-2" {
		t.Errorf("deleted = %v, want the two original keys", api.deleted)
	}
	if got := readFile(t, filepath.Join(h.env.VerdaDir, "agents", "opencode.key")); got != "dcs_plaintext0003" {
		t.Errorf("opencode key not rotated: %q", got)
	}
	if kimi := readFile(t, h.path(".kimi/config.toml")); !strings.Contains(kimi, "dcs_plaintext0004") || strings.Contains(kimi, "dcs_plaintext0002") {
		t.Errorf("kimi key not rotated:\n%s", kimi)
	}
	st, _ := loadState(h.env.statePath())
	if rec := st.agent(defaultProfile, agentKimi); rec == nil || rec.KeyID != "key-4" {
		t.Errorf("state not updated: %+v", rec)
	}
}

func TestSetupReplacesKeyDeletedServerSide(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	if _, _, err := runInference(t, api, h, runOpts{}, "setup", "opencode", "--model", "kimi-k2", "--yes"); err != nil {
		t.Fatal(err)
	}
	api.keys = nil
	if _, _, err := runInference(t, api, h, runOpts{}, "setup", "opencode", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(api.created) != 2 {
		t.Errorf("expected a replacement key, created = %v", api.created)
	}
}

func TestSetupDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	h.vars["CODEX_HOME"] = h.path("codex")
	out, _, err := runInference(t, api, h, runOpts{}, "setup", "opencode", "kimi", "codex", "--experimental-agents", "--model", "qwen3-coder", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(h.env.Home)
	if len(entries) != 0 {
		t.Errorf("dry run wrote files: %v", entries)
	}
	if len(api.created) != 0 || len(api.subscribed) != 0 {
		t.Errorf("dry run called mutating APIs: keys %v, subs %v", api.created, api.subscribed)
	}
	for _, want := range []string{"Dry run", "subscribe the project to qwen3-coder", `create API key "verda-cli opencode (testhost)"`,
		"create ~/.config/opencode/opencode.json", "create ~/.kimi/config.toml", "alias block in ~/.zshrc"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
}

func TestSetupNonInteractiveInputErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		code string
	}{
		{"no agent", []string{"setup", "--model", "kimi-k2", "--yes"}, "MISSING_REQUIRED_FLAGS"},
		{"no model", []string{"setup", "opencode", "--yes"}, "MISSING_REQUIRED_FLAGS"},
		{"no yes", []string{"setup", "opencode", "--model", "kimi-k2"}, "CONFIRMATION_REQUIRED"},
		{"unknown agent", []string{"setup", "vim", "--yes"}, "VALIDATION_ERROR"},
		{"experimental hidden", []string{"setup", "codex", "--model", "kimi-k2", "--yes"}, "VALIDATION_ERROR"},
		{"unknown model", []string{"setup", "opencode", "--model", "nope", "--yes"}, "VALIDATION_ERROR"},
		{"coming soon", []string{"setup", "opencode", "--model", "future", "--yes"}, "VALIDATION_ERROR"},
		{"bad url", []string{"setup", "opencode", "--model", "kimi-k2", "--yes", "--inference-url", "ftp://x"}, "VALIDATION_ERROR"},
		{"remove without yes", []string{"remove", "opencode"}, "CONFIRMATION_REQUIRED"},
		{"remove without agent", []string{"remove", "--yes"}, "MISSING_REQUIRED_FLAGS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			api, h := newFakeAPI(), newTestHost(t)
			_, _, err := runInference(t, api, h, runOpts{agent: true, format: "json"}, tc.args...)
			if got := errCode(err); got != tc.code {
				t.Fatalf("code = %q, want %q (err: %v)", got, tc.code, err)
			}
			if len(api.created) != 0 {
				t.Errorf("keys created despite the error: %v", api.created)
			}
		})
	}
}

func TestSetupRefusesAgentOwnedByOtherProfile(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	st, _ := loadState(h.env.statePath())
	st.setAgent("staging", agentOpenCode, &agentRecord{KeyID: "key-x"})
	if err := saveState(h.env.statePath(), st); err != nil {
		t.Fatal(err)
	}
	_, _, err := runInference(t, api, h, runOpts{}, "setup", "opencode", "--model", "kimi-k2", "--yes")
	if err == nil || !strings.Contains(err.Error(), `profile "staging"`) {
		t.Fatalf("err = %v, want ownership error", err)
	}
}

func TestSetupFailureDeletesNewKey(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	writeFile(t, h.path(".kimi/config.toml"), "this is = = not toml\n")
	_, _, err := runInference(t, api, h, runOpts{}, "setup", "kimi", "--model", "kimi-k2", "--yes")
	if err == nil || !strings.Contains(err.Error(), "not valid TOML") {
		t.Fatalf("err = %v, want TOML error", err)
	}
	if len(api.created) != 0 {
		t.Errorf("a key was created although the config could not be edited: %v", api.created)
	}
	if got := readFile(t, h.path(".kimi/config.toml")); got != "this is = = not toml\n" {
		t.Errorf("broken config was rewritten: %q", got)
	}
}

func TestSetupOpenCodeV2(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	h.version = "opencode 2.0.3\n"
	path := h.path(".config/opencode/opencode.json")
	writeFile(t, path, "{\n  \"provider\": {\n    \"verda\": {\"npm\": \"old\"}\n  }\n}\n")
	if _, _, err := runInference(t, api, h, runOpts{}, "setup", "opencode", "--model", "qwen3-coder", "--yes", "--inference-url", "https://staging.example/v1/"); err != nil {
		t.Fatal(err)
	}
	oc := readFile(t, path)
	for _, want := range []string{`"providers"`, `"package": "` + openCodeV2Package + `"`, `"settings"`,
		`"baseURL": "https://staging.example/v1"`, `"qwen3-coder": {`, `"modelID": "Qwen/Qwen3-Coder"`} {
		if !strings.Contains(oc, want) {
			t.Errorf("v2 config missing %s:\n%s", want, oc)
		}
	}
	if strings.Contains(oc, `"old"`) {
		t.Errorf("stale v1 verda entry kept:\n%s", oc)
	}
}

func TestSetupExperimentalAgentsWriteAliasBlock(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	rc := h.path(".zshrc")
	writeFile(t, rc, "export PATH=$PATH:/opt/bin\n")
	if _, _, err := runInference(t, api, h, runOpts{}, "setup", "codex", "claude", "--experimental-agents", "--model", "qwen3-coder", "--yes"); err != nil {
		t.Fatal(err)
	}
	block := readFile(t, rc)
	for _, want := range []string{rcBlockStart, `alias codex-verda='VERDA_INFERENCE_API_KEY="$(cat "$HOME/.verda/agents/codex.key")" codex --profile verda'`,
		`alias claude-verda='claude --settings "$HOME/.verda/agents/claude-code.json"'`, rcBlockEnd} {
		if !strings.Contains(block, want) {
			t.Errorf(".zshrc missing %s:\n%s", want, block)
		}
	}
	codex := readFile(t, h.path(".codex/config.toml"))
	if !strings.Contains(codex, "[model_providers.verda]") || !strings.Contains(codex, `wire_api = "responses"`) || !strings.Contains(codex, `env_key = "VERDA_INFERENCE_API_KEY"`) {
		t.Errorf("codex config:\n%s", codex)
	}
	if prof := readFile(t, h.path(".codex/verda.config.toml")); !strings.Contains(prof, `model_provider = "verda"`) || !strings.Contains(prof, "model_context_window = 262144") {
		t.Errorf("codex profile:\n%s", prof)
	}
	settings := filepath.Join(h.env.VerdaDir, "agents", "claude-code.json")
	assertMode(t, settings, 0o600)
	var s claudeCodeSettings
	if err := json.Unmarshal([]byte(readFile(t, settings)), &s); err != nil {
		t.Fatal(err)
	}
	if s.Env["ANTHROPIC_BASE_URL"] != defaultInferenceBaseURL || s.Env["ANTHROPIC_MODEL"] != "Qwen/Qwen3-Coder" || !strings.HasPrefix(s.Env["ANTHROPIC_AUTH_TOKEN"], "dcs_") {
		t.Errorf("claude settings: %+v", s.Env)
	}

	if _, _, err := runInference(t, api, h, runOpts{}, "remove", "codex", "--yes"); err != nil {
		t.Fatal(err)
	}
	block = readFile(t, rc)
	if strings.Contains(block, "codex-verda") || !strings.Contains(block, "claude-verda") {
		t.Errorf("alias block after removing codex:\n%s", block)
	}
	assertMissing(t, h.path(".codex/verda.config.toml"))
	assertMissing(t, h.path(".codex/config.toml"))

	if _, _, err := runInference(t, api, h, runOpts{}, "remove", "claude-code", "--yes"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, rc); got != "export PATH=$PATH:/opt/bin\n" {
		t.Errorf(".zshrc not restored: %q", got)
	}
	assertMissing(t, settings)
}

func TestSetupAgentModeOutputHasNoPrompts(t *testing.T) {
	t.Parallel()
	api, h := newFakeAPI(), newTestHost(t)
	out, stderr, err := runInference(t, api, h, runOpts{agent: true, format: "json"}, "setup", "opencode", "--model", "kimi-k2", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("stdout is not JSON:\n%s", out)
	}
	if stderr != "" {
		t.Errorf("agent mode wrote to stderr: %s", stderr)
	}
}

// Not parallel: flips the package-level terminal detection.
func TestSetupInteractive(t *testing.T) {
	orig := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = orig })

	api, h := newFakeAPI(), newTestHost(t)
	h.installed = []string{"kimi"}
	p := tuitest.New().AddMultiSelect([]int{1}).AddMultiSelect([]int{0}).AddConfirm(true)
	out, stderr, err := runInference(t, api, h, runOpts{prompter: p}, "setup")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "Setup will:") || !strings.Contains(out, "Kimi Code CLI is connected") || !strings.Contains(out, "kimi -m verda/qwen3-coder") {
		t.Errorf("stdout:\n%s\nstderr:\n%s", out, stderr)
	}
	if len(api.subscribed) != 1 {
		t.Errorf("subscriptions = %v", api.subscribed)
	}

	declined := tuitest.New().AddMultiSelect([]int{0}).AddMultiSelect([]int{0}).AddConfirm(false)
	if _, _, err := runInference(t, api, h, runOpts{prompter: declined}, "setup"); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, h.path(".config/opencode/opencode.json"))
}

func TestKeyLabelLength(t *testing.T) {
	t.Parallel()
	if got := keyLabel("opencode", "host"); got != "verda-cli opencode (host)" {
		t.Errorf("label = %q", got)
	}
	long := keyLabel("claude-code", strings.Repeat("h", 300))
	if n := len([]rune(long)); n != maxKeyLabel || !strings.HasSuffix(long, ")") {
		t.Errorf("long label has %d runes: %q", n, long)
	}
}

func TestRCFileSelection(t *testing.T) {
	t.Parallel()
	h := newTestHost(t)
	exists := func(p string) bool { return p == h.path(".bashrc") }
	none := func(string) bool { return false }
	cases := []struct {
		shell, goos string
		exists      func(string) bool
		want        string
	}{
		{"/bin/zsh", "darwin", none, h.path(".zshrc")},
		{"/bin/bash", "linux", none, h.path(".bashrc")},
		{"/bin/bash", "darwin", none, h.path(".bash_profile")},
		{"/bin/bash", "darwin", exists, h.path(".bashrc")},
		{"/usr/bin/fish", "linux", none, ""},
	}
	for _, tc := range cases {
		h.vars["SHELL"], h.env.GOOS = tc.shell, tc.goos
		if got := rcFileFor(h.env, tc.exists); got != tc.want {
			t.Errorf("%s/%s: got %q, want %q", tc.shell, tc.goos, got, tc.want)
		}
	}
}

func TestReplaceRCBlock(t *testing.T) {
	t.Parallel()
	orig := "# zshrc\nexport A=1\n"
	block := renderRCBlock([]string{"alias a='b'"})
	added := replaceRCBlock(orig, block)
	if added != orig+"\n"+block {
		t.Errorf("add: %q", added)
	}
	if again := replaceRCBlock(added, block); again != added {
		t.Errorf("update not idempotent: %q", again)
	}
	updated := replaceRCBlock(added+"export B=2\n", renderRCBlock([]string{"alias c='d'"}))
	if !strings.Contains(updated, "alias c='d'") || strings.Contains(updated, "alias a='b'") || !strings.HasSuffix(updated, "export B=2\n") {
		t.Errorf("update: %q", updated)
	}
	if removed := replaceRCBlock(added, ""); removed != orig {
		t.Errorf("remove: %q", removed)
	}
}
