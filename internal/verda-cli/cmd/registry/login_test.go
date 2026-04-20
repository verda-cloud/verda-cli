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

package registry

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// newLoginStreams returns IOStreams backed by buffers (no stdin needed).
func newLoginStreams() (cmdutil.IOStreams, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return cmdutil.IOStreams{Out: out, ErrOut: errOut}, out, errOut
}

// runLoginForTest invokes NewCmdLogin via its cobra flag machinery so tests
// exercise the same code path the CLI binary does.
func runLoginForTest(t *testing.T, f cmdutil.Factory, streams cmdutil.IOStreams, args ...string) error {
	t.Helper()
	cmd := NewCmdLogin(f, streams)
	cmd.SetArgs(args)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

// writeLoginCredsFile writes a minimal ini credentials file and points
// VERDA_REGISTRY_CREDENTIALS_FILE at it. Returns the file path.
func writeLoginCredsFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write credentials: %v", err)
		}
	}
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)
	return path
}

// freshHome creates an isolated HOME for the test and clears DOCKER_CONFIG
// so the default Docker config path resolves under the temp tree.
func freshHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DOCKER_CONFIG", "")
	return home
}

// healthyCredsBody builds a credentials INI body with an expiry far in the
// future so checkExpiry is happy.
func healthyCredsBody(secret string) string {
	future := time.Now().Add(30 * 24 * time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	return `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = ` + secret + `
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
verda_registry_expires_at = ` + future + `
`
}

// expectedAuth returns the base64("user:secret") Docker expects under
// auths[<host>].auth.
func expectedAuth(user, secret string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + secret))
}

// readConfigTop parses the top-level Docker config JSON as a generic map so
// tests can assert key preservation without a Go struct.
func readConfigTop(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read docker config %s: %v", path, err)
	}
	top := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("parse docker config %s: %v\nraw: %s", path, err, data)
	}
	return top
}

