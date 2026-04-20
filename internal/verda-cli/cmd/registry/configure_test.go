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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/ini.v1"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
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
