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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// newShowStreams returns IOStreams backed by buffers (no stdin needed).
func newShowStreams() (cmdutil.IOStreams, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return cmdutil.IOStreams{Out: out, ErrOut: errOut}, out, errOut
}

// runShowForTest invokes the command's RunE for the given args so we get
// the same flag-parsing path as the real binary.
func runShowForTest(t *testing.T, f cmdutil.Factory, streams cmdutil.IOStreams, args ...string) error {
	t.Helper()
	cmd := NewCmdShow(f, streams)
	cmd.SetArgs(args)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

// writeCredsFile writes a minimal INI file with the given body to a temp
// credentials file and wires the env var to point at it.
func writeCredsFile(t *testing.T, body string) string {
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

func TestShow_NotConfigured_MissingFile(t *testing.T) {
	// Set env to a path that does not exist.
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", path)
	t.Setenv("VERDA_HOME", dir)

	f := cmdutil.NewTestFactory(nil)
	streams, out, errOut := newShowStreams()

	if err := runShowForTest(t, f, streams); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "not configured") {
		t.Errorf("missing 'not configured' in stdout: %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("unexpected stderr: %q", errOut.String())
	}
}

func TestShow_NotConfigured_MissingSection(t *testing.T) {
	writeCredsFile(t, `[other]
verda_registry_username = vcr-xyz+cli
`)

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newShowStreams()

	if err := runShowForTest(t, f, streams); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "not configured") {
		t.Errorf("missing 'not configured' in stdout: %q", out.String())
	}
}

func TestShow_NotConfigured_EmptyKeys(t *testing.T) {
	writeCredsFile(t, `[default]
verda_registry_username =
verda_registry_secret =
verda_registry_endpoint =
`)

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newShowStreams()

	if err := runShowForTest(t, f, streams); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "not configured") {
		t.Errorf("missing 'not configured' in stdout: %q", out.String())
	}
}

func TestShow_Configured_Healthy(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	writeCredsFile(t, `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
verda_registry_expires_at = `+future+`
`)

	f := cmdutil.NewTestFactory(nil)
	streams, out, errOut := newShowStreams()

	if err := runShowForTest(t, f, streams); err != nil {
		t.Fatalf("run: %v", err)
	}

	o := out.String()
	for _, want := range []string{
		"registry_configured",
		"true",
		"vcr-abc+cli",
		"vccr.io",
		"abc",
		"expires_at",
		"days_remaining",
	} {
		if !strings.Contains(o, want) {
			t.Errorf("stdout missing %q: %q", want, o)
		}
	}
	if strings.Contains(errOut.String(), "WARNING") || strings.Contains(errOut.String(), "expire in") {
		t.Errorf("unexpected warning in stderr for healthy creds: %q", errOut.String())
	}
}

func TestShow_Configured_NearExpiry(t *testing.T) {
	// 3 days remaining — pick a value strictly inside the < 7 window.
	near := time.Now().Add(3*24*time.Hour + 6*time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	writeCredsFile(t, `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
verda_registry_expires_at = `+near+`
`)

	f := cmdutil.NewTestFactory(nil)
	streams, out, errOut := newShowStreams()

	if err := runShowForTest(t, f, streams); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "vcr-abc+cli") {
		t.Errorf("stdout missing username: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "expire in 3 days") {
		t.Errorf("stderr missing 'expire in 3 days': %q", errOut.String())
	}
}

func TestShow_Configured_Expired(t *testing.T) {
	past := time.Now().Add(-2 * 24 * time.Hour).UTC().Round(time.Second)
	pastStr := past.Format(time.RFC3339)
	writeCredsFile(t, `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
verda_registry_expires_at = `+pastStr+`
`)

	f := cmdutil.NewTestFactory(nil)
	streams, out, errOut := newShowStreams()

	if err := runShowForTest(t, f, streams); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "expired") || !strings.Contains(out.String(), "true") {
		t.Errorf("stdout missing 'expired: true': %q", out.String())
	}
	if !strings.Contains(errOut.String(), "WARNING") ||
		!strings.Contains(errOut.String(), "expired on "+past.Format("2006-01-02")) {
		t.Errorf("stderr missing expired WARNING line: %q", errOut.String())
	}
}

