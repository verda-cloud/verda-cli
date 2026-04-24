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

// Shared test plumbing for the registry package. These helpers originated
// in ls_test.go. `ls` has a checkered lifecycle — removed once when the
// VCR robot account lacked list permissions, then re-added when the
// permission was granted — so the helpers live in a command-neutral file
// and every cross-cutting test (tags / push / copy / ls) shares them.
//
// The `Ls` suffix on some identifiers is a legacy name retained for a
// lower-churn diff; renaming would touch every call site across the
// package.

package registry

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// newLsStreams returns IOStreams backed by buffers (no stdin needed).
func newLsStreams() (cmdutil.IOStreams, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return cmdutil.IOStreams{Out: out, ErrOut: errOut}, out, errOut
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

// withFakeHarborLister swaps harborListerBuilder to always return fake,
// restoring the original at test cleanup. The RepositoryLister interface
// is distinct from Registry (Harbor REST vs Docker Registry v2 — see
// harbor.go), hence the separate swap helper.
func withFakeHarborLister(t *testing.T, fake RepositoryLister) {
	t.Helper()
	orig := harborListerBuilder
	harborListerBuilder = func(creds *options.RegistryCredentials, cfg RetryConfig) RepositoryLister { return fake }
	t.Cleanup(func() { harborListerBuilder = orig })
}

// fakeLister is a minimal RepositoryLister used by ls / delete tests.
// It records invocations so tests can assert the CLI is forwarding
// projectName / repoName / reference values faithfully.
//
// Lookup maps follow the Harbor-client contract: keys use the
// project-relative RepositoryInfo.Name (e.g. "library/hello-world");
// missing keys return empty slices (ListArtifacts) or nil (the delete
// helpers), mirroring Harbor's 200-empty / 204-no-content shapes.
type fakeLister struct {
	repos      []RepositoryInfo
	err        error
	gotProject string
	callCount  int

	artifactsByRepo map[string][]ArtifactInfo
	artifactsErr    error
	gotArtifactRepo string

	// deleteRepoErr is returned by DeleteRepository when non-nil. When
	// nil, DeleteRepository records the repoName in deletedRepos and
	// returns nil.
	deleteRepoErr error
	deletedRepos  []string

	// deleteArtifactErr is returned by DeleteArtifact when non-nil.
	// When nil, DeleteArtifact records {repo, ref} in deletedArtifacts.
	deleteArtifactErr error
	deletedArtifacts  []fakeDeletedArtifact
}

// fakeDeletedArtifact captures a single DeleteArtifact call for
// assertion — which repo, which reference (tag or digest), in order.
type fakeDeletedArtifact struct {
	Repo      string
	Reference string
}

func (f *fakeLister) ListRepositories(_ context.Context, projectName string) ([]RepositoryInfo, error) {
	f.gotProject = projectName
	f.callCount++
	if f.err != nil {
		return nil, f.err
	}
	return f.repos, nil
}

func (f *fakeLister) ListArtifacts(_ context.Context, _ /* projectName */, repoName string) ([]ArtifactInfo, error) {
	f.gotArtifactRepo = repoName
	if f.artifactsErr != nil {
		return nil, f.artifactsErr
	}
	return f.artifactsByRepo[repoName], nil
}

func (f *fakeLister) DeleteRepository(_ context.Context, _, repoName string) error {
	f.deletedRepos = append(f.deletedRepos, repoName)
	return f.deleteRepoErr
}

func (f *fakeLister) DeleteArtifact(_ context.Context, _, repoName, reference string) error {
	f.deletedArtifacts = append(f.deletedArtifacts, fakeDeletedArtifact{
		Repo:      repoName,
		Reference: reference,
	})
	return f.deleteArtifactErr
}

// validLsCredsBody builds a non-expired credentials INI body for ls tests.
// Pairs with expiredLsCredsBody which simulates the expiry-error path.
func validLsCredsBody(host, project string) string {
	future := time.Now().Add(7 * 24 * time.Hour).UTC().Round(time.Second).Format(time.RFC3339)
	return `[default]
verda_registry_username = vcr-` + project + `+cli
verda_registry_secret = s3cret
verda_registry_endpoint = ` + host + `
verda_registry_project_id = ` + project + `
verda_registry_expires_at = ` + future + `
`
}

// recordingRegistry is a minimal in-memory Registry used by tags / push /
// copy tests. It embeds a nil Registry so any method a given test doesn't
// stub (Head/Write/Read) nil-dispatches — panicking loudly the moment a
// test accidentally leaves the pre-network pipeline.
type recordingRegistry struct {
	Registry // nil; accidental dispatch to Head/Write/Read panics.

	tagsByRepo  map[string][]string
	tagsErrRepo string
	tagsErr     error
	tagsCalls   []string
}

func (r *recordingRegistry) Tags(_ context.Context, repo string) ([]string, error) {
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
