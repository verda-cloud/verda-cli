package vm

import (
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestAvailableActions_Offline(t *testing.T) {
	t.Parallel()
	actions := availableActions(verda.StatusOffline)

	labels := make(map[string]bool, len(actions))
	for _, a := range actions {
		labels[a.Label] = true
	}

	if !labels["Start"] {
		t.Error("expected Start action for offline instances")
	}
	if labels["Shutdown"] {
		t.Error("Shutdown should not be available for offline instances")
	}
	if !labels["Delete instance"] {
		t.Error("expected Delete action (always available)")
	}
}

func TestAvailableActions_Running(t *testing.T) {
	t.Parallel()
	actions := availableActions(verda.StatusRunning)

	labels := make(map[string]bool, len(actions))
	for _, a := range actions {
		labels[a.Label] = true
	}

	if labels["Start"] {
		t.Error("Start should not be available for running instances")
	}
	if !labels["Shutdown"] {
		t.Error("expected Shutdown action for running instances")
	}
	if !labels["Force shutdown"] {
		t.Error("expected Force shutdown action for running instances")
	}
	if !labels["Hibernate"] {
		t.Error("expected Hibernate action for running instances")
	}
	if !labels["Delete instance"] {
		t.Error("expected Delete action (always available)")
	}
}

func TestAvailableActions_Provisioning(t *testing.T) {
	t.Parallel()
	// For non-terminal, non-running/offline statuses, only delete should be available.
	actions := availableActions("provisioning")

	if len(actions) != 1 {
		t.Fatalf("expected 1 action (delete) for provisioning, got %d", len(actions))
	}
	if actions[0].Label != "Delete instance" {
		t.Errorf("expected Delete instance, got %s", actions[0].Label)
	}
}

func TestFilterByStatus_Running(t *testing.T) {
	t.Parallel()
	instances := []verda.Instance{
		{ID: "1", Status: verda.StatusRunning},
		{ID: "2", Status: verda.StatusOffline},
		{ID: "3", Status: verda.StatusRunning},
		{ID: "4", Status: "provisioning"},
	}

	filtered := filterByStatus(instances, []string{verda.StatusRunning})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 running instances, got %d", len(filtered))
	}
	for _, inst := range filtered {
		if inst.Status != verda.StatusRunning {
			t.Errorf("expected running status, got %s", inst.Status)
		}
	}
}

func TestFilterByStatus_MultipleStatuses(t *testing.T) {
	t.Parallel()
	instances := []verda.Instance{
		{ID: "1", Status: verda.StatusRunning},
		{ID: "2", Status: verda.StatusOffline},
		{ID: "3", Status: "provisioning"},
	}

	filtered := filterByStatus(instances, []string{verda.StatusRunning, verda.StatusOffline})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(filtered))
	}
}

func TestFilterByStatus_NoMatch(t *testing.T) {
	t.Parallel()
	instances := []verda.Instance{
		{ID: "1", Status: verda.StatusRunning},
	}

	filtered := filterByStatus(instances, []string{"nonexistent"})
	if len(filtered) != 0 {
		t.Fatalf("expected 0 instances, got %d", len(filtered))
	}
}
