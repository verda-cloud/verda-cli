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

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	tuitest "github.com/verda-cloud/verdagostack/pkg/tui/testing"
)

// runDeleteForTest exercises the real flag-parsing path so test argv
// matches production behavior.
func runDeleteForTest(t *testing.T, f cmdutil.Factory, streams cmdutil.IOStreams, args ...string) error {
	t.Helper()
	cmd := NewCmdDelete(f, streams)
	cmd.SetArgs(args)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

// ---------- classifyTarget ----------

// TestClassifyTarget pins the three-way split the command relies on to
// decide repo vs artifact vs digest. Host:port and earlier ":" in full
// refs must not mis-classify.
func TestClassifyTarget(t *testing.T) {
	cases := []struct {
		in           string
		wantArtifact bool
		wantDigest   bool
	}{
		{"library/hello-world", false, false},
		{"hello-world", false, false},
		{"library/hello-world:latest", true, false},
		{"library/hello-world@sha256:abc", true, true},
		{"vccr.io/abc/library/hello-world", false, false},
		{"vccr.io/abc/library/hello-world:v1", true, false},
		{"vccr.io/abc/library/hello-world@sha256:abc", true, true},
		// Port in host must not leak into last-segment parsing.
		{"registry.local:5000/proj/app", false, false},
		{"registry.local:5000/proj/app:v2", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			isArt, isDig := classifyTarget(tc.in)
			if isArt != tc.wantArtifact || isDig != tc.wantDigest {
				t.Errorf("classifyTarget(%q) = (%v, %v), want (%v, %v)",
					tc.in, isArt, isDig, tc.wantArtifact, tc.wantDigest)
			}
		})
	}
}

// ---------- scripted (positional-argument) path ----------

// TestDelete_Repository_YesFlag is the happy path for scripted delete:
// positional repo + --yes dispatches to DeleteRepository with no
// prompts and reports the deletion.
func TestDelete_Repository_YesFlag(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": sampleArtifacts(),
		},
	}
	withFakeHarborLister(t, lister)

	f := cmdutil.NewTestFactory(tuitest.New())
	streams, out, _ := newLsStreams()

	if err := runDeleteForTest(t, f, streams, "library/hello-world", "--yes"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, want := lister.deletedRepos, []string{"library/hello-world"}; !equalStrings(got, want) {
		t.Errorf("deletedRepos = %v, want %v", got, want)
	}
	if len(lister.deletedArtifacts) != 0 {
		t.Errorf("unexpected artifact deletes: %+v", lister.deletedArtifacts)
	}
	if !strings.Contains(out.String(), "Deleted repository library/hello-world") {
		t.Errorf("expected success line, got:\n%s", out.String())
	}
	// Best-effort artifact count should be surfaced in the success
	// message when ListArtifacts succeeded (1 canned artifact).
	if !strings.Contains(out.String(), "1 artifact(s) removed") {
		t.Errorf("expected artifact count in success message, got:\n%s", out.String())
	}
}

// TestDelete_Artifact_ByTag dispatches the tag form to DeleteArtifact.
func TestDelete_Artifact_ByTag(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": sampleArtifacts(),
		},
	}
	withFakeHarborLister(t, lister)

	f := cmdutil.NewTestFactory(tuitest.New())
	streams, out, _ := newLsStreams()

	if err := runDeleteForTest(t, f, streams, "library/hello-world:latest", "--yes"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(lister.deletedArtifacts) != 1 {
		t.Fatalf("deletedArtifacts = %+v, want 1 entry", lister.deletedArtifacts)
	}
	if got := lister.deletedArtifacts[0]; got.Repo != "library/hello-world" || got.Reference != "latest" {
		t.Errorf("deletedArtifacts[0] = %+v, want repo=library/hello-world ref=latest", got)
	}
	if !strings.Contains(out.String(), "Deleted image latest") {
		t.Errorf("expected success line, got:\n%s", out.String())
	}
}

