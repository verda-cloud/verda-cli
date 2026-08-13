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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestAgentError_Error(t *testing.T) {
	ae := &AgentError{Code: "TEST_CODE", Message: "test message"}
	if got := ae.Error(); got != "TEST_CODE: test message" {
		t.Errorf("Error() = %q, want %q", got, "TEST_CODE: test message")
	}
}

func TestWriteAgentError(t *testing.T) {
	var buf bytes.Buffer
	ae := NewMissingFlagsError([]string{"--foo", "--bar"})
	WriteAgentError(&buf, ae)

	var envelope struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if envelope.Error.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code = %q, want MISSING_REQUIRED_FLAGS", envelope.Error.Code)
	}
	missing, ok := envelope.Error.Details["missing"].([]any)
	if !ok || len(missing) != 2 {
		t.Errorf("missing = %v, want [--foo, --bar]", envelope.Error.Details["missing"])
	}
}

func TestNewConfirmationRequiredError(t *testing.T) {
	ae := NewConfirmationRequiredError("delete")
	if ae.Code != "CONFIRMATION_REQUIRED" {
		t.Errorf("code = %q, want CONFIRMATION_REQUIRED", ae.Code)
	}
	if ae.ExitCode != ExitBadArgs {
		t.Errorf("exit code = %d, want %d", ae.ExitCode, ExitBadArgs)
	}
}

func TestNewPromptBlockedError(t *testing.T) {
	ae := NewPromptBlockedError("select", "Pick one", []string{"a", "b"})
	if ae.Code != "INTERACTIVE_PROMPT_BLOCKED" {
		t.Errorf("code = %q, want INTERACTIVE_PROMPT_BLOCKED", ae.Code)
	}
	choices, ok := ae.Details["choices"].([]string)
	if !ok || len(choices) != 2 {
		t.Errorf("choices = %v, want [a, b]", ae.Details["choices"])
	}
}

func TestClassifyError_Nil(t *testing.T) {
	if got := ClassifyError(nil); got != nil {
		t.Errorf("ClassifyError(nil) = %v, want nil", got)
	}
}

func TestClassifyError_AlreadyAgentError(t *testing.T) {
	orig := NewAuthError("bad creds")
	got := ClassifyError(orig)
	if got.Code != "AUTH_ERROR" {
		t.Errorf("code = %q, want AUTH_ERROR", got.Code)
	}
}

func TestClassifyError_SDKAPIError401(t *testing.T) {
	err := &verda.APIError{StatusCode: 401, Message: "unauthorized"}
	got := ClassifyError(err)
	if got.Code != "AUTH_ERROR" {
		t.Errorf("code = %q, want AUTH_ERROR", got.Code)
	}
	if got.ExitCode != ExitAuth {
		t.Errorf("exit code = %d, want %d", got.ExitCode, ExitAuth)
	}
}

func TestClassifyError_SDKAPIError404(t *testing.T) {
	err := &verda.APIError{StatusCode: 404, Message: "not found"}
	got := ClassifyError(err)
	if got.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", got.Code)
	}
	if got.ExitCode != ExitNotFound {
		t.Errorf("exit code = %d, want %d", got.ExitCode, ExitNotFound)
	}
}

func TestClassifyError_SDKAPIError402(t *testing.T) {
	err := &verda.APIError{StatusCode: 402, Message: "insufficient balance"}
	got := ClassifyError(err)
	if got.Code != "INSUFFICIENT_BALANCE" {
		t.Errorf("code = %q, want INSUFFICIENT_BALANCE", got.Code)
	}
	if got.ExitCode != ExitInsufficientBal {
		t.Errorf("exit code = %d, want %d", got.ExitCode, ExitInsufficientBal)
	}
}

func TestClassifyError_SDKAPIError500(t *testing.T) {
	err := &verda.APIError{StatusCode: 500, Message: "internal server error"}
	got := ClassifyError(err)
	if got.Code != "API_ERROR" {
		t.Errorf("code = %q, want API_ERROR", got.Code)
	}
	if got.ExitCode != ExitAPI {
		t.Errorf("exit code = %d, want %d", got.ExitCode, ExitAPI)
	}
}

