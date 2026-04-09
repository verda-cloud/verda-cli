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
	root.SetArgs([]string{"vm", "shutdown", "--all", "--id", "inst-123", "--yes"})

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
	root.SetArgs([]string{"vm", "shutdown", "inst-123", "--all", "--yes"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when combining --all with positional arg")
	}
	if !strings.Contains(err.Error(), "cannot combine --all with --id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFiltersRequireAll(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := &cmdutil.TestFactory{AgentModeOverride: true}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "hostname without all",
			args: []string{"vm", "shutdown", "--hostname", "test-*"},
			want: "--hostname can only be used with --all",
		},
		{
			name: "status without all",
			args: []string{"vm", "shutdown", "--status", "running"},
			want: "--status can only be used with --all",
		},
		{
			name: "both filters without all",
			args: []string{"vm", "shutdown", "--status", "running", "--hostname", "test-*"},
			want: "--status and --hostname can only be used with --all",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
			root.AddCommand(NewCmdVM(f, ioStreams))
			root.SetArgs(tt.args)

			err := root.Execute()
			if err == nil {
				t.Fatal("expected error for filter without --all")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q, got: %v", tt.want, err)
			}
		})
	}
}

func TestBatchRejectsWithVolumesOnNonDelete(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := &cmdutil.TestFactory{AgentModeOverride: true}

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdVM(f, ioStreams))
	root.SetArgs([]string{"vm", "shutdown", "--all", "--with-volumes", "--yes"})

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
	root.SetArgs([]string{"vm", "shutdown", "--all"})

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

// ---------------------------------------------------------------------------
// Part E: Integration Tests (Task 9)
// ---------------------------------------------------------------------------

func TestShortcutCommandsHaveBatchFlags(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	vmCmd := NewCmdVM(f, ioStreams)

	// The shortcut commands to check. Some may be aliases.
	wantCommands := []string{"shutdown", "start", "hibernate", "delete"}
	for _, name := range wantCommands {
		t.Run(name, func(t *testing.T) {
			var found *cobra.Command
			for _, sub := range vmCmd.Commands() {
				if sub.Name() == name {
					found = sub
					break
				}
			}
			if found == nil {
				t.Fatalf("subcommand %q not found", name)
			}
			for _, flag := range []string{"all", "status", "with-volumes"} {
				if found.Flags().Lookup(flag) == nil {
					t.Errorf("%s missing --%s flag", name, flag)
				}
			}
		})
	}
}

func TestWithVolumesHiddenOnNonDelete(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	vmCmd := NewCmdVM(f, ioStreams)

	for _, sub := range vmCmd.Commands() {
		wvFlag := sub.Flags().Lookup("with-volumes")
		if wvFlag == nil {
			continue
		}
		isDelete := sub.Name() == "delete"
		if !isDelete && !wvFlag.Hidden {
			t.Errorf("--with-volumes should be hidden on %s", sub.Name())
		}
		if isDelete && wvFlag.Hidden {
			t.Errorf("--with-volumes should NOT be hidden on delete")
		}
	}
}

func TestFilterByHostname(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "1", Hostname: "test-worker-1"},
		{ID: "2", Hostname: "test-worker-2"},
		{ID: "3", Hostname: "prod-server-1"},
		{ID: "4", Hostname: "test-db"},
	}

	// Glob matching
	filtered := filterByHostname(instances, "test-*")
	if len(filtered) != 3 {
		t.Fatalf("expected 3 matches for 'test-*', got %d", len(filtered))
	}

	// Exact match
	exact := filterByHostname(instances, "prod-server-1")
	if len(exact) != 1 {
		t.Fatalf("expected 1 match for exact hostname, got %d", len(exact))
	}

	// No matches
	none := filterByHostname(instances, "staging-*")
	if len(none) != 0 {
		t.Fatalf("expected 0 matches for 'staging-*', got %d", len(none))
	}

	// More specific pattern
	specific := filterByHostname(instances, "test-worker-?")
	if len(specific) != 2 {
		t.Fatalf("expected 2 matches for 'test-worker-?', got %d", len(specific))
	}
}

func TestShutdownCommandExists(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	vmCmd := NewCmdVM(f, ioStreams)

	var found bool
	for _, sub := range vmCmd.Commands() {
		if sub.Name() == "shutdown" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("'shutdown' command should exist as a standalone command")
	}
}

func TestHostnameFlag(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	vmCmd := NewCmdVM(f, ioStreams)

	for _, name := range []string{"shutdown", "start", "hibernate", "delete"} {
		t.Run(name, func(t *testing.T) {
			var found *cobra.Command
			for _, sub := range vmCmd.Commands() {
				if sub.Name() == name {
					found = sub
					break
				}
			}
			if found == nil {
				t.Fatalf("subcommand %q not found", name)
			}
			if found.Flags().Lookup("hostname") == nil {
				t.Errorf("%s missing --hostname flag", name)
			}
		})
	}
}

func TestActionNameToAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"start", verda.ActionStart},
		{"shutdown", verda.ActionShutdown},
		{"hibernate", verda.ActionHibernate},
		{"delete", verda.ActionDelete},
		{"force_shutdown", verda.ActionForceShutdown},
		{"force-shutdown", verda.ActionForceShutdown},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := actionNameToAPI(tt.input)
			if got != tt.want {
				t.Errorf("actionNameToAPI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