// TestDelete_Artifact_ByDigest dispatches the digest form to
// DeleteArtifact with the digest as the reference. The tag returned
// by a ListArtifacts lookup should NOT override the caller-supplied
// digest.
func TestDelete_Artifact_ByDigest(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	const digest = "sha256:d1a8d0a4eeb63aff09f5f34d4d80505e0ba81905f36158cc3970d8e07179e59e"
	lister := &fakeLister{
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": sampleArtifacts(),
		},
	}
	withFakeHarborLister(t, lister)

	f := cmdutil.NewTestFactory(tuitest.New())
	streams, _, _ := newLsStreams()

	if err := runDeleteForTest(t, f, streams, "library/hello-world@"+digest, "--yes"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(lister.deletedArtifacts) != 1 {
		t.Fatalf("deletedArtifacts = %+v, want 1 entry", lister.deletedArtifacts)
	}
	if got := lister.deletedArtifacts[0].Reference; got != digest {
		t.Errorf("reference = %q, want %q", got, digest)
	}
}

// TestDelete_NoTarget_NonTerminal refuses to be interactive when
// stdout isn't a terminal — scripts/CI must always pass a target.
func TestDelete_NoTarget_NonTerminal(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	withFakeHarborLister(t, &fakeLister{})

	f := cmdutil.NewTestFactory(tuitest.New())
	streams, _, _ := newLsStreams()

	err := runDeleteForTest(t, f, streams)
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryInvalidReference {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryInvalidReference)
	}
}

// TestDelete_CrossProjectRejected refuses to forward deletes aimed at
// a different project than the active credential covers. Without this
// guard the user would get a confusing 403 from the server.
func TestDelete_CrossProjectRejected(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{}
	withFakeHarborLister(t, lister)

	f := cmdutil.NewTestFactory(tuitest.New())
	streams, _, _ := newLsStreams()

	err := runDeleteForTest(t, f, streams,
		"vccr.io/other-project/library/hello-world", "--yes")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryInvalidReference {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryInvalidReference)
	}
	if len(lister.deletedRepos)+len(lister.deletedArtifacts) != 0 {
		t.Errorf("cross-project target must not reach the lister: %+v / %+v",
			lister.deletedRepos, lister.deletedArtifacts)
	}
}

// TestDelete_PropagatesDeleteBlocked surfaces registry_delete_blocked
// from the lister through the command unchanged (same pass-through
// pattern as ls's 403 test).
func TestDelete_PropagatesDeleteBlocked(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	blocked := &cmdutil.AgentError{
		Code:     kindRegistryDeleteBlocked,
		Message:  "blocked by retention rule",
		ExitCode: cmdutil.ExitAPI,
	}
	withFakeHarborLister(t, &fakeLister{
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": sampleArtifacts(),
		},
		deleteRepoErr: blocked,
	})

	f := cmdutil.NewTestFactory(tuitest.New())
	streams, _, _ := newLsStreams()

	err := runDeleteForTest(t, f, streams, "library/hello-world", "--yes")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryDeleteBlocked {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryDeleteBlocked)
	}
}

// ---------- agent mode ----------

// TestDelete_Agent_MissingYes returns CONFIRMATION_REQUIRED so
// automations can retry with --yes after prompting the human upstream.
// Same contract as vm delete in agent mode.
func TestDelete_Agent_MissingYes(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": sampleArtifacts(),
		},
	}
	withFakeHarborLister(t, lister)

	f := cmdutil.NewTestFactory(tuitest.New())
	f.AgentModeOverride = true
	f.OutputFormatOverride = "json"
	streams, _, _ := newLsStreams()

	err := runDeleteForTest(t, f, streams, "library/hello-world")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "CONFIRMATION_REQUIRED" {
		t.Errorf("Code = %q, want CONFIRMATION_REQUIRED", agentErr.Code)
	}
	if len(lister.deletedRepos) != 0 {
		t.Errorf("agent mode without --yes must not call DeleteRepository, got %v", lister.deletedRepos)
	}
}

