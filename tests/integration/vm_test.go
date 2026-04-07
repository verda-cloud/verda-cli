//go:build integration

package integration

import (
	"testing"
)

func TestVMList(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "vm", "list")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var instances []map[string]any
	parseJSON(t, r, &instances)
	// May be empty, that's OK — just verify it returns valid JSON array
	t.Logf("found %d instance(s)", len(instances))
}

func TestVMList_StatusFilter(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "vm", "list", "--status", "running")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var instances []map[string]any
	parseJSON(t, r, &instances)

	for _, inst := range instances {
		status, _ := inst["status"].(string)
		if status != "running" {
			t.Errorf("expected status 'running', got %q", status)
		}
	}
}

func TestVMDescribe_InvalidID(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "vm", "describe", "nonexistent-id-12345")
	if r.ExitCode == 0 {
		t.Fatal("expected non-zero exit code for invalid instance ID")
	}
}

func TestVMList_AgentMode(t *testing.T) {
	requireProfile(t, "test")

	r := runAgent(t, "test", "vm", "list")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	// Agent mode should return JSON
	var instances []map[string]any
	parseJSON(t, r, &instances)
}

func TestSSHKeyList(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "ssh-key", "list")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var keys []map[string]any
	parseJSON(t, r, &keys)
	t.Logf("found %d SSH key(s)", len(keys))
}

func TestVolumeList(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "volume", "list")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var volumes []map[string]any
	parseJSON(t, r, &volumes)
	t.Logf("found %d volume(s)", len(volumes))
}
