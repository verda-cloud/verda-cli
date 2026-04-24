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
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// testServer spins up an in-process Docker Registry v2 server and
// returns the host:port the client should talk to. The server accepts
// any auth credentials.
func testServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	return u.Host
}

// testCreds builds RegistryCredentials pointing at host. The in-process
// registry ignores the credential values, so any non-empty strings work.
func testCreds(host string) *options.RegistryCredentials {
	return &options.RegistryCredentials{
		Endpoint: host,
		Username: "anyuser",
		Secret:   "anysecret",
	}
}

// newTestRegistry returns a Registry + a helper to build refs inside it.
func newTestRegistry(t *testing.T) (Registry, string) {
	t.Helper()
	host := testServer(t)
	return newGGCRRegistry(testCreds(host)), host
}

// writeRandomImage pushes a synthetic image to ref and returns it.
func writeRandomImage(t *testing.T, r Registry, ref string) v1.Image {
	t.Helper()
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	if err := r.Write(context.Background(), ref, img, WriteOptions{}); err != nil {
		t.Fatalf("Write %s: %v", ref, err)
	}
	return img
}

func TestRegistry_WriteThenRead(t *testing.T) {
	r, host := newTestRegistry(t)
	ref := host + "/proj/app:v1"

	srcImg := writeRandomImage(t, r, ref)

	gotImg, err := r.Read(context.Background(), ref)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	wantDigest, err := srcImg.Digest()
	if err != nil {
		t.Fatalf("src digest: %v", err)
	}
	gotDigest, err := gotImg.Digest()
	if err != nil {
		t.Fatalf("got digest: %v", err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("digest mismatch: got %s, want %s", gotDigest, wantDigest)
	}
}

func TestRegistry_Tags(t *testing.T) {
	r, host := newTestRegistry(t)

	writeRandomImage(t, r, host+"/proj/app:v1")
	writeRandomImage(t, r, host+"/proj/app:v2")

	tags, err := r.Tags(context.Background(), "proj/app")
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	sort.Strings(tags)

	want := map[string]bool{"v1": true, "v2": true}
	for _, got := range tags {
		delete(want, got)
	}
	if len(want) != 0 {
		t.Fatalf("missing tags: %v (got %v)", want, tags)
	}
}

func TestRegistry_Head(t *testing.T) {
	r, host := newTestRegistry(t)
	ref := host + "/proj/app:v1"

	srcImg := writeRandomImage(t, r, ref)

	desc, err := r.Head(context.Background(), ref)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if desc == nil {
		t.Fatal("Head returned nil descriptor")
	}

	wantDigest, err := srcImg.Digest()
	if err != nil {
		t.Fatalf("src digest: %v", err)
	}
	if desc.Digest != wantDigest {
		t.Fatalf("digest mismatch: got %s, want %s", desc.Digest, wantDigest)
	}
	if desc.Size <= 0 {
		t.Fatalf("expected positive size, got %d", desc.Size)
	}
}

func TestRegistry_WriteWithProgress(t *testing.T) {
	r, host := newTestRegistry(t)
	ref := host + "/proj/app:v1"

	img, err := random.Image(2048, 2)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}

	progress := make(chan v1.Update, 32)
	var updates []v1.Update
	done := make(chan struct{})
	go func() {
		defer close(done)
		for u := range progress {
			updates = append(updates, u)
		}
	}()

	if err := r.Write(context.Background(), ref, img, WriteOptions{Progress: progress}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// ggcr closes the progress channel when Write returns. Give the drain
	// goroutine a chance to finish under a bounded timeout.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("progress channel not closed after Write returned")
	}

	if len(updates) == 0 {
		t.Fatal("expected at least one progress update")
	}
}

func TestRegistry_WriteWithJobs(t *testing.T) {
	r, host := newTestRegistry(t)
	ref := host + "/proj/app:v1"

	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	// Non-zero Jobs should wire through WithJobs without error.
	if err := r.Write(context.Background(), ref, img, WriteOptions{Jobs: 3}); err != nil {
		t.Fatalf("Write with Jobs=3: %v", err)
	}
}

