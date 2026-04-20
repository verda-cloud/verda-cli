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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingServer wraps httptest.NewServer with a counter of incoming
// requests plus a "fail N times, then succeed" helper. Tests reach into
// the atomic counter directly; recorded bodies are captured per-request.
type countingServer struct {
	srv      *httptest.Server
	count    int32
	failures int32 // remaining failure responses (decremented each hit)

	mu        sync.Mutex
	bodies    [][]byte // captured per-request body bytes
	onRequest func(w http.ResponseWriter, r *http.Request, n int32)
}

// newCountingServer starts a server that fails `failures` times with
// status `status` before returning 200 OK. Each request is counted, and
// its body (if any) captured into bodies. The server is Close()d via
// t.Cleanup.
func newCountingServer(t *testing.T, failures int32, status int) *countingServer {
	t.Helper()
	cs := &countingServer{failures: failures}
	cs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&cs.count, 1)

		// Capture body (safe for retry tests; responses are small).
		if r.Body != nil {
			data, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			cs.mu.Lock()
			cs.bodies = append(cs.bodies, data)
			cs.mu.Unlock()
		}

		if cs.onRequest != nil {
			cs.onRequest(w, r, n)
			return
		}

		if atomic.LoadInt32(&cs.failures) > 0 {
			atomic.AddInt32(&cs.failures, -1)
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(cs.srv.Close)
	return cs
}

// URL returns the base URL of the test server.
func (cs *countingServer) URL() string { return cs.srv.URL }

// hits returns the number of requests seen so far.
func (cs *countingServer) hits() int32 { return atomic.LoadInt32(&cs.count) }

// bodyAt returns the captured body for request index i (0-indexed).
func (cs *countingServer) bodyAt(i int) []byte {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if i < 0 || i >= len(cs.bodies) {
		return nil
	}
	return cs.bodies[i]
}

// recordingSleep returns a Sleep hook that appends every observed wait
// duration into durs (thread-safe). The hook never actually sleeps, so
// tests stay fast.
func recordingSleep(durs *[]time.Duration, mu *sync.Mutex) func(time.Duration) {
	return func(d time.Duration) {
		mu.Lock()
		*durs = append(*durs, d)
		mu.Unlock()
	}
}

// doGet is a context-aware shim around client.Do that keeps the retry
// tests lint-clean (linter rejects bare client.Get). Body must be closed
// by the caller.
func doGet(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	return resp
}

// doPost is the POST equivalent of doGet. Body must be closed by caller.
func doPost(t *testing.T, client *http.Client, url, contentType string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, body)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	return resp
}