// TestDelete_Agent_JSON verifies the structured envelope for repo
// deletes: action=delete_repository, status=completed, a best-effort
// deleted_artifacts count.
func TestDelete_Agent_JSON(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": sampleArtifacts(),
		},
	}
	withFakeHarborLister(t, lister)

	f := cmdutil.NewTestFactory(tuitest.New())
	f.AgentModeOverride = true
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runDeleteForTest(t, f, streams, "library/hello-world", "--yes"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var payload deleteResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if payload.Action != deleteActionRepository {
		t.Errorf("Action = %q, want %q", payload.Action, deleteActionRepository)
	}
	if payload.Repository != "library/hello-world" {
		t.Errorf("Repository = %q", payload.Repository)
	}
	if payload.Status != "completed" {
		t.Errorf("Status = %q, want completed", payload.Status)
	}
	if payload.DeletedArtifacts != 1 {
		t.Errorf("DeletedArtifacts = %d, want 1", payload.DeletedArtifacts)
	}
}

// TestDelete_Agent_JSON_Artifact verifies the artifact-delete
// envelope, including the best-effort digest / removed_tags fields
// populated from the ListArtifacts pre-probe.
func TestDelete_Agent_JSON_Artifact(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": sampleArtifacts(),
		},
	}
	withFakeHarborLister(t, lister)

	f := cmdutil.NewTestFactory(tuitest.New())
	f.AgentModeOverride = true
	f.OutputFormatOverride = "json"
	streams, out, _ := newLsStreams()

	if err := runDeleteForTest(t, f, streams, "library/hello-world:latest", "--yes"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var payload deleteResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if payload.Action != deleteActionArtifact {
		t.Errorf("Action = %q, want %q", payload.Action, deleteActionArtifact)
	}
	if payload.Reference != "latest" {
		t.Errorf("Reference = %q, want latest", payload.Reference)
	}
	if !strings.HasPrefix(payload.Digest, "sha256:d1a8d0a4") {
		t.Errorf("Digest = %q, want sha256:d1a8d0a4… from ListArtifacts probe", payload.Digest)
	}
	if !equalStrings(payload.RemovedTags, []string{"latest"}) {
		t.Errorf("RemovedTags = %v, want [latest]", payload.RemovedTags)
	}
}

// ---------- interactive path ----------

// TestDelete_Interactive_RepositoryFlow drives the full interactive
// path for the repo-wide delete: list repos → pick → inner menu picks
// "Delete repository" → confirm yes → lister.DeleteRepository called.
func TestDelete_Interactive_RepositoryFlow(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{
		repos: sampleRepos(),
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": sampleArtifacts(),
		},
	}
	withFakeHarborLister(t, lister)
	withForcedTTY(t, true)

	mock := tuitest.New().
		AddSelect(0).     // pick library/hello-world (after sort)
		AddSelect(1).     // menu -> Delete repository
		AddConfirm(true). // confirm yes
		AddSelect(2)      // outer picker -> Exit

	f := cmdutil.NewTestFactory(mock)
	streams, out, _ := newLsStreams()

	if err := runDeleteForTest(t, f, streams); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !equalStrings(lister.deletedRepos, []string{"library/hello-world"}) {
		t.Errorf("deletedRepos = %v", lister.deletedRepos)
	}
	if !strings.Contains(out.String(), "Deleted repository library/hello-world") {
		t.Errorf("expected success line, got:\n%s", out.String())
	}
}

// TestDelete_Interactive_RepositoryFlow_Declined verifies that a "no"
// answer keeps the repo intact and keeps the loop alive (user picks
// Exit on the next outer iteration).
func TestDelete_Interactive_RepositoryFlow_Declined(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	lister := &fakeLister{repos: sampleRepos()}
	withFakeHarborLister(t, lister)
	withForcedTTY(t, true)

	mock := tuitest.New().
		AddSelect(0).      // pick library/hello-world
		AddSelect(1).      // menu -> Delete repository
		AddConfirm(false). // decline
		AddSelect(2)       // outer picker -> Exit

	f := cmdutil.NewTestFactory(mock)
	streams, _, errOut := newLsStreams()

	if err := runDeleteForTest(t, f, streams); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(lister.deletedRepos) != 0 {
		t.Errorf("decline path must not delete: %v", lister.deletedRepos)
	}
	if !strings.Contains(errOut.String(), "Canceled") {
		t.Errorf("expected Canceled message, got:\n%s", errOut.String())
	}
}

