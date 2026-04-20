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
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"go.yaml.in/yaml/v3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// runTagsForTest exercises the real flag-parsing path so tests match
// production argv behavior.
func runTagsForTest(t *testing.T, f cmdutil.Factory, streams cmdutil.IOStreams, args ...string) error {
	t.Helper()
	cmd := NewCmdTags(f, streams)
	cmd.SetArgs(args)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

// healthyVCRCredsBody builds an INI body with a VCR-style project so short
// refs can be expanded via Normalize. The endpoint defaults to the in-process
// test server host.
func healthyVCRCredsBody(host, project string) string {
	return `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = ` + host + `
verda_registry_project_id = ` + project + `
`
}

// ---------- tests ----------

// TestTags_HappyPath: push 3 tags to proj/my-app, assert each appears in the
// human table along with DIGEST/SIZE columns populated.
func TestTags_HappyPath(t *testing.T) {
	r, host := newTestRegistry(t)
	writeRandomImage(t, r, host+"/proj/my-app:v1")
	writeRandomImage(t, r, host+"/proj/my-app:v2")
	writeRandomImage(t, r, host+"/proj/my-app:latest")

	writeLsCredsFile(t, healthyVCRCredsBody(host, "proj"))

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newLsStreams()

	if err := runTagsForTest(t, f, streams, "my-app"); err != nil {
		t.Fatalf("tags my-app: %v", err)
	}

	got := out.String()
	for _, want := range []string{"TAG", "DIGEST", "SIZE", "PUSHED", "v1", "v2", "latest"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	// Digest should be truncated — look for the sha256:<12>… pattern.
	if !strings.Contains(got, "sha256:") || !strings.Contains(got, "…") {
		t.Errorf("expected truncated sha256 digest with ellipsis in human output:\n%s", got)
	}
	// Size column should carry a unit suffix (B, KiB, MiB, …).
	if !strings.Contains(got, "B") {
		t.Errorf("expected byte size suffix in human output:\n%s", got)
	}
}

// TestTags_JSON: structured JSON contains full digests (not truncated) and
// integer sizes.
func TestTags_JSON(t *testing.T) {
	r, host := newTestRegistry(t)
	writeRandomImage(t, r, host+"/proj/my-app:v1")
	writeRandomImage(t, r, host+"/proj/my-app:v2")
	writeRandomImage(t, r, host+"/proj/my-app:latest")

	writeLsCredsFile(t, healthyVCRCredsBody(host, "proj"))

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runTagsForTest(t, f, streams, "my-app"); err != nil {
		t.Fatalf("tags -o json: %v", err)
	}

	var payload tagsPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if !strings.Contains(payload.Repository, "/proj/my-app") {
		t.Errorf("Repository = %q, want suffix /proj/my-app", payload.Repository)
	}
	if len(payload.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %+v", len(payload.Tags), payload.Tags)
	}

	for _, row := range payload.Tags {
		// Full digest: "sha256:" + 64 hex chars = 71 runes.
		if !strings.HasPrefix(row.Digest, "sha256:") {
			t.Errorf("tag %s: Digest = %q, want sha256: prefix", row.Tag, row.Digest)
		}
		if len(row.Digest) != 71 {
			t.Errorf("tag %s: Digest len = %d, want 71 (full sha256)", row.Tag, len(row.Digest))
		}
		// Size must be a positive integer in JSON.
		if row.Size <= 0 {
			t.Errorf("tag %s: Size = %d, want > 0", row.Tag, row.Size)
		}
		// No ellipsis in JSON output.
		if strings.Contains(row.Digest, "…") {
			t.Errorf("tag %s: JSON digest must not be truncated, got %q", row.Tag, row.Digest)
		}
	}

	// PushedAt should not appear in JSON (v1 nil / omitempty).
	if strings.Contains(out.String(), `"pushed_at":"`) {
		t.Errorf("unexpected non-nil pushed_at in output:\n%s", out.String())
	}
}

// TestTags_YAML: structured YAML output parses and contains the expected
// shape.
func TestTags_YAML(t *testing.T) {
	r, host := newTestRegistry(t)
	writeRandomImage(t, r, host+"/proj/only-one:v1")

	writeLsCredsFile(t, healthyVCRCredsBody(host, "proj"))

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "yaml"
	streams, out, _ := newLsStreams()

	if err := runTagsForTest(t, f, streams, "only-one"); err != nil {
		t.Fatalf("tags -o yaml: %v", err)
	}

	var payload struct {
		Repository string `yaml:"repository"`
		Tags       []struct {
			Tag    string `yaml:"tag"`
			Digest string `yaml:"digest"`
			Size   int64  `yaml:"size"`
		} `yaml:"tags"`
	}
	if err := yaml.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid yaml: %v\n%s", err, out.String())
	}
	if !strings.Contains(payload.Repository, "/proj/only-one") {
		t.Errorf("Repository = %q, want suffix /proj/only-one", payload.Repository)
	}
	if len(payload.Tags) != 1 {
		t.Fatalf("expected 1 tag, got %d: %+v", len(payload.Tags), payload.Tags)
	}
	if payload.Tags[0].Tag != "v1" {
		t.Errorf("Tag = %q, want v1", payload.Tags[0].Tag)
	}
	if !strings.HasPrefix(payload.Tags[0].Digest, "sha256:") {
		t.Errorf("Digest = %q, want sha256: prefix", payload.Tags[0].Digest)
	}
	if payload.Tags[0].Size <= 0 {
		t.Errorf("Size = %d, want > 0", payload.Tags[0].Size)
	}
}

// TestTags_NoTagsForRepo: ask for tags on a repository that was never pushed.
// The ggcr in-process registry returns NAME_UNKNOWN (404) for unknown repos,
// which our translator maps to kind=registry_repo_not_found. The user gets an
// actionable error — NOT a silent crash.
func TestTags_NoTagsForRepo(t *testing.T) {
	_, host := newTestRegistry(t)
	writeLsCredsFile(t, healthyVCRCredsBody(host, "proj"))

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runTagsForTest(t, f, streams, "never-pushed")
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryRepoNotFound {
		t.Errorf("Code = %q, want %q (err=%v)", ae.Code, kindRegistryRepoNotFound, err)
	}
}

// TestTags_ShortRefExpanded: pass "my-app" short and verify it resolves to
// "<host>/proj/my-app" via Normalize + creds.
func TestTags_ShortRefExpanded(t *testing.T) {
	r, host := newTestRegistry(t)
	writeRandomImage(t, r, host+"/proj/my-app:v1")
	writeRandomImage(t, r, host+"/proj/my-app:v2")

	writeLsCredsFile(t, healthyVCRCredsBody(host, "proj"))

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runTagsForTest(t, f, streams, "my-app"); err != nil {
		t.Fatalf("tags my-app: %v", err)
	}
	var payload tagsPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	// The resolved Repository key should include host + project + repo.
	want := host + "/proj/my-app"
	if !strings.Contains(payload.Repository, want) {
		t.Errorf("Repository = %q, want to contain %q", payload.Repository, want)
	}
	if len(payload.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d: %+v", len(payload.Tags), payload.Tags)
	}
}

// TestTags_FullRefPreserved: pass a full reference directly; tags resolve the
// same way.
func TestTags_FullRefPreserved(t *testing.T) {
	r, host := newTestRegistry(t)
	writeRandomImage(t, r, host+"/proj/my-app:v1")
	writeRandomImage(t, r, host+"/proj/my-app:v2")

	writeLsCredsFile(t, healthyVCRCredsBody(host, "proj"))

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runTagsForTest(t, f, streams, host+"/proj/my-app"); err != nil {
		t.Fatalf("tags full-ref: %v", err)
	}
	var payload tagsPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	want := host + "/proj/my-app"
	if !strings.Contains(payload.Repository, want) {
		t.Errorf("Repository = %q, want to contain %q", payload.Repository, want)
	}
	if len(payload.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d: %+v", len(payload.Tags), payload.Tags)
	}
}

// TestTags_LimitCapsMetadata: push 5 tags, --limit=2. The first 2 rows have
// digest + size populated; the remaining 3 rows have Size=0 / Digest="" in
// structured output (the "not looked up" state — we do NOT use a negative
// sentinel because Size is naturally non-negative, and callers can detect
// "not looked up" via empty Digest).
func TestTags_LimitCapsMetadata(t *testing.T) {
	r, host := newTestRegistry(t)
	for _, tag := range []string{"a", "b", "c", "d", "e"} {
		writeRandomImage(t, r, host+"/proj/my-app:"+tag)
	}

	writeLsCredsFile(t, healthyVCRCredsBody(host, "proj"))

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runTagsForTest(t, f, streams, "my-app", "--limit", "2"); err != nil {
		t.Fatalf("tags --limit 2: %v", err)
	}

	var payload tagsPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if len(payload.Tags) != 5 {
		t.Fatalf("expected 5 rows (Tags should list all), got %d: %+v",
			len(payload.Tags), payload.Tags)
	}

	// First 2 rows: HEAD was called → Digest set, Size > 0.
	for i := 0; i < 2; i++ {
		if payload.Tags[i].Digest == "" {
			t.Errorf("row %d (%s): Digest empty, want populated (should be looked up)",
				i, payload.Tags[i].Tag)
		}
		if payload.Tags[i].Size <= 0 {
			t.Errorf("row %d (%s): Size = %d, want > 0", i, payload.Tags[i].Tag, payload.Tags[i].Size)
		}
	}
	// Remaining rows: no HEAD → Digest empty, Size == 0.
	for i := 2; i < 5; i++ {
		if payload.Tags[i].Digest != "" {
			t.Errorf("row %d (%s): Digest = %q, want empty (should NOT be looked up)",
				i, payload.Tags[i].Tag, payload.Tags[i].Digest)
		}
		if payload.Tags[i].Size != 0 {
			t.Errorf("row %d (%s): Size = %d, want 0 (should NOT be looked up)",
				i, payload.Tags[i].Tag, payload.Tags[i].Size)
		}
	}

	// Re-run in human mode; rows past the cap should render "--" under
	// DIGEST and SIZE.
	f.OutputFormatOverride = ""
	streams, out, _ = newLsStreams()
	if err := runTagsForTest(t, f, streams, "my-app", "--limit", "2"); err != nil {
		t.Fatalf("tags --limit 2 (human): %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "--") {
		t.Errorf("expected dashes for unlooked-up rows, got:\n%s", got)
	}
}

// TestTags_NotConfigured: missing credentials → registry_not_configured, and
// no network is hit (the fake Registry's Tags would panic on nil dispatch).
func TestTags_NotConfigured(t *testing.T) {
	fake := &recordingRegistry{}
	withFakeRegistry(t, fake)

	// Point creds env at a non-existent file.
	dir := t.TempDir()
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", dir+"/does-not-exist")
	t.Setenv("VERDA_HOME", dir)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	// Use a full ref so Normalize won't itself fail on missing creds — the
	// contract is that the "not configured" check fires first.
	err := runTagsForTest(t, f, streams, "vccr.io/proj/my-app")
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
	if len(fake.tagsCalls) != 0 {
		t.Errorf("Tags should not be called when not configured, got %d call(s)", len(fake.tagsCalls))
	}
}

// TestTags_ExpiredCreds: expired creds short-circuit before any network call.
func TestTags_ExpiredCreds(t *testing.T) {
	fake := &recordingRegistry{tagsByRepo: map[string][]string{"proj/my-app": {"v1"}}}
	withFakeRegistry(t, fake)

	writeLsCredsFile(t, expiredLsCredsBody("vccr.io"))

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runTagsForTest(t, f, streams, "my-app")
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
	if len(fake.tagsCalls) != 0 {
		t.Errorf("Tags should not be called on expired creds, got %d call(s)", len(fake.tagsCalls))
	}
}

// tagsOnlyRegistry is a Registry with only Tags (and optionally Head) wired
// up; other methods nil-dispatch through the embedded Registry and panic.
type tagsOnlyRegistry struct {
	Registry // nil; accidental dispatch to unset methods panics.
	tagsFn   func(ctx context.Context, repo string) ([]string, error)
	headFn   func(ctx context.Context, ref string) (*v1.Descriptor, error)
}

func (r *tagsOnlyRegistry) Tags(ctx context.Context, repo string) ([]string, error) {
	return r.tagsFn(ctx, repo)
}

func (r *tagsOnlyRegistry) Head(ctx context.Context, ref string) (*v1.Descriptor, error) {
	if r.headFn != nil {
		return r.headFn(ctx, ref)
	}
	return nil, errors.New("head not implemented")
}

// TestTags_NetworkError: Tags returns a connection-refused error; the
// command surfaces it as registry_unreachable.
func TestTags_NetworkError(t *testing.T) {
	fake := &tagsOnlyRegistry{
		tagsFn: func(ctx context.Context, repo string) ([]string, error) {
			return nil, errors.New("dial tcp: connection refused")
		},
	}
	// Install via the package-level swap point, same as ls_test.
	orig := clientBuilder
	clientBuilder = func(creds *options.RegistryCredentials, cfg RetryConfig) Registry { return fake }
	t.Cleanup(func() { clientBuilder = orig })

	writeLsCredsFile(t, healthyVCRCredsBody("vccr.io", "proj"))

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runTagsForTest(t, f, streams, "my-app")
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
