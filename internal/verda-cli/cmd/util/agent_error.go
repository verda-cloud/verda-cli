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

package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// Agent-mode exit codes.
const (
	ExitOK              = 0
	ExitGeneral         = 1
	ExitBadArgs         = 2
	ExitAuth            = 3
	ExitAPI             = 4
	ExitNotFound        = 5
	ExitInsufficientBal = 6
)

// AgentError is a structured error returned in --agent mode.
// It serializes to JSON on stderr so that calling agents can parse it.
type AgentError struct {
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
	ExitCode int            `json:"-"`
}

func (e *AgentError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// agentErrorEnvelope is the JSON envelope written to stderr.
type agentErrorEnvelope struct {
	Error *AgentError `json:"error"`
}

// WriteAgentError writes a structured JSON error to w.
func WriteAgentError(w io.Writer, ae *AgentError) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(agentErrorEnvelope{Error: ae})
}

// NewMissingFlagsError creates an AgentError for missing required flags.
func NewMissingFlagsError(missing []string) *AgentError {
	return &AgentError{
		Code:     "MISSING_REQUIRED_FLAGS",
		Message:  "required flags not provided",
		Details:  map[string]any{"missing": missing},
		ExitCode: ExitBadArgs,
	}
}

// NewConfirmationRequiredError creates an AgentError for destructive actions
// that need --yes in agent mode.
func NewConfirmationRequiredError(action string) *AgentError {
	return &AgentError{
		Code:     "CONFIRMATION_REQUIRED",
		Message:  fmt.Sprintf("destructive action %q requires --yes in agent mode", action),
		Details:  map[string]any{"action": action},
		ExitCode: ExitBadArgs,
	}
}

// NewPromptBlockedError creates an AgentError when an interactive prompt
// is attempted in agent mode.
func NewPromptBlockedError(promptType, prompt string, choices []string) *AgentError {
	details := map[string]any{
		"prompt_type": promptType,
		"prompt":      prompt,
	}
	if len(choices) > 0 {
		details["choices"] = choices
	}
	return &AgentError{
		Code:     "INTERACTIVE_PROMPT_BLOCKED",
		Message:  promptType + " prompt not available in agent mode",
		Details:  details,
		ExitCode: ExitBadArgs,
	}
}

// NewAuthError creates an AgentError for authentication failures.
func NewAuthError(msg string) *AgentError {
	return &AgentError{
		Code:     "AUTH_ERROR",
		Message:  msg,
		ExitCode: ExitAuth,
	}
}

// NewAPIError creates an AgentError for API-level failures.
func NewAPIError(msg string, status int) *AgentError {
	details := map[string]any{}
	if status > 0 {
		details["status"] = status
	}
	return &AgentError{
		Code:     "API_ERROR",
		Message:  msg,
		Details:  details,
		ExitCode: ExitAPI,
	}
}

// NewValidationError creates an AgentError for input validation failures.
func NewValidationError(field, reason string) *AgentError {
	return &AgentError{
		Code:    "VALIDATION_ERROR",
		Message: fmt.Sprintf("invalid value for %s: %s", field, reason),
		Details: map[string]any{
			"field":  field,
			"reason": reason,
		},
		ExitCode: ExitBadArgs,
	}
}

// NewNotFoundError creates an AgentError for missing resources.
func NewNotFoundError(resource, id string) *AgentError {
	return &AgentError{
		Code:    "NOT_FOUND",
		Message: fmt.Sprintf("%s %q not found", resource, id),
		Details: map[string]any{
			"resource": resource,
			"id":       id,
		},
		ExitCode: ExitNotFound,
	}
}

// IsAgentError returns true if the error is (or wraps) an *AgentError.
// Use this to decide whether to emit structured JSON output.
func IsAgentError(err error) bool {
	var ae *AgentError
	return errors.As(err, &ae)
}

