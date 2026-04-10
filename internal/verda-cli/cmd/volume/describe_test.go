package volume

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func strPtr(s string) *string { return &s }

func TestRenderVolumeSummary(t *testing.T) {
	t.Parallel()

	vol := &verda.Volume{
		ID:       "vol-abc",
		Name:     "data-volume",
		Size:     500,
		Type:     "NVMe",
		Status:   "attached",
		Location: "FIN-01",
	}

	var buf bytes.Buffer
	renderVolumeSummary(&buf, vol)

	out := buf.String()
	if len(out) == 0 {
		t.Fatal("renderVolumeSummary produced empty output")
	}
}

func TestRenderVolumeSummaryWithInstance(t *testing.T) {
	t.Parallel()

	vol := &verda.Volume{
		ID:         "vol-def",
		Name:       "os-vol",
		Size:       100,
		Type:       "NVMe",
		Status:     "attached",
		Location:   "FIN-01",
		IsOSVolume: true,
		InstanceID: strPtr("inst-123"),
	}

	var buf bytes.Buffer
	renderVolumeSummary(&buf, vol)

	out := buf.String()
	if len(out) == 0 {
		t.Fatal("renderVolumeSummary produced empty output")
	}
}

func TestRenderVolumeSummaryDetached(t *testing.T) {
	t.Parallel()

	vol := &verda.Volume{
		ID:       "vol-ghi",
		Name:     "spare-vol",
		Size:     200,
		Type:     "HDD",
		Status:   "detached",
		Location: "FIN-03",
	}

	var buf bytes.Buffer
	renderVolumeSummary(&buf, vol)

	if buf.Len() == 0 {
		t.Fatal("renderVolumeSummary produced empty output for detached volume")
	}
}

// TestVolumeDescribeAcceptsZeroArgs verifies that verda volume describe can be
// called without arguments (for the interactive picker flow).
func TestVolumeDescribeAcceptsZeroArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	volCmd := &cobra.Command{Use: "volume"}
	volCmd.AddCommand(NewCmdDescribe(f, ioStreams))
	root.AddCommand(volCmd)
	root.SetArgs([]string{"volume", "describe"})

	err := root.Execute()
	if err != nil && err.Error() == "accepts at most 1 arg(s), received 0" {
		t.Fatal("volume describe should accept zero arguments for interactive mode")
	}
}

// TestVolumeDescribeAcceptsOneArg verifies that verda volume describe <id> works.
func TestVolumeDescribeAcceptsOneArg(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	volCmd := &cobra.Command{Use: "volume"}
	volCmd.AddCommand(NewCmdDescribe(f, ioStreams))
	root.AddCommand(volCmd)
	root.SetArgs([]string{"volume", "describe", "vol-123"})

	err := root.Execute()
	if err != nil && err.Error() == "accepts at most 1 arg(s), received 2" {
		t.Fatal("volume describe should accept one argument")
	}
}

// TestVolumeDescribeRejectsTwoArgs ensures extra positional args are rejected.
func TestVolumeDescribeRejectsTwoArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &buf, ErrOut: &buf}
	f := cmdutil.NewTestFactory(nil)

	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	volCmd := &cobra.Command{Use: "volume"}
	volCmd.AddCommand(NewCmdDescribe(f, ioStreams))
	root.AddCommand(volCmd)
	root.SetArgs([]string{"volume", "describe", "id1", "id2"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for two positional args")
	}
}
