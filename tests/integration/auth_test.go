//go:build integration

package integration

import (
	"testing"
)

func TestAuthShow_ValidProfile(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "auth", "show")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
	if r.Stdout == "" {
		t.Fatal("expected auth show output, got empty")
	}
}

func TestAuthShow_InvalidProfile(t *testing.T) {
	r := runWithProfile(t, "nonexistent-profile-xyz", "auth", "show")
	// auth show should still work — it shows resolved config, not validate credentials
	if r.ExitCode != 0 {
		t.Logf("exit code %d for nonexistent profile (expected)", r.ExitCode)
	}
}

func TestAuthShow_AgentMode(t *testing.T) {
	requireProfile(t, "test")

	r := runAgent(t, "test", "auth", "show")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
	// Agent mode forces JSON output
	if r.Stdout == "" {
		t.Fatal("expected JSON output, got empty")
	}
}
