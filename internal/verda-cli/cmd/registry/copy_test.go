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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// ---------- copy test helpers ----------

// copyStreams returns IOStreams backed by buffers + a stdin reader.
// Separate from newLsStreams so copy-specific tests can wire stdin for
// --src-password-stdin without touching the ls helper.
func copyStreams(stdin string) (cmdutil.IOStreams, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	var in *bytes.Buffer
	if stdin != "" {
		in = bytes.NewBufferString(stdin)
	} else {
		in = &bytes.Buffer{}
	}
	return cmdutil.IOStreams{In: in, Out: out, ErrOut: errOut}, out, errOut
}

// runCopyForTest invokes NewCmdCopy with args so tests exercise the same
// flag-parsing path as the production binary.
func runCopyForTest(t *testing.T, f cmdutil.Factory, streams cmdutil.IOStreams, args ...string) error {
	t.Helper()
	cmd := NewCmdCopy(f, streams)
	cmd.SetArgs(args)
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

// primeSourceImage pushes a random image to host+ref and returns it.
// Handy for test setup: copy's source side reads whatever is already
// present on the source registry.
func primeSourceImage(t *testing.T, host, ref string) v1.Image {
	t.Helper()
	r := newGGCRRegistry(testCreds(host))
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	if err := r.Write(context.Background(), host+"/"+ref, img, WriteOptions{}); err != nil {
		t.Fatalf("prime source: %v", err)
	}
	return img
}

// writeCopyCredsFile is a copy-local wrapper around writeLsCredsFile so
// copy tests don't depend on ls-test naming. Uses the same "healthy"
// INI body shape.
func writeCopyCredsFile(t *testing.T, host, project string) {
	t.Helper()
	writeLsCredsFile(t, healthyPushCredsBody(host, project))
}

// ---------- tests ----------

// TestCopy_SingleRefHappy: end-to-end copy between two in-process
// registries. Asserts the digest survives the round trip.
func TestCopy_SingleRefHappy(t *testing.T) {
	srcHost := testServer(t)
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	srcImg := primeSourceImage(t, srcHost, "ns/app:v1")

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := copyStreams("")

	srcArg := srcHost + "/ns/app:v1"
	dstArg := dstHost + "/proj/app:v1"

	if err := runCopyForTest(t, f, streams, srcArg, dstArg); err != nil {
		t.Fatalf("copy: %v", err)
	}

	dstReg := newGGCRRegistry(testCreds(dstHost))
	desc, err := dstReg.Head(context.Background(), dstArg)
	if err != nil {
		t.Fatalf("Head(dst): %v", err)
	}
	want, _ := srcImg.Digest()
	if desc.Digest != want {
		t.Fatalf("digest mismatch: got %s, want %s", desc.Digest, want)
	}

	if !strings.Contains(out.String(), "copied "+srcArg+" -> "+dstArg) {
		t.Errorf("expected copied summary line, got:\n%s", out.String())
	}
}

// TestCopy_DefaultDestSynthesis: no dst arg → default to
// <creds.Endpoint>/<project>/<src-repo>:<src-tag>.
func TestCopy_DefaultDestSynthesis(t *testing.T) {
	srcHost := testServer(t)
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	srcImg := primeSourceImage(t, srcHost, "ns/app:v1")

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := copyStreams("")

	srcArg := srcHost + "/ns/app:v1"
	if err := runCopyForTest(t, f, streams, srcArg); err != nil {
		t.Fatalf("copy: %v", err)
	}

	wantDst := dstHost + "/proj/ns/app:v1"
	dstReg := newGGCRRegistry(testCreds(dstHost))
	desc, err := dstReg.Head(context.Background(), wantDst)
	if err != nil {
		t.Fatalf("Head(%s): %v", wantDst, err)
	}
	want, _ := srcImg.Digest()
	if desc.Digest != want {
		t.Fatalf("digest mismatch: got %s, want %s", desc.Digest, want)
	}

	if !strings.Contains(out.String(), "-> "+wantDst) {
		t.Errorf("expected synthesized destination in summary, got:\n%s", out.String())
	}
}

// TestCopy_ShortDst: a short dst like "my-app:prod" expands to
// <endpoint>/<project>/my-app:prod via Normalize.
func TestCopy_ShortDst(t *testing.T) {
	srcHost := testServer(t)
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	srcImg := primeSourceImage(t, srcHost, "ns/app:v1")

	f := cmdutil.NewTestFactory(nil)
	streams, out, _ := copyStreams("")

	srcArg := srcHost + "/ns/app:v1"
	if err := runCopyForTest(t, f, streams, srcArg, "my-app:prod"); err != nil {
		t.Fatalf("copy: %v", err)
	}

	wantDst := dstHost + "/proj/my-app:prod"
	dstReg := newGGCRRegistry(testCreds(dstHost))
	desc, err := dstReg.Head(context.Background(), wantDst)
	if err != nil {
		t.Fatalf("Head(%s): %v", wantDst, err)
	}
	want, _ := srcImg.Digest()
	if desc.Digest != want {
		t.Fatalf("digest mismatch: got %s, want %s", desc.Digest, want)
	}

	if !strings.Contains(out.String(), "-> "+wantDst) {
		t.Errorf("expected short dst expansion in summary, got:\n%s", out.String())
	}
}

// TestCopy_SrcAuthAnonymous: --src-auth anonymous bypasses the keychain
// and still reads a public image from the in-process server.
func TestCopy_SrcAuthAnonymous(t *testing.T) {
	srcHost := testServer(t)
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	_ = primeSourceImage(t, srcHost, "ns/app:v1")

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	// Sentinel keychain that fails if resolved; guarantees anonymous mode
	// didn't route through the keychain path.
	withSourceKeychain(t, &failingKeychain{t: t})

	srcArg := srcHost + "/ns/app:v1"
	dstArg := dstHost + "/proj/app:v1"
	if err := runCopyForTest(t, f, streams,
		srcArg, dstArg, "--src-auth", "anonymous"); err != nil {
		t.Fatalf("copy --src-auth anonymous: %v", err)
	}
}

// TestCopy_SrcAuthBasic: --src-auth basic + --src-username + secret on
// stdin builds an authn.AuthConfig with those values and uses it on the
// source side. We verify this by pointing the source at a tiny server
// that echoes back the Authorization header and asserting it decodes
// to user:password. The source doesn't need to be a real registry — we
// intercept at the TCP layer via a recording authenticator hook.
func TestCopy_SrcAuthBasic(t *testing.T) {
	// Intercept the source-registry-builder so we can examine the
	// authenticator the command constructed. No network traffic needed.
	var captured authn.Authenticator
	prev := sourceRegistryBuilder
	sourceRegistryBuilder = func(auth authn.Authenticator, _ RetryConfig) Registry {
		captured = auth
		// Return a harmless stub that fails Read so the command exits
		// early — we're only checking the authenticator shape.
		return &errorOnReadRegistry{err: errors.New("intercept: no network")}
	}
	t.Cleanup(func() { sourceRegistryBuilder = prev })

	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("s3cret-pw\n")

	// We expect a non-nil error (the stub returns one), so don't fail on
	// it — only assert the authenticator was built correctly.
	_ = runCopyForTest(t, f, streams,
		"docker.io/library/nginx:1.25",
		"--src-auth", "basic",
		"--src-username", "jdoe",
		"--src-password-stdin")

	if captured == nil {
		t.Fatal("expected sourceRegistryBuilder to be invoked")
	}
	cfg, err := captured.Authorization()
	if err != nil {
		t.Fatalf("Authorization(): %v", err)
	}
	if cfg.Username != "jdoe" {
		t.Errorf("Username = %q, want jdoe", cfg.Username)
	}
	if cfg.Password != "s3cret-pw" {
		t.Errorf("Password = %q, want s3cret-pw", cfg.Password)
	}
}

// TestCopy_NotConfigured: missing creds surfaces
// registry_not_configured without touching the network.
func TestCopy_NotConfigured(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VERDA_REGISTRY_CREDENTIALS_FILE", dir+"/does-not-exist")
	t.Setenv("VERDA_HOME", dir)

	// Any accidental dispatch panics on nil — prove we never built a client.
	var srcCalled bool
	prev := sourceRegistryBuilder
	sourceRegistryBuilder = func(_ authn.Authenticator, _ RetryConfig) Registry {
		srcCalled = true
		return &errorOnReadRegistry{}
	}
	t.Cleanup(func() { sourceRegistryBuilder = prev })
	fakeDst := &recordingRegistry{}
	withFakeRegistry(t, fakeDst)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	err := runCopyForTest(t, f, streams, "docker.io/library/nginx:1.25")
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
	if srcCalled {
		t.Error("source registry builder should not be invoked when creds missing")
	}
}

// TestCopy_ExpiredCreds: expired creds → registry_credential_expired
// before any network call.
func TestCopy_ExpiredCreds(t *testing.T) {
	writeLsCredsFile(t, expiredLsCredsBody("vccr.io"))

	var srcCalled bool
	prev := sourceRegistryBuilder
	sourceRegistryBuilder = func(_ authn.Authenticator, _ RetryConfig) Registry {
		srcCalled = true
		return &errorOnReadRegistry{}
	}
	t.Cleanup(func() { sourceRegistryBuilder = prev })

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	err := runCopyForTest(t, f, streams, "docker.io/library/nginx:1.25")
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
	if srcCalled {
		t.Error("source registry builder should not be invoked when creds expired")
	}
}

// TestCopy_SourceReadFailure: pointing the source at a ref that doesn't
// exist surfaces registry_repo_not_found (or similar) and never writes
// to the destination.
func TestCopy_SourceReadFailure(t *testing.T) {
	srcHost := testServer(t)
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")
	// No prime: the source ref will 404.

	// Wrap the real dst client to prove Write is never called on the
	// destination registry.
	var dstWriteCalled atomic.Bool
	prev := clientBuilder
	clientBuilder = func(creds *options.RegistryCredentials, cfg RetryConfig) Registry {
		return &writeCountRegistry{inner: prev(creds, cfg), counter: &dstWriteCalled}
	}
	t.Cleanup(func() { clientBuilder = prev })

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	srcArg := srcHost + "/ns/missing:v1"
	dstArg := dstHost + "/proj/app:v1"

	err := runCopyForTest(t, f, streams, srcArg, dstArg)
	if err == nil {
		t.Fatal("expected error for missing source ref")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	// NAME_UNKNOWN / MANIFEST_UNKNOWN are both acceptable here — the
	// ggcr test server returns whichever fits the request path.
	if ae.Code != kindRegistryRepoNotFound && ae.Code != kindRegistryTagNotFound &&
		ae.Code != kindRegistryInternalError {
		t.Errorf("Code = %q, want repo_not_found / tag_not_found / internal_error", ae.Code)
	}
	if dstWriteCalled.Load() {
		t.Error("destination Write should NOT be called when source read fails")
	}
}

// TestCopy_DestWriteAuthFailure: swap the dest clientBuilder so Write
// returns an UNAUTHORIZED transport error; the command surfaces
// registry_auth_failed.
func TestCopy_DestWriteAuthFailure(t *testing.T) {
	srcHost := testServer(t)
	// Use a synthetic dst host so creds don't expire: endpoint doesn't
	// need to be reachable — the swapped clientBuilder returns an
	// in-memory fake.
	writeCopyCredsFile(t, "vccr.io", "proj")

	_ = primeSourceImage(t, srcHost, "ns/app:v1")

	authErr := &transport.Error{
		Errors: []transport.Diagnostic{
			{Code: transport.UnauthorizedErrorCode, Message: "unauthorized"},
		},
		StatusCode: http.StatusUnauthorized,
	}
	prev := clientBuilder
	clientBuilder = func(_ *options.RegistryCredentials, _ RetryConfig) Registry {
		return &writeErrorRegistry{writeErr: authErr}
	}
	t.Cleanup(func() { clientBuilder = prev })

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	err := runCopyForTest(t, f, streams,
		srcHost+"/ns/app:v1",
		"vccr.io/proj/app:v1")
	if err == nil {
		t.Fatal("expected auth failure")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryAuthFailed)
	}
}

// TestCopy_JSONOutput: -o json emits a valid structured payload and
// keeps stdout free of human lines.
func TestCopy_JSONOutput(t *testing.T) {
	srcHost := testServer(t)
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	_ = primeSourceImage(t, srcHost, "ns/app:v1")

	f := cmdutil.NewTestFactory(nil)
	f.OutputFormatOverride = "json"
	streams, out, _ := copyStreams("")

	srcArg := srcHost + "/ns/app:v1"
	dstArg := dstHost + "/proj/app:v1"
	if err := runCopyForTest(t, f, streams, srcArg, dstArg); err != nil {
		t.Fatalf("copy -o json: %v", err)
	}

	var payload copyPayload
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if payload.Source != srcArg {
		t.Errorf("Source = %q, want %q", payload.Source, srcArg)
	}
	if payload.Destination != dstArg {
		t.Errorf("Destination = %q, want %q", payload.Destination, dstArg)
	}
	if payload.Status != "copied" {
		t.Errorf("Status = %q, want copied", payload.Status)
	}
	if payload.Error != "" {
		t.Errorf("Error = %q, want empty", payload.Error)
	}

	// No human "copied ... ->" bleed into stdout.
	if strings.Contains(out.String(), "copied "+srcArg+" -> ") {
		t.Errorf("unexpected human summary in JSON stdout:\n%s", out.String())
	}
}

// TestCopy_PlainProgress: --progress plain prints a single plain
// completion line on ErrOut.
func TestCopy_PlainProgress(t *testing.T) {
	srcHost := testServer(t)
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	_ = primeSourceImage(t, srcHost, "ns/app:v1")

	f := cmdutil.NewTestFactory(nil)
	streams, _, errOut := copyStreams("")

	srcArg := srcHost + "/ns/app:v1"
	dstArg := dstHost + "/proj/app:v1"
	if err := runCopyForTest(t, f, streams,
		srcArg, dstArg, "--progress", "plain"); err != nil {
		t.Fatalf("copy --progress plain: %v", err)
	}

	got := errOut.String()
	if !strings.Contains(got, "copied "+srcArg+" -> "+dstArg) {
		t.Errorf("expected plain progress line on ErrOut, got:\n%s", got)
	}
}

// TestCopy_RetriesOnTransient503: a transient 503 on the source side
// triggers the retrying transport and the copy ultimately succeeds.
// We run a manual src server that fails the first two manifest GETs
// with 503 and then delegates to the real in-process ggcr registry.
func TestCopy_RetriesOnTransient503(t *testing.T) {
	// Real in-process registry hiding behind a proxy that injects
	// transient 503s on the first few GETs against a specific path.
	inner := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(inner.Close)

	innerURL, err := url.Parse(inner.URL)
	if err != nil {
		t.Fatalf("parse inner URL: %v", err)
	}

	var failuresRemaining atomic.Int32
	failuresRemaining.Store(2)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only fail idempotent reads (GET/HEAD) on manifest/blob paths —
		// PUTs (used to prime the source) succeed immediately.
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			failuresRemaining.Load() > 0 {
			failuresRemaining.Add(-1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// Simple reverse proxy: rewrite host and forward.
		r2 := r.Clone(r.Context())
		r2.RequestURI = ""
		r2.URL.Scheme = innerURL.Scheme
		r2.URL.Host = innerURL.Host
		r2.Host = innerURL.Host
		resp, err := http.DefaultTransport.RoundTrip(r2)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = copyBody(w, resp.Body)
	}))
	t.Cleanup(proxy.Close)

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	proxyHost := proxyURL.Host

	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	// Prime the source by pointing writeRandomImage at the inner
	// registry directly (writes bypass the proxy's failure injection).
	innerReg := newGGCRRegistry(testCreds(innerURL.Host))
	img := writeRandomImage(t, innerReg, innerURL.Host+"/ns/app:v1")
	// Sanity: failuresRemaining is still 2 (writes don't fail because
	// writes go direct to inner; the proxy only handles reads).

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	// Use --retries 5 (the default) — the retrying transport should
	// absorb the first two 503 responses and succeed on the third.
	if err := runCopyForTest(t, f, streams,
		proxyHost+"/ns/app:v1",
		dstHost+"/proj/app:v1"); err != nil {
		t.Fatalf("copy with transient 503s: %v", err)
	}

	if got := failuresRemaining.Load(); got != 0 {
		t.Errorf("expected all 2 transient failures to be consumed, got %d remaining", got)
	}

	dstReg := newGGCRRegistry(testCreds(dstHost))
	desc, err := dstReg.Head(context.Background(), dstHost+"/proj/app:v1")
	if err != nil {
		t.Fatalf("Head(dst): %v", err)
	}
	want, _ := img.Digest()
	if desc.Digest != want {
		t.Fatalf("digest mismatch: got %s, want %s", desc.Digest, want)
	}
}

