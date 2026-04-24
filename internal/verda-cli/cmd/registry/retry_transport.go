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
	"errors"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Default tuning constants for RetryConfig. Zero-valued fields on
// RetryConfig are back-filled with these in NewRetryingTransport.
const (
	defaultRetryAttempts    = 5
	defaultRetryBaseDelay   = 500 * time.Millisecond
	defaultRetryMaxInterval = 30 * time.Second
)

// RetryConfig controls automatic retries of idempotent HTTP operations.
// Zero value disables retries.
type RetryConfig struct {
	MaxAttempts int           // default 5 (applied if 0)
	BaseDelay   time.Duration // default 500ms
	MaxInterval time.Duration // cap on per-attempt wait (default 30s)
	// Optional clock injection for tests.
	Now   func() time.Time
	Sleep func(time.Duration)
}

// enabled reports whether retries should be attempted at all.
//
// A zero-valued RetryConfig (MaxAttempts == 0) explicitly disables
// retries — this is the shape the ls/tags/login call sites pass today.
// Negative MaxAttempts is treated as a typo and also disables retries.
func (c RetryConfig) enabled() bool {
	return c.MaxAttempts > 1
}

// withDefaults returns a copy of c with zero-valued fields replaced by
// their default. Only called when retries are enabled.
func (c RetryConfig) withDefaults() RetryConfig {
	if c.MaxAttempts == 0 {
		c.MaxAttempts = defaultRetryAttempts
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = defaultRetryBaseDelay
	}
	if c.MaxInterval <= 0 {
		c.MaxInterval = defaultRetryMaxInterval
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Sleep == nil {
		c.Sleep = time.Sleep
	}
	return c
}

// retryTransport wraps a base RoundTripper with retry logic. See
// NewRetryingTransport for the full contract.
type retryTransport struct {
	base http.RoundTripper
	cfg  RetryConfig
	// rng is seeded per-instance so jitter is deterministic inside a
	// single process boundary (tests can still inject their own timing
	// via cfg.Sleep; the RNG draw is irrelevant in that case).
	rng *rand.Rand
}

// NewRetryingTransport wraps base with retry logic. The wrapper retries
// idempotent methods (GET, HEAD, PUT, DELETE, PATCH) on:
//   - response status 408, 429, 500, 502, 503, 504
//   - net.Error with Timeout() == true
//   - *url.Error wrapping either of the above
//
// Non-idempotent methods (POST) are NOT retried — this avoids
// double-creating upload sessions. For POST /v2/<name>/blobs/uploads/
// specifically, a single failure is fine: the client restarts the
// session on the next attempt.
//
// Respects Retry-After on 429/503 (seconds or HTTP-date). Falls back to
// exponential backoff with jitter when the header is absent.
//
// If cfg.MaxAttempts <= 1 the wrapper is a no-op passthrough (this is
// the "disabled" shape ls/tags/login pass today).
func NewRetryingTransport(base http.RoundTripper, cfg RetryConfig) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if !cfg.enabled() {
		return base
	}
	return &retryTransport{
		base: base,
		cfg:  cfg.withDefaults(),
		//nolint:gosec // jitter does not require a cryptographic RNG
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// RoundTrip implements http.RoundTripper.
func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Non-idempotent methods are passed through unchanged — we never
	// retry POST so that /v2/<name>/blobs/uploads/ doesn't double-create
	// upload sessions, and so that an HTTP server can assume it will
	// not see duplicate requests.
	if !isIdempotentMethod(req.Method) {
		return t.base.RoundTrip(req)
	}

	// Body-rewind preflight: if we have a body and no way to reset it,
	// we can only safely attempt the request once. Fall through to the
	// base transport without the retry wrapper in that case.
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return t.base.RoundTrip(req)
	}

	var (
		resp    *http.Response
		lastErr error
	)

	for attempt := 1; attempt <= t.cfg.MaxAttempts; attempt++ {
		// On attempt > 1, rewind the body if we have one.
		if attempt > 1 && req.Body != nil && req.Body != http.NoBody {
			if req.GetBody == nil {
				// Shouldn't reach here (preflight above); be defensive.
				return resp, lastErr
			}
			body, err := req.GetBody()
			if err != nil {
				// We can't safely retry without a fresh body; return
				// whatever we already had in hand.
				return resp, lastErr
			}
			req.Body = body
		}

		resp, lastErr = t.base.RoundTrip(req)

		// Decide whether this attempt is retriable.
		retryable, retryAfter := t.classify(resp, lastErr)
		if !retryable {
			return resp, lastErr
		}

		// Last attempt: surface the last response + error unchanged.
		if attempt == t.cfg.MaxAttempts {
			return resp, lastErr
		}

		// Drain + close the response body so the connection can be
		// returned to the pool. The caller never sees this response.
		drainAndClose(resp)
		resp = nil

		wait := retryAfter
		if wait <= 0 {
			wait = t.backoff(attempt)
		}

		// Context-aware sleep: a canceled request short-circuits the
		// backoff wait. We don't consult cfg.Sleep here because that
		// helper is synchronous; instead we drive a timer + the
		// request context through select, and only call cfg.Sleep on
		// its timer-fired branch (tests that inject a fake Sleep still
		// observe the intended duration).
		if err := t.waitWithCtx(req, wait); err != nil {
			return nil, err
		}
	}

	return resp, lastErr
}

