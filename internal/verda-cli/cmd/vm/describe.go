package vm

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdDescribe creates the vm describe cobra command.
func NewCmdDescribe(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "describe <instance-id>",
		Aliases: []string{"get", "show"},
		Short:   "Show detailed information about a VM instance",
		Long: cmdutil.LongDesc(`
			Display detailed information about a single VM instance,
			including compute specs, networking, and attached volumes.
		`),
		Example: cmdutil.Examples(`
			verda vm describe abc-123-def
			verda vm show abc-123-def
			verda vm describe abc-123-def -o json
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDescribe(cmd, f, ioStreams, args[0])
		},
	}
	return cmd
}

func runDescribe(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, instanceID string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading instance...")
	}
	inst, err := client.Instances.GetByID(ctx, instanceID)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return fmt.Errorf("fetching instance: %w", err)
	}

	volumes := fetchInstanceVolumes(ctx, client, inst)

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Instance details:", inst)

	// Structured output.
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), inst); wrote {
		return err
	}

	// Human-readable card.
	_, _ = fmt.Fprint(ioStreams.Out, renderInstanceCard(inst, volumes...))
	return nil
}
