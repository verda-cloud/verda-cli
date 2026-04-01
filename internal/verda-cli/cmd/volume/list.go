package volume

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type listOptions struct {
	Status string
}

// NewCmdList creates the volume list cobra command.
func NewCmdList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List volumes",
		Long: cmdutil.LongDesc(`
			List all block storage volumes, showing Name, ID, Size, Type,
			Status, and Location. Optionally filter by status.
		`),
		Example: cmdutil.Examples(`
			verda volume list
			verda vol ls
			verda volume list --status attached
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, f, ioStreams, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Status, "status", "", "Filter by volume status (e.g., attached, detached, ordered)")

	return cmd
}

func runList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *listOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading volumes...")
	}

	var volumes []verda.Volume
	if opts.Status != "" {
		volumes, err = client.Volumes.ListVolumesByStatus(ctx, opts.Status)
	} else {
		volumes, err = client.Volumes.ListVolumes(ctx)
	}
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	if len(volumes) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No volumes found.")
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %d volume(s) found\n\n", len(volumes))
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-36s  %6s  %-10s  %-10s  %s\n", "NAME", "ID", "SIZE", "TYPE", "STATUS", "LOCATION")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-36s  %6s  %-10s  %-10s  %s\n", "----", "--", "----", "----", "------", "--------")
	for _, v := range volumes {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-36s  %4dGB  %-10s  %-10s  %s\n",
			v.Name, v.ID, v.Size, v.Type, v.Status, v.Location)
	}
	return nil
}
