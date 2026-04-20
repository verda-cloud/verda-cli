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
	"time"

	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// Registry-specific AgentError kinds. Command files surface these via
// translateError / translateErrorWithExpiry so ggcr transport details
// never leak to callers.
const (
	kindRegistryAuthFailed        = "registry_auth_failed"
	kindRegistryCredentialExpired = "registry_credential_expired" //nolint:gosec // error-code constant, not a credential
	kindRegistryNotConfigured     = "registry_not_configured"
	kindRegistryRepoNotFound      = "registry_repo_not_found"
	kindRegistryTagNotFound       = "registry_tag_not_found"
	kindRegistryAccessDenied      = "registry_access_denied"
	kindRegistryRateLimited       = "registry_rate_limited"
	kindRegistryUploadFailed      = "registry_upload_failed"
	kindRegistryInvalidReference  = "registry_invalid_reference"
	kindRegistryUnreachable       = "registry_unreachable"
	kindRegistryInternalError     = "registry_internal_error"
)

// translateError maps a generic ggcr/network error to a *cmdutil.AgentError.
// It does NOT know about credential expiry. Callers that have the active
// credentials in hand should prefer translateErrorWithExpiry so UNAUTHORIZED
// responses against an expired credential become registry_credential_expired
// instead of a generic auth failure.
//
// Returns nil for nil input. Returns err unchanged when err is (or wraps)
// context.Canceled / context.DeadlineExceeded so cobra can print its usual
// short cancellation message.
func translateError(err error) error {
	if err == nil {
		return nil
	}

	// Leave context cancellation / deadline errors untouched so higher
	// layers can handle them uniformly.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	// ggcr transport errors carry structured Diagnostic codes.
	var terr *transport.Error
	if errors.As(err, &terr) {
		return translateTransportError(terr)
	}

	// Common network failures (DNS, connection refused, i/o timeout).
	if isNetworkError(err) {
		return &cmdutil.AgentError{
			Code:     kindRegistryUnreachable,
			Message:  fmt.Sprintf("Cannot reach registry: %s.", err.Error()),
			ExitCode: cmdutil.ExitAPI,
		}
	}

	// Fallback: preserve the original message but classify it so agent
	// mode still gets a structured envelope.
	return &cmdutil.AgentError{
		Code:     kindRegistryInternalError,
		Message:  err.Error(),
		ExitCode: cmdutil.ExitAPI,
	}
}

// translateErrorWithExpiry is the preferred entry point for command RunE
// functions that have access to the active credentials. When the underlying
// error is UNAUTHORIZED AND creds.ExpiresAt is in the past, it returns
// registry_credential_expired with the expiry date in the message. Otherwise
// it delegates to translateError.
func translateErrorWithExpiry(err error, creds *options.RegistryCredentials) error {
	if err == nil {
		return nil
	}

	var terr *transport.Error
	if errors.As(err, &terr) && hasDiagnosticCode(terr, transport.UnauthorizedErrorCode) {
		if creds != nil && !creds.ExpiresAt.IsZero() && time.Now().After(creds.ExpiresAt) {
			return &cmdutil.AgentError{
				Code: kindRegistryCredentialExpired,
				Message: fmt.Sprintf(
					"Registry credential expired on %s. Create a new credential in the Verda UI, then run `verda registry configure`.",
					creds.ExpiresAt.Format(time.RFC3339),
				),
				Details: map[string]any{
					"expires_at": creds.ExpiresAt.Format(time.RFC3339),
				},
				ExitCode: cmdutil.ExitAuth,
			}
		}
	}
	return translateError(err)
}

// translateTransportError maps a *transport.Error's first recognized
// Diagnostic code to an AgentError. When no diagnostic code matches, the
// raw transport message is wrapped as registry_internal_error.
func translateTransportError(terr *transport.Error) error {
	raw := terr.Error()

	switch {
	case hasDiagnosticCode(terr, transport.UnauthorizedErrorCode):
		return &cmdutil.AgentError{
			Code:     kindRegistryAuthFailed,
			Message:  "Registry authentication failed. Run `verda registry configure`.",
			ExitCode: cmdutil.ExitAuth,
		}
	case hasDiagnosticCode(terr, transport.NameUnknownErrorCode):
		repo := repoFromTransportError(terr)
		msg := "Repository not found."
		if repo != "" {
			msg = fmt.Sprintf("Repository %q not found.", repo)
		}
		return &cmdutil.AgentError{
			Code:    kindRegistryRepoNotFound,
			Message: msg,
			Details: func() map[string]any {
				if repo == "" {
					return nil
				}
				return map[string]any{"repository": repo}
			}(),
			ExitCode: cmdutil.ExitNotFound,
		}
	case hasDiagnosticCode(terr, transport.ManifestUnknownErrorCode):
		return &cmdutil.AgentError{
			Code:     kindRegistryTagNotFound,
			Message:  "Tag or digest not found in repository.",
			ExitCode: cmdutil.ExitNotFound,
		}
	case hasDiagnosticCode(terr, transport.DeniedErrorCode):
		return &cmdutil.AgentError{
			Code:     kindRegistryAccessDenied,
			Message:  fmt.Sprintf("Access denied: %s.", raw),
			ExitCode: cmdutil.ExitAuth,
		}
	case hasDiagnosticCode(terr, transport.TooManyRequestsErrorCode):
		return &cmdutil.AgentError{
			Code:     kindRegistryRateLimited,
			Message:  "Rate limited by registry.",
			ExitCode: cmdutil.ExitAPI,
		}
	case hasDiagnosticCode(terr, transport.BlobUploadInvalidErrorCode):
		return &cmdutil.AgentError{
			Code:     kindRegistryUploadFailed,
			Message:  "Layer upload failed; retry the push.",
			ExitCode: cmdutil.ExitAPI,
		}
	case hasDiagnosticCode(terr, transport.NameInvalidErrorCode),
		hasDiagnosticCode(terr, transport.TagInvalidErrorCode),
		hasDiagnosticCode(terr, transport.ManifestInvalidErrorCode):
		return &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("Invalid reference: %s.", raw),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}

	return &cmdutil.AgentError{
		Code:     kindRegistryInternalError,
		Message:  raw,
		ExitCode: cmdutil.ExitAPI,
	}
}

// hasDiagnosticCode reports whether any diagnostic on terr carries code.
func hasDiagnosticCode(terr *transport.Error, code transport.ErrorCode) bool {
	for _, d := range terr.Errors {
		if d.Code == code {
			return true
		}
	}
	return false
}

// repoFromTransportError best-effort extracts a repository name from the
// Diagnostic.Detail payload the distribution spec attaches to NAME_UNKNOWN
// responses. The field is not strongly typed (any), so we probe the common
// shapes: a plain string, or a map with a "name" key.
func repoFromTransportError(terr *transport.Error) string {
	for _, d := range terr.Errors {
		if d.Code != transport.NameUnknownErrorCode {
			continue
		}
		switch v := d.Detail.(type) {
		case string:
			if v != "" {
				return v
			}
		case map[string]any:
			if name, ok := v["name"].(string); ok && name != "" {
				return name
			}
		}
	}
	return ""
}

// isNetworkError returns true if err represents a DNS lookup failure, a
// refused TCP connection, or a request-level timeout. These are folded
// into registry_unreachable so users get a single actionable class.
func isNetworkError(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	// Generic net.Error interface (covers net.Error values not matched above,
	// e.g. tls handshake timeouts wrapped by the stdlib).
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout")
}
