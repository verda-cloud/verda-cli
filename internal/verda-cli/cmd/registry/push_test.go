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
	"io"
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

// ---------- interactive picker helpers ----------

// fakePickerDaemon is a minimal DaemonLister double for the interactive
// picker tests. pingErr / listErr / images are read at call time so tests
// can mutate the struct before invoking the command.
type fakePickerDaemon struct {
	pingErr  error
	listErr  error
	images   []DaemonImage
	pingHits int
	listHits int
}

func (f *fakePickerDaemon) Ping(_ context.Context) error {
	f.pingHits++
	return f.pingErr
}

func (f *fakePickerDaemon) List(_ context.Context) ([]DaemonImage, error) {
	f.listHits++
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]DaemonImage, len(f.images))
	copy(out, f.images)
	return out, nil
}

// withFakeDaemonLister swaps daemonListerBuilder to return fake. The
// returned *fakePickerDaemon is the same pointer the builder hands out,
// so tests can mutate it after the swap.
func withFakeDaemonLister(t *testing.T, fake *fakePickerDaemon) {
	t.Helper()
	prev := daemonListerBuilder
	daemonListerBuilder = func() (DaemonLister, error) { return fake, nil }
	t.Cleanup(func() { daemonListerBuilder = prev })
}

// withTerminalErrOut pretends ErrOut is a real TTY so isInteractivePush
// returns true even with a *bytes.Buffer-backed stream. Restored on
// cleanup.
func withTerminalErrOut(t *testing.T, isTTY bool) {
	t.Helper()
	prev := isTerminalFn
	isTerminalFn = func(_ io.Writer) bool { return isTTY }
	t.Cleanup(func() { isTerminalFn = prev })
}

// withRunPickerFn swaps the picker driver with a stub that returns
// canned (refs, proceed) values. Restored on cleanup.
func withRunPickerFn(
	t *testing.T,
	stub func(ctx context.Context, ioStreams cmdutil.IOStreams, images []DaemonImage, creds *options.RegistryCredentials) ([]string, bool),
) {
	t.Helper()
	prev := runPickerFn
	runPickerFn = stub
	t.Cleanup(func() { runPickerFn = prev })
}

// TestPush_InteractiveMode_AgentModeErrors: zero positional args in
// agent mode → structured "requires a TTY" error. Never attempts a TUI,
// never dials the daemon, never invokes the source loader.
func TestPush_InteractiveMode_AgentModeErrors(t *testing.T) {
	writeLsCredsFile(t, healthyPushCredsBody("vccr.io", "proj"))

	fake := &fakeSourceLoader{}
	withFakeSourceLoader(t, fake)
	daemon := &fakePickerDaemon{}
	withFakeDaemonLister(t, daemon)

	f := cmdutil.NewTestFactory(nil)
	f.AgentModeOverride = true
	streams, _, _ := newLsStreams()

	err := runPushForTest(t, f, streams)
	if err == nil {
		t.Fatal("expected error when no args supplied in agent mode")
	}
	if !strings.Contains(err.Error(), "interactive push requires a TTY") {
		t.Errorf("unexpected error: %v", err)
	}
	if len(fake.loadCalls) != 0 {
		t.Errorf("loader should not be called in agent mode, got %d call(s)", len(fake.loadCalls))
	}
	if daemon.pingHits != 0 || daemon.listHits != 0 {
		t.Errorf("daemon should not be probed in agent mode, got ping=%d list=%d",
			daemon.pingHits, daemon.listHits)
	}
}

// TestPush_InteractiveMode_NonTTYErrors: zero positional args with
// ErrOut non-TTY → same "requires a TTY" error. Guards against a piped
// ErrOut accidentally entering the picker path.
func TestPush_InteractiveMode_NonTTYErrors(t *testing.T) {
	writeLsCredsFile(t, healthyPushCredsBody("vccr.io", "proj"))

	fake := &fakeSourceLoader{}
	withFakeSourceLoader(t, fake)
	daemon := &fakePickerDaemon{}
	withFakeDaemonLister(t, daemon)
	withTerminalErrOut(t, false) // ErrOut is NOT a TTY

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runPushForTest(t, f, streams)
	if err == nil {
		t.Fatal("expected error when no args supplied with non-TTY ErrOut")
	}
	if !strings.Contains(err.Error(), "interactive push requires a TTY") {
		t.Errorf("unexpected error: %v", err)
	}
	if daemon.pingHits != 0 || daemon.listHits != 0 {
		t.Errorf("daemon should not be probed without a TTY, got ping=%d list=%d",
			daemon.pingHits, daemon.listHits)
	}
}