// TestCopy_DefaultKeychainSelectedByDefault: --src-auth unset (defaults
// to docker-config) routes through the package-level keychain swap
// point. We assert by substituting a recording keychain and confirming
// it was consulted.
func TestCopy_DefaultKeychainSelectedByDefault(t *testing.T) {
	srcHost := testServer(t)
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	_ = primeSourceImage(t, srcHost, "ns/app:v1")

	rec := &recordingKeychain{}
	withSourceKeychain(t, rec)

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	srcArg := srcHost + "/ns/app:v1"
	dstArg := dstHost + "/proj/app:v1"
	if err := runCopyForTest(t, f, streams, srcArg, dstArg); err != nil {
		t.Fatalf("copy (default docker-config): %v", err)
	}
	if rec.calls.Load() == 0 {
		t.Error("expected default keychain to be consulted at least once")
	}
}

// TestCopy_UnknownSrcAuth: invalid --src-auth value → usage-style
// bad-args error.
func TestCopy_UnknownSrcAuth(t *testing.T) {
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	err := runCopyForTest(t, f, streams,
		"docker.io/library/nginx:1.25",
		"--src-auth", "weird-mode")
	if err == nil {
		t.Fatal("expected error for unknown --src-auth")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryInvalidReference {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryInvalidReference)
	}
}