// TestRetryingTransport_SucceedsWithoutRetries — server returns 200 on
// first try → exactly 1 request is made and no retries kick in.
func TestRetryingTransport_SucceedsWithoutRetries(t *testing.T) {
	cs := newCountingServer(t, 0, http.StatusServiceUnavailable)
	rt := NewRetryingTransport(http.DefaultTransport, RetryConfig{
		MaxAttempts: 3,
		Sleep:       func(time.Duration) { t.Fatalf("sleep should not be called") },
	})
	client := &http.Client{Transport: rt}

	resp := doGet(t, client, cs.URL()+"/")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := cs.hits(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

// TestRetryingTransport_RetriesOn503 — server returns 503 twice then
// 200 → 3 requests total, final response OK.
func TestRetryingTransport_RetriesOn503(t *testing.T) {
	cs := newCountingServer(t, 2, http.StatusServiceUnavailable)
	var durs []time.Duration
	var mu sync.Mutex
	rt := NewRetryingTransport(http.DefaultTransport, RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		Sleep:       recordingSleep(&durs, &mu),
	})
	client := &http.Client{Transport: rt}

	resp := doGet(t, client, cs.URL()+"/")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := cs.hits(); got != 3 {
		t.Fatalf("request count = %d, want 3", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(durs) != 2 {
		t.Fatalf("observed %d sleeps, want 2", len(durs))
	}
}

// TestRetryingTransport_HonorsRetryAfterSeconds — server returns 429
// with `Retry-After: 2` then 200 → sleep ≥ 2s is recorded.
func TestRetryingTransport_HonorsRetryAfterSeconds(t *testing.T) {
	cs := newCountingServer(t, 0, 0)
	cs.onRequest = func(w http.ResponseWriter, r *http.Request, n int32) {
		if n == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}

	var durs []time.Duration
	var mu sync.Mutex
	rt := NewRetryingTransport(http.DefaultTransport, RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		Sleep:       recordingSleep(&durs, &mu),
	})
	client := &http.Client{Transport: rt}

	resp := doGet(t, client, cs.URL()+"/")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := cs.hits(); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(durs) != 1 {
		t.Fatalf("observed %d sleeps, want 1", len(durs))
	}
	if durs[0] < 2*time.Second {
		t.Fatalf("sleep = %v, want ≥ 2s (Retry-After seconds honored)", durs[0])
	}
}

// TestRetryingTransport_HonorsRetryAfterHTTPDate — `Retry-After:
// <future HTTP-date>` → waits until that time using the synthetic clock.
func TestRetryingTransport_HonorsRetryAfterHTTPDate(t *testing.T) {
	// Fix Now() so the header-to-duration computation is deterministic.
	fixedNow := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	// Target 5 seconds in the future.
	future := fixedNow.Add(5 * time.Second).UTC().Format(http.TimeFormat)

	cs := newCountingServer(t, 0, 0)
	cs.onRequest = func(w http.ResponseWriter, r *http.Request, n int32) {
		if n == 1 {
			w.Header().Set("Retry-After", future)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}

	var durs []time.Duration
	var mu sync.Mutex
	rt := NewRetryingTransport(http.DefaultTransport, RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		Now:         func() time.Time { return fixedNow },
		Sleep:       recordingSleep(&durs, &mu),
	})
	client := &http.Client{Transport: rt}

	resp := doGet(t, client, cs.URL()+"/")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(durs) != 1 {
		t.Fatalf("observed %d sleeps, want 1", len(durs))
	}
	// http.ParseTime truncates to whole seconds; expect 4s–5s.
	if durs[0] < 4*time.Second || durs[0] > 5*time.Second {
		t.Fatalf("sleep = %v, want ~5s (Retry-After HTTP-date honored)", durs[0])
	}
}

// TestRetryingTransport_DoesNotRetryPOST — server returns 503; POST is
// non-idempotent, so only 1 request is made and the 503 response is
// surfaced to the caller.
func TestRetryingTransport_DoesNotRetryPOST(t *testing.T) {
	cs := newCountingServer(t, 10, http.StatusServiceUnavailable)
	rt := NewRetryingTransport(http.DefaultTransport, RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		Sleep:       func(time.Duration) { t.Fatalf("sleep should not be called for POST") },
	})
	client := &http.Client{Transport: rt}

	resp := doPost(t, client, cs.URL()+"/", "application/json", bytes.NewReader([]byte(`{}`)))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if got := cs.hits(); got != 1 {
		t.Fatalf("request count = %d, want 1 (POST must not retry)", got)
	}
}

// TestRetryingTransport_RetryBudgetExhausted — server always returns
// 503; with MaxAttempts=3, exactly 3 requests are made and the last
// 503 surfaces to the caller.
func TestRetryingTransport_RetryBudgetExhausted(t *testing.T) {
	cs := newCountingServer(t, 999, http.StatusServiceUnavailable)
	rt := NewRetryingTransport(http.DefaultTransport, RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Millisecond,
		Sleep:       func(time.Duration) {},
	})
	client := &http.Client{Transport: rt}

	resp := doGet(t, client, cs.URL()+"/")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if got := cs.hits(); got != 3 {
		t.Fatalf("request count = %d, want 3 (MaxAttempts cap)", got)
	}
}

// TestRetryingTransport_ContextCanceledDuringBackoff — server returns
// 503; ctx is canceled during the backoff sleep → error surfaces
// promptly and no further requests hit the server.
func TestRetryingTransport_ContextCanceledDuringBackoff(t *testing.T) {
	cs := newCountingServer(t, 999, http.StatusServiceUnavailable)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Sleep hook blocks on a channel we control. The test cancels ctx
	// while we're parked here, which should unblock the retry loop.
	unblock := make(chan struct{})
	rt := NewRetryingTransport(http.DefaultTransport, RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   10 * time.Second, // long enough that we cancel first
		Sleep: func(d time.Duration) {
			<-unblock
		},
	})
	client := &http.Client{Transport: rt}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cs.URL()+"/", http.NoBody)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	// Drive the request on a goroutine; cancel once we know the retry
	// loop has reached the sleep.
	errCh := make(chan error, 1)
	go func() {
		resp, rerr := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		errCh <- rerr
	}()

	// Give the first request a chance to land and the retry loop to
	// enter the sleep.
	deadline := time.Now().Add(2 * time.Second)
	for cs.hits() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// Cancel during backoff.
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected error from canceled context, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled (or wrap thereof)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not return after context cancellation")
	}

	// Release the parked Sleep hook so the goroutine in waitWithCtx can
	// exit cleanly.
	close(unblock)

	if got := cs.hits(); got > 1 {
		t.Fatalf("request count = %d after cancel, want 1", got)
	}
}

// fakeTimeoutError is a net.Error that reports Timeout() == true.
type fakeTimeoutError struct{}

func (fakeTimeoutError) Error() string   { return "fake timeout" }
func (fakeTimeoutError) Timeout() bool   { return true }
func (fakeTimeoutError) Temporary() bool { return true }

// flakyTransport returns a fakeTimeoutError on the first N calls then
// delegates to base for the rest.
type flakyTransport struct {
	base     http.RoundTripper
	failures int32
	count    int32
}

