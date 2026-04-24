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
	"net/http"
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
	kindRegistryAuthFailed         = "registry_auth_failed"
	kindRegistryCredentialExpired  = "registry_credential_expired" //nolint:gosec // error-code constant, not a credential
	kindRegistryNotConfigured      = "registry_not_configured"
	kindRegistryRepoNotFound       = "registry_repo_not_found"
	kindRegistryTagNotFound        = "registry_tag_not_found"
	kindRegistryAccessDenied       = "registry_access_denied"
	kindRegistryRateLimited        = "registry_rate_limited"
	kindRegistryUploadFailed       = "registry_upload_failed"
	kindRegistryInvalidReference   = "registry_invalid_reference"
	kindRegistryUnreachable        = "registry_unreachable"
	kindRegistryInternalError      = "registry_internal_error"
	kindRegistryNoImageSource      = "registry_no_image_source"
	kindRegistryCopyPartialFailure = "registry_copy_partial_failure"
	kindRegistryDeleteBlocked      = "registry_delete_blocked"
)

// registryDeleteBlockedRecoveryMessage is the user-facing text for a 412
// "Precondition Failed" from Harbor's delete endpoints. Harbor uses 412 to
// signal project-policy blocks — Tag Immutability rules and Tag Retention
// rules are the two production sources. The practical fix is always "edit
// the policy in the web UI then retry", so the message funnels users there
// first and keeps support as the escalation path.
const registryDeleteBlockedRecoveryMessage = "Deletion is blocked by a Verda project policy.\n" +
	"\n" +
	"To fix:\n" +
	"  1. Open the project in the Verda web UI and review Tag Immutability\n" +
	"     and Tag Retention rules — one of them matches the artifact / tag\n" +
	"     you are trying to delete.\n" +
	"  2. Adjust or remove the rule, then retry the `verda registry delete`\n" +
	"     command. If the rule is required and you still need the artifact\n" +
	"     gone, contact Verda support at support@verda.cloud."

// registryAuthFailedRecoveryMessage is the user-facing text for a 401 /
// "UNAUTHORIZED" from the registry after the pre-flight expiry check has
// already passed. If the credential were merely expired, translateError-
// WithExpiry rewrites the code to registry_credential_expired — so by the
// time this message fires the credential is still inside its expiry
// window, which means the secret was revoked server-side (the web UI
// "delete credential" action) or otherwise invalidated.
//
// Every 401-surfacing AgentError in this package reuses this constant so
// the recovery steps stay in sync across `ls` (Harbor REST) and
// push/tags/copy/login (ggcr transport). Update this one place when the
// portal URL or support address changes.
const registryAuthFailedRecoveryMessage = "Registry authentication failed. Your credential may be invalid or revoked.\n" +
	"\n" +
	"To fix:\n" +
	"  1. Create a new credential in the Verda web UI and run `verda registry configure`\n" +
	"     with the docker-login string the UI shows.\n" +
	"  2. If the problem persists, contact Verda support at support@verda.cloud."

// registryAccessDeniedRecoveryMessage is the user-facing text for a 403
// from the Harbor REST listing endpoints (`ls` / artifact drill-down).
// 403 here means the credential itself is valid but the robot account
// does not carry the required `list repository` / `pull artifact`
// permission bit. The practical fix from a user's perspective is
// identical to 401 — mint a fresh credential — because a freshly-minted
// robot inherits the current permission policy, which (post-Apr 2026)
// includes the listing permissions. Contacting support is the second
// step when a rotation doesn't help.
const registryAccessDeniedRecoveryMessage = "Your registry credential does not have permission for this operation.\n" +
	"\n" +
	"To fix:\n" +
	"  1. Create a new credential in the Verda web UI (newly-minted credentials include\n" +
	"     the required list / pull permissions) and run `verda registry configure` with\n" +
	"     the docker-login string the UI shows.\n" +
	"  2. If the problem persists, contact Verda support at support@verda.cloud."

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

	// Pre-translated errors (e.g. from harbor.go's translateHarborError, or
	// any future pluggable client) pass through unchanged. Without this
	// guard they hit the fallback below and get rewrapped as
	// registry_internal_error, hiding the original structured code.
	var pre *cmdutil.AgentError
	if errors.As(err, &pre) {
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
			Message:  registryAuthFailedRecoveryMessage,
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

	// HEAD responses have no body, so ggcr's test registry (and some
	// production registries) can surface a 404 without a structured
	// Diagnostic slice. Classify those as tag_not_found so callers
	// (e.g. copy's overwrite pre-flight) can treat them as "safe to
	// write" rather than bailing with an opaque internal_error.
	if terr.StatusCode == http.StatusNotFound {
		return &cmdutil.AgentError{
			Code:     kindRegistryTagNotFound,
			Message:  "Tag or digest not found in repository.",
			ExitCode: cmdutil.ExitNotFound,
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