// TestCopy_ShortSrcRejected: short src refs ("my-app:v1") are invalid
// for copy — Parse rejects them, Copy surfaces registry_invalid_reference.
func TestCopy_ShortSrcRejected(t *testing.T) {
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	err := runCopyForTest(t, f, streams, "my-app:v1")
	if err == nil {
		t.Fatal("expected error for short source ref")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryInvalidReference {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryInvalidReference)
	}
}

// TestCopy_CopyAlias: the `cp` alias dispatches to the copy RunE. We
// assert by invoking via the parent command tree and checking that a
// short-ref src fails with the same error as TestCopy_ShortSrcRejected
// (proving the alias routed through the same code path).
func TestCopy_CopyAlias(t *testing.T) {
	dstHost := testServer(t)
	writeCopyCredsFile(t, dstHost, "proj")

	f := cmdutil.NewTestFactory(nil)
	streams, _, _ := copyStreams("")

	// Exercise via the top-level registry command so the alias table
	// has to resolve "cp" to the copy subcommand.
	cmd := NewCmdRegistry(f, streams)
	cmd.SetArgs([]string{"cp", "my-app:v1"})
	cmd.SetOut(streams.Out)
	cmd.SetErr(streams.ErrOut)
	cmd.SetContext(context.Background())
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected short-ref rejection via cp alias")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AgentError, got %T (%v)", err, err)
	}
	if ae.Code != kindRegistryInvalidReference {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryInvalidReference)
	}
}

