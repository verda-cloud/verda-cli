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

package contract

import (
	"strings"
	"testing"
)

// TestDebugRedaction: --debug dumps every request/response body to stderr, so
// the OAuth token exchange must never leak credential values (review H1).
func TestDebugRedaction(t *testing.T) {
	t.Parallel()

	assertNoSecrets := func(t *testing.T, r cliResult) {
		t.Helper()
		if strings.Contains(r.Stderr, testClientSecret) {
			t.Fatalf("stderr leaks client_secret value:\n%s", r.Stderr)
		}
		if strings.Contains(r.Stderr, "mock-access-token") {
			t.Fatalf("stderr leaks issued access token:\n%s", r.Stderr)
		}
		if !strings.Contains(r.Stderr, "DEBUG:") {
			t.Fatalf("expected debug output on stderr, got none:\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
		}
	}

	// JSON token request: redactSensitiveJSON handles "client_secret": "...".
	t.Run("json token request", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		r := runCLI(t, srv, "--debug", "-o", "json", "locations")
		requireExit(t, r, 0)
		assertNoSecrets(t, r)
	})

	// The SDK retries the token request form-encoded when the API answers the
	// JSON attempt with 400. Review H1: that body (incl. client_secret=...)
	// must be redacted too, and the redacted marker proves the redactor ran
	// (not merely that the body vanished).
	t.Run("form-encoded token fallback", func(t *testing.T) {
		t.Parallel()
		srv := newServer(t)
		srv.ForceFormTokenFallback(true)
		r := runCLI(t, srv, "--debug", "-o", "json", "locations")
		requireExit(t, r, 0)
		assertNoSecrets(t, r)
		if !strings.Contains(r.Stderr, "client_secret=<redacted>") {
			t.Fatalf("expected form-body redaction marker in stderr, got none:\n%s", r.Stderr)
		}
	})
}
