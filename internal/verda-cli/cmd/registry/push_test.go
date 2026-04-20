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
	"fmt"
	"net/http"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// ---------- push test helpers ----------

// runPushForTest invokes NewCmdPush with args so tests exercise the same
// flag-parsing path as the production binary.
func runPushForTest(t *testing.T, f cmdutil.Factory, streams cmdutil.IOStreams, args ...string) error {
	t.Helper()
	cmd := NewCmdPush(f, streams)
	cmd.SetArgs(args)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

// fakeSourceLoader returns canned images / errors per raw ref. Unknown
// refs produce an AgentError so tests surface accidental lookups.
type fakeSourceLoader struct {
	imagesByRef map[string]v1.Image
	errByRef    map[string]error

	// loadCalls records each (src, rawRef) pair seen so tests can assert
	// the loader was or was not invoked.
	loadCalls []struct {
		Source ImageSource
		Ref    string
	}
}

func (f *fakeSourceLoader) Load(_ context.Context, src ImageSource, ref string) (v1.Image, error) {
	f.loadCalls = append(f.loadCalls, struct {
		Source ImageSource
		Ref    string
	}{Source: src, Ref: ref})
	if err, ok := f.errByRef[ref]; ok {
		return nil, err
	}
	if img, ok := f.imagesByRef[ref]; ok {
		return img, nil
	}
	return nil, &cmdutil.AgentError{
		Code:     kindRegistryInvalidReference,
		Message:  fmt.Sprintf("fakeSourceLoader: unknown ref %q", ref),
		ExitCode: cmdutil.ExitBadArgs,
	}
}

// withFakeSourceLoader swaps sourceLoaderBuilder so pushOneImage resolves
// refs through fake. Restored at test cleanup.
func withFakeSourceLoader(t *testing.T, fake SourceLoader) {
	t.Helper()
	prev := sourceLoaderBuilder
	sourceLoaderBuilder = func(_ func(ctx context.Context) error) SourceLoader { return fake }
	t.Cleanup(func() { sourceLoaderBuilder = prev })
}

// healthyPushCredsBody is the push-specific counterpart to
// healthyVCRCredsBody in tags_test.go. Kept local so push tests don't
// depend on tags_test helpers (and vice versa).
func healthyPushCredsBody(host, project string) string {
	return `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = ` + host + `
verda_registry_project_id = ` + project + `
`
}

// expiredPushCredsBody mirrors expiredLsCredsBody but lives in this file
// so the helper is self-contained.
func expiredPushCredsBody(host string) string {
	return expiredLsCredsBody(host)
}

// randomPushImage is a tiny wrapper around ggcr's random image generator
// used by all push-test fixtures.
func randomPushImage(t *testing.T) v1.Image {
	t.Helper()
	img, err := random.Image(512, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	return img
}

// ---------- tests ----------

// TestPush_SingleImageHappy: one fake image, push succeeds, the in-process
// registry reports the same digest back.
func TestPush_SingleImageHappy(t *testing.T) {
	host := testServer(t)
	writeLsCredsFile(t, healthyPushCredsBody(host, "proj"))

	img := randomPushImage(t)
	fake := &fakeSourceLoader{imagesByRef: map[string]v1.Image{"my-app:v1": img}}
	withFakeSourceLoader(t, fake)

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newLsStreams()

	if err := runPushForTest(t, f, streams, "my-app:v1"); err != nil {
		t.Fatalf("push: %v", err)
	}

	// Verify the image appears at the destination by reading it back through
	// the production Registry (not the swap point).
	dstReg := newGGCRRegistry(testCreds(host))
	dstRef := host + "/proj/my-app:v1"
	desc, err := dstReg.Head(context.Background(), dstRef)
	if err != nil {
		t.Fatalf("Head(%s): %v", dstRef, err)
	}
	wantDigest, _ := img.Digest()
	if desc.Digest != wantDigest {
		t.Fatalf("digest mismatch: got %s, want %s", desc.Digest, wantDigest)
	}

	got := out.String()
	if !strings.Contains(got, "pushed my-app:v1 ->") {
		t.Errorf("expected human summary line, got:\n%s", got)
	}
}

// TestPush_MultipleImagesSequential: three fake images push in order to
// separate destinations.
func TestPush_MultipleImagesSequential(t *testing.T) {
	host := testServer(t)
	writeLsCredsFile(t, healthyPushCredsBody(host, "proj"))

	imgA := randomPushImage(t)
	imgB := randomPushImage(t)
	imgC := randomPushImage(t)
	fake := &fakeSourceLoader{imagesByRef: map[string]v1.Image{
		"alpha:v1": imgA,
		"beta:v1":  imgB,
		"gamma:v1": imgC,
	}}
	withFakeSourceLoader(t, fake)

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newLsStreams()

	if err := runPushForTest(t, f, streams, "alpha:v1", "beta:v1", "gamma:v1"); err != nil {
		t.Fatalf("push: %v", err)
	}

	dstReg := newGGCRRegistry(testCreds(host))
	cases := []struct {
		ref string
		img v1.Image
	}{
		{host + "/proj/alpha:v1", imgA},
		{host + "/proj/beta:v1", imgB},
		{host + "/proj/gamma:v1", imgC},
	}
	for _, c := range cases {
		desc, err := dstReg.Head(context.Background(), c.ref)
		if err != nil {
			t.Fatalf("Head(%s): %v", c.ref, err)
		}
		want, _ := c.img.Digest()
		if desc.Digest != want {
			t.Errorf("%s: digest = %s, want %s", c.ref, desc.Digest, want)
		}
	}

	// Sanity: all 3 loads happened in order.
	if len(fake.loadCalls) != 3 {
		t.Fatalf("loader invoked %d times, want 3", len(fake.loadCalls))
	}
	wantOrder := []string{"alpha:v1", "beta:v1", "gamma:v1"}
	for i, want := range wantOrder {
		if fake.loadCalls[i].Ref != want {
			t.Errorf("loadCalls[%d].Ref = %q, want %q", i, fake.loadCalls[i].Ref, want)
		}
	}

	got := out.String()
	for _, want := range wantOrder {
		if !strings.Contains(got, "pushed "+want) {
			t.Errorf("missing summary line for %s:\n%s", want, got)
		}
	}
}

// TestPush_RepoAndTagOverride: --repo custom/path --tag prod should land
// at <host>/<project>/custom/path:prod.
func TestPush_RepoAndTagOverride(t *testing.T) {
	host := testServer(t)
	writeLsCredsFile(t, healthyPushCredsBody(host, "proj"))

	img := randomPushImage(t)
	fake := &fakeSourceLoader{imagesByRef: map[string]v1.Image{"my-app:v1": img}}
	withFakeSourceLoader(t, fake)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	if err := runPushForTest(t, f, streams,
		"my-app:v1", "--repo", "custom/path", "--tag", "prod"); err != nil {
		t.Fatalf("push: %v", err)
	}

	dstReg := newGGCRRegistry(testCreds(host))
	dstRef := host + "/proj/custom/path:prod"
	desc, err := dstReg.Head(context.Background(), dstRef)
	if err != nil {
		t.Fatalf("Head(%s): %v", dstRef, err)
	}
	want, _ := img.Digest()
	if desc.Digest != want {
		t.Errorf("digest = %s, want %s", desc.Digest, want)
	}
}

// TestPush_RepoFlagRejectedWithMultipleArgs: multi-image + --repo → usage
// error before any network call. The loader must not be invoked.
func TestPush_RepoFlagRejectedWithMultipleArgs(t *testing.T) {
	host := testServer(t)
	writeLsCredsFile(t, healthyPushCredsBody(host, "proj"))

	fake := &fakeSourceLoader{}
	withFakeSourceLoader(t, fake)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runPushForTest(t, f, streams, "alpha:v1", "beta:v1", "--repo", "foo")
	if err == nil {
		t.Fatal("expected usage error for multi-image + --repo")
	}
	if !strings.Contains(err.Error(), "--repo and --tag cannot be combined") {
		t.Errorf("unexpected error message: %v", err)
	}
	if len(fake.loadCalls) != 0 {
		t.Errorf("loader should not be called, got %d call(s)", len(fake.loadCalls))
	}
}

// TestPush_ExpiredCreds: expired creds short-circuit before any loader
// or registry call.
func TestPush_ExpiredCreds(t *testing.T) {
	writeLsCredsFile(t, expiredPushCredsBody("vccr.io"))

	fake := &fakeSourceLoader{}
	withFakeSourceLoader(t, fake)

	// Also swap the registry client so any accidental Write would panic.
	fakeReg := &recordingRegistry{}
	withFakeRegistry(t, fakeReg)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runPushForTest(t, f, streams, "my-app:v1")
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
	if len(fake.loadCalls) != 0 {
		t.Errorf("loader should not be called on expired creds, got %d call(s)", len(fake.loadCalls))
	}
}

// TestPush_NotConfigured: missing creds → registry_not_configured. The
// loader must not be invoked.
func TestPush_NotConfigured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", dir+"/does-not-exist")
	t.Setenv("VERDA_HOME", dir)

	fake := &fakeSourceLoader{}
	withFakeSourceLoader(t, fake)

	fakeReg := &recordingRegistry{}
	withFakeRegistry(t, fakeReg)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runPushForTest(t, f, streams, "my-app:v1")
	if err == nil {
		t.Fatal("expected error for missing creds")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryNotConfigured {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryNotConfigured)
	}
	if len(fake.loadCalls) != 0 {
		t.Errorf("loader should not be called when creds missing, got %d call(s)", len(fake.loadCalls))
	}
}

// TestPush_SourceLoaderFailure: fake loader returns err for the SECOND
// image. First image pushes successfully; second surfaces the error in
// results; the command exits non-zero.
func TestPush_SourceLoaderFailure(t *testing.T) {
	host := testServer(t)
	writeLsCredsFile(t, healthyPushCredsBody(host, "proj"))

	imgA := randomPushImage(t)
	loaderErr := &cmdutil.AgentError{
		Code:     kindRegistryInvalidReference,
		Message:  "synthetic loader error",
		ExitCode: cmdutil.ExitBadArgs,
	}
	fake := &fakeSourceLoader{
		imagesByRef: map[string]v1.Image{"alpha:v1": imgA},
		errByRef:    map[string]error{"bad:v1": loaderErr},
	}
	withFakeSourceLoader(t, fake)

	f := cmdutil.NewTestFactory(nil)
	streams, out, errOut := newLsStreams()

	err := runPushForTest(t, f, streams, "alpha:v1", "bad:v1")
	if err == nil {
		t.Fatal("expected non-nil exit error after loader failure")
	}
	if !errors.Is(err, loaderErr) {
		var ae *cmdutil.AgentError
		if !errors.As(err, &ae) || ae.Code != kindRegistryInvalidReference {
			t.Fatalf("expected the loader error to surface, got %v", err)
		}
	}

	// Verify the first image was actually pushed to the destination.
	dstReg := newGGCRRegistry(testCreds(host))
	desc, hErr := dstReg.Head(context.Background(), host+"/proj/alpha:v1")
	if hErr != nil {
		t.Fatalf("Head(alpha): %v", hErr)
	}
	want, _ := imgA.Digest()
	if desc.Digest != want {
		t.Errorf("alpha digest = %s, want %s", desc.Digest, want)
	}

	// Summary: alpha → pushed line on Out, bad → FAILED line on ErrOut.
	if !strings.Contains(out.String(), "pushed alpha:v1") {
		t.Errorf("expected pushed line for alpha:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "FAILED bad:v1") {
		t.Errorf("expected FAILED line for bad:\n%s", errOut.String())
	}
}

// writeErrorRegistry is a Registry whose Write always returns a canned
// transport error. Used by the auth-failure test to drive the error
// translator without configuring a fake HTTP server.
type writeErrorRegistry struct {
	Registry // nil; other methods panic if accidentally dispatched.
	writeErr error
}

func (r *writeErrorRegistry) Write(_ context.Context, _ string, _ v1.Image, opts WriteOptions) error {
	// Close the progress channel so drainProgress terminates cleanly —
	// ggcr does this in production and push waits on drain to finish.
	if opts.Progress != nil {
		close(opts.Progress)
	}
	return r.writeErr
}

// TestPush_AuthFailureAtWrite: Registry.Write returns a transport.Error
// with UnauthorizedErrorCode; push translates it to registry_auth_failed.
func TestPush_AuthFailureAtWrite(t *testing.T) {
	// Minimal creds — no expiry → NOT expired → translator falls through
	// to registry_auth_failed instead of registry_credential_expired.
	writeLsCredsFile(t, `[default]
verda_registry_username = vcr-abc+cli
verda_registry_secret = s3cret
verda_registry_endpoint = vccr.io
verda_registry_project_id = proj
`)

	img := randomPushImage(t)
	fake := &fakeSourceLoader{imagesByRef: map[string]v1.Image{"my-app:v1": img}}
	withFakeSourceLoader(t, fake)

	authErr := &transport.Error{
		Errors: []transport.Diagnostic{
			{Code: transport.UnauthorizedErrorCode, Message: "unauthorized"},
		},
		StatusCode: http.StatusUnauthorized,
	}
	reg := &writeErrorRegistry{writeErr: authErr}
	origBuilder := clientBuilder
	clientBuilder = func(_ *options.RegistryCredentials, _ RetryConfig) Registry { return reg }
	t.Cleanup(func() { clientBuilder = origBuilder })

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runPushForTest(t, f, streams, "my-app:v1")
	if err == nil {
		t.Fatal("expected error after auth failure")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q (err=%v)", ae.Code, kindRegistryAuthFailed, err)
	}
}

// TestPush_JSONOutput: -o json produces a parseable payload with per-
// image rows; no human progress text bleeds into Out.
func TestPush_JSONOutput(t *testing.T) {
	host := testServer(t)
	writeLsCredsFile(t, healthyPushCredsBody(host, "proj"))

	img := randomPushImage(t)
	fake := &fakeSourceLoader{imagesByRef: map[string]v1.Image{"my-app:v1": img}}
	withFakeSourceLoader(t, fake)

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, errOut := newLsStreams()

	if err := runPushForTest(t, f, streams, "my-app:v1"); err != nil {
		t.Fatalf("push -o json: %v", err)
	}

	var payload pushPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if len(payload.Results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(payload.Results), payload.Results)
	}
	row := payload.Results[0]
	if row.Status != "pushed" {
		t.Errorf("Status = %q, want pushed", row.Status)
	}
	if row.Source != "my-app:v1" {
		t.Errorf("Source = %q, want my-app:v1", row.Source)
	}
	wantDst := host + "/proj/my-app:v1"
	if row.Destination != wantDst {
		t.Errorf("Destination = %q, want %q", row.Destination, wantDst)
	}
	if row.Error != "" {
		t.Errorf("Error = %q, want empty", row.Error)
	}

	// No human progress lines should leak into stdout in JSON mode.
	if strings.Contains(out.String(), "pushed my-app:v1 ->") {
		t.Errorf("unexpected human summary in JSON stdout:\n%s", out.String())
	}
	// ErrOut may carry the no-mount note etc., but must not carry a
	// progress "pushed layer data" line when progress output is gated off
	// by structured mode.
	if strings.Contains(errOut.String(), "pushed layer data") {
		t.Errorf("unexpected progress line in JSON stderr:\n%s", errOut.String())
	}
}

// TestPush_InteractiveMode_ReturnsNotYetWiredError: zero positional args
// in agent mode → structured error pointing users at the flag-driven
// usage. The interactive picker is Task 23/24.
func TestPush_InteractiveMode_ReturnsNotYetWiredError(t *testing.T) {
	writeLsCredsFile(t, healthyPushCredsBody("vccr.io", "proj"))

	fake := &fakeSourceLoader{}
	withFakeSourceLoader(t, fake)

	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true
	streams, _, _ := newLsStreams()

	err := runPushForTest(t, f, streams)
	if err == nil {
		t.Fatal("expected error when no args supplied")
	}
	if !strings.Contains(err.Error(), "interactive push picker is not yet wired") {
		t.Errorf("unexpected error: %v", err)
	}
	if len(fake.loadCalls) != 0 {
		t.Errorf("loader should not be called when no args, got %d call(s)", len(fake.loadCalls))
	}
}

// TestPush_ProgressLinePrinted: a successful push emits at least one
// "pushed ..." line on ErrOut in human mode.
func TestPush_ProgressLinePrinted(t *testing.T) {
	host := testServer(t)
	writeLsCredsFile(t, healthyPushCredsBody(host, "proj"))

	// Use a slightly bigger random image so ggcr emits multiple progress
	// updates and exercises the drain loop.
	img, err := random.Image(4096, 2)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}

	fake := &fakeSourceLoader{imagesByRef: map[string]v1.Image{"my-app:v1": img}}
	withFakeSourceLoader(t, fake)

	f := cmdutil.NewTestFactory(nil)
	streams, _, errOut := newLsStreams()

	if err := runPushForTest(t, f, streams, "my-app:v1"); err != nil {
		t.Fatalf("push: %v", err)
	}

	got := errOut.String()
	if !strings.Contains(got, "pushed layer data for ") {
		t.Errorf("expected progress line on ErrOut, got:\n%s", got)
	}
}

