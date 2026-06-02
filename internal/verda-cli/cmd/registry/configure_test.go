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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/ini.v1"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
)

// newTestStreams returns IOStreams backed by buffers, with `stdin` providing
// the reader the command will consume for --password-stdin.
func newTestStreams(stdin string) (cmdutil.IOStreams, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return cmdutil.IOStreams{
		In:     strings.NewReader(stdin),
		Out:    out,
		ErrOut: errOut,
	}, out, errOut
}

// runConfigureForTest invokes the command's RunE for the given args so we
// get the same flag-parsing path the real binary does.
func runConfigureForTest(t *testing.T, f cmdutil.Factory, streams cmdutil.IOStreams, args ...string) error {
	t.Helper()
	cmd := NewCmdConfigure(f, streams)
	cmd.SetArgs(args)
	cmd.SetIn(streams.In)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	// SilenceUsage/Errors so failed runs don't pollute test output with
	// cobra's generated usage text.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func TestConfigure_PasteHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newTestStreams("")

	err := runConfigureForTest(t, f, streams,
		"--paste", "docker login -u vcr-abc+cli -p s3cret vccr.io/abc",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !strings.Contains(out.String(), "Registry credentials saved to profile \"default\"") {
		t.Errorf("missing confirmation in stdout: %q", out.String())
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	sec := cfg.Section("default")
	if sec.Key("verda_registry_username").String() != "vcr-abc+cli" {
		t.Errorf("username: got %q", sec.Key("verda_registry_username").String())
	}
	if sec.Key("verda_registry_secret").String() != "s3cret" {
		t.Errorf("secret: got %q", sec.Key("verda_registry_secret").String())
	}
	if sec.Key("verda_registry_endpoint").String() != "vccr.io" {
		t.Errorf("endpoint: got %q", sec.Key("verda_registry_endpoint").String())
	}
	if sec.Key("verda_registry_project_id").String() != "abc" {
		t.Errorf("project_id: got %q", sec.Key("verda_registry_project_id").String())
	}
	raw := sec.Key("verda_registry_expires_at").String()
	if raw == "" {
		t.Fatal("expires_at missing")
	}
	if _, err := time.Parse(time.RFC3339, raw); err != nil {
		t.Errorf("expires_at not RFC3339: %q (%v)", raw, err)
	}
}

func TestConfigure_UsernamePasswordStdin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newTestStreams("s3cret\n")

	err := runConfigureForTest(t, f, streams,
		"--username", "vcr-abc+cli",
		"--password-stdin",
		"--endpoint", "vccr.io",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	sec := cfg.Section("default")
	if sec.Key("verda_registry_username").String() != "vcr-abc+cli" {
		t.Errorf("username: got %q", sec.Key("verda_registry_username").String())
	}
	// Trailing \n must be stripped.
	if sec.Key("verda_registry_secret").String() != "s3cret" {
		t.Errorf("secret: got %q, want %q", sec.Key("verda_registry_secret").String(), "s3cret")
	}
	if sec.Key("verda_registry_endpoint").String() != "vccr.io" {
		t.Errorf("endpoint: got %q", sec.Key("verda_registry_endpoint").String())
	}
	if sec.Key("verda_registry_project_id").String() != "abc" {
		t.Errorf("project_id: got %q", sec.Key("verda_registry_project_id").String())
	}
}

func TestConfigure_PasteMalformedDoesNotWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newTestStreams("")

	err := runConfigureForTest(t, f, streams,
		"--paste", "this is not a docker login",
	)
	if err == nil {
		t.Fatal("expected parser error, got nil")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("credentials file should not exist after parser failure")
	}
}

func TestConfigure_AgentModeWithoutFlagsErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true
	streams, _, _ := newTestStreams("")

	err := runConfigureForTest(t, f, streams /* no flags */)
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
	if !strings.Contains(err.Error(), "agent mode") && !strings.Contains(err.Error(), "--paste") {
		t.Errorf("unexpected error text: %v", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("credentials file should not exist after usage error")
	}
}

// The Task-6 placeholder that returned a friendly "wizard not yet wired"
// error has been replaced by the Task-7 wizard. End-to-end testing of the
// full bubbletea flow would require a fake TTY; buildConfigureFlow is
// exercised directly in TestBuildConfigureFlow_* below instead.

func TestConfigure_PreservesExistingS3KeysInSameProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	existing := `[default]
verda_s3_access_key = AKIAEXAMPLE
verda_s3_secret_key = s3-secret
verda_s3_endpoint = https://objects.example.com
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newTestStreams("")

	err := runConfigureForTest(t, f, streams,
		"--paste", "docker login -u vcr-abc+cli -p s3cret vccr.io",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	sec := cfg.Section("default")
	// S3 keys are intact.
	if sec.Key("verda_s3_access_key").String() != "AKIAEXAMPLE" {
		t.Error("S3 access_key clobbered")
	}
	if sec.Key("verda_s3_secret_key").String() != "s3-secret" {
		t.Error("S3 secret_key clobbered")
	}
	if sec.Key("verda_s3_endpoint").String() != "https://objects.example.com" {
		t.Error("S3 endpoint clobbered")
	}
	// Registry keys were added.
	if sec.Key("verda_registry_username").String() != "vcr-abc+cli" {
		t.Error("registry_username not written")
	}
	if sec.Key("verda_registry_secret").String() != "s3cret" {
		t.Error("registry_secret not written")
	}
}

func TestConfigure_ProfileWritesToNamedSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	existing := `[default]
verda_client_id = api-id
verda_client_secret = api-secret
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newTestStreams("")

	err := runConfigureForTest(t, f, streams,
		"--profile", "staging",
		"--paste", "docker login -u vcr-stg+cli -p stgsecret vccr.io/stg",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// default section left untouched.
	def := cfg.Section("default")
	if def.Key("verda_client_id").String() != "api-id" {
		t.Error("[default] verda_client_id changed")
	}
	if def.HasKey("verda_registry_username") {
		t.Error("registry keys leaked into [default]")
	}
	// staging has the new keys.
	stg := cfg.Section("staging")
	if stg.Key("verda_registry_username").String() != "vcr-stg+cli" {
		t.Error("[staging] registry_username not written")
	}
	if stg.Key("verda_registry_project_id").String() != "stg" {
		t.Error("[staging] registry_project_id not written")
	}
}

// TestConfigure_UsernamePasswordStdin_DefaultsEndpointToProduction:
// when --endpoint is omitted and no saved profile exists, the command
// silently falls back to defaultRegistryEndpoint ("vccr.io") and emits a
// provenance line on stderr so users on staging can tell what happened.
func TestConfigure_UsernamePasswordStdin_DefaultsEndpointToProduction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	f := cmdutil.NewTestFactory(nil)
	streams, _, errOut := newTestStreams("s3cret\n")

	err := runConfigureForTest(t, f, streams,
		"--username", "vcr-abc+cli",
		"--password-stdin",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Section("default").Key("verda_registry_endpoint").String(); got != defaultRegistryEndpoint {
		t.Errorf("endpoint = %q, want %q", got, defaultRegistryEndpoint)
	}
	if !strings.Contains(errOut.String(), "production default") {
		t.Errorf("stderr should explain the production-default choice, got: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), defaultRegistryEndpoint) {
		t.Errorf("stderr should name the chosen endpoint, got: %q", errOut.String())
	}
}

// TestConfigure_UsernamePasswordStdin_ReusesSavedEndpoint: a previously
// saved endpoint for the same profile is reused on rotation, so staging
// users don't accidentally rotate onto the production host.
func TestConfigure_UsernamePasswordStdin_ReusesSavedEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	existing := `[default]
verda_registry_username = vcr-abc+oldcli
verda_registry_secret = oldsecret
verda_registry_endpoint = registry.staging.internal.datacrunch.io
verda_registry_project_id = abc
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	f := cmdutil.NewTestFactory(nil)
	streams, _, errOut := newTestStreams("newsecret\n")

	err := runConfigureForTest(t, f, streams,
		"--username", "vcr-abc+newcli",
		"--password-stdin",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := cfg.Section("default").Key("verda_registry_endpoint").String()
	if got != "registry.staging.internal.datacrunch.io" {
		t.Errorf("endpoint = %q, want saved staging host preserved", got)
	}
	if cfg.Section("default").Key("verda_registry_secret").String() != "newsecret" {
		t.Error("secret should have been updated to the new rotation value")
	}
	if !strings.Contains(errOut.String(), "saved in profile") {
		t.Errorf("stderr should explain reuse of saved endpoint, got: %q", errOut.String())
	}
	// Non-TTY overwrite of an already-configured profile proceeds (rotation
	// intent) but isn't silent — it notes the replace on stderr.
	if !strings.Contains(errOut.String(), "Replacing existing registry credentials") {
		t.Errorf("stderr should note the credential replace, got: %q", errOut.String())
	}
}

// TestConfigure_OverwriteExistingProfile_TTYDeclineAborts: on a terminal,
// configuring a profile that already has registry credentials prompts before
// the irreversible replace. Declining writes nothing.
func TestConfigure_OverwriteExistingProfile_TTYDeclineAborts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	existing := `[default]
verda_registry_username = vcr-abc+oldcli
verda_registry_secret = oldsecret
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	withForcedTTY(t, true)
	mock := tuitest.New().AddConfirm(false) // decline the replace
	f := cmdutil.NewTestFactory(mock)
	streams, out, errOut := newTestStreams("")

	err := runConfigureForTest(t, f, streams,
		"--paste", "docker login -u vcr-abc+newcli -p newsecret vccr.io",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Old credentials must be intact — nothing was written.
	if got := cfg.Section("default").Key("verda_registry_secret").String(); got != "oldsecret" {
		t.Errorf("secret = %q, want oldsecret (declined overwrite)", got)
	}
	if got := cfg.Section("default").Key("verda_registry_username").String(); got != "vcr-abc+oldcli" {
		t.Errorf("username = %q, want vcr-abc+oldcli (declined overwrite)", got)
	}
	if !strings.Contains(errOut.String(), "Canceled") {
		t.Errorf("expected Canceled on stderr, got: %q", errOut.String())
	}
	if strings.Contains(out.String(), "saved to profile") {
		t.Errorf("should not have reported a save; stdout: %q", out.String())
	}
}

// TestConfigure_OverwriteExistingProfile_TTYConfirmReplaces: accepting the
// prompt rotates the credentials in place.
func TestConfigure_OverwriteExistingProfile_TTYConfirmReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	existing := `[default]
verda_registry_username = vcr-abc+oldcli
verda_registry_secret = oldsecret
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	withForcedTTY(t, true)
	mock := tuitest.New().AddConfirm(true) // accept the replace
	f := cmdutil.NewTestFactory(mock)
	streams, out, _ := newTestStreams("")

	err := runConfigureForTest(t, f, streams,
		"--paste", "docker login -u vcr-abc+newcli -p newsecret vccr.io",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Section("default").Key("verda_registry_secret").String(); got != "newsecret" {
		t.Errorf("secret = %q, want newsecret (confirmed overwrite)", got)
	}
	if got := cfg.Section("default").Key("verda_registry_username").String(); got != "vcr-abc+newcli" {
		t.Errorf("username = %q, want vcr-abc+newcli (confirmed overwrite)", got)
	}
	if !strings.Contains(out.String(), "saved to profile") {
		t.Errorf("expected save confirmation; stdout: %q", out.String())
	}
}

// TestConfigure_UsernamePasswordStdin_FlagWinsOverSaved: an explicit
// --endpoint flag beats any saved profile value.
func TestConfigure_UsernamePasswordStdin_FlagWinsOverSaved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	existing := `[default]
verda_registry_username = vcr-abc+oldcli
verda_registry_secret = oldsecret
verda_registry_endpoint = registry.staging.internal.datacrunch.io
verda_registry_project_id = abc
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	f := cmdutil.NewTestFactory(nil)
	streams, _, errOut := newTestStreams("newsecret\n")

	err := runConfigureForTest(t, f, streams,
		"--username", "vcr-abc+newcli",
		"--password-stdin",
		"--endpoint", "vccr.io",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Section("default").Key("verda_registry_endpoint").String(); got != "vccr.io" {
		t.Errorf("endpoint = %q, want %q (flag should win)", got, "vccr.io")
	}
	// No provenance line should be emitted when the user set --endpoint
	// explicitly; that stderr chatter is only for the surprise-avoidance
	// case.
	if strings.Contains(errOut.String(), "Using registry endpoint") {
		t.Errorf("stderr should not emit provenance line when --endpoint is explicit, got: %q", errOut.String())
	}
}

// TestConfigure_UsernamePasswordStdin_NamedProfileDoesNotLeakDefault:
// when --profile points to a section that doesn't exist yet (or has no
// saved endpoint), we fall back to the production default rather than
// accidentally reusing another profile's saved host.
func TestConfigure_UsernamePasswordStdin_NamedProfileDoesNotLeakDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	// [default] has staging. [staging] section doesn't exist yet.
	existing := `[default]
verda_registry_endpoint = registry.staging.internal.datacrunch.io
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newTestStreams("stgsecret\n")

	err := runConfigureForTest(t, f, streams,
		"--profile", "staging",
		"--username", "vcr-abc+cli",
		"--password-stdin",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	stg := cfg.Section("staging")
	if got := stg.Key("verda_registry_endpoint").String(); got != defaultRegistryEndpoint {
		t.Errorf("staging endpoint = %q, want %q (not the default-profile's host)",
			got, defaultRegistryEndpoint)
	}
}

// TestConfigure_UsernamePasswordStdin_SuggestsPasteOnUsageError: when
// neither --paste nor --username+--password-stdin is supplied in agent
// mode (where the wizard can't run), the error hints at --paste as the
// easier path so users on unknown hosts aren't stuck.
func TestConfigure_UsernamePasswordStdin_SuggestsPasteOnUsageError(t *testing.T) {
	// This test exists to pin the wording: if someone renames the hint
	// they should update the web UI docs in lockstep.
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true
	streams, _, _ := newTestStreams("")

	err := runConfigureForTest(t, f, streams /* no flags */)
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
	if !strings.Contains(err.Error(), "--paste") {
		t.Errorf("agent-mode usage error should mention --paste as an option, got: %v", err)
	}
}

// TestConfigure_DockerConfigFlagWritesDockerConfig verifies that --docker-config
// merges the just-saved credentials into ~/.docker/config.json (here redirected
// via DOCKER_CONFIG) using the same writer `registry login` uses.
func TestConfigure_DockerConfigFlagWritesDockerConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)
	dockerDir := filepath.Join(dir, "docker")
	t.Setenv("DOCKER_CONFIG", dockerDir)

	f := cmdutil.NewTestFactory(nil)
	streams, _, errOut := newTestStreams("")

	err := runConfigureForTest(t, f, streams,
		"--paste", "docker login -u vcr-abc+cli -p s3cret vccr.io",
		"--docker-config",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dockerDir, "config.json"))
	if err != nil {
		t.Fatalf("docker config not written: %v", err)
	}
	wantAuth := base64.StdEncoding.EncodeToString([]byte("vcr-abc+cli:s3cret"))
	if !strings.Contains(string(data), wantAuth) {
		t.Errorf("docker config missing base64 auth entry; got: %s", data)
	}
	if !strings.Contains(string(data), "vccr.io") {
		t.Errorf("docker config missing registry host; got: %s", data)
	}
	if !strings.Contains(errOut.String(), "Also wrote") {
		t.Errorf("expected docker-config confirmation on stderr, got: %q", errOut.String())
	}
}

// TestResolveRegistryInputs_WizardManualPath: the wizard manual path provides
// Username + Secret (no stdin, no endpoint). The endpoint is derived (vccr.io
// base / saved host) and the project id is parsed from the credential name.
func TestResolveRegistryInputs_WizardManualPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newTestStreams("")
	cmd := NewCmdConfigure(f, streams)

	opts := &configureOptions{
		Profile:       defaultProfileName,
		Username:      "vcr-abc+cli",
		Secret:        "s3cret",
		ExpiresInDays: defaultExpiresInDays,
	}
	creds, err := resolveRegistryInputs(cmd, f, streams, opts)
	if err != nil {
		t.Fatalf("resolveRegistryInputs: %v", err)
	}
	if creds.Username != "vcr-abc+cli" {
		t.Errorf("Username = %q, want vcr-abc+cli", creds.Username)
	}
	if creds.Secret != "s3cret" {
		t.Errorf("Secret = %q, want s3cret", creds.Secret)
	}
	if creds.ProjectID != "abc" {
		t.Errorf("ProjectID = %q, want abc (parsed from credential name)", creds.ProjectID)
	}
	if creds.Endpoint != defaultRegistryEndpoint {
		t.Errorf("Endpoint = %q, want %q (production base)", creds.Endpoint, defaultRegistryEndpoint)
	}
}

// TestResolveRegistryInputs_WizardManualPath_ReusesSavedEndpoint: when the
// profile already has a saved host (e.g. staging), the manual path reuses it
// rather than defaulting to vccr.io.
func TestResolveRegistryInputs_WizardManualPath_ReusesSavedEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)

	existing := `[default]
verda_registry_endpoint = registry.staging.internal.datacrunch.io
`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newTestStreams("")
	cmd := NewCmdConfigure(f, streams)

	opts := &configureOptions{
		Profile:       defaultProfileName,
		Username:      "vcr-abc+cli",
		Secret:        "s3cret",
		ExpiresInDays: defaultExpiresInDays,
	}
	creds, err := resolveRegistryInputs(cmd, f, streams, opts)
	if err != nil {
		t.Fatalf("resolveRegistryInputs: %v", err)
	}
	if creds.Endpoint != "registry.staging.internal.datacrunch.io" {
		t.Errorf("Endpoint = %q, want the saved staging host", creds.Endpoint)
	}
}

// TestConfigure_WritesToActiveProfile: with no --profile, configure writes to
// the active profile (VERDA_PROFILE here), not the literal "default". This is
// the write side of the active-profile fix; the read commands resolve the same
// way via resolveProfile/loadCredsFromFactory.
func TestConfigure_WritesToActiveProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)
	t.Setenv("VERDA_PROFILE", "production")

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newTestStreams("")

	err := runConfigureForTest(t, f, streams,
		"--paste", "docker login -u vcr-abc+cli -p s3cret vccr.io",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Section("production").HasKey("verda_registry_username") {
		t.Error("credentials should have been written to the active profile [production]")
	}
	if cfg.Section("default").HasKey("verda_registry_username") {
		t.Error("credentials must NOT leak into [default] when the active profile is production")
	}
	if !strings.Contains(out.String(), `profile "production"`) {
		t.Errorf("should report writing to the active profile, got: %q", out.String())
	}
}

func TestConfigure_ExpiresInRespected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newTestStreams("")

	before := time.Now().UTC()
	err := runConfigureForTest(t, f, streams,
		"--paste", "docker login -u vcr-abc+cli -p s3cret vccr.io",
		"--expires-in", "7",
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	after := time.Now().UTC()

	cfg, err := ini.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	raw := cfg.Section("default").Key("verda_registry_expires_at").String()
	got, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse expires_at: %v", err)
	}
	// got should be 7 days from "now" ± clock skew tolerance (1 min window
	// covers the wall-clock jitter between `before` and `after`).
	low := before.Add(7*24*time.Hour - time.Minute)
	high := after.Add(7*24*time.Hour + time.Minute)
	if got.Before(low) || got.After(high) {
		t.Errorf("expires_at = %v, want within [%v, %v]", got, low, high)
	}
}
