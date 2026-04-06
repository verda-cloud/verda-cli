//go:build integration

// Package integration provides end-to-end tests for the Verda CLI.
//
// These tests run the actual verda binary against a real API (staging)
// using credential profiles configured in ~/.verda/credentials.
//
// Required profiles:
//
//	[test]            — valid staging credentials
//	[test-invalid]    — wrong client_id/secret
//	[test-empty]      — no client_id/secret
//
// Run:
//
//	go test -tags=integration -v ./tests/integration/
//
// Override binary path:
//
//	VERDA_BIN=/path/to/verda go test -tags=integration -v ./tests/integration/
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// verdaBin returns the path to the verda binary.
func verdaBin() string {
	if bin := os.Getenv("VERDA_BIN"); bin != "" {
		return bin
	}
	return "verda"
}

// cliResult holds the output of a CLI command.
type cliResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// runCLI executes the verda CLI with the given arguments and returns the result.
func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()
	start := time.Now()

	cmd := exec.Command(verdaBin(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run verda %s: %v", strings.Join(args, " "), err)
		}
	}

	return cliResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Duration: duration,
	}
}

// runAgent runs a CLI command in --agent mode with the given profile.
func runAgent(t *testing.T, profile string, args ...string) cliResult {
	t.Helper()
	fullArgs := []string{"--agent", "--auth.profile", profile}
	fullArgs = append(fullArgs, args...)
	return runCLI(t, fullArgs...)
}

// runWithProfile runs a CLI command with a specific auth profile and JSON output.
func runWithProfile(t *testing.T, profile string, args ...string) cliResult {
	t.Helper()
	fullArgs := []string{"--auth.profile", profile, "-o", "json"}
	fullArgs = append(fullArgs, args...)
	return runCLI(t, fullArgs...)
}

// parseJSON unmarshals the stdout of a CLI result into the target.
func parseJSON(t *testing.T, r cliResult, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.Stdout), target); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nstdout: %s\nstderr: %s", err, r.Stdout, r.Stderr)
	}
}

// parseAgentError parses a structured agent error from stderr.
func parseAgentError(t *testing.T, r cliResult) agentErrorEnvelope {
	t.Helper()
	var envelope agentErrorEnvelope
	if err := json.Unmarshal([]byte(r.Stderr), &envelope); err != nil {
		t.Fatalf("failed to parse agent error: %v\nstderr: %s\nstdout: %s", err, r.Stderr, r.Stdout)
	}
	return envelope
}

type agentErrorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details,omitempty"`
	} `json:"error"`
}

// requireProfile skips the test if the given profile is not configured.
func requireProfile(t *testing.T, profile string) {
	t.Helper()
	r := runCLI(t, "--auth.profile", profile, "auth", "show")
	if r.ExitCode != 0 {
		t.Skipf("profile %q not configured, skipping: %s", profile, r.Stderr)
	}
}

// TestMain verifies the binary exists before running tests.
func TestMain(m *testing.M) {
	bin := verdaBin()
	if _, err := exec.LookPath(bin); err != nil {
		fmt.Fprintf(os.Stderr, "verda binary not found at %q. Set VERDA_BIN or add to PATH.\n", bin)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