// TestPush_NoMountWarning: --no-mount surfaces the "not yet wired" note.
func TestPush_NoMountWarning(t *testing.T) {
	host := testServer(t)
	writeLsCredsFile(t, healthyPushCredsBody(host, "proj"))

	img := randomPushImage(t)
	fake := &fakeSourceLoader{imagesByRef: map[string]v1.Image{"my-app:v1": img}}
	withFakeSourceLoader(t, fake)

	f := cmdutil.NewTestFactory(nil)
	streams, _, errOut := newLsStreams()

	if err := runPushForTest(t, f, streams, "my-app:v1", "--no-mount"); err != nil {
		t.Fatalf("push --no-mount: %v", err)
	}

	got := errOut.String()
	if !strings.Contains(got, "--no-mount is not yet wired") {
		t.Errorf("expected --no-mount warning on ErrOut, got:\n%s", got)
	}
}

// ---------- unit tests for resolveDestination ----------

// resolveDestination is the internals of the pipeline; exercising it
// directly guards against regressions in the short-vs-full ref heuristic.
func TestResolveDestination_TableDriven(t *testing.T) {
	creds := &options.RegistryCredentials{
		Endpoint:  "vccr.io",
		ProjectID: "proj",
	}
	cases := []struct {
		name string
		raw  string
		repo string
		tag  string
		want string
	}{
		{"short name only", "my-app", "", "", "vccr.io/proj/my-app:latest"},
		{"short with tag", "my-app:v1", "", "", "vccr.io/proj/my-app:v1"},
		{"multi-segment short", "team/app:v1", "", "", "vccr.io/proj/team/app:v1"},
		{"host prefix stripped", "localhost/app:v1", "", "", "vccr.io/proj/app:v1"},
		{"repo override", "my-app:v1", "other/app", "", "vccr.io/proj/other/app:v1"},
		{"tag override", "my-app:v1", "", "prod", "vccr.io/proj/my-app:prod"},
		{"repo and tag override", "my-app:v1", "other/app", "prod", "vccr.io/proj/other/app:prod"},
		{"digest stripped", "my-app@sha256:" + strings.Repeat("a", 64), "", "", "vccr.io/proj/my-app:latest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveDestination(tc.raw, creds, tc.repo, tc.tag)
			if err != nil {
				t.Fatalf("resolveDestination(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("resolveDestination(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
