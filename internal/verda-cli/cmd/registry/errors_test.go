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
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// newTransportError is a small helper for building *transport.Error values
// with a single Diagnostic. All tests funnel through it so the exact
// struct shape is documented in one place.
func newTransportError(status int, code transport.ErrorCode, message string, detail any) *transport.Error {
	return &transport.Error{
		StatusCode: status,
		Errors: []transport.Diagnostic{
			{Code: code, Message: message, Detail: detail},
		},
	}
}

func TestTranslateError_Nil(t *testing.T) {
	t.Parallel()
	if translateError(nil) != nil {
		t.Fatal("translateError(nil) should be nil")
	}
}

func TestTranslateError_ContextCanceledPassesThrough(t *testing.T) {
	t.Parallel()
	err := translateError(context.Canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled to pass through, got %v", err)
	}
	var ae *cmdutil.AgentError
	if errors.As(err, &ae) {
		t.Fatalf("context.Canceled should NOT be mapped to AgentError, got %+v", ae)
	}
}

func TestTranslateError_ContextDeadlinePassesThrough(t *testing.T) {
	t.Parallel()
	err := translateError(context.DeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded to pass through, got %v", err)
	}
}

func TestTranslateError_TransportCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		status   int
		code     transport.ErrorCode
		message  string
		wantKind string
		wantExit int
	}{
		{"Unauthorized", 401, transport.UnauthorizedErrorCode, "unauthorized", kindRegistryAuthFailed, cmdutil.ExitAuth},
		{"NameUnknown", 404, transport.NameUnknownErrorCode, "repo missing", kindRegistryRepoNotFound, cmdutil.ExitNotFound},
		{"ManifestUnknown", 404, transport.ManifestUnknownErrorCode, "tag missing", kindRegistryTagNotFound, cmdutil.ExitNotFound},
		{"Denied", 403, transport.DeniedErrorCode, "not allowed", kindRegistryAccessDenied, cmdutil.ExitAuth},
		{"TooManyRequests", 429, transport.TooManyRequestsErrorCode, "slow down", kindRegistryRateLimited, cmdutil.ExitAPI},
		{"BlobUploadInvalid", 400, transport.BlobUploadInvalidErrorCode, "upload broke", kindRegistryUploadFailed, cmdutil.ExitAPI},
		{"NameInvalid", 400, transport.NameInvalidErrorCode, "bad name", kindRegistryInvalidReference, cmdutil.ExitBadArgs},
		{"TagInvalid", 400, transport.TagInvalidErrorCode, "bad tag", kindRegistryInvalidReference, cmdutil.ExitBadArgs},
		{"ManifestInvalid", 400, transport.ManifestInvalidErrorCode, "bad manifest", kindRegistryInvalidReference, cmdutil.ExitBadArgs},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			terr := newTransportError(tc.status, tc.code, tc.message, nil)
			got := translateError(terr)

			var ae *cmdutil.AgentError
			if !errors.As(got, &ae) {
				t.Fatalf("expected *AgentError, got %T (%v)", got, got)
			}
			if ae.Code != tc.wantKind {
				t.Errorf("Code = %q, want %q", ae.Code, tc.wantKind)
			}
			if ae.ExitCode != tc.wantExit {
				t.Errorf("ExitCode = %d, want %d", ae.ExitCode, tc.wantExit)
			}
			if ae.Message == "" {
				t.Error("Message should not be empty")
			}
			if !strings.HasSuffix(ae.Message, ".") {
				t.Errorf("Message %q should end with a period", ae.Message)
			}
		})
	}
}

func TestTranslateError_NameUnknownIncludesRepoFromStringDetail(t *testing.T) {
	t.Parallel()
	terr := newTransportError(404, transport.NameUnknownErrorCode, "repo missing", "proj/app")
	got := translateError(terr)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if !strings.Contains(ae.Message, `"proj/app"`) {
		t.Errorf("Message = %q, expected it to quote the repository", ae.Message)
	}
	if ae.Details["repository"] != "proj/app" {
		t.Errorf(`Details["repository"] = %v, want "proj/app"`, ae.Details["repository"])
	}
}

func TestTranslateError_NameUnknownIncludesRepoFromMapDetail(t *testing.T) {
	t.Parallel()
	detail := map[string]any{"name": "ns/thing"}
	terr := newTransportError(404, transport.NameUnknownErrorCode, "repo missing", detail)
	got := translateError(terr)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if !strings.Contains(ae.Message, `"ns/thing"`) {
		t.Errorf("Message = %q, expected it to quote the repository", ae.Message)
	}
}

func TestTranslateError_NameUnknownWithoutRepoDetail(t *testing.T) {
	t.Parallel()
	terr := newTransportError(404, transport.NameUnknownErrorCode, "repo missing", nil)
	got := translateError(terr)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryRepoNotFound {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryRepoNotFound)
	}
	if ae.Message != "Repository not found." {
		t.Errorf("Message = %q, want %q", ae.Message, "Repository not found.")
	}
}

