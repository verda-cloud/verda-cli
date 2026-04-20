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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"go.yaml.in/yaml/v3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// ---------- shared test plumbing ----------

// newLsStreams returns IOStreams backed by buffers (no stdin needed).
func newLsStreams() (cmdutil.IOStreams, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return cmdutil.IOStreams{Out: out, ErrOut: errOut}, out, errOut
}

// runLsForTest invokes NewCmdLs with the supplied args so tests exercise the
// same flag-parsing path as the real binary.
func runLsForTest(t *testing.T, f cmdutil.Factory, streams cmdutil.IOStreams, args ...string) error {
	t.Helper()
	cmd := NewCmdLs(f, streams)
	cmd.SetArgs(args)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

// writeLsCredsFile writes an INI credentials body to a temp file and points
// VERDA_REGISTRY_CREDENTIALS_FILE at it. Returns the file path.
func writeLsCredsFile(t *testing.T, body string) string {
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

// healthyLsCredsBody builds an INI body with an endpoint pointing at host and
// an expiry 30 days in the future.
func healthyLsCredsBody(host string) string {
	future := time.Now().Add(30 * 24 * time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	return `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = ` + host + `
verda_registry_project_id = abc
verda_registry_expires_at = ` + future + `
`
}

// expiredLsCredsBody builds an INI body with an endpoint and an expiry 48h in
// the past so checkExpiry fires.
func expiredLsCredsBody(host string) string {
	past := time.Now().Add(-48 * time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	return `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = ` + host + `
verda_registry_project_id = abc
verda_registry_expires_at = ` + past + `
`
}

// withFakeRegistry swaps clientBuilder to always return fake, restoring the
// original at test cleanup. Mirrors cmd/s3/ls_test.go's withFakeClient.
func withFakeRegistry(t *testing.T, fake Registry) {
	t.Helper()
	orig := clientBuilder
	clientBuilder = func(creds *options.RegistryCredentials, cfg RetryConfig) Registry { return fake }
	t.Cleanup(func() { clientBuilder = orig })
}

// ---------- fake Registry implementations ----------

// recordingRegistry is a minimal in-memory Registry used to exercise ls
// without hitting a real server. It embeds a nil Registry so any method ls
// shouldn't call (Head/Write/Read) nil-dispatches — panicking loudly the
// moment a test accidentally leaves the pre-network pipeline.
type recordingRegistry struct {
	Registry // nil; accidental dispatch to Head/Write/Read panics.

	repos        []string
	tagsByRepo   map[string][]string
	catalogErr   error
	tagsErrRepo  string
	tagsErr      error
	catalogCalls int
	tagsCalls    []string
}

func (r *recordingRegistry) Catalog(ctx context.Context) ([]string, error) {
	r.catalogCalls++
	if r.catalogErr != nil {
		return nil, r.catalogErr
	}
	out := make([]string, len(r.repos))
	copy(out, r.repos)
	return out, nil
}

func (r *recordingRegistry) Tags(ctx context.Context, repo string) ([]string, error) {
	r.tagsCalls = append(r.tagsCalls, repo)
	if r.tagsErr != nil && (r.tagsErrRepo == "" || r.tagsErrRepo == repo) {
		return nil, r.tagsErr
	}
	tags, ok := r.tagsByRepo[repo]
	if !ok {
		return []string{}, nil
	}
	return tags, nil
}

// ---------- tests ----------

// TestLs_EmptyCatalog: no repos pushed. Human mode prints a friendly
// message; structured mode returns {repositories: []}.
func TestLs_EmptyCatalog(t *testing.T) {
	host := testServer(t)
	writeLsCredsFile(t, healthyLsCredsBody(host))

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls: %v", err)
	}

	if got := out.String(); !strings.Contains(got, "No repositories found.") {
		t.Errorf("expected empty-state message, got: %q", got)
	}

	// Now exercise structured mode.
	f.OutputFormatOverride = "json"
	streams, out, _ = newLsStreams()
	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls -o json: %v", err)
	}

	var payload struct {
		Repositories []repoRow `json:"repositories"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if len(payload.Repositories) != 0 {
		t.Errorf("expected empty Repositories slice, got %+v", payload.Repositories)
	}
	// repositories key must be present as a concrete empty array, not null.
	if !strings.Contains(out.String(), `"repositories":`) {
		t.Errorf("json missing repositories key:\n%s", out.String())
	}
}

// TestLs_HumanTable: push 3 repos with varying tags and verify each name +
// tag count appears in the human output.
func TestLs_HumanTable(t *testing.T) {
	r, host := newTestRegistry(t)
	writeRandomImage(t, r, host+"/alpha:v1")
	writeRandomImage(t, r, host+"/beta:v1")
	writeRandomImage(t, r, host+"/beta:v2")
	writeRandomImage(t, r, host+"/gamma:latest")

	writeLsCredsFile(t, healthyLsCredsBody(host))

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls: %v", err)
	}

	got := out.String()
	for _, want := range []string{"REPOSITORY", "TAGS", "LAST PUSH", "alpha", "beta", "gamma"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	// beta has two tags; confirm the count column shows "2" somewhere on
	// the beta row (allowing width padding on either side).
	lines := strings.Split(got, "\n")
	var betaLine string
	for _, line := range lines {
		if strings.Contains(line, "beta") && !strings.Contains(line, "REPOSITORY") {
			betaLine = line
			break
		}
	}
	if betaLine == "" {
		t.Fatalf("no beta row in:\n%s", got)
	}
	if !strings.Contains(betaLine, " 2 ") && !strings.HasSuffix(strings.TrimSpace(betaLine), "--") {
		// Expect "2" somewhere inside the TAGS column.
		if !strings.Contains(betaLine, "2") {
			t.Errorf("beta row missing tag count 2: %q", betaLine)
		}
	}
}

// TestLs_JSON: structured JSON output contains the right shape + tag counts.
func TestLs_JSON(t *testing.T) {
	r, host := newTestRegistry(t)
	writeRandomImage(t, r, host+"/alpha:v1")
	writeRandomImage(t, r, host+"/beta:v1")
	writeRandomImage(t, r, host+"/beta:v2")

	writeLsCredsFile(t, healthyLsCredsBody(host))

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls -o json: %v", err)
	}

	var payload struct {
		Repositories []repoRow `json:"repositories"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}

	tagByRepo := map[string]int{}
	for _, row := range payload.Repositories {
		tagByRepo[row.Repository] = row.TagCount
	}
	if tagByRepo["alpha"] != 1 {
		t.Errorf("alpha tag count = %d, want 1 (rows: %+v)", tagByRepo["alpha"], payload.Repositories)
	}
	if tagByRepo["beta"] != 2 {
		t.Errorf("beta tag count = %d, want 2 (rows: %+v)", tagByRepo["beta"], payload.Repositories)
	}

	// LastPushAt should serialize to JSON null or be omitted per omitempty.
	if strings.Contains(out.String(), `"last_push_at":"`) {
		t.Errorf("unexpected non-nil last_push_at in output:\n%s", out.String())
	}
}

// TestLs_YAML: structured YAML output parses and contains expected keys.
func TestLs_YAML(t *testing.T) {
	r, host := newTestRegistry(t)
	writeRandomImage(t, r, host+"/only-one:v1")

	writeLsCredsFile(t, healthyLsCredsBody(host))

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "yaml"
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls -o yaml: %v", err)
	}

	var payload struct {
		Repositories []struct {
			Repository string `yaml:"repository"`
			TagCount   int    `yaml:"tag_count"`
		} `yaml:"repositories"`
	}
	if err := yaml.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid yaml: %v\n%s", err, out.String())
	}
	if len(payload.Repositories) != 1 {
		t.Fatalf("expected 1 repo, got %d: %+v", len(payload.Repositories), payload.Repositories)
	}
	if payload.Repositories[0].Repository != "only-one" {
		t.Errorf("Repository = %q, want only-one", payload.Repositories[0].Repository)
	}
	if payload.Repositories[0].TagCount != 1 {
		t.Errorf("TagCount = %d, want 1", payload.Repositories[0].TagCount)
	}
}