// readConfigAuths returns the auths sub-map keyed by registry host.
func readConfigAuths(t *testing.T, path string) map[string]dockerAuthEntry {
	t.Helper()
	top := readConfigTop(t, path)
	raw, ok := top["auths"]
	if !ok {
		t.Fatalf("docker config %s missing 'auths' key: %s", path, mustJSON(t, top))
	}
	auths := map[string]dockerAuthEntry{}
	if err := json.Unmarshal(raw, &auths); err != nil {
		t.Fatalf("parse auths: %v\nraw: %s", err, raw)
	}
	return auths
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// TestLogin_FreshConfig: Docker config doesn't exist; after login, it's
// created with the correct auth entry and nothing else.
func TestLogin_FreshConfig(t *testing.T) {
	writeLoginCredsFile(t, healthyCredsBody("s3cret"))
	home := freshHome(t)

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newLoginStreams()

	if err := runLoginForTest(t, f, streams); err != nil {
		t.Fatalf("login: %v", err)
	}

	configPath := filepath.Join(home, ".docker", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected docker config at %s: %v", configPath, err)
	}

	auths := readConfigAuths(t, configPath)
	entry, ok := auths["vccr.io"]
	if !ok {
		t.Fatalf("auths missing vccr.io entry: %+v", auths)
	}
	if got, want := entry.Auth, expectedAuth("vcr-abc+cli", "s3cret"); got != want {
		t.Errorf("auth = %q, want %q", got, want)
	}

	top := readConfigTop(t, configPath)
	if len(top) != 1 {
		t.Errorf("expected only 'auths' key in fresh config, got %d keys: %s", len(top), mustJSON(t, top))
	}

	if !strings.Contains(out.String(), "Logged in to vccr.io") {
		t.Errorf("stdout missing success line: %q", out.String())
	}
	if !strings.Contains(out.String(), configPath) {
		t.Errorf("stdout missing config path %q: %q", configPath, out.String())
	}
}

// TestLogin_MergesExistingAuths: pre-existing entry for another registry
// must survive alongside the new vccr.io entry.
func TestLogin_MergesExistingAuths(t *testing.T) {
	writeLoginCredsFile(t, healthyCredsBody("s3cret"))
	home := freshHome(t)

	configPath := filepath.Join(home, ".docker", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := `{"auths":{"docker.io":{"auth":"abc"}}}`
	if err := os.WriteFile(configPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()
	if err := runLoginForTest(t, f, streams); err != nil {
		t.Fatalf("login: %v", err)
	}

	auths := readConfigAuths(t, configPath)
	if got := auths["docker.io"].Auth; got != "abc" {
		t.Errorf("docker.io auth = %q, want preserved 'abc'", got)
	}
	if got, want := auths["vccr.io"].Auth, expectedAuth("vcr-abc+cli", "s3cret"); got != want {
		t.Errorf("vccr.io auth = %q, want %q", got, want)
	}
}

// TestLogin_PreservesUnknownKeys: credsStore, HttpHeaders, random custom keys
// at the top level must not be clobbered.
func TestLogin_PreservesUnknownKeys(t *testing.T) {
	writeLoginCredsFile(t, healthyCredsBody("s3cret"))
	home := freshHome(t)

	configPath := filepath.Join(home, ".docker", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := `{
  "auths": {},
  "credsStore": "osxkeychain",
  "HttpHeaders": {"User-Agent": "foo"},
  "psFormat": "table",
  "unknownFutureKey": {"nested": [1, 2, 3]}
}`
	if err := os.WriteFile(configPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()
	if err := runLoginForTest(t, f, streams); err != nil {
		t.Fatalf("login: %v", err)
	}

	top := readConfigTop(t, configPath)

	for key, wantSubstr := range map[string]string{
		"credsStore":       `"osxkeychain"`,
		"HttpHeaders":      `"User-Agent"`,
		"psFormat":         `"table"`,
		"unknownFutureKey": `"nested"`,
	} {
		raw, ok := top[key]
		if !ok {
			t.Errorf("top-level key %q lost after login", key)
			continue
		}
		if !strings.Contains(string(raw), wantSubstr) {
			t.Errorf("top-level key %q lost content (want %q): %s", key, wantSubstr, raw)
		}
	}

	// And the new auth entry exists.
	auths := readConfigAuths(t, configPath)
	if _, ok := auths["vccr.io"]; !ok {
		t.Errorf("auths missing vccr.io entry: %+v", auths)
	}
}

// TestLogin_UpdatesExistingVCREntry: an existing vccr.io entry must be
// updated in place to the new base64.
func TestLogin_UpdatesExistingVCREntry(t *testing.T) {
	writeLoginCredsFile(t, healthyCredsBody("newsecret"))
	home := freshHome(t)

	configPath := filepath.Join(home, ".docker", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := `{"auths":{"vccr.io":{"auth":"old"}}}`
	if err := os.WriteFile(configPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()
	if err := runLoginForTest(t, f, streams); err != nil {
		t.Fatalf("login: %v", err)
	}

	auths := readConfigAuths(t, configPath)
	want := expectedAuth("vcr-abc+cli", "newsecret")
	if got := auths["vccr.io"].Auth; got != want {
		t.Errorf("vccr.io auth not updated: got %q, want %q", got, want)
	}
}

// TestLogin_RemovesPlaintextFields: if the existing entry carried plaintext
// username/password/identitytoken, they must be dropped. Only `auth` remains.
func TestLogin_RemovesPlaintextFields(t *testing.T) {
	writeLoginCredsFile(t, healthyCredsBody("s3cret"))
	home := freshHome(t)

	configPath := filepath.Join(home, ".docker", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := `{"auths":{"vccr.io":{"auth":"old","username":"u","password":"p","identitytoken":"t","registrytoken":"r"}}}`
	if err := os.WriteFile(configPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()
	if err := runLoginForTest(t, f, streams); err != nil {
		t.Fatalf("login: %v", err)
	}

	// Parse the raw auths sub-object so we can see every field the writer emitted.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, forbidden := range []string{"username", "password", "identitytoken", "registrytoken"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("forbidden field %q still present in config: %s", forbidden, data)
		}
	}

	auths := readConfigAuths(t, configPath)
	want := expectedAuth("vcr-abc+cli", "s3cret")
	if got := auths["vccr.io"].Auth; got != want {
		t.Errorf("vccr.io auth = %q, want %q", got, want)
	}
}

// TestLogin_NotConfigured: no credentials file at all → structured
// registry_not_configured error, Docker config not created.
func TestLogin_NotConfigured(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", missing)
	t.Setenv("VERDA_HOME", dir)
	home := freshHome(t)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()

	err := runLoginForTest(t, f, streams)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if ae.Code != kindRegistryNotConfigured {
		t.Errorf("error code = %q, want %q", ae.Code, kindRegistryNotConfigured)
	}

	configPath := filepath.Join(home, ".docker", "config.json")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("docker config should NOT exist, got err=%v", err)
	}
}

// TestLogin_ExpiredCreds: credentials past expiry → registry_credential_expired.
// No docker config write.
func TestLogin_ExpiredCreds(t *testing.T) {
	past := time.Now().Add(-2 * 24 * time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	writeLoginCredsFile(t, `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
verda_registry_expires_at = `+past+`
`)
	home := freshHome(t)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()

	err := runLoginForTest(t, f, streams)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if ae.Code != kindRegistryCredentialExpired {
		t.Errorf("error code = %q, want %q", ae.Code, kindRegistryCredentialExpired)
	}

	configPath := filepath.Join(home, ".docker", "config.json")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("docker config should NOT exist after expired-creds refusal, got err=%v", err)
	}
}

// TestLogin_FilePermissions: the final file must be mode 0600 on non-Windows.
func TestLogin_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode bits are not meaningful on Windows")
	}
	writeLoginCredsFile(t, healthyCredsBody("s3cret"))
	home := freshHome(t)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()
	if err := runLoginForTest(t, f, streams); err != nil {
		t.Fatalf("login: %v", err)
	}

	configPath := filepath.Join(home, ".docker", "config.json")
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 0600", perm)
	}
}

// TestLogin_CustomConfigFlag: --config wins over HOME/DOCKER_CONFIG.
func TestLogin_CustomConfigFlag(t *testing.T) {
	writeLoginCredsFile(t, healthyCredsBody("s3cret"))
	home := freshHome(t)

	customDir := t.TempDir()
	custom := filepath.Join(customDir, "custom", "config.json")

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()
	if err := runLoginForTest(t, f, streams, "--config", custom); err != nil {
		t.Fatalf("login: %v", err)
	}

	if _, err := os.Stat(custom); err != nil {
		t.Fatalf("custom config not written at %s: %v", custom, err)
	}

	defaultPath := filepath.Join(home, ".docker", "config.json")
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Errorf("default path should not be written when --config set, got err=%v", err)
	}

	auths := readConfigAuths(t, custom)
	if _, ok := auths["vccr.io"]; !ok {
		t.Errorf("vccr.io entry missing in custom config: %+v", auths)
	}
}

// TestLogin_DockerConfigEnvVar: DOCKER_CONFIG=/foo → /foo/config.json.
func TestLogin_DockerConfigEnvVar(t *testing.T) {
	writeLoginCredsFile(t, healthyCredsBody("s3cret"))
	home := freshHome(t)

	dockerDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dockerDir)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()
	if err := runLoginForTest(t, f, streams); err != nil {
		t.Fatalf("login: %v", err)
	}

	wantPath := filepath.Join(dockerDir, "config.json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected %s to exist: %v", wantPath, err)
	}

	defaultPath := filepath.Join(home, ".docker", "config.json")
	if _, err := os.Stat(defaultPath); !os.IsNotExist(err) {
		t.Errorf("default path should not be written when DOCKER_CONFIG set, got err=%v", err)
	}

	// On macOS /tmp is a symlink to /private/tmp. Resolve both sides so we
	// don't compare symlink vs. real path.
	resolvedWant, _ := filepath.EvalSymlinks(wantPath)
	auths := readConfigAuths(t, resolvedWant)
	if _, ok := auths["vccr.io"]; !ok {
		t.Errorf("vccr.io entry missing at %s: %+v", wantPath, auths)
	}
}

