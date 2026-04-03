package util

import (
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestInstanceStatusMessageKnown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status string
		want   string
	}{
		{verda.StatusNew, "Creating instance..."},
		{verda.StatusProvisioning, "Provisioning instance..."},
		{verda.StatusRunning, "Running"},
		{verda.StatusOffline, "Offline"},
		{verda.StatusError, "Error"},
		{verda.StatusDeleting, "Deleting..."},
	}

	for _, tt := range tests {
		if got := InstanceStatusMessage(tt.status); got != tt.want {
			t.Errorf("InstanceStatusMessage(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestInstanceStatusMessageFallback(t *testing.T) {
	t.Parallel()

	// Unknown status should return the raw string.
	got := InstanceStatusMessage("some-unknown-status")
	if got != "some-unknown-status" {
		t.Errorf("expected fallback to raw status, got %q", got)
	}
}

func TestInstanceTerminalStatuses(t *testing.T) {
	t.Parallel()

	// These should be terminal.
	for _, s := range []string{verda.StatusRunning, verda.StatusOffline, verda.StatusError} {
		if !InstanceTerminalStatuses[s] {
			t.Errorf("expected %q to be terminal", s)
		}
	}

	// These should NOT be terminal.
	for _, s := range []string{verda.StatusNew, verda.StatusProvisioning, verda.StatusPending} {
		if InstanceTerminalStatuses[s] {
			t.Errorf("expected %q to NOT be terminal", s)
		}
	}
}

func TestVolumeTerminalStatuses(t *testing.T) {
	t.Parallel()

	if !VolumeTerminalStatuses["attached"] {
		t.Error("expected 'attached' to be terminal")
	}
	if !VolumeTerminalStatuses["detached"] {
		t.Error("expected 'detached' to be terminal")
	}
	if VolumeTerminalStatuses["ordered"] {
		t.Error("expected 'ordered' to NOT be terminal")
	}
}
