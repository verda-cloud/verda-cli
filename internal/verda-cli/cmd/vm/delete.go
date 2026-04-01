package vm

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type deleteOptions struct {
	IDs              []string
	DeletePermanently bool
	Force            bool
}

// NewCmdDelete creates the vm delete cobra command.
func NewCmdDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &deleteOptions{}

	cmd := &cobra.Command{
		Use:     "delete",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete VM instances",
		Long: cmdutil.LongDesc(`
			Delete one or more Verda VM instances. If no --id is provided,
			an interactive list lets you select the instance to delete.
		`),
		Example: cmdutil.Examples(`
			# Interactive: select from list
			verda vm delete

			# By ID
			verda vm delete --id abc-123

			# Multiple IDs, skip confirmation
			verda vm delete --id abc-123 --id def-456 --force

			# Permanently delete (don't move to trash)
			verda vm delete --id abc-123 --permanent
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringSliceVar(&opts.IDs, "id", nil, "Instance ID to delete; repeat for multiple")
	flags.BoolVar(&opts.DeletePermanently, "permanent", false, "Delete permanently instead of moving to trash")
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

	// If no IDs given, show interactive selection.
	if len(opts.IDs) == 0 {
		selected, err := selectInstances(ctx, f, ioStreams, client)
		if err != nil {
			return err
		}
		if len(selected) == 0 {
			return nil
		}
		opts.IDs = selected
	}

	// Fetch instance details for confirmation display.
	var instances []*verda.Instance
	for _, id := range opts.IDs {
		inst, err := client.Instances.GetByID(ctx, id)
		if err != nil {
			return fmt.Errorf("fetching instance %s: %w", id, err)
		}
		instances = append(instances, inst)
	}

	// Confirm deletion.
	if !opts.Force {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "\nThe following instance(s) will be deleted:")
		for _, inst := range instances {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  - %s (%s, %s)\n", inst.Hostname, inst.ID, inst.Status)
		}
		if opts.DeletePermanently {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "\n  WARNING: This will permanently delete the instance(s).")
		}
		_, _ = fmt.Fprintln(ioStreams.ErrOut)

		confirmed, err := prompter.Confirm(ctx, "Are you sure?")
		if err != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Cancelled.")
			return nil
		}
	}

	// Delete.
	deleteCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(deleteCtx, "Deleting instance(s)...")
	}
	err = client.Instances.Delete(deleteCtx, opts.IDs, nil, opts.DeletePermanently)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	for _, inst := range instances {
		_, _ = fmt.Fprintf(ioStreams.Out, "Deleted: %s (%s)\n", inst.Hostname, inst.ID)
	}
	return nil
}

func selectInstances(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client) ([]string, error) {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading instances...")
	}
	instances, err := client.Instances.Get(ctx, "")
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return nil, err
	}

	if len(instances) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No instances found.")
		return nil, nil
	}

	labels := make([]string, len(instances))
	for i, inst := range instances {
		labels[i] = formatInstanceRow(inst)
	}
	labels = append(labels, "Cancel")

	idx, err := f.Prompter().Select(ctx, "Select instance to delete (↑/↓ move, type to filter)", labels)
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	if idx == len(instances) { // "Cancel"
		return nil, nil
	}

	return []string{instances[idx].ID}, nil
}