// ---------- test helpers: stub registries ----------

// errorOnReadRegistry is a Registry that errors out on Read (optionally
// via err) and panics on any other method. Used to short-circuit copy's
// source-side read without performing network I/O.
type errorOnReadRegistry struct {
	Registry
	err error
}

func (e *errorOnReadRegistry) Read(_ context.Context, _ string) (v1.Image, error) {
	if e.err != nil {
		return nil, e.err
	}
	return nil, errors.New("errorOnReadRegistry: Read called without err set")
}

// writeCountRegistry wraps an inner Registry and flips counter on the
// first Write call. Used to prove copy doesn't write to the destination
// when the source-side read fails.
type writeCountRegistry struct {
	inner   Registry
	counter *atomic.Bool
}

func (w *writeCountRegistry) Catalog(ctx context.Context) ([]string, error) {
	return w.inner.Catalog(ctx)
}

func (w *writeCountRegistry) Tags(ctx context.Context, repo string) ([]string, error) {
	return w.inner.Tags(ctx, repo)
}

func (w *writeCountRegistry) Head(ctx context.Context, ref string) (*v1.Descriptor, error) {
	return w.inner.Head(ctx, ref)
}

func (w *writeCountRegistry) Write(ctx context.Context, ref string, img v1.Image, opts WriteOptions) error {
	w.counter.Store(true)
	return w.inner.Write(ctx, ref, img, opts)
}

