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

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d instance(s):", len(instances)), instances)

	// Structured output: emit JSON/YAML and return.
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), instances); wrote {
		return err
	}

	if len(instances) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No instances found.")
		return nil
	}

	// Non-interactive table when piped or redirected.
	if !cmdutil.IsStdoutTerminal() {
		return printInstanceTable(ioStreams, instances)
	}

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %d instance(s) found\n\n", len(instances))

	prompter := f.Prompter()
	labels := make([]string, 0, len(instances)+1)
	for i := range instances {
		labels = append(labels, formatInstanceRow(&instances[i]))
	}
	labels = append(labels, "Exit")

	for {
		idx, err := prompter.Select(cmd.Context(), "Select instance (type to filter)", labels)
		if err != nil {
			return nil
		}
		if idx == len(instances) { // "Exit"
			return nil
		}

		// Fetch fresh details and volumes.
		inst, err := client.Instances.GetByID(cmd.Context(), instances[idx].ID)
		if err != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "Error: %v\n", err)
			continue
		}
		volumes := fetchInstanceVolumes(cmd.Context(), client, inst)
		_, _ = fmt.Fprint(ioStreams.Out, renderInstanceCard(inst, volumes...))

		// After showing details, loop back to the list.
	}
}

// fetchInstanceVolumes fetches volume details for an instance's attached volumes.
func fetchInstanceVolumes(ctx context.Context, client *verda.Client, inst *verda.Instance) []verda.Volume {
	seen := make(map[string]bool)
	var ids []string

	// OS volume first.
	if inst.OSVolumeID != nil && *inst.OSVolumeID != "" {
		ids = append(ids, *inst.OSVolumeID)
		seen[*inst.OSVolumeID] = true
	}
	// Then data volumes, deduplicating.
	for _, id := range inst.VolumeIDs {
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}

	volumes := make([]verda.Volume, 0, len(ids))
	for _, id := range ids {
		vol, err := client.Volumes.GetVolume(ctx, id)
		if err != nil {
			continue
		}
		volumes = append(volumes, *vol)
	}
	return volumes
}

func printInstanceTable(ioStreams cmdutil.IOStreams, instances []verda.Instance) error {
	_, _ = fmt.Fprintf(ioStreams.Out, "  %d instance(s) found\n\n", len(instances))
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-13s  %-18s  %-8s  %s\n", "HOSTNAME", "STATUS", "TYPE", "LOCATION", "IP")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-13s  %-18s  %-8s  %s\n", "--------", "------", "----", "--------", "--")
	for i := range instances {
		ip := ""
		if instances[i].IP != nil && *instances[i].IP != "" {
			ip = *instances[i].IP
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-13s  %-18s  %-8s  %s\n",
			instances[i].Hostname,
			instances[i].Status,
			instances[i].InstanceType,
			instances[i].Location,
			ip)
	}
	return nil
}

func formatInstanceRow(inst *verda.Instance) string {
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