// TestLs_LimitCapsMetadata: push 5 repos, --limit=2 → repos 3/4/5 have "--"
// TAGS while repos 1/2 have real counts. Catalog still returns all 5.
func TestLs_LimitCapsMetadata(t *testing.T) {
	r, host := newTestRegistry(t)
	for _, name := range []string{"r1", "r2", "r3", "r4", "r5"} {
		writeRandomImage(t, r, host+"/"+name+":v1")
	}

	writeLsCredsFile(t, healthyLsCredsBody(host))

	// Verify Catalog shape directly — ensures all 5 names come back.
	reg := buildClient(&options.RegistryCredentials{Endpoint: host, Username: "u", Secret: "p"}, RetryConfig{})
	allRepos, err := reg.Catalog(context.Background())
	if err != nil {
		t.Fatalf("direct Catalog: %v", err)
	}
	if len(allRepos) != 5 {
		t.Fatalf("pre-check: expected 5 repos on server, got %d: %v", len(allRepos), allRepos)
	}

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams, "--limit", "2"); err != nil {
		t.Fatalf("ls --limit 2: %v", err)
	}

	var payload lsPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}

	if len(payload.Repositories) != 5 {
		t.Fatalf("expected 5 rows (Catalog should list all), got %d: %+v",
			len(payload.Repositories), payload.Repositories)
	}

	// First 2 rows have tag_count=1 (the image we pushed).
	for i := 0; i < 2; i++ {
		if payload.Repositories[i].TagCount != 1 {
			t.Errorf("row %d (%s): TagCount = %d, want 1 (should be looked up)",
				i, payload.Repositories[i].Repository, payload.Repositories[i].TagCount)
		}
	}
	// Remaining rows have tag_count=0 (sentinel stripped in structured
	// output — they were NOT looked up).
	for i := 2; i < 5; i++ {
		if payload.Repositories[i].TagCount != 0 {
			t.Errorf("row %d (%s): TagCount = %d, want 0 (should NOT be looked up)",
				i, payload.Repositories[i].Repository, payload.Repositories[i].TagCount)
		}
	}

	// Re-run in human mode; rows past the cap should render "--" under TAGS.
	f.OutputFormatOverride = ""
	streams, out, _ = newLsStreams()
	if err := runLsForTest(t, f, streams, "--limit", "2"); err != nil {
		t.Fatalf("ls --limit 2 (human): %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "--") {
		t.Errorf("expected dashes for unlooked-up tag counts, got:\n%s", got)
	}
}

