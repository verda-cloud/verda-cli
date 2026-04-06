//go:build integration

package integration

import (
	"testing"
)

func TestAgentMode_ForcesJSONOutput(t *testing.T) {
	requireProfile(t, "test")

	r := runAgent(t, "test", "locations")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	// Should be valid JSON
	var locations []map[string]any
	parseJSON(t, r, &locations)

	if len(locations) == 0 {
		t.Fatal("expected at least one location")
	}
}

func TestAgentMode_MissingRequiredFlags_VMCreate(t *testing.T) {
	requireProfile(t, "test")

	r := runAgent(t, "test", "vm", "create")
	if r.ExitCode != 2 {
		t.Fatalf("expected exit code 2 (bad args), got %d", r.ExitCode)
	}

	envelope := parseAgentError(t, r)
	if envelope.Error.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("expected code MISSING_REQUIRED_FLAGS, got %q", envelope.Error.Code)
	}

	// Verify missing flags are listed
	missing, ok := envelope.Error.Details["missing"]
	if !ok {
		t.Fatal("expected 'missing' in details")
	}
	missingList, ok := missing.([]any)
	if !ok || len(missingList) == 0 {
		t.Fatalf("expected non-empty missing list, got %v", missing)
	}
	t.Logf("missing flags: %v", missingList)
}

func TestAgentMode_MissingRequiredFlags_VMAction(t *testing.T) {
	requireProfile(t, "test")

	// --agent vm action without --id and --action
	r := runAgent(t, "test", "vm", "action")
	if r.ExitCode != 2 {
		t.Fatalf("expected exit code 2, got %d", r.ExitCode)
	}

	envelope := parseAgentError(t, r)
	if envelope.Error.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("expected code MISSING_REQUIRED_FLAGS, got %q", envelope.Error.Code)
	}

	missing, _ := envelope.Error.Details["missing"].([]any)
	// Should include --id and --action
	hasID, hasAction := false, false
	for _, m := range missing {
		s, _ := m.(string)
		if s == "--id" {
			hasID = true
		}
		if s == "--action" {
			hasAction = true
		}
	}
	if !hasID {
		t.Error("expected --id in missing flags")
	}
	if !hasAction {
		t.Error("expected --action in missing flags")
	}
}

func TestAgentMode_ConfirmationRequired_VMAction(t *testing.T) {
	requireProfile(t, "test")

	// Need a real instance ID for this test. List instances first.
	listResult := runAgent(t, "test", "vm", "list")
	if listResult.ExitCode != 0 {
		t.Skipf("cannot list VMs: %s", listResult.Stderr)
	}

	var instances []map[string]any
	parseJSON(t, listResult, &instances)
	if len(instances) == 0 {
		t.Skip("no instances available for action test")
	}

	instanceID, _ := instances[0]["id"].(string)
	if instanceID == "" {
		t.Skip("first instance has no ID")
	}

	// Try shutdown without --yes
	r := runAgent(t, "test", "vm", "action", "--id", instanceID, "--action", "shutdown")

	// Should get either CONFIRMATION_REQUIRED or the action may not be valid for status
	if r.ExitCode == 0 {
		t.Fatal("expected non-zero exit code without --yes")
	}

	envelope := parseAgentError(t, r)
	// Accept either CONFIRMATION_REQUIRED or other valid error codes
	validCodes := map[string]bool{
		"CONFIRMATION_REQUIRED": true,
		"API_ERROR":             true, // action may not be valid for current status
		"ERROR":                 true, // generic error is acceptable
	}
	if !validCodes[envelope.Error.Code] {
		t.Errorf("unexpected error code %q", envelope.Error.Code)
	}
	t.Logf("got expected error: %s: %s", envelope.Error.Code, envelope.Error.Message)
}

func TestAgentMode_AuthError_NoCredentials(t *testing.T) {
	r := runAgent(t, "test-empty", "vm", "list")
	if r.ExitCode == 0 {
		t.Fatal("expected non-zero exit code with empty credentials")
	}

	envelope := parseAgentError(t, r)
	validCodes := map[string]bool{
		"AUTH_ERROR": true,
		"ERROR":      true, // generic error with auth message
	}
	if !validCodes[envelope.Error.Code] {
		t.Errorf("expected AUTH_ERROR or ERROR, got %q", envelope.Error.Code)
	}
	t.Logf("got: %s: %s", envelope.Error.Code, envelope.Error.Message)
}

func TestAgentMode_AuthError_InvalidCredentials(t *testing.T) {
	r := runAgent(t, "test-invalid", "vm", "list")
	if r.ExitCode == 0 {
		t.Fatal("expected non-zero exit code with invalid credentials")
	}

	envelope := parseAgentError(t, r)
	// Should be AUTH_ERROR or API_ERROR depending on how the server responds
	if r.ExitCode != 3 && r.ExitCode != 4 && r.ExitCode != 1 {
		t.Errorf("expected exit code 1, 3, or 4, got %d", r.ExitCode)
	}
	t.Logf("got: %s: %s", envelope.Error.Code, envelope.Error.Message)
}
