package volume

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdTrash creates the volume trash command.
func NewCmdTrash(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "trash",
		Short: "List volumes in trash",
		Long: cmdutil.LongDesc(`
			List deleted volumes that can still be restored (within 96 hours).
		`),
		Example: cmdutil.Examples(`
			verda volume trash
			verda vol trash
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrash(cmd, f, ioStreams)
		},
	}
}

func runTrash(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading trash...")
	}
	volumes, err := client.Volumes.ListVolumesByStatus(ctx, "deleted")
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	if len(volumes) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "Trash is empty.")
		return nil
	}

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold := lipgloss.NewStyle().Bold(true)

	_, _ = fmt.Fprintf(ioStreams.Out, "  %d volume(s) in trash\n\n", len(volumes))

	for _, v := range volumes {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %s\n", bold.Render(v.Name))
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %s\n", dim.Render("ID:      "), v.ID)
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %dGB\n", dim.Render("Size:    "), v.Size)
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %s\n", dim.Render("Type:    "), v.Type)
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %s\n", dim.Render("Location:"), v.Location)
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %s\n\n", dim.Render("Deleted: "), v.CreatedAt.Format("2 Jan 2006, 15:04"))
	}

	return nil
}