// TestPush_InteractiveMode_NoDaemon: zero args + TTY + daemon ping
// fails → registry_no_image_source error. Picker never invoked.
func TestPush_InteractiveMode_NoDaemon(t *testing.T) {
	writeLsCredsFile(t, healthyPushCredsBody("vccr.io", "proj"))

	fake := &fakeSourceLoader{}
	withFakeSourceLoader(t, fake)
	daemon := &fakePickerDaemon{pingErr: errors.New("docker daemon not reachable")}
	withFakeDaemonLister(t, daemon)
	withTerminalErrOut(t, true)

	pickerHits := 0
	withRunPickerFn(t, func(_ context.Context, _ cmdutil.IOStreams, _ []DaemonImage, _ *options.RegistryCredentials) ([]string, bool) {
		pickerHits++
		return nil, false
	})

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	err := runPushForTest(t, f, streams)
	if err == nil {
		t.Fatal("expected registry_no_image_source error when daemon unreachable")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryNoImageSource {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryNoImageSource)
	}
	if !strings.Contains(ae.Message, "Docker daemon not reachable") {
		t.Errorf("expected friendly daemon message, got: %v", ae.Message)
	}
	if daemon.pingHits == 0 {
		t.Error("expected Ping to be attempted")
	}
	if daemon.listHits != 0 {
		t.Errorf("List should NOT be called when Ping fails, got %d call(s)", daemon.listHits)
	}
	if pickerHits != 0 {
		t.Errorf("picker should NOT be invoked when daemon unreachable, got %d call(s)", pickerHits)
	}
}

