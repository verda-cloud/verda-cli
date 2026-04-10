package locations

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdLocations creates the locations command.
func NewCmdLocations(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:     "locations",
		Aliases: []string{"location", "loc"},
		Short:   "List available datacenter locations",
		Long: cmdutil.LongDesc(`
			List all available Verda Cloud datacenter locations.
		`),
		Example: cmdutil.Examples(`
			verda locations
			verda locations -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLocations(cmd, f, ioStreams)
		},
	}
}

func runLocations(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading locations...")
	}
	locations, err := client.Locations.Get(ctx)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d location(s):", len(locations)), locations)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), locations); wrote {
		return err
	}

	if len(locations) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No locations found.")
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %-10s  %-20s  %s\n", "CODE", "NAME", "COUNTRY")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-10s  %-20s  %s\n", "----", "----", "-------")
	for _, loc := range locations {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-10s  %-20s  %s\n", loc.Code, loc.Name, loc.CountryCode)
	}
	return nil
}
