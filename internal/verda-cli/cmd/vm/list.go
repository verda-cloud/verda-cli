package vm

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type listOptions struct {
	Status string
	JSON   bool
}

// NewCmdList creates the vm list cobra command.
func NewCmdList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List VM instances",
		Long: cmdutil.LongDesc(`
			List all Verda VM instances. Select an instance to view details.
		`),
		Example: cmdutil.Examples(`
			verda vm list
			verda vm ls
			verda vm list --status running
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Status, "status", "", "Filter by status (e.g., running, offline, provisioning)")

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
		sp, _ = status.Spinner(ctx, "Loading instances...")
	}
	instances, err := client.Instances.Get(ctx, opts.Status)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	if len(instances) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No instances found.")
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %d instance(s) found\n\n", len(instances))

	prompter := f.Prompter()
	labels := make([]string, len(instances))
	for i, inst := range instances {
		labels[i] = formatInstanceRow(inst)
	}
	labels = append(labels, "Exit")

	for {
		idx, err := prompter.Select(cmd.Context(), "Select instance for details (↑/↓ move, type to filter, Esc to exit)", labels)
		if err != nil {
			return nil //nolint:nilerr // User pressed Esc/Ctrl+C.
		}
		if idx == len(instances) { // "Exit"
			return nil
		}

		// Fetch fresh details.
		inst, err := client.Instances.GetByID(cmd.Context(), instances[idx].ID)
		if err != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "Error: %v\n", err)
			continue
		}
		_, _ = fmt.Fprint(ioStreams.Out, renderInstanceCard(inst))

		// After showing details, loop back to the list.
	}
}

func formatInstanceRow(inst verda.Instance) string {
	ip := ""
	if inst.IP != nil && *inst.IP != "" {
		ip = "  " + *inst.IP
	}

	return fmt.Sprintf("%-20s  ● %-13s  %-18s  %s%s",
		inst.Hostname,
		inst.Status,
		inst.InstanceType,
		inst.Location,
		ip)
}


