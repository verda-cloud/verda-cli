package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// ---------------------------------------------------------------------------
// Part A: Validation and Routing (Task 2)
// ---------------------------------------------------------------------------

func TestBatchRejectsAllWithID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := &cmdutil.TestFactory{AgentModeOverride: true}

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdVM(f, ioStreams))
	root.SetArgs([]string{"vm", "stop", "--all", "--id", "inst-123", "--yes"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when combining --all with --id")
	}
	if !strings.Contains(err.Error(), "cannot combine --all with --id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBatchRejectsAllWithPositionalArg(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := &cmdutil.TestFactory{AgentModeOverride: true}

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdVM(f, ioStreams))
	root.SetArgs([]string{"vm", "stop", "inst-123", "--all", "--yes"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when combining --all with positional arg")
	}
	if !strings.Contains(err.Error(), "cannot combine --all with --id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBatchRejectsWithVolumesOnNonDelete(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := &cmdutil.TestFactory{AgentModeOverride: true}

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdVM(f, ioStreams))
	root.SetArgs([]string{"vm", "stop", "--all", "--with-volumes", "--yes"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for --with-volumes on non-delete action")
	}
	if !strings.Contains(err.Error(), "--with-volumes is only valid with the delete action") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBatchAgentModeRequiresYes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := &cmdutil.TestFactory{AgentModeOverride: true}

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdVM(f, ioStreams))
	root.SetArgs([]string{"vm", "stop", "--all"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error in agent mode without --yes")
	}
	if !strings.Contains(err.Error(), "CONFIRMATION_REQUIRED") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Part B: Instance Fetching and Filtering (Task 3)
// ---------------------------------------------------------------------------

func TestFilterByValidFrom(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "1", Hostname: "running-1", Status: verda.StatusRunning},
		{ID: "2", Hostname: "offline-1", Status: verda.StatusOffline},
		{ID: "3", Hostname: "running-2", Status: verda.StatusRunning},
		{ID: "4", Hostname: "error-1", Status: verda.StatusError},
	}

	t.Run("shutdown filters to running only", func(t *testing.T) {
		t.Parallel()
		filtered := filterByValidFrom(instances, "shutdown")
		if len(filtered) != 2 {
			t.Fatalf("expected 2 running instances, got %d", len(filtered))
		}
		for _, inst := range filtered {
			if inst.Status != verda.StatusRunning {
				t.Fatalf("expected running status, got %s", inst.Status)
			}
		}
	})

	t.Run("delete returns all instances", func(t *testing.T) {
		t.Parallel()
		filtered := filterByValidFrom(instances, "delete")
		if len(filtered) != len(instances) {
			t.Fatalf("expected %d instances (all), got %d", len(instances), len(filtered))
		}
	})

	t.Run("start filters to offline only", func(t *testing.T) {
		t.Parallel()
		filtered := filterByValidFrom(instances, "start")
		if len(filtered) != 1 {
			t.Fatalf("expected 1 offline instance, got %d", len(filtered))
		}
		if filtered[0].Status != verda.StatusOffline {
			t.Fatalf("expected offline status, got %s", filtered[0].Status)
		}
	})
}

// ---------------------------------------------------------------------------
// Part C: Confirmation Display (Task 4)
// ---------------------------------------------------------------------------

func TestFormatBatchConfirmation(t *testing.T) {
	t.Parallel()

	ip := "10.0.0.1"
	instances := []verda.Instance{
		{ID: "1", Hostname: "gpu-runner-1", Status: verda.StatusRunning, InstanceType: "1V100.6V", Location: "FIN-01", IP: &ip},
		{ID: "2", Hostname: "gpu-runner-2", Status: verda.StatusRunning, InstanceType: "1V100.6V", Location: "FIN-01"},
	}

	output := formatBatchConfirmation("shutdown", instances)

	if !strings.Contains(output, "2 instances") {
		t.Fatalf("expected instance count in output, got: %s", output)
	}
	if !strings.Contains(output, "gpu-runner-1") {
		t.Fatalf("expected hostname gpu-runner-1 in output, got: %s", output)
	}
	if !strings.Contains(output, "gpu-runner-2") {
		t.Fatalf("expected hostname gpu-runner-2 in output, got: %s", output)
	}
	if !strings.Contains(output, "10.0.0.1") {
		t.Fatalf("expected IP in output, got: %s", output)
	}
	if !strings.Contains(output, "About to shutdown") {
		t.Fatalf("expected action label in output, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// Part D: Result Display (Task 5)
// ---------------------------------------------------------------------------

func TestFormatBatchResultsAllSuccess(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "inst-1", Hostname: "host-1"},
		{ID: "inst-2", Hostname: "host-2"},
	}
	results := []verda.InstanceActionResult{
		{InstanceID: "inst-1", Status: "completed"},
		{InstanceID: "inst-2", Status: "completed"},
	}

	output := formatBatchResults("Shutdown", instances, results)

	if !strings.Contains(output, "2 instances") {
		t.Fatalf("expected '2 instances' in output, got: %s", output)
	}
	if strings.Contains(output, " of ") {
		t.Fatalf("should not contain 'of' for all-success, got: %s", output)
	}
	if !strings.Contains(output, "host-1") || !strings.Contains(output, "host-2") {
		t.Fatalf("expected hostnames in output, got: %s", output)
	}
}

func TestFormatBatchResultsPartialFailure(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "inst-1", Hostname: "host-1"},
		{ID: "inst-2", Hostname: "host-2"},
		{ID: "inst-3", Hostname: "host-3"},
	}
	results := []verda.InstanceActionResult{
		{InstanceID: "inst-1", Status: "completed"},
		{InstanceID: "inst-2", Status: "error", Error: "instance locked"},
		{InstanceID: "inst-3", Status: "completed"},
	}

	output := formatBatchResults("Shutdown", instances, results)

	// Should show "N of M" format for partial failure.
	if !strings.Contains(output, "2 of 3") {
		t.Fatalf("expected '2 of 3' in output for partial failure, got: %s", output)
	}
	if !strings.Contains(output, "instance locked") {
		t.Fatalf("expected error message in output, got: %s", output)
	}
}

func TestFormatBatchResultsNilResults(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "inst-1", Hostname: "host-1"},
		{ID: "inst-2", Hostname: "host-2"},
	}

	output := formatBatchResults("Shutdown", instances, nil)

	if !strings.Contains(output, "2 instances") {
		t.Fatalf("expected '2 instances' in output, got: %s", output)
	}
	if !strings.Contains(output, "host-1") || !strings.Contains(output, "host-2") {
		t.Fatalf("expected hostnames in output, got: %s", output)
	}
}