// ClassifyError inspects err and returns a structured AgentError if the
// error can be classified. Returns nil if the error is nil or if agent
// mode is not active (detected by checking for an existing AgentError).
//
// Classification priority:
//  1. Already an *AgentError → return as-is
//  2. CLI *UsageError (flag/argument misuse) → VALIDATION_ERROR, exit 2
//  3. SDK *verda.APIError → map status codes to error codes
//  4. SDK *verda.ValidationError → VALIDATION_ERROR
//  5. Auth-related error messages → AUTH_ERROR
//  6. Fallback → generic ERROR
func ClassifyError(err error) *AgentError {
	if err == nil {
		return nil
	}

	// 1. Already classified.
	var ae *AgentError
	if errors.As(err, &ae) {
		return ae
	}

	// 2. CLI usage errors (flag misuse) — bad input, exit 2.
	var usageErr *UsageError
	if errors.As(err, &usageErr) {
		return &AgentError{
			Code:     "VALIDATION_ERROR",
			Message:  usageErr.Message(),
			ExitCode: ExitBadArgs,
		}
	}

	// 3. SDK API error with status code.
	var apiErr *verda.APIError
	if errors.As(err, &apiErr) {
		return classifyAPIError(apiErr)
	}

	// 4. SDK validation error.
	var valErr *verda.ValidationError
	if errors.As(err, &valErr) {
		return NewValidationError(valErr.Field, valErr.Message)
	}

	// 5. Auth-related errors (heuristic on message).
	msg := err.Error()
	if isAuthError(msg) {
		return NewAuthError(msg)
	}

	// 6. Fallback.
	return &AgentError{
		Code:     "ERROR",
		Message:  msg,
		ExitCode: ExitGeneral,
	}
}

func classifyAPIError(apiErr *verda.APIError) *AgentError {
	switch apiErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &AgentError{
			Code:     "AUTH_ERROR",
			Message:  apiErr.Message,
			Details:  map[string]any{"status": apiErr.StatusCode},
			ExitCode: ExitAuth,
		}
	case http.StatusNotFound:
		return &AgentError{
			Code:     "NOT_FOUND",
			Message:  apiErr.Message,
			Details:  map[string]any{"status": apiErr.StatusCode},
			ExitCode: ExitNotFound,
		}
	case http.StatusPaymentRequired:
		return &AgentError{
			Code:     "INSUFFICIENT_BALANCE",
			Message:  apiErr.Message,
			Details:  map[string]any{"status": apiErr.StatusCode},
			ExitCode: ExitInsufficientBal,
		}
	case http.StatusBadRequest:
		if ae := sshKeyRequired(apiErr); ae != nil {
			return ae
		}
		return NewAPIError(apiErr.Error(), apiErr.StatusCode)
	default:
		return NewAPIError(apiErr.Error(), apiErr.StatusCode)
	}
}

// sshKeyRequired recognizes the one API 400 whose own text cannot be shown to a
// user: POST /instances rejects a request that omits ssh_key_ids with "SSH keys
// can be an array of UUID's, a single UUID string, null value or not defined" —
// while the field *was* not defined. Verified live on staging 2026-08-12 with the
// request body captured via --debug: no ssh_key_ids key was sent. Passing that
// through tells the user their correct input was wrong, in the one wording they
// cannot act on.
//
// Returns nil for any other 400 so the generic API_ERROR path still applies.
// Delete this once the API either accepts an absent value or says what it means.
func sshKeyRequired(apiErr *verda.APIError) *AgentError {
	if !strings.Contains(strings.ToLower(apiErr.Message), "ssh key") {
		return nil
	}
	return &AgentError{
		Code: "SSH_KEY_REQUIRED",
		Message: "the API requires at least one SSH key to create an instance, and this request had none: " +
			"pass --ssh-key <id> (CLI) or ssh_key_ids (MCP); list ids with \"verda ssh-key list\"",
		Details: map[string]any{
			"status": apiErr.StatusCode,
			// Verbatim: the only record of what the server actually said.
			"api_message": apiErr.Message,
		},
		ExitCode: ExitBadArgs,
	}
}

func isAuthError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "no credentials configured") ||
		strings.Contains(lower, "auth") && strings.Contains(lower, "failed") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "token expired")
}
