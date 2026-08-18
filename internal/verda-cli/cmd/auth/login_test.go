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

package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// These tests cover the flag-driven write path only. Supplying both
// --client-id and --client-secret is what skips the wizard (login.go checks
// them with OR), and a wizard here would start a real tea.Program against the
// developer's stdin — a hang when `go test` runs from a terminal. The
// post-wizard "still empty" validation gate is therefore unreachable from a
// test; covering it needs the engine injected rather than constructed inline.

// sandboxHome points the whole config dir at a temp tree. VERDA_HOME, not
// VERDA_SHARED_CREDENTIALS_FILE: login calls options.EnsureVerdaDir, which
// resolves through VerdaDir and would mkdir the developer's real ~/.verda even
// with the credentials path redirected elsewhere.
func sandboxHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("VERDA_HOME", dir)
	t.Setenv("VERDA_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	return dir
}

func runAuthLoginForTest(t *testing.T, args ...string) error {
	t.Helper()
	streams := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
	cmd := NewCmdLogin(cmdutil.NewTestFactory(nil), streams)
	cmd.SetArgs(args)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func loadProfile(t *testing.T, path, profile string) *options.SharedCredentials {
	t.Helper()
	creds, err := options.LoadSharedCredentialsForProfile(path, profile)
	if err != nil {
		t.Fatalf("LoadSharedCredentialsForProfile(%q, %q): %v", path, profile, err)
	}
	return creds
}

func TestLoginWritesNewProfile(t *testing.T) {
	dir := sandboxHome(t)
	path := filepath.Join(dir, "credentials")

	if err := runAuthLoginForTest(t, "--client-id", "id-1", "--client-secret", "secret-1"); err != nil {
		t.Fatalf("login: %v", err)
	}

	got := loadProfile(t, path, "default")
	if got.ClientID != "id-1" {
		t.Errorf("ClientID = %q, want id-1", got.ClientID)
	}
	if got.ClientSecret != "secret-1" {
		t.Errorf("ClientSecret = %q, want secret-1", got.ClientSecret)
	}
	if got.BaseURL != defaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", got.BaseURL, defaultBaseURL)
	}
}

// A leaked secret is not recoverable, so the 0600 is load-bearing rather than
// cosmetic. Windows has no mode bits to assert.
func TestLoginRestrictsFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix mode bits on Windows")
	}
	dir := sandboxHome(t)
	path := filepath.Join(dir, "credentials")

	if err := runAuthLoginForTest(t, "--client-id", "id", "--client-secret", "secret"); err != nil {
		t.Fatalf("login: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %#o, want 0600", perm)
	}
}

func TestLoginWritesNamedProfileAndBaseURL(t *testing.T) {
	dir := sandboxHome(t)
	path := filepath.Join(dir, "credentials")

	err := runAuthLoginForTest(t,
		"--profile", "staging",
		"--base-url", "https://staging-api.verda.com/v1",
		"--client-id", "stg-id",
		"--client-secret", "stg-secret",
	)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	got := loadProfile(t, path, "staging")
	if got.BaseURL != "https://staging-api.verda.com/v1" {
		t.Errorf("BaseURL = %q", got.BaseURL)
	}
	if got.ClientID != "stg-id" {
		t.Errorf("ClientID = %q, want stg-id", got.ClientID)
	}

	if _, err := options.LoadSharedCredentialsForProfile(path, "default"); err == nil {
		t.Error("a [default] section appeared; --profile must write only the named section")
	}
}

// The writer merges into the existing INI. Dropping unrelated profiles would
// destroy credentials the user cannot recover from the API.
func TestLoginPreservesOtherProfiles(t *testing.T) {
	dir := sandboxHome(t)
	path := filepath.Join(dir, "credentials")

	seed := "[other]\n" +
		"verda_base_url      = https://other.verda.com/v1\n" +
		"verda_client_id     = other-id\n" +
		"verda_client_secret = other-secret\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := runAuthLoginForTest(t, "--client-id", "new-id", "--client-secret", "new-secret"); err != nil {
		t.Fatalf("login: %v", err)
	}

	other := loadProfile(t, path, "other")
	if other.ClientID != "other-id" || other.ClientSecret != "other-secret" {
		t.Errorf("[other] was modified: %+v", other)
	}
	if added := loadProfile(t, path, "default"); added.ClientID != "new-id" {
		t.Errorf("[default] ClientID = %q, want new-id", added.ClientID)
	}
}

// Re-running login against a profile is the documented re-auth path: rotating a
// secret must replace the stored one, not append or refuse.
func TestLoginOverwritesSameProfile(t *testing.T) {
	dir := sandboxHome(t)
	path := filepath.Join(dir, "credentials")

	if err := runAuthLoginForTest(t, "--client-id", "old", "--client-secret", "old-secret"); err != nil {
		t.Fatalf("first login: %v", err)
	}
	if err := runAuthLoginForTest(t, "--client-id", "new", "--client-secret", "new-secret"); err != nil {
		t.Fatalf("second login: %v", err)
	}

	got := loadProfile(t, path, "default")
	if got.ClientID != "new" || got.ClientSecret != "new-secret" {
		t.Errorf("re-login did not replace credentials: %+v", got)
	}

	data, err := os.ReadFile(path) //nolint:gosec // test-owned temp file
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n := strings.Count(string(data), "[default]"); n != 1 {
		t.Errorf("found %d [default] sections, want 1", n)
	}
	if strings.Contains(string(data), "old-secret") {
		t.Error("the replaced secret is still present in the file")
	}
}

// --credentials-file outranks VERDA_SHARED_CREDENTIALS_FILE; sandboxHome sets
// the env var, so a write landing at the flag path proves the precedence.
func TestLoginCredentialsFileFlagWinsOverEnv(t *testing.T) {
	dir := sandboxHome(t)
	flagPath := filepath.Join(dir, "explicit-credentials")

	err := runAuthLoginForTest(t,
		"--credentials-file", flagPath,
		"--client-id", "flag-id",
		"--client-secret", "flag-secret",
	)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if got := loadProfile(t, flagPath, "default"); got.ClientID != "flag-id" {
		t.Errorf("ClientID = %q, want flag-id", got.ClientID)
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials")); !os.IsNotExist(err) {
		t.Error("the env-var path was written despite --credentials-file")
	}
}
