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
	"io"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
)

// runLsForTest exercises the real flag-parsing path so tests match
// production argv behavior.
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

// sampleRepos returns three RepositoryInfo rows with deterministic order-
// dependent fields so table/JSON assertions don't need wildcards.
func sampleRepos() []RepositoryInfo {
	t1 := time.Date(2026, 4, 22, 13, 40, 5, 0, time.UTC)
	t2 := time.Date(2026, 4, 22, 13, 37, 43, 0, time.UTC)
	return []RepositoryInfo{
		{Name: "library/hello-world", FullName: "abc/library/hello-world", ArtifactCount: 1, PullCount: 0, UpdateTime: t1},
		{Name: "public/autoscaler", FullName: "abc/public/autoscaler", ArtifactCount: 3, PullCount: 42, UpdateTime: t2},
	}
}

// TestLs_HappyPath_Human renders the table, asserting all rows + columns
// appear and that the name-sort is applied.
func TestLs_HappyPath_Human(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{repos: sampleRepos()}
	withFakeHarborLister(t, lister)

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"REPOSITORY", "ARTIFACTS", "PULLS", "UPDATED",
		"library/hello-world", "public/autoscaler",
		"2026-04-22 13:40:05", "42",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if lister.gotProject != "abc" {
		t.Errorf("projectName forwarded to lister = %q, want %q", lister.gotProject, "abc")
	}
	// Sort check: library/* must appear before public/* lexicographically.
	if idxLib := strings.Index(got, "library/hello-world"); idxLib == -1 ||
		idxLib > strings.Index(got, "public/autoscaler") {
		t.Errorf("expected library row before public row:\n%s", got)
	}
}

// TestLs_JSON verifies structured output carries the full schema.
func TestLs_JSON(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	withFakeHarborLister(t, &fakeLister{repos: sampleRepos()})

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls -o json: %v", err)
	}
	var payload lsPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if payload.Project != "abc" {
		t.Errorf("Project = %q, want abc", payload.Project)
	}
	if len(payload.Repositories) != 2 {
		t.Fatalf("len(Repositories) = %d, want 2: %+v", len(payload.Repositories), payload.Repositories)
	}
	if got := payload.Repositories[0].Name; got != "library/hello-world" {
		t.Errorf("first repo Name = %q, want library/hello-world", got)
	}
	if got := payload.Repositories[0].FullName; got != "abc/library/hello-world" {
		t.Errorf("first repo FullName = %q, want abc/library/hello-world", got)
	}
	if payload.Repositories[1].PullCount != 42 {
		t.Errorf("autoscaler PullCount = %d, want 42", payload.Repositories[1].PullCount)
	}
}

// TestLs_YAML mirrors TestLs_JSON for the YAML path.
func TestLs_YAML(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	withFakeHarborLister(t, &fakeLister{repos: sampleRepos()})

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "yaml"
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls -o yaml: %v", err)
	}
	var payload lsPayload
	if err := yaml.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid yaml: %v\n%s", err, out.String())
	}
	if len(payload.Repositories) != 2 {
		t.Fatalf("len(Repositories) = %d, want 2", len(payload.Repositories))
	}
}

// TestLs_EmptyProject renders a friendly empty message (not a bare table).
func TestLs_EmptyProject(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	withFakeHarborLister(t, &fakeLister{repos: []RepositoryInfo{}})

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "No repositories found") {
		t.Errorf("expected empty-state message, got:\n%s", got)
	}
}

// TestLs_NotConfigured collapses every "no usable creds" shape into the
// structured registry_not_configured agent error.
func TestLs_NotConfigured(t *testing.T) {
	writeLsCredsFile(t, "")

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runLsForTest(t, f, streams)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryNotConfigured {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryNotConfigured)
	}
}

// TestLs_Expired surfaces registry_credential_expired when the creds'
// ExpiresAt is in the past.
func TestLs_Expired(t *testing.T) {
	writeLsCredsFile(t, expiredLsCredsBody("vccr.io"))

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runLsForTest(t, f, streams)
	if err == nil {
		t.Fatalf("expected expiry error")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryCredentialExpired {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryCredentialExpired)
	}
}

// TestLs_MissingProjectID fails closed when a creds row lacks a
// project_id. Without this guard we'd pass "" to the Harbor lookup and
// get a confusing 404 from upstream.
func TestLs_MissingProjectID(t *testing.T) {
	future := time.Now().Add(24 * time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	body := `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = vccr.io
verda_registry_expires_at = ` + future + `
`
	writeLsCredsFile(t, body)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runLsForTest(t, f, streams)
	if err == nil {
		t.Fatalf("expected error for missing project id")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryNotConfigured {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryNotConfigured)
	}
}

// TestLs_ListError propagates a lister error through translateErrorWithExpiry.
// A 403-like agent error from the lister is passed through as-is (the
// translator only rewrites UNAUTHORIZED-for-expired-creds).
func TestLs_ListError(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	wantErr := &cmdutil.AgentError{
		Code:     kindRegistryAccessDenied,
		Message:  "denied",
		ExitCode: cmdutil.ExitAuth,
	}
	withFakeHarborLister(t, &fakeLister{err: wantErr})

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runLsForTest(t, f, streams)
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryAccessDenied {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryAccessDenied)
	}
}

