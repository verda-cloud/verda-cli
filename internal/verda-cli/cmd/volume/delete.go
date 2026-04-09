package volume

import (
	"context"
	"errors"
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type deleteOptions struct {
	VolumeID string
	All      bool
	Status   string
	Yes      bool
}

// NewCmdDelete creates the volume delete shortcut command.
func NewCmdDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &deleteOptions{}

	cmd := &cobra.Command{
		Use:     "delete [volume-id]",
		Aliases: []string{"rm"},
		Short:   "Delete volumes",
		Long: cmdutil.LongDesc(`
			Delete one or more volumes. Deleted storage can be restored
			within 96 hours via the trash command.

			Without arguments, an interactive multi-select picker is shown.
			With a positional argument or --id, a single volume is deleted.
			Use --all to target all volumes matching optional --status filter.
		`),
		Example: cmdutil.Examples(`
			# Delete a specific volume
			verda volume delete vol-abc-123

			# Interactive: select from list
			verda volume delete

			# Batch: delete all detached volumes
			verda volume delete --all --status detached --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.VolumeID = args[0]
			}
			return runDelete(cmd, f, ioStreams, opts)
		},
	}

	cmd.Flags().StringVar(&opts.VolumeID, "id", "", "Volume ID (alternative to positional argument)")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Target all volumes (use with --status to filter)")
	cmd.Flags().StringVar(&opts.Status, "status", "", "Filter by status, requires --all (e.g., detached, attached)")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip confirmation for destructive actions")

	return cmd
}

func runDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *deleteOptions) error {
	// Validate: --status is a filter that requires --all.
	if !opts.All && opts.Status != "" {
		return errors.New("--status can only be used with --all")
	}

	// Validate: --all cannot combine with --id or positional arg.
	if opts.All && opts.VolumeID != "" {
		return errors.New("cannot combine --all with --id or positional volume ID")
	}

	// Agent mode: --all requires --yes.
	if opts.All && f.AgentMode() && !opts.Yes {
		return cmdutil.NewConfirmationRequiredError("delete --all")
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	// Batch mode: --all
	if opts.All {
		return runBatchVolumeDelete(ctx, f, ioStreams, client, opts)
	}

	// Single volume by ID
	if opts.VolumeID != "" {
		return runSingleVolumeDelete(ctx, f, ioStreams, client, opts.VolumeID, opts.Yes)
	}

	// Interactive: multi-select picker
	return runInteractiveVolumeDelete(ctx, f, ioStreams, client, opts)
}

func runSingleVolumeDelete(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, volumeID string, skipConfirm bool) error {
	vol, err := client.Volumes.GetVolume(ctx, volumeID)
	if err != nil {
		return fmt.Errorf("fetching volume: %w", err)
	}

	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)

	if !skipConfirm && !f.AgentMode() {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  Deleted storage can be restored within 96 hours.\n")
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n\n", warnStyle.Render("This action cannot be undone after the recovery period."))

		confirmed, confirmErr := f.Prompter().Confirm(ctx, fmt.Sprintf("Delete %s (%dGB %s)?", vol.Name, vol.Size, vol.Type))
		if confirmErr != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete volume:", map[string]string{
		"volume_id": vol.ID,
		"name":      vol.Name,
	})

	deleteCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(deleteCtx, fmt.Sprintf("Deleting %s...", vol.Name))
	}
	err = client.Volumes.DeleteVolume(deleteCtx, vol.ID, false) // soft delete
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted: %s (%s)\n", vol.Name, vol.ID)
	return nil
}

func runInteractiveVolumeDelete(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, opts *deleteOptions) error {
	// Load all volumes (--status filter is only valid with --all, validated earlier).
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading volumes...")
	}

	volumes, err := client.Volumes.ListVolumes(ctx)
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

	labels := make([]string, len(volumes))
	for i := range volumes {
		status := volumes[i].Status
		if volumes[i].IsOSVolume {
			status = "OS"
		}
		labels[i] = fmt.Sprintf("%-25s  %-10s  %5dGB  %-6s  %s", volumes[i].Name, status, volumes[i].Size, volumes[i].Type, volumes[i].Location)
	}

	indices, err := f.Prompter().MultiSelect(ctx, "Select volumes to delete", labels)
	if err != nil {
		return nil //nolint:nilerr // User pressed Esc/Ctrl+C.
	}
	if len(indices) == 0 {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "No volumes selected.")
		return nil
	}

	selected := make([]verda.Volume, len(indices))
	for i, idx := range indices {
		selected[i] = volumes[idx]
	}

	// Single selection -> single delete flow
	if len(selected) == 1 {
		return runSingleVolumeDelete(ctx, f, ioStreams, client, selected[0].ID, opts.Yes)
	}

	// Multiple -> batch delete
	return executeBatchVolumeDelete(ctx, f, ioStreams, client, selected, opts.Yes)
}

func runBatchVolumeDelete(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, opts *deleteOptions) error {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading volumes...")
	}

	var volumes []verda.Volume
	var err error
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
		_, _ = fmt.Fprintln(ioStreams.Out, "No matching volumes found.")
		return nil
	}

	return executeBatchVolumeDelete(ctx, f, ioStreams, client, volumes, opts.Yes)
}

func executeBatchVolumeDelete(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, volumes []verda.Volume, skipConfirm bool) error {
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	// Show confirmation.
	if !skipConfirm && !f.AgentMode() {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  About to delete %d volumes:\n", len(volumes))
		for i := range volumes {
			v := &volumes[i]
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "    %-25s  %5dGB  %-6s  %s\n", v.Name, v.Size, v.Type, v.Location)
		}

		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  Deleted storage can be restored within 96 hours.\n")
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n\n", warnStyle.Render("This action cannot be undone after the recovery period."))

		confirmed, confirmErr := f.Prompter().Confirm(ctx, fmt.Sprintf("Delete %d volumes?", len(volumes)))
		if confirmErr != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil //nolint:nilerr // User pressed Esc/Ctrl+C during prompt.
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("Batch delete %d volumes:", len(volumes)), volumes)

	// Delete each volume individually (no batch API).
	var succeeded, failed int
	type result struct {
		Name    string `json:"name"`
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(volumes))

	for i := range volumes {
		v := &volumes[i]
		deleteCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
		err := client.Volumes.DeleteVolume(deleteCtx, v.ID, false) // soft delete
		cancel()

		if err != nil {
			failed++
			results = append(results, result{Name: v.Name, Success: false, Error: err.Error()})
		} else {
			succeeded++
			results = append(results, result{Name: v.Name, Success: true})
		}
	}

	// Display results.
	if f.AgentMode() {
		output := map[string]any{
			"action":    "delete",
			"total":     len(results),
			"succeeded": succeeded,
			"failed":    failed,
			"results":   results,
		}
		_, _ = cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), output)
		return nil
	}

	if failed > 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "Deleted %d of %d volumes:\n", succeeded, len(results))
	} else {
		_, _ = fmt.Fprintf(ioStreams.Out, "Deleted %d volumes:\n", len(results))
	}

	for _, r := range results {
		if r.Success {
			_, _ = fmt.Fprintf(ioStreams.Out, "  %s %s\n", greenStyle.Render("\u2713"), r.Name)
		} else {
			_, _ = fmt.Fprintf(ioStreams.Out, "  %s %s  %s\n", redStyle.Render("\u2717"), r.Name, r.Error)
		}
	}
	return nil
}