// TestLs_AllOverridesLimit: --limit=1 --all → all 3 repos have real counts.
func TestLs_AllOverridesLimit(t *testing.T) {
	r, host := newTestRegistry(t)
	for _, name := range []string{"aa", "bb", "cc"} {
		writeRandomImage(t, r, host+"/"+name+":v1")
	}

	writeLsCredsFile(t, healthyLsCredsBody(host))

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams, "--limit", "1", "--all"); err != nil {
		t.Fatalf("ls --limit 1 --all: %v", err)
	}

	var payload lsPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if len(payload.Repositories) != 3 {
		t.Fatalf("expected 3 repos, got %d: %+v", len(payload.Repositories), payload.Repositories)
	}
	for _, row := range payload.Repositories {
		if row.TagCount != 1 {
			t.Errorf("repo %s: TagCount = %d, want 1 (--all should look up every repo)",
				row.Repository, row.TagCount)
		}
	}
}

// TestLs_LimitZeroUnlimited: --limit 0 is the Unix convention for "no cap".
// It behaves identically to --all.
func TestLs_LimitZeroUnlimited(t *testing.T) {
	r, host := newTestRegistry(t)
	for _, name := range []string{"aa", "bb", "cc"} {
		writeRandomImage(t, r, host+"/"+name+":v1")
	}

	writeLsCredsFile(t, healthyLsCredsBody(host))

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams, "--limit", "0"); err != nil {
		t.Fatalf("ls --limit 0: %v", err)
	}

	var payload lsPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if len(payload.Repositories) != 3 {
		t.Fatalf("expected 3 repos, got %d", len(payload.Repositories))
	}
	for _, row := range payload.Repositories {
		if row.TagCount != 1 {
			t.Errorf("repo %s: TagCount = %d, want 1 (--limit 0 = unlimited)",
				row.Repository, row.TagCount)
		}
	}
}