// TestPush_InteractiveMode_NoImages: zero args + daemon returns empty
// list → prints helpful stdout message and exits 0. Picker never runs.
func TestPush_InteractiveMode_NoImages(t *testing.T) {
	writeLsCredsFile(t, healthyPushCredsBody("vccr.io", "proj"))

	fake := &fakeSourceLoader{}
	withFakeSourceLoader(t, fake)
	daemon := &fakePickerDaemon{images: nil}
	withFakeDaemonLister(t, daemon)
	withTerminalErrOut(t, true)

	pickerHits := 0
	withRunPickerFn(t, func(_ context.Context, _ cmdutil.IOStreams, _ []DaemonImage, _ *options.RegistryCredentials) ([]string, bool) {
		pickerHits++
		return nil, false
	})

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newLsStreams()

	if err := runPushForTest(t, f, streams); err != nil {
		t.Fatalf("expected nil error on empty daemon list, got: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "No local Docker images found") {
		t.Errorf("expected helpful empty-state message on stdout, got:\n%s", got)
	}
	if daemon.listHits != 1 {
		t.Errorf("expected List to be called once, got %d", daemon.listHits)
	}
	if pickerHits != 0 {
		t.Errorf("picker should not be invoked on empty list, got %d call(s)", pickerHits)
	}
	if len(fake.loadCalls) != 0 {
		t.Errorf("source loader should not be called, got %d call(s)", len(fake.loadCalls))
	}
}

// TestPush_InteractiveMode_UserCancels: picker returns (nil, false) →
// exit 0, nothing pushed. Stub picker so we don't drive a real TUI.
func TestPush_InteractiveMode_UserCancels(t *testing.T) {
	writeLsCredsFile(t, healthyPushCredsBody("vccr.io", "proj"))

	fake := &fakeSourceLoader{}
	withFakeSourceLoader(t, fake)
	// A single fake image so the picker is reached (non-empty list).
	daemon := &fakePickerDaemon{images: []DaemonImage{
		{ID: "sha256:a", RepoTags: []string{"my-app:v1"}, Size: 100},
	}}
	withFakeDaemonLister(t, daemon)
	withTerminalErrOut(t, true)

	withRunPickerFn(t, func(_ context.Context, _ cmdutil.IOStreams, _ []DaemonImage, _ *options.RegistryCredentials) ([]string, bool) {
		return nil, false
	})

	// Registry should NOT be called — swap in a recording one so accidental
	// dispatch panics loudly.
	fakeReg := &recordingRegistry{}
	withFakeRegistry(t, fakeReg)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	if err := runPushForTest(t, f, streams); err != nil {
		t.Fatalf("cancel should exit 0, got: %v", err)
	}
	if len(fake.loadCalls) != 0 {
		t.Errorf("source loader should not be called on cancel, got %d call(s)", len(fake.loadCalls))
	}
}

// TestPush_InteractiveMode_EmptySelection: picker returns ([]string{},
// true) → exit 0, nothing pushed. Unticking everything is not an error.
func TestPush_InteractiveMode_EmptySelection(t *testing.T) {
	writeLsCredsFile(t, healthyPushCredsBody("vccr.io", "proj"))

	fake := &fakeSourceLoader{}
	withFakeSourceLoader(t, fake)
	daemon := &fakePickerDaemon{images: []DaemonImage{
		{ID: "sha256:a", RepoTags: []string{"my-app:v1"}, Size: 100},
	}}
	withFakeDaemonLister(t, daemon)
	withTerminalErrOut(t, true)

	withRunPickerFn(t, func(_ context.Context, _ cmdutil.IOStreams, _ []DaemonImage, _ *options.RegistryCredentials) ([]string, bool) {
		return []string{}, true
	})

	fakeReg := &recordingRegistry{}
	withFakeRegistry(t, fakeReg)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := newLsStreams()

	if err := runPushForTest(t, f, streams); err != nil {
		t.Fatalf("empty selection should exit 0, got: %v", err)
	}
	if len(fake.loadCalls) != 0 {
		t.Errorf("source loader should not be called on empty selection, got %d call(s)", len(fake.loadCalls))
	}
}

// TestPush_InteractiveMode_HappyPath: picker returns a ref which is
// then pushed end-to-end through the real ggcr test server. Asserts
// the selected image lands at the expected destination.
func TestPush_InteractiveMode_HappyPath(t *testing.T) {
	host := testServer(t)
	writeLsCredsFile(t, healthyPushCredsBody(host, "proj"))

	img := randomPushImage(t)
	fake := &fakeSourceLoader{imagesByRef: map[string]v1.Image{"my-app:v1": img}}
	withFakeSourceLoader(t, fake)

	daemon := &fakePickerDaemon{images: []DaemonImage{
		{ID: "sha256:a", RepoTags: []string{"my-app:v1"}, Size: 100},
	}}
	withFakeDaemonLister(t, daemon)
	withTerminalErrOut(t, true)

	pickerHits := 0
	withRunPickerFn(t, func(_ context.Context, _ cmdutil.IOStreams, images []DaemonImage, creds *options.RegistryCredentials) ([]string, bool) {
		pickerHits++
		// Sanity: the picker driver must see the daemon's image list and
		// correctly-constructed creds.
		if len(images) != 1 || len(images[0].RepoTags) != 1 || images[0].RepoTags[0] != "my-app:v1" {
			t.Errorf("picker received unexpected images: %+v", images)
		}
		if creds == nil || creds.ProjectID != "proj" {
			t.Errorf("picker received unexpected creds: %+v", creds)
		}
		return []string{"my-app:v1"}, true
	})

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := newLsStreams()

	// --progress plain gates off the bubbletea progress view so the test
	// doesn't try to drive a real TUI through a *bytes.Buffer.
	// isInteractivePush ignores --progress (by design); the picker still
	// runs. shouldUseBubbletea, which routes the downstream push, honors
	// --progress=plain and picks the flat-text path.
	if err := runPushForTest(t, f, streams, "--progress", "plain"); err != nil {
		t.Fatalf("push (interactive happy path): %v", err)
	}
	if pickerHits != 1 {
		t.Errorf("expected picker to run once, got %d", pickerHits)
	}

	// Verify the image landed at the expected destination.
	dstReg := newGGCRRegistry(testCreds(host))
	dstRef := host + "/proj/my-app:v1"
	desc, err := dstReg.Head(context.Background(), dstRef)
	if err != nil {
		t.Fatalf("Head(%s): %v", dstRef, err)
	}
	wantDigest, _ := img.Digest()
	if desc.Digest != wantDigest {
		t.Errorf("digest mismatch: got %s, want %s", desc.Digest, wantDigest)
	}
	if !strings.Contains(out.String(), "pushed my-app:v1 ->") {
		t.Errorf("expected summary line, got:\n%s", out.String())
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
