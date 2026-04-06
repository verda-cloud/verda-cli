//go:build integration

package integration

import (
	"testing"
)

func TestCostBalance(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "cost", "balance")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var balance map[string]any
	parseJSON(t, r, &balance)

	if _, ok := balance["amount"]; !ok {
		t.Error("balance missing 'amount' field")
	}
}

func TestCostEstimate(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "cost", "estimate", "--type", "1A6000.10V")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
}

func TestCostEstimate_WithStorage(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "cost", "estimate", "--type", "1A6000.10V", "--os-volume", "100", "--storage", "500")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
}

func TestCostRunning(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "cost", "running")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
}

func TestCostEstimate_InvalidType(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "cost", "estimate", "--type", "NONEXISTENT.TYPE")
	if r.ExitCode == 0 {
		t.Fatal("expected non-zero exit code for invalid instance type")
	}
}