// TestLs_ExpiredCreds: expired credentials short-circuit before any network
// call. The fake Registry's Catalog panics — the test must NOT hit it.
func TestLs_ExpiredCreds(t *testing.T) {
	fake := &recordingRegistry{repos: []string{"should-never-be-listed"}}
	withFakeRegistry(t, fake)

	writeLsCredsFile(t, expiredLsCredsBody("vccr.io"))

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runLsForTest(t, f, streams)
	if err == nil {
		t.Fatal("expected error for expired creds")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryCredentialExpired {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryCredentialExpired)
	}
	if fake.catalogCalls != 0 {
		t.Errorf("Catalog should not be called on expired creds, got %d call(s)", fake.catalogCalls)
	}
}

// TestLs_NotConfigured: missing credentials file surfaces
// registry_not_configured without touching the network.
func TestLs_NotConfigured(t *testing.T) {
	fake := &recordingRegistry{}
	withFakeRegistry(t, fake)

	// Point at a non-existent credentials file.
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", missing)
	t.Setenv("VERDA_HOME", dir)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runLsForTest(t, f, streams)
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryNotConfigured {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryNotConfigured)
	}
	if fake.catalogCalls != 0 {
		t.Errorf("Catalog should not be called when not configured, got %d call(s)", fake.catalogCalls)
	}
}

// catalogOnlyRegistry is a Registry that only implements Catalog via the
// supplied function; Head/Write/Read nil-dispatch through the embedded
// Registry and panic if called.
type catalogOnlyRegistry struct {
	Registry // nil; accidental dispatch panics.
	fn       func(ctx context.Context) ([]string, error)
	tagsFn   func(ctx context.Context, repo string) ([]string, error)
}

func (r *catalogOnlyRegistry) Catalog(ctx context.Context) ([]string, error) {
	return r.fn(ctx)
}

func (r *catalogOnlyRegistry) Tags(ctx context.Context, repo string) ([]string, error) {
	if r.tagsFn != nil {
		return r.tagsFn(ctx, repo)
	}
	return nil, errors.New("tags not implemented")
}

// TestLs_NetworkError: Catalog returns a network error; ls translates it
// to registry_unreachable.
func TestLs_NetworkError(t *testing.T) {
	fake := &catalogOnlyRegistry{
		fn: func(ctx context.Context) ([]string, error) {
			return nil, errors.New("dial tcp: connection refused")
		},
	}
	withFakeRegistry(t, fake)

	writeLsCredsFile(t, healthyLsCredsBody("vccr.io"))

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runLsForTest(t, f, streams)
	if err == nil {
		t.Fatal("expected network error")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryUnreachable {
		t.Errorf("Code = %q, want %q (err=%v)", ae.Code, kindRegistryUnreachable, err)
	}
}

// TestLs_AuthFailure: Catalog returns a *transport.Error with
// UnauthorizedErrorCode; ls translates it to registry_auth_failed (since
// the creds are not expired, the expiry branch should be skipped).
func TestLs_AuthFailure(t *testing.T) {
	terr := &transport.Error{
		StatusCode: 401,
		Errors: []transport.Diagnostic{
			{Code: transport.UnauthorizedErrorCode, Message: "unauthorized"},
		},
	}
	fake := &catalogOnlyRegistry{
		fn: func(ctx context.Context) ([]string, error) {
			return nil, fmt.Errorf("catalog failed: %w", terr)
		},
	}
	withFakeRegistry(t, fake)

	writeLsCredsFile(t, healthyLsCredsBody("vccr.io"))

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runLsForTest(t, f, streams)
	if err == nil {
		t.Fatal("expected auth error")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryAuthFailed)
	}
}
