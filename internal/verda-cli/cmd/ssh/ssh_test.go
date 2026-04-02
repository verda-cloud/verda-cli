package ssh

import (
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func strPtr(s string) *string { return &s }

func TestResolveInstanceByID(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "abc-123", Hostname: "gpu-runner", Status: verda.StatusRunning, IP: strPtr("1.2.3.4")},
		{ID: "def-456", Hostname: "cpu-worker", Status: verda.StatusOffline, IP: strPtr("5.6.7.8")},
	}

	inst := resolveInstance(instances, "def-456")
	if inst == nil {
		t.Fatal("expected to find instance by ID")
	}
	if inst.Hostname != "cpu-worker" {
		t.Fatalf("expected hostname 'cpu-worker', got %q", inst.Hostname)
	}
}

func TestResolveInstanceByHostname(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "abc-123", Hostname: "gpu-runner", Status: verda.StatusRunning, IP: strPtr("1.2.3.4")},
	}

	inst := resolveInstance(instances, "gpu-runner")
	if inst == nil {
		t.Fatal("expected to find instance by hostname")
	}
	if inst.ID != "abc-123" {
		t.Fatalf("expected ID 'abc-123', got %q", inst.ID)
	}
}

func TestResolveInstanceByHostnameCaseInsensitive(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "abc-123", Hostname: "GPU-Runner", Status: verda.StatusRunning, IP: strPtr("1.2.3.4")},
	}

	inst := resolveInstance(instances, "gpu-runner")
	if inst == nil {
		t.Fatal("expected case-insensitive hostname match")
	}
}

func TestResolveInstanceIDTakesPrecedence(t *testing.T) {
	t.Parallel()

	// Edge case: hostname of one instance matches the ID of another.
	instances := []verda.Instance{
		{ID: "match-me", Hostname: "first", Status: verda.StatusRunning, IP: strPtr("1.1.1.1")},
		{ID: "other", Hostname: "match-me", Status: verda.StatusRunning, IP: strPtr("2.2.2.2")},
	}

	inst := resolveInstance(instances, "match-me")
	if inst == nil {
		t.Fatal("expected to find instance")
	}
	// ID match should win over hostname match.
	if inst.Hostname != "first" {
		t.Fatalf("expected ID match to take precedence, got hostname %q", inst.Hostname)
	}
}

func TestResolveInstanceNotFound(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "abc-123", Hostname: "gpu-runner"},
	}

	inst := resolveInstance(instances, "nonexistent")
	if inst != nil {
		t.Fatal("expected nil for nonexistent instance")
	}
}

func TestResolveInstanceEmptyList(t *testing.T) {
	t.Parallel()

	inst := resolveInstance(nil, "anything")
	if inst != nil {
		t.Fatal("expected nil for empty instance list")
	}
}
