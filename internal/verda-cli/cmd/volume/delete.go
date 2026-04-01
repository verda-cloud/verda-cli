package volume

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type deleteOptions struct {
	ID    string
	Force bool
}

// NewCmdDelete creates the volume delete cobra command.
func NewCmdDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &deleteOptions{}

	cmd := &cobra.Command{
		Use:     "delete",
		Aliases: []string{"rm"},
		Short:   "Delete a volume",
		Long: cmdutil.LongDesc(`
			Delete a block storage volume. In interactive mode you will be
			prompted to select a volume and confirm deletion. Use --id for
			non-interactive use. Use --force to skip confirmation.
		`),
		Example: cmdutil.Examples(`
			# Interactive
			verda volume delete

			# Non-interactive
			verda volume delete --id abc-123

			# Skip confirmation
			verda volume delete --id abc-123 --force
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.ID, "id", "", "Volume ID to delete")
	flags.BoolVar(&opts.Force, "force", false, "Skip confirmation prompt")

	return cmd
}

func runDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *deleteOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	prompter := f.Prompter()
	ctx := cmd.Context()

	volumeID := opts.ID
	volumeName := volumeID

	if volumeID == "" {
		// Interactive: list volumes and let user select.
		listCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
		defer cancel()

		var sp interface{ Stop(string) }
		if status := f.Status(); status != nil {
			sp, _ = status.Spinner(listCtx, "Loading volumes...")
		}
		volumes, err := client.Volumes.ListVolumes(listCtx)
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
		for i, v := range volumes {
			labels[i] = fmt.Sprintf("%-20s  %s  %dGB  %s  %s", v.Name, v.ID, v.Size, v.Type, v.Status)
		}
		labels = append(labels, "Cancel")

		idx, err := prompter.Select(ctx, "Select volume to delete", labels)
		if err != nil {
			return nil //nolint:nilerr
		}
		if idx == len(volumes) {
			return nil
		}

		volumeID = volumes[idx].ID
		volumeName = volumes[idx].Name
	}

	// Confirm deletion unless --force is set.
	if !opts.Force {
		confirmed, err := prompter.Confirm(ctx, fmt.Sprintf("Are you sure you want to delete volume %q?", volumeName))
		if err != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Cancelled.")
			return nil
		}
	}

	deleteCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp2 interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp2, _ = status.Spinner(deleteCtx, "Deleting volume...")
	}
	err = client.Volumes.DeleteVolume(deleteCtx, volumeID, opts.Force)
	if sp2 != nil {
		sp2.Stop("")
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted volume: %s (%s)\n", volumeName, volumeID)
	return nil
}