// classify inspects the response/error pair produced by a single
// RoundTrip call and decides whether to retry. The second return is
// the duration to wait before the next attempt; zero means "use
// exponential backoff".
func (t *retryTransport) classify(resp *http.Response, err error) (bool, time.Duration) {
	if err != nil {
		if isTimeoutError(err) {
			return true, 0
		}
		// Every other transport error is treated as a hard failure;
		// ggcr surfaces these through its own error types and the
		// caller gets a clear signal.
		return false, 0
	}
	if resp == nil {
		return false, 0
	}
	switch resp.StatusCode {
	case http.StatusRequestTimeout, // 408
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusGatewayTimeout:      // 504
		return true, 0
	case http.StatusTooManyRequests, // 429
		http.StatusServiceUnavailable: // 503
		return true, parseRetryAfter(resp.Header.Get("Retry-After"), t.cfg.Now())
	default:
		return false, 0
	}
}

// backoff returns the exponential-with-jitter wait for a given attempt.
// attempt is 1-indexed (the wait AFTER attempt 1 uses BaseDelay).
func (t *retryTransport) backoff(attempt int) time.Duration {
	// exp = BaseDelay * 2^(attempt-1). Cap at MaxInterval before
	// adding jitter so the pre-jitter component is predictable.
	shift := attempt - 1
	if shift < 0 {
		shift = 0
	}
	// Guard against overflow on absurd MaxAttempts values.
	if shift > 20 {
		shift = 20
	}
	exp := t.cfg.BaseDelay << shift
	if exp <= 0 || exp > t.cfg.MaxInterval {
		exp = t.cfg.MaxInterval
	}
	// Jitter: uniform [0, exp/2). time.Duration is int64 nanoseconds.
	half := int64(exp / 2)
	var jitter time.Duration
	if half > 0 {
		jitter = time.Duration(t.rng.Int63n(half))
	}
	return exp + jitter
}

// waitWithCtx sleeps for d honoring req.Context() cancellation. The
// cfg.Sleep hook is invoked with d so tests can observe the intended
// wait duration, but the cancellation short-circuit is driven by a
// real timer so context cancellation during sleep is observable.
func (t *retryTransport) waitWithCtx(req *http.Request, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	ctx := req.Context()
	// Fast-path: context already canceled.
	if err := ctx.Err(); err != nil {
		return err
	}
	// Tests that inject cfg.Sleep want to observe d. We fire the hook
	// on a goroutine and race it against ctx.Done so cancel-during-sleep
	// still returns promptly. The production path uses time.Sleep,
	// which is indistinguishable for our purposes.
	done := make(chan struct{})
	go func() {
		t.cfg.Sleep(d)
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isIdempotentMethod reports whether a method is safe to retry without
// risk of side effects from duplicate delivery.
func isIdempotentMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPut,
		http.MethodDelete, http.MethodPatch:
		return true
	default:
		return false
	}
}

// isTimeoutError reports whether err (or something it wraps) is a
// net.Error with Timeout() == true. *url.Error wraps transport errors
// for the net/http client, so we unwrap that case specifically.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isTimeoutError(urlErr.Err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// parseRetryAfter parses the Retry-After header value. It accepts both
// integer seconds and HTTP-date forms per RFC 7231 §7.1.3. Returns zero
// when the header is absent or unparseable — callers fall back to
// exponential backoff.
func parseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	// Integer seconds (most common).
	if secs, err := strconv.Atoi(value); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	// HTTP-date form.
	if t, err := http.ParseTime(value); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

// drainAndClose reads and closes resp.Body so the underlying connection
// is returned to the HTTP pool. A nil resp or nil body is a no-op.
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