func TestRegistry_ContextCanceled(t *testing.T) {
	r, host := newTestRegistry(t)
	ref := host + "/proj/app:v1"

	writeRandomImage(t, r, ref)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled

	_, err := r.Read(ctx, ref)
	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
	// Accept any error type — the in-process server may surface the
	// cancellation as context.Canceled, a wrapped url.Error, or a
	// connection-reset. We only assert the call errored out.
	if errors.Is(err, context.Canceled) {
		return
	}
}

func TestBuildClient_UsesSwap(t *testing.T) {
	// Do NOT use t.Parallel — we're mutating a package-level var.
	called := false
	orig := clientBuilder
	t.Cleanup(func() { clientBuilder = orig })

	sentinel := newGGCRRegistry(testCreds("vccr.io"))
	clientBuilder = func(creds *options.RegistryCredentials, cfg RetryConfig) Registry {
		called = true
		return sentinel
	}

	got := buildClient(&options.RegistryCredentials{Endpoint: "vccr.io"}, RetryConfig{})
	if !called {
		t.Fatal("swapped builder not invoked")
	}
	if got != sentinel {
		t.Fatal("buildClient did not return the swapped-in Registry")
	}
}

func TestLoadCredsFromFactory_ProfileFallback(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials")
	contents := "[default]\n" +
		"verda_registry_username   = vcr-abc+cli\n" +
		"verda_registry_secret     = s3cret\n" +
		"verda_registry_endpoint   = vccr.io\n" +
		"verda_registry_project_id = abc\n"
	if err := os.WriteFile(credsPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write creds file: %v", err)
	}
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", credsPath)

	f := &cmdutil.TestFactory{
		OptionsOverride: &options.Options{
			AuthOptions: &options.AuthOptions{}, // Profile is empty
		},
	}

	creds, err := loadCredsFromFactory(f, "", "")
	if err != nil {
		t.Fatalf("loadCredsFromFactory: %v", err)
	}
	if creds.Username != "vcr-abc+cli" {
		t.Errorf("Username = %q, want vcr-abc+cli (empty means we hit synthetic DEFAULT section)", creds.Username)
	}
	if creds.Secret != "s3cret" {
		t.Errorf("Secret = %q, want s3cret", creds.Secret)
	}
	if creds.Endpoint != "vccr.io" {
		t.Errorf("Endpoint = %q, want vccr.io", creds.Endpoint)
	}
	if creds.ProjectID != "abc" {
		t.Errorf("ProjectID = %q, want abc", creds.ProjectID)
	}
}

func TestLoadCredsFromFactory_ExplicitOverride(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials")
	contents := "[default]\n" +
		"verda_registry_username = default-user\n" +
		"verda_registry_secret   = default-secret\n" +
		"verda_registry_endpoint = vccr.io\n" +
		"\n" +
		"[staging]\n" +
		"verda_registry_username = staging-user\n" +
		"verda_registry_secret   = staging-secret\n" +
		"verda_registry_endpoint = staging.vccr.io\n"
	if err := os.WriteFile(credsPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write creds file: %v", err)
	}
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", credsPath)

	f := &cmdutil.TestFactory{
		OptionsOverride: &options.Options{
			AuthOptions: &options.AuthOptions{Profile: "ignored"},
		},
	}

	creds, err := loadCredsFromFactory(f, "staging", "")
	if err != nil {
		t.Fatalf("loadCredsFromFactory: %v", err)
	}
	if creds.Username != "staging-user" {
		t.Errorf("Username = %q, want staging-user", creds.Username)
	}
	if creds.Secret != "staging-secret" {
		t.Errorf("Secret = %q, want staging-secret", creds.Secret)
	}
	if creds.Endpoint != "staging.vccr.io" {
		t.Errorf("Endpoint = %q, want staging.vccr.io", creds.Endpoint)
	}
}
