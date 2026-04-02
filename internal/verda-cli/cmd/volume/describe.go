package volume

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdDescribe creates the volume describe cobra command.
func NewCmdDescribe(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "describe <volume-id>",
		Aliases: []string{"get", "show"},
		Short:   "Show detailed information about a volume",
		Long: cmdutil.LongDesc(`
			Display detailed information about a single volume,
			including size, type, status, and attached instance.
		`),
		Example: cmdutil.Examples(`
			verda volume describe abc-123-def
			verda vol show abc-123-def
			verda volume describe abc-123-def -o json
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDescribe(cmd, f, ioStreams, args[0])
		},
	}
	return cmd
}

func runDescribe(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, volumeID string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading volume...")
	}
	vol, err := client.Volumes.GetVolume(ctx, volumeID)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return fmt.Errorf("fetching volume: %w", err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Volume details:", vol)

	// Structured output.
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), vol); wrote {
		return err
	}

	// Human-readable summary.
	renderVolumeSummary(ioStreams.Out, vol)
	return nil
}
