package util

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

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