// TestDelete_Interactive_ImageBatch drives the image MultiSelect flow:
// pick repo → menu -> Delete image(s) → multi-select two rows → confirm
// yes → both artifacts deleted via their digests (tag 0 when no digest
// would be fallback — covered by unit of classifyTarget).
func TestDelete_Interactive_ImageBatch(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	arts := []ArtifactInfo{
		{Digest: "sha256:aaaa", Tags: []string{"v1"}, Size: 1024},
		{Digest: "sha256:bbbb", Tags: []string{"v2"}, Size: 2048},
		{Digest: "sha256:cccc", Tags: []string{"v3"}, Size: 4096},
	}
	lister := &fakeLister{
		repos: sampleRepos(),
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": arts,
		},
	}
	withFakeHarborLister(t, lister)
	withForcedTTY(t, true)

	mock := tuitest.New().
		AddSelect(0).                // pick library/hello-world
		AddSelect(0).                // menu -> Delete image(s)
		AddMultiSelect([]int{0, 2}). // pick v1 and v3
		AddConfirm(true).            // confirm
		AddSelect(2).                // menu -> Back to repository list (3rd choice = index 2)
		AddSelect(2)                 // outer picker -> Exit

	f := cmdutil.NewTestFactory(mock)
	streams, _, _ := newLsStreams()

	if err := runDeleteForTest(t, f, streams); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(lister.deletedArtifacts) != 2 {
		t.Fatalf("deletedArtifacts = %+v, want 2", lister.deletedArtifacts)
	}
	// The command prefers digest over tag when both are available.
	gotRefs := []string{
		lister.deletedArtifacts[0].Reference,
		lister.deletedArtifacts[1].Reference,
	}
	if !equalStrings(gotRefs, []string{"sha256:aaaa", "sha256:cccc"}) {
		t.Errorf("deleted references = %v, want [sha256:aaaa sha256:cccc]", gotRefs)
	}
}

// TestDelete_Interactive_ImageBatch_SelectAll mirrors the user pressing
// Ctrl+A inside the MultiSelect: every artifact is queued for delete in
// a single batch. tuitest's AddMultiSelect([]int{0,1,2,...}) emulates
// the final indices the real component would return after a Ctrl+A.
func TestDelete_Interactive_ImageBatch_SelectAll(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	arts := []ArtifactInfo{
		{Digest: "sha256:aaaa", Tags: []string{"v1"}},
		{Digest: "sha256:bbbb", Tags: []string{"v2"}},
		{Digest: "sha256:cccc", Tags: []string{"v3"}},
	}
	lister := &fakeLister{
		repos: sampleRepos(),
		artifactsByRepo: map[string][]ArtifactInfo{
			"library/hello-world": arts,
		},
	}
	withFakeHarborLister(t, lister)
	withForcedTTY(t, true)

	mock := tuitest.New().
		AddSelect(0).                   // pick library/hello-world
		AddSelect(0).                   // menu -> Delete image(s)
		AddMultiSelect([]int{0, 1, 2}). // Ctrl+A equivalent
		AddConfirm(true).
		AddSelect(2). // menu -> Back
		AddSelect(2)  // outer -> Exit

	f := cmdutil.NewTestFactory(mock)
	streams, _, _ := newLsStreams()

	if err := runDeleteForTest(t, f, streams); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(lister.deletedArtifacts) != 3 {
		t.Fatalf("deletedArtifacts = %+v, want 3", lister.deletedArtifacts)
	}
}

// TestDelete_Interactive_EmptyRepos short-circuits before the picker.
func TestDelete_Interactive_EmptyRepos(t *testing.T) {
	writeLsCredsFile(t, validLsCredsBody("vccr.io", "abc"))
	withFakeHarborLister(t, &fakeLister{repos: []RepositoryInfo{}})
	withForcedTTY(t, true)

	f := cmdutil.NewTestFactory(tuitest.New())
	streams, out, _ := newLsStreams()

	if err := runDeleteForTest(t, f, streams); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(out.String(), "No repositories found") {
		t.Errorf("expected empty-state message, got:\n%s", out.String())
	}
}

// ---------- shared helpers ----------

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
