package vm

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// instanceAction defines a supported action with its display label and executor.
type instanceAction struct {
	Label   string
	Danger  bool // requires explicit confirmation
	Execute func(ctx context.Context, client *verda.Client, inst *verda.Instance) error
}

var actions = []instanceAction{
	{Label: "Start", Execute: func(ctx context.Context, c *verda.Client, inst *verda.Instance) error {
		return c.Instances.Start(ctx, inst.ID)
	}},
	{Label: "Shutdown", Execute: func(ctx context.Context, c *verda.Client, inst *verda.Instance) error {
		return c.Instances.Shutdown(ctx, inst.ID)
	}},
	{Label: "Force shutdown", Execute: func(ctx context.Context, c *verda.Client, inst *verda.Instance) error {
		return c.Instances.ForceShutdown(ctx, inst.ID)
	}},
	{Label: "Hibernate", Execute: func(ctx context.Context, c *verda.Client, inst *verda.Instance) error {
		return c.Instances.Hibernate(ctx, inst.ID)
	}},
	{Label: "Delete instance", Danger: true, Execute: func(ctx context.Context, c *verda.Client, inst *verda.Instance) error {
		return c.Instances.Delete(ctx, []string{inst.ID}, nil, false)
	}},
}

// NewCmdAction creates the vm action cobra command.
func NewCmdAction(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var instanceID string

	cmd := &cobra.Command{
		Use:     "action",
		Aliases: []string{"delete", "rm", "start", "stop", "shutdown"},
		Short:   "Perform actions on a VM instance",
		Long: cmdutil.LongDesc(`
			Select a VM instance and perform an action: start, shutdown,
			force shutdown, hibernate, or delete.
		`),
		Example: cmdutil.Examples(`
			# Interactive: select instance then action
			verda vm action

			# Specify instance ID
			verda vm action --id abc-123
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAction(cmd, f, ioStreams, instanceID)
		},
	}

	cmd.Flags().StringVar(&instanceID, "id", "", "Instance ID to act on")

	return cmd
}

func runAction(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, instanceID string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	prompter := f.Prompter()
	ctx := cmd.Context()

	// Select instance if not provided.
	if instanceID == "" {
		selected, err := selectInstance(ctx, f, ioStreams, client)
		if err != nil {
			return err
		}
		if selected == "" {
			return nil
		}
		instanceID = selected
	}

	// Fetch instance details.
	inst, err := client.Instances.GetByID(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("fetching instance: %w", err)
	}

	// Show instance summary.
	_, _ = fmt.Fprint(ioStreams.Out, renderInstanceCard(inst))

	// Select action.
	actionLabels := make([]string, len(actions))
	for i, a := range actions {
		actionLabels[i] = a.Label
	}
	actionLabels = append(actionLabels, "Cancel")

	actionIdx, err := prompter.Select(ctx, "Select action", actionLabels)
	if err != nil {
		return nil //nolint:nilerr
	}
	if actionIdx == len(actions) { // Cancel
		return nil
	}

	action := actions[actionIdx]

	// Confirm dangerous actions.
	if action.Danger {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  WARNING: %s on %s (%s)\n\n",
			action.Label, inst.Hostname, inst.ID)
		confirmed, err := prompter.Confirm(ctx, "Are you sure?")
		if err != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Cancelled.")
			return nil
		}
	}

	// Execute action with spinner.
	actionCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(actionCtx, fmt.Sprintf("%s %s...", action.Label, inst.Hostname))
	}
	err = action.Execute(actionCtx, client, inst)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Done: %s on %s (%s)\n", action.Label, inst.Hostname, inst.ID)
	return nil
}

func selectInstance(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client) (string, error) {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading instances...")
	}
	instances, err := client.Instances.Get(ctx, "")
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return "", err
	}

	if len(instances) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No instances found.")
		return "", nil
	}

	labels := make([]string, len(instances))
	for i, inst := range instances {
		labels[i] = formatInstanceRow(inst)
	}
	labels = append(labels, "Cancel")

	idx, err := f.Prompter().Select(ctx, "Select instance (↑/↓ move, type to filter)", labels)
	if err != nil {
		return "", nil //nolint:nilerr
	}
	if idx == len(instances) {
		return "", nil
	}

	return instances[idx].ID, nil
}
