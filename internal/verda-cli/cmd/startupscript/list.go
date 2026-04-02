package startupscript

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdList creates the startup-script list cobra command.
func NewCmdList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List startup scripts",
		Long: cmdutil.LongDesc(`
			List all startup scripts in your account, showing Name, ID, and CreatedAt.
		`),
		Example: cmdutil.Examples(`
			verda startup-script list
			verda script ls
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, f, ioStreams)
		},
	}
	return cmd
}

func runList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading startup scripts...")
	}
	scripts, err := client.StartupScripts.GetAllStartupScripts(ctx)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d startup script(s):", len(scripts)), scripts)

	if len(scripts) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No startup scripts found.")
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %d startup script(s) found\n\n", len(scripts))
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-36s  %s\n", "NAME", "ID", "CREATED")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-36s  %s\n", "----", "--", "-------")
	for _, s := range scripts {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-36s  %s\n", s.Name, s.ID, s.CreatedAt.Format("2006-01-02 15:04"))
	}
	return nil
}