func (w *writeCountRegistry) Read(ctx context.Context, ref string) (v1.Image, error) {
	return w.inner.Read(ctx, ref)
}

// ---------- test helpers: keychain hooks ----------

// withSourceKeychain swaps the package-level sourceKeychainBuilder so a
// test can assert what the command passed into the keychainAuth adapter
// for --src-auth docker-config.
func withSourceKeychain(t *testing.T, kc authn.Keychain) {
	t.Helper()
	prev := sourceKeychainBuilder
	sourceKeychainBuilder = kc
	t.Cleanup(func() { sourceKeychainBuilder = prev })
}

// recordingKeychain counts calls to Resolve. Always returns anonymous
// so public-image reads still succeed on the in-process test server.
type recordingKeychain struct {
	calls atomic.Int32
}

func (k *recordingKeychain) Resolve(_ authn.Resource) (authn.Authenticator, error) {
	k.calls.Add(1)
	return authn.Anonymous, nil
}

// failingKeychain fails any Resolve call. Used to prove --src-auth
// anonymous never touches the keychain path.
type failingKeychain struct {
	t *testing.T
}

func (k *failingKeychain) Resolve(_ authn.Resource) (authn.Authenticator, error) {
	k.t.Helper()
	k.t.Error("failingKeychain.Resolve called — --src-auth anonymous should not use keychain")
	return authn.Anonymous, nil
}

// copyBody is a tiny wrapper so the 503-retry test can forward bytes
// without bringing in io.Copy-on-bytes.Buffer ambiguity with the
// top-level `copy` function name.
func copyBody(dst http.ResponseWriter, src interface{ Read([]byte) (int, error) }) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return total, nil
			}
			return total, err
		}
	}
}
