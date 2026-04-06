package util

import (
	"encoding/json"
	"fmt"
	"io"
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