func TestClassifyError_SDKValidationError(t *testing.T) {
	err := &verda.ValidationError{Field: "hostname", Message: "too long"}
	got := ClassifyError(err)
	if got.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", got.Code)
	}
	if got.Details["field"] != "hostname" {
		t.Errorf("field = %q, want hostname", got.Details["field"])
	}
}

func TestClassifyError_AuthHeuristic(t *testing.T) {
	err := errors.New("no credentials configured\n\nRun verda auth login")
	got := ClassifyError(err)
	if got.Code != "AUTH_ERROR" {
		t.Errorf("code = %q, want AUTH_ERROR", got.Code)
	}
}

func TestClassifyError_Fallback(t *testing.T) {
	err := errors.New("something unexpected happened")
	got := ClassifyError(err)
	if got.Code != "ERROR" {
		t.Errorf("code = %q, want ERROR", got.Code)
	}
	if got.ExitCode != ExitGeneral {
		t.Errorf("exit code = %d, want %d", got.ExitCode, ExitGeneral)
	}
}

// Usage/flag-misuse errors must classify as VALIDATION_ERROR with exit 2 so
// agents can tell bad input apart from server failures (exit 4/1) and fix the
// call. The --help hint stays out of the envelope (human-facing text only).
func TestClassifyError_UsageError(t *testing.T) {
	cmd := &cobra.Command{Use: "verda volume delete"}
	err := UsageErrorf(cmd, "--status can only be used with --all")

	got := ClassifyError(err)
	if got.Code != "VALIDATION_ERROR" {
		t.Errorf("code = %q, want VALIDATION_ERROR", got.Code)
	}
	if got.ExitCode != ExitBadArgs {
		t.Errorf("exit code = %d, want %d", got.ExitCode, ExitBadArgs)
	}
	if got.Message != "--status can only be used with --all" {
		t.Errorf("message = %q — the --help hint must not leak into the envelope", got.Message)
	}

	// Human-facing text keeps the hint.
	if !strings.Contains(err.Error(), "--help") {
		t.Errorf("Error() lost the help hint: %q", err.Error())
	}

	// Wrapping keeps the classification.
	wrapped := fmt.Errorf("volume delete: %w", err)
	if ClassifyError(wrapped).Code != "VALIDATION_ERROR" {
		t.Error("wrapped UsageError lost the VALIDATION_ERROR classification")
	}
}

// Unactionable upstream wording becomes a code both surfaces can act on, with
// the original preserved in details.
func TestClassifySSHKeyRequired(t *testing.T) {
	t.Parallel()

	const apiMsg = "SSH keys can be an array of UUID's, a single UUID string, null value or not defined"
	ae := ClassifyError(&verda.APIError{StatusCode: 400, Message: apiMsg})

	if ae.Code != "SSH_KEY_REQUIRED" {
		t.Fatalf("code = %q, want SSH_KEY_REQUIRED", ae.Code)
	}
	if ae.ExitCode != ExitBadArgs {
		t.Errorf("exit = %d, want %d (bad input, not an API fault)", ae.ExitCode, ExitBadArgs)
	}
	if !strings.Contains(ae.Message, "--ssh-key") {
		t.Errorf("message must name the CLI flag: %q", ae.Message)
	}
	if got := ae.Details["api_message"]; got != apiMsg {
		t.Errorf("details.api_message = %v, want the verbatim server text", got)
	}
	if got := ae.Details["status"]; got != 400 {
		t.Errorf("details.status = %v, want 400", got)
	}
}

// Any other 400 keeps the generic mapping — the special case must not widen.
func TestClassifyOtherBadRequestStaysAPIError(t *testing.T) {
	t.Parallel()

	ae := ClassifyError(&verda.APIError{StatusCode: 400, Message: "hostname already in use"})
	if ae.Code != "API_ERROR" {
		t.Errorf("code = %q, want API_ERROR", ae.Code)
	}
	if ae.ExitCode != ExitAPI {
		t.Errorf("exit = %d, want %d", ae.ExitCode, ExitAPI)
	}
}
