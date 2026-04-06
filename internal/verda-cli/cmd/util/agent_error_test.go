package util

import (
	"bytes"
	"encoding/json"
	"testing"
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
