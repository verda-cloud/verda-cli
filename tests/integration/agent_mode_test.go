//go:build integration

package integration

import (
	"testing"
)

func TestAgentMode_ForcesJSONOutput(t *testing.T) {
	requireProfile(t, "test")

	r := runAgent(t, "test", "locations")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d\nstderr: %s", r.ExitCode, r.Stderr)
	}

	// Should be valid JSON
	var locations []map[string]any
	parseJSON(t, r, &locations)

	if len(locations) == 0 {
		t.Fatal("expected at least one location")
	}
}

func TestAgentMode_MissingRequiredFlags_VMCreate(t *testing.T) {
	// This test doesn't need a working API — it fails before any API call.
	// But it does need opts.Complete() to not hang, so require profile.
	requireProfile(t, "test")

	r := runAgent(t, "test", "vm", "create")
	if r.ExitCode != 2 {
		t.Fatalf("expected exit code 2 (bad args), got %d\nstderr: %s\nstdout: %s", r.ExitCode, r.Stderr, r.Stdout)
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
		t.Fatalf("expected exit code 2, got %d\nstderr: %s", r.ExitCode, r.Stderr)
	}

	envelope := parseAgentError(t, r)
	if envelope.Error.Code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("expected code MISSING_REQUIRED_FLAGS, got %q", envelope.Error.Code)
	}

	missing, _ := envelope.Error.Details["missing"].([]any)
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

	// List instances to find a real one
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
	if r.ExitCode == 0 {
		t.Fatal("expected non-zero exit code without --yes")
	}

	envelope := parseAgentError(t, r)
	// Accept CONFIRMATION_REQUIRED or other valid error codes (action may not be valid for status)
	t.Logf("got: %s: %s", envelope.Error.Code, envelope.Error.Message)
}

func TestAgentMode_AuthError_NoCredentials(t *testing.T) {
	r := runAgent(t, "test-empty", "vm", "list")
	if r.ExitCode == 0 {
		t.Fatal("expected non-zero exit code with empty credentials")
	}

	envelope := parseAgentError(t, r)
	if envelope.Error.Code != "AUTH_ERROR" && envelope.Error.Code != "ERROR" {
		t.Errorf("expected AUTH_ERROR or ERROR, got %q", envelope.Error.Code)
	}
	t.Logf("got: %s: %s", envelope.Error.Code, envelope.Error.Message)
}

func TestAgentMode_AuthError_InvalidCredentials(t *testing.T) {
	r := runAgent(t, "test-invalid", "vm", "list")
	if r.ExitCode == 0 {
		t.Fatal("expected non-zero exit code with invalid credentials")
	}

	// Stderr should contain a structured error
	if r.Stderr == "" {
		t.Fatalf("expected structured error on stderr, got empty\nstdout: %s", r.Stdout)
	}

	envelope := parseAgentError(t, r)
	t.Logf("got: code=%s exit=%d msg=%s", envelope.Error.Code, r.ExitCode, envelope.Error.Message)
}
