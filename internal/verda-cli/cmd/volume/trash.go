package volume

import (
	"context"
	"fmt"
	"time"

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
	volumes, err := client.Volumes.GetVolumesInTrash(ctx)
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
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	_, _ = fmt.Fprintf(ioStreams.Out, "  %d volume(s) in trash\n\n", len(volumes))

	for _, v := range volumes {
		volType := "Block"
		if v.IsOSVolume {
			volType = "OS"
		}

		permStatus := ""
		if v.IsPermanentlyDeleted {
			permStatus = warnStyle.Render("  (permanently deleted)")
		}

		_, _ = fmt.Fprintf(ioStreams.Out, "  %s  %s%s\n", bold.Render(v.Name), dim.Render(volType), permStatus)
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %s\n", dim.Render("ID:      "), v.ID)
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %dGB %s\n", dim.Render("Size:    "), v.Size, v.Type)
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %s\n", dim.Render("Location:"), v.Location)
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %s\n", dim.Render("Contract:"), v.Contract)
		if v.MonthlyPrice > 0 {
			_, _ = fmt.Fprintf(ioStreams.Out, "    %s  $%.2f/mo (%s)\n", dim.Render("Price:   "), v.MonthlyPrice, v.Currency)
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %s\n", dim.Render("Deleted: "), v.DeletedAt.Format("2 Jan 2006, 15:04"))

		if !v.IsPermanentlyDeleted && !v.DeletedAt.IsZero() {
			expiresAt := v.DeletedAt.Add(96 * time.Hour)
			remaining := time.Until(expiresAt)
			if remaining > 0 {
				_, _ = fmt.Fprintf(ioStreams.Out, "    %s  %s\n", dim.Render("Expires: "), fmt.Sprintf("%s (%s remaining)", expiresAt.Format("2 Jan 2006, 15:04"), formatDuration(remaining)))
			}
		}
		_, _ = fmt.Fprintln(ioStreams.Out)
	}

	return nil
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	if h >= 24 {
		return fmt.Sprintf("%dd %dh", h/24, h%24)
	}
	return fmt.Sprintf("%dh", h)
}