func TestTranslateError_UnknownDiagnosticCode(t *testing.T) {
	t.Parallel()
	terr := newTransportError(500, transport.UnknownErrorCode, "server boom", nil)
	got := translateError(terr)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryInternalError {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryInternalError)
	}
	if !strings.Contains(ae.Message, "server boom") {
		t.Errorf("Message = %q, expected to preserve original text", ae.Message)
	}
}

func TestTranslateError_DNSFailure(t *testing.T) {
	t.Parallel()
	dnsErr := &net.DNSError{Err: "no such host", Name: "does-not-exist.invalid"}
	got := translateError(dnsErr)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryUnreachable {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryUnreachable)
	}
}

func TestTranslateError_ConnectionRefused(t *testing.T) {
	t.Parallel()
	// net.OpError{Err: "connection refused"} is the shape net.Dial surfaces.
	op := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	got := translateError(op)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryUnreachable {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryUnreachable)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestTranslateError_TimeoutViaURLError(t *testing.T) {
	t.Parallel()
	urlErr := &url.Error{Op: "Get", URL: "https://vccr.io/v2/", Err: timeoutError{}}
	got := translateError(urlErr)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryUnreachable {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryUnreachable)
	}
}

func TestTranslateError_UnknownErrorFallback(t *testing.T) {
	t.Parallel()
	base := errors.New("something weird happened")
	got := translateError(base)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryInternalError {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryInternalError)
	}
	if !strings.Contains(ae.Message, "something weird happened") {
		t.Errorf("Message = %q, expected original text preserved", ae.Message)
	}
}

func TestTranslateError_WrappedTransportError(t *testing.T) {
	t.Parallel()
	// The command layer typically wraps ggcr errors via fmt.Errorf("...: %w", err).
	// translateError must still classify them through errors.As.
	terr := newTransportError(401, transport.UnauthorizedErrorCode, "unauthorized", nil)
	wrapped := fmt.Errorf("pushing image: %w", terr)
	got := translateError(wrapped)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryAuthFailed)
	}
}

func TestTranslateErrorWithExpiry_Nil(t *testing.T) {
	t.Parallel()
	if translateErrorWithExpiry(nil, nil) != nil {
		t.Fatal("nil err should stay nil")
	}
}

func TestTranslateErrorWithExpiry_UnauthorizedWithExpiredCreds(t *testing.T) {
	t.Parallel()
	expiry := time.Now().Add(-48 * time.Hour)
	creds := &options.RegistryCredentials{ExpiresAt: expiry}
	terr := newTransportError(401, transport.UnauthorizedErrorCode, "unauthorized", nil)

	got := translateErrorWithExpiry(terr, creds)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryCredentialExpired {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryCredentialExpired)
	}
	if ae.ExitCode != cmdutil.ExitAuth {
		t.Errorf("ExitCode = %d, want %d", ae.ExitCode, cmdutil.ExitAuth)
	}
	if !strings.Contains(ae.Message, expiry.Format(time.RFC3339)) {
		t.Errorf("Message = %q, expected to contain expiry timestamp", ae.Message)
	}
	if ae.Details["expires_at"] != expiry.Format(time.RFC3339) {
		t.Errorf(`Details["expires_at"] = %v, want %q`, ae.Details["expires_at"], expiry.Format(time.RFC3339))
	}
}

func TestTranslateErrorWithExpiry_UnauthorizedWithHealthyCreds(t *testing.T) {
	t.Parallel()
	creds := &options.RegistryCredentials{ExpiresAt: time.Now().Add(24 * time.Hour)}
	terr := newTransportError(401, transport.UnauthorizedErrorCode, "unauthorized", nil)

	got := translateErrorWithExpiry(terr, creds)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q (should fall back to translateError when creds healthy)", ae.Code, kindRegistryAuthFailed)
	}
}

func TestTranslateErrorWithExpiry_UnauthorizedWithZeroExpiry(t *testing.T) {
	t.Parallel()
	// ExpiresAt zero is "unknown / non-expiring" — must not be treated as expired.
	creds := &options.RegistryCredentials{}
	terr := newTransportError(401, transport.UnauthorizedErrorCode, "unauthorized", nil)

	got := translateErrorWithExpiry(terr, creds)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryAuthFailed)
	}
}

func TestTranslateErrorWithExpiry_NilCredsDelegates(t *testing.T) {
	t.Parallel()
	terr := newTransportError(401, transport.UnauthorizedErrorCode, "unauthorized", nil)

	got := translateErrorWithExpiry(terr, nil)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryAuthFailed)
	}
}

func TestTranslateErrorWithExpiry_NonAuthError(t *testing.T) {
	t.Parallel()
	// A non-UNAUTHORIZED error should not consult the expiry, even when it's past.
	creds := &options.RegistryCredentials{ExpiresAt: time.Now().Add(-48 * time.Hour)}
	terr := newTransportError(404, transport.ManifestUnknownErrorCode, "missing", nil)

	got := translateErrorWithExpiry(terr, creds)
	var ae *cmdutil.AgentError
	if !errors.As(got, &ae) {
		t.Fatalf("expected *AgentError, got %T", got)
	}
	if ae.Code != kindRegistryTagNotFound {
		t.Errorf("Code = %q, want %q", ae.Code, kindRegistryTagNotFound)
	}
}