func (f *flakyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	n := atomic.AddInt32(&f.count, 1)
	if n <= atomic.LoadInt32(&f.failures) {
		return nil, fakeTimeoutError{}
	}
	return f.base.RoundTrip(r)
}

// TestRetryingTransport_TimeoutError — base transport returns a
// net.Error with Timeout() == true; retry kicks in; on next attempt we
// reach the real server and get 200.
func TestRetryingTransport_TimeoutError(t *testing.T) {
	cs := newCountingServer(t, 0, 0)
	flaky := &flakyTransport{base: http.DefaultTransport, failures: 2}
	rt := NewRetryingTransport(flaky, RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		Sleep:       func(time.Duration) {},
	})
	client := &http.Client{Transport: rt}

	resp := doGet(t, client, cs.URL()+"/")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&flaky.count); got != 3 {
		t.Fatalf("round-trip count = %d, want 3 (2 fakes + 1 success)", got)
	}
	if got := cs.hits(); got != 1 {
		t.Fatalf("server hit count = %d, want 1", got)
	}
}

// TestRetryingTransport_NoRetryOn400 — 400 is not retried; exactly 1
// request is made and 400 is returned.
func TestRetryingTransport_NoRetryOn400(t *testing.T) {
	cs := newCountingServer(t, 0, 0)
	cs.onRequest = func(w http.ResponseWriter, r *http.Request, n int32) {
		w.WriteHeader(http.StatusBadRequest)
	}

	rt := NewRetryingTransport(http.DefaultTransport, RetryConfig{
		MaxAttempts: 5,
		BaseDelay:   1 * time.Millisecond,
		Sleep:       func(time.Duration) { t.Fatalf("sleep should not be called on 400") },
	})
	client := &http.Client{Transport: rt}

	resp := doGet(t, client, cs.URL()+"/")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if got := cs.hits(); got != 1 {
		t.Fatalf("request count = %d, want 1", got)
	}
}

// TestRetryingTransport_BodyRewoundOnRetry — a PUT with a GetBody-
// bearing request is retried; the server sees the same body on both
// attempts.
func TestRetryingTransport_BodyRewoundOnRetry(t *testing.T) {
	cs := newCountingServer(t, 1, http.StatusServiceUnavailable)
	rt := NewRetryingTransport(http.DefaultTransport, RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Millisecond,
		Sleep:       func(time.Duration) {},
	})
	client := &http.Client{Transport: rt}

	payload := []byte("hello-retry-body")
	// NewRequestWithContext with a *bytes.Reader body populates
	// req.GetBody automatically, which is what the rewind logic relies
	// on.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, cs.URL()+"/", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.ContentLength = int64(len(payload))

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := cs.hits(); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}
	// Both captured bodies must match the payload exactly.
	for i := 0; i < 2; i++ {
		body := cs.bodyAt(i)
		if !bytes.Equal(body, payload) {
			t.Fatalf("body[%d] = %q, want %q", i, string(body), string(payload))
		}
	}
}

// TestParseRetryAfter exercises the header parser directly. Not in the
// required list but it pins down the two-form parsing contract.
func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t.Run("integer seconds", func(t *testing.T) {
		d := parseRetryAfter("7", now)
		if d != 7*time.Second {
			t.Fatalf("got %v, want 7s", d)
		}
	})
	t.Run("http date future", func(t *testing.T) {
		future := now.Add(10 * time.Second).UTC().Format(http.TimeFormat)
		d := parseRetryAfter(future, now)
		if d < 9*time.Second || d > 10*time.Second {
			t.Fatalf("got %v, want ~10s", d)
		}
	})
	t.Run("http date past", func(t *testing.T) {
		past := now.Add(-10 * time.Second).UTC().Format(http.TimeFormat)
		d := parseRetryAfter(past, now)
		if d != 0 {
			t.Fatalf("got %v, want 0 (past date)", d)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if d := parseRetryAfter("", now); d != 0 {
			t.Fatalf("got %v, want 0", d)
		}
	})
	t.Run("garbage", func(t *testing.T) {
		if d := parseRetryAfter("nonsense", now); d != 0 {
			t.Fatalf("got %v, want 0", d)
		}
	})
	t.Run("negative integer", func(t *testing.T) {
		if d := parseRetryAfter(strconv.Itoa(-5), now); d != 0 {
			t.Fatalf("got %v, want 0", d)
		}
	})
}

// TestRetryingTransport_DisabledConfig verifies the zero-valued
// RetryConfig is a passthrough (no wrapping), matching ls/tags/login
// call sites.
func TestRetryingTransport_DisabledConfig(t *testing.T) {
	base := http.DefaultTransport
	rt := NewRetryingTransport(base, RetryConfig{})
	if rt != base {
		t.Fatalf("zero-valued RetryConfig should return base unchanged")
	}
}