// withForcedTTY flips isTerminalFn so the ls picker branch runs even
// though stdout is a *bytes.Buffer in tests.
func withForcedTTY(t *testing.T, on bool) {
	t.Helper()
	prev := isTerminalFn
	isTerminalFn = func(_ io.Writer) bool { return on }
	t.Cleanup(func() { isTerminalFn = prev })
}

// sampleArtifacts returns a canned artifact set for the
// "library/hello-world" repo, matching the layout surfaced by the
// Harbor staging probe (single artifact with a "latest" tag, 3.92 KiB,
// never pulled).
func sampleArtifacts() []ArtifactInfo {
	pushed := time.Date(2026, 4, 22, 13, 40, 5, 0, time.UTC)
	return []ArtifactInfo{
		{
			Digest:   "sha256:d1a8d0a4eeb63aff09f5f34d4d80505e0ba81905f36158cc3970d8e07179e59e",
			Tags:     []string{"latest"},
			Size:     4015,
			PushTime: pushed,
			// PullTime intentionally zero — renders as "--".
		},
	}
}

// TestLs_Interactive_DrillsIntoArtifacts exercises the full TTY path:
// picker selects the first repo, ls fetches artifacts, renders the
// detail card, then the picker loops and the user picks Exit.
//
// This is the headline test for the feature — if the wiring between
// prompter.Select, ListArtifacts, and the renderer regresses, this is
// the assertion that catches it.
func TestLs_Interactive_DrillsIntoArtifacts(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{
		repos: sampleRepos(),
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": sampleArtifacts(),
		},
	}
	withFakeHarborLister(t, lister)
	withForcedTTY(t, true)

	// Two selects: pick the first repo (library/hello-world after sort),
	// then pick Exit (index 2 == len(repos)).
	mock := tuitest.New().AddSelect(0).AddSelect(2)
	f := cmdutil.NewTestFactory(mock)
	streams, out, errOut := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls (interactive): %v", err)
	}
	if lister.gotArtifactRepo != "library/hello-world" {
		t.Errorf("ListArtifacts(repo) = %q, want %q",
			lister.gotArtifactRepo, "library/hello-world")
	}

	got := out.String()
	for _, want := range []string{
		"library/hello-world",
		"DIGEST", "TAGS", "SIZE", "PUSHED", "PULLED",
		"sha256:d1a8d0a4eeb6",
		"latest",
		"3.92 KiB",
		"2026-04-22 13:40:05",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("drill-down output missing %q:\n%s", want, got)
		}
	}

	// The header summary ("N repository(ies) in project X") goes to
	// ErrOut in the interactive path so piping stdout to jq / less
	// stays clean.
	if !strings.Contains(errOut.String(), "repository(ies)") {
		t.Errorf("expected summary on ErrOut, got:\n%s", errOut.String())
	}
}

// TestLs_Interactive_UntaggedArtifact verifies the renderer surfaces
// <untagged> for an artifact with an empty tag list (common for
// dangling referrer manifests and SBOM artifacts).
func TestLs_Interactive_UntaggedArtifact(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{
		repos: []RepositoryInfo{sampleRepos()[0]}, // library/hello-world only
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": {{
				Digest: "sha256:deadbeefcafef00d1234567890abcdef1234567890abcdef1234567890abcdef",
				Size:   1024,
			}},
		},
	}
	withFakeHarborLister(t, lister)
	withForcedTTY(t, true)

	mock := tuitest.New().AddSelect(0).AddSelect(1) // pick repo then Exit.
	f := cmdutil.NewTestFactory(mock)
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "<untagged>") {
		t.Errorf("expected <untagged> marker, got:\n%s", got)
	}
}

// TestLs_Interactive_ArtifactsErrorStaysInLoop simulates a transient
// ListArtifacts failure: the error is printed to stderr but the picker
// doesn't exit. Users can still pick Exit or try another repo.
func TestLs_Interactive_ArtifactsErrorStaysInLoop(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{
		repos:        sampleRepos(),
		artifactsErr: errors.New("transient: 502"),
	}
	withFakeHarborLister(t, lister)
	withForcedTTY(t, true)

	// First select: pick repo 0 (fails). Second: pick Exit.
	mock := tuitest.New().AddSelect(0).AddSelect(2)
	f := cmdutil.NewTestFactory(mock)
	streams, out, errOut := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(errOut.String(), "transient: 502") {
		t.Errorf("expected artifact error on ErrOut, got:\n%s", errOut.String())
	}
	// No image table should have been printed for the failed repo.
	if strings.Contains(out.String(), "DIGEST") {
		t.Errorf("expected no artifact table on failure, got:\n%s", out.String())
	}
}

// TestLs_Interactive_EmptyRepos short-circuits: an empty project prints
// the friendly message and never enters the picker loop.
func TestLs_Interactive_EmptyRepos(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	withFakeHarborLister(t, &fakeLister{repos: []RepositoryInfo{}})
	withForcedTTY(t, true)

	// No selects queued — if the code path tried to prompt we'd block
	// on the mock; tuitest returns an error for unprimed calls, which
	// would bubble up as a test failure.
	f := cmdutil.NewTestFactory(tuitest.New())
	streams, out, _ := newLsStreams()

	if err := runLsForTest(t, f, streams); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(out.String(), "No repositories found") {
		t.Errorf("expected empty-state message, got:\n%s", out.String())
	}
}