func TestShow_NoSecretLeak(t *testing.T) {
	const secret = "SHOULD-NEVER-APPEAR"
	future := time.Now().Add(14 * 24 * time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	writeCredsFile(t, `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = `+secret+`
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
verda_registry_expires_at = `+future+`
`)

	// Run in every output format to cover the full matrix.
	for _, format := range []string{"table", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			f := cmdutil.NewTestFactory(nil)
			f.OutputFormatOverride = format
			streams, out, errOut := newShowStreams()

			if err := runShowForTest(t, f, streams); err != nil {
				t.Fatalf("run: %v", err)
			}
			if strings.Contains(out.String(), secret) {
				t.Errorf("%s: secret leaked to stdout: %q", format, out.String())
			}
			if strings.Contains(errOut.String(), secret) {
				t.Errorf("%s: secret leaked to stderr: %q", format, errOut.String())
			}
		})
	}
}

func TestShow_JSONOutput(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	writeCredsFile(t, `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
verda_registry_expires_at = `+future+`
`)

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := newShowStreams()

	if err := runShowForTest(t, f, streams); err != nil {
		t.Fatalf("run: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json unmarshal: %v\nraw: %q", err, out.String())
	}
	configured, ok := got["registry_configured"].(bool)
	if !ok {
		t.Fatalf("registry_configured not a bool: %T", got["registry_configured"])
	}
	if !configured {
		t.Errorf("registry_configured = false, want true")
	}
	for _, k := range []string{"profile", "username", "endpoint", "project_id", "expires_at", "days_remaining"} {
		if _, ok := got[k]; !ok {
			t.Errorf("JSON missing key %q: %v", k, got)
		}
	}
	if got["username"] != "vcr-abc+cli" {
		t.Errorf("username = %v, want vcr-abc+cli", got["username"])
	}
	if _, present := got["secret"]; present {
		t.Errorf("secret must never appear in JSON payload: %v", got)
	}
}

func TestShow_ZeroExpiresAtOmitted(t *testing.T) {
	// No verda_registry_expires_at key at all — ExpiresAt stays zero.
	writeCredsFile(t, `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
`)

	// --- JSON mode: must omit expires_at and days_remaining. ---
	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, errOut := newShowStreams()
	if err := runShowForTest(t, f, streams); err != nil {
		t.Fatalf("run json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json unmarshal: %v\nraw: %q", err, out.String())
	}
	if _, present := got["expires_at"]; present {
		t.Errorf("JSON should omit expires_at for zero time, got %v", got)
	}
	if _, present := got["days_remaining"]; present {
		t.Errorf("JSON should omit days_remaining for zero time, got %v", got)
	}
	if errOut.Len() != 0 {
		t.Errorf("no warning expected when expiry is unknown, got %q", errOut.String())
	}

	// --- Human mode: no days_remaining row; expires_at rendered as "(none)". ---
	fh := cmdutil.NewTestFactory(nil)
	streamsH, outH, errOutH := newShowStreams()
	if err := runShowForTest(t, fh, streamsH); err != nil {
		t.Fatalf("run human: %v", err)
	}
	if strings.Contains(outH.String(), "days_remaining") {
		t.Errorf("human output should omit days_remaining: %q", outH.String())
	}
	if !strings.Contains(outH.String(), "expires_at") || !strings.Contains(outH.String(), "(none)") {
		t.Errorf("human output should show expires_at: (none) sentinel: %q", outH.String())
	}
	if errOutH.Len() != 0 {
		t.Errorf("no warning expected when expiry is unknown, got %q", errOutH.String())
	}
}

func TestShow_ProfileFallback(t *testing.T) {
	future := time.Now().Add(30 * 24 * time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	writeCredsFile(t, `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = vccr.io
verda_registry_project_id = abc
verda_registry_expires_at = `+future+`
`)

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newShowStreams()

	// Explicitly pass --profile="" which should fall back to "default".
	if err := runShowForTest(t, f, streams, "--profile", ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out.String(), "vcr-abc+cli") {
		t.Errorf("expected fallback to [default] section, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "registry_configured") {
		t.Errorf("expected configured status for fallback profile, got: %q", out.String())
	}
}
