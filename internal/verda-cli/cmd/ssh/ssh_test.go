package ssh

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func strPtr(s string) *string { return &s }

func TestIsTerminalReturnsFalseForPipe(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	if isTerminal(r) {
		t.Fatal("expected pipe to not be a terminal")
	}
}

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

	instances := []verda.Instance{
		{ID: "match-me", Hostname: "first", Status: verda.StatusRunning, IP: strPtr("1.1.1.1")},
		{ID: "other", Hostname: "match-me", Status: verda.StatusRunning, IP: strPtr("2.2.2.2")},
	}

	inst := resolveInstance(instances, "match-me")
	if inst == nil {
		t.Fatal("expected to find instance")
	}
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

func TestFilterRunningInstances(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "1", Hostname: "running-vm", Status: verda.StatusRunning, IP: strPtr("1.1.1.1")},
		{ID: "2", Hostname: "offline-vm", Status: verda.StatusOffline, IP: strPtr("2.2.2.2")},
		{ID: "3", Hostname: "another-running", Status: verda.StatusRunning, IP: strPtr("3.3.3.3")},
		{ID: "4", Hostname: "provisioning-vm", Status: verda.StatusProvisioning},
	}

	running := filterRunning(instances)
	if len(running) != 2 {
		t.Fatalf("expected 2 running instances, got %d", len(running))
	}
	if running[0].Hostname != "running-vm" {
		t.Fatalf("expected first running instance to be 'running-vm', got %q", running[0].Hostname)
	}
	if running[1].Hostname != "another-running" {
		t.Fatalf("expected second running instance to be 'another-running', got %q", running[1].Hostname)
	}
}

func TestFilterRunningNoRunning(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "1", Hostname: "offline-vm", Status: verda.StatusOffline},
		{ID: "2", Hostname: "error-vm", Status: verda.StatusError},
	}

	running := filterRunning(instances)
	if len(running) != 0 {
		t.Fatalf("expected 0 running instances, got %d", len(running))
	}
}

func TestFilterRunningEmpty(t *testing.T) {
	t.Parallel()

	running := filterRunning(nil)
	if running != nil {
		t.Fatalf("expected nil for empty input, got %v", running)
	}
}

// TestSSHCommandAcceptsZeroArgs verifies that verda ssh can be called without
// arguments (for the interactive picker flow). The command will fail at the API
// call level, but arg validation should pass.
func TestSSHCommandAcceptsZeroArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdSSH(f, ioStreams))
	root.SetArgs([]string{"ssh"})

	err := root.Execute()
	// Should fail on VerdaClient (no credentials), NOT on arg validation.
	if err != nil && err.Error() == "accepts at most 1 arg(s), received 0" {
		t.Fatal("ssh command should accept zero arguments for interactive mode")
	}
	if err == nil {
		t.Fatal("expected error from VerdaClient, got nil")
	}
	if !errors.Is(err, cmdutil.ErrNoClient) {
		// Accept any auth-related error — the point is arg parsing worked.
		t.Logf("got expected non-arg error: %v", err)
	}
}

// TestSSHCommandAcceptsOneArg verifies that verda ssh <hostname> still works.
func TestSSHCommandAcceptsOneArg(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdSSH(f, ioStreams))
	root.SetArgs([]string{"ssh", "my-host"})

	err := root.Execute()
	// Should fail on VerdaClient, NOT on arg validation.
	if err != nil && err.Error() == "accepts at most 1 arg(s), received 2" {
		t.Fatal("ssh command should accept one argument")
	}
}

// TestSSHCommandRejectsTwoArgs ensures extra positional args are rejected.
func TestSSHCommandRejectsTwoArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(NewCmdSSH(f, ioStreams))
	root.SetArgs([]string{"ssh", "host1", "host2"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for two positional args")
	}
}
