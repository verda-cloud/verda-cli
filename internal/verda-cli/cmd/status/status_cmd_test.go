package status

import (
	"bytes"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestNewCmdStatusHasCorrectUse(t *testing.T) {
	t.Parallel()

	f := cmdutil.NewTestFactory(nil)
	ioStreams := cmdutil.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}

	cmd := NewCmdStatus(f, ioStreams)

	if cmd.Use != "status" {
		t.Fatalf("expected Use 'status', got %q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Fatal("expected Short description to be non-empty")
	}
}