// TestLogin_AtomicWrite: after a successful write, no `.new` sibling is
// left behind in the target directory.
func TestLogin_AtomicWrite(t *testing.T) {
	writeLoginCredsFile(t, healthyCredsBody("s3cret"))
	home := freshHome(t)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()
	if err := runLoginForTest(t, f, streams); err != nil {
		t.Fatalf("login: %v", err)
	}

	dir := filepath.Join(home, ".docker")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".new") {
			t.Errorf("stale temp file left behind: %s", e.Name())
		}
	}
}

// TestLogin_IdempotentOutput: running login twice produces byte-for-byte
// identical files (tab indentation stable, no timestamps, map ordering
// deterministic). This is the core guarantee that makes `verda registry
// login` safe in config-management / Ansible loops.
func TestLogin_IdempotentOutput(t *testing.T) {
	writeLoginCredsFile(t, healthyCredsBody("s3cret"))
	home := freshHome(t)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLoginStreams()

	if err := runLoginForTest(t, f, streams); err != nil {
		t.Fatalf("login 1: %v", err)
	}
	configPath := filepath.Join(home, ".docker", "config.json")
	first, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read first: %v", err)
	}

	// Second run with a fresh command (flag state reset) must yield the
	// same bytes.
	streams2, _, _ := newLoginStreams()
	if err := runLoginForTest(t, f, streams2); err != nil {
		t.Fatalf("login 2: %v", err)
	}
	second, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read second: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("login is not byte-for-byte idempotent\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}
