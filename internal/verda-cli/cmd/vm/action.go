package vm

import (
	"context"
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// instanceAction defines a supported action with its display label and executor.
type instanceAction struct {
	Label        string
	ConfirmMsg   string   // shown before confirmation prompt; empty = no confirm needed
	ValidFrom    []string // instance statuses where this action is available; empty = always
	ExpectStatus string   // poll until this status; empty = no polling
	Execute      func(ctx context.Context, client *verda.Client, inst *verda.Instance) error
}

var allActions = []instanceAction{
	{
		Label:        "Start",
		ValidFrom:    []string{verda.StatusOffline},
		ExpectStatus: verda.StatusRunning,
		Execute:      func(ctx context.Context, c *verda.Client, inst *verda.Instance) error { return c.Instances.Start(ctx, inst.ID) },
	},
	{
		Label:     "Shutdown",
		ValidFrom: []string{verda.StatusRunning},
		ConfirmMsg: "Shutting down the instance temporarily pauses it so technical\n" +
			"  processes can occur, such as attaching or detaching volumes.\n\n" +
			"  Shutdown instances continue to charge your account.",
		ExpectStatus: verda.StatusOffline,
		Execute:      func(ctx context.Context, c *verda.Client, inst *verda.Instance) error { return c.Instances.Shutdown(ctx, inst.ID) },
	},
	{
		Label:     "Force shutdown",
		ValidFrom: []string{verda.StatusRunning},
		ConfirmMsg: "Force shutdown immediately stops the instance without graceful\n" +
			"  shutdown. This may cause data loss.",
		ExpectStatus: verda.StatusOffline,
		Execute:      func(ctx context.Context, c *verda.Client, inst *verda.Instance) error { return c.Instances.ForceShutdown(ctx, inst.ID) },
	},
	{
		Label:     "Hibernate",
		ValidFrom: []string{verda.StatusRunning},
		ConfirmMsg: "Hibernating the instance saves its state and stops billing.\n" +
			"  You can resume it later.",
		ExpectStatus: verda.StatusOffline,
		Execute:      func(ctx context.Context, c *verda.Client, inst *verda.Instance) error { return c.Instances.Hibernate(ctx, inst.ID) },
	},
	{
		Label:   "Delete instance",
		Execute: nil, // handled specially in runAction via runDeleteFlow
	},
}

// availableActions returns actions valid for the given instance status.
func availableActions(status string) []instanceAction {
	var result []instanceAction
	for _, a := range allActions {
		if len(a.ValidFrom) == 0 {
			result = append(result, a)
			continue
		}
		for _, s := range a.ValidFrom {
			if s == status {
				result = append(result, a)
				break
			}
		}
	}
	return result
}

// NewCmdAction creates the vm action cobra command.
func NewCmdAction(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var instanceID string

	cmd := &cobra.Command{
		Use:     "action",
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

	// Filter actions by current status.
	validActions := availableActions(inst.Status)
	if len(validActions) == 0 {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "No actions available for status %q.\n", inst.Status)
		return nil
	}

	actionLabels := make([]string, len(validActions))
	for i, a := range validActions {
		actionLabels[i] = a.Label
	}
	actionLabels = append(actionLabels, "Cancel")

	actionIdx, err := prompter.Select(ctx, "Select action", actionLabels)
	if err != nil {
		return nil //nolint:nilerr
	}
	if actionIdx == len(validActions) { // Cancel
		return nil
	}

	action := validActions[actionIdx]

	// Special handling for delete — needs volume selection sub-flow.
	if action.Execute == nil {
		return runDeleteFlow(ctx, f, ioStreams, client, inst)
	}

	// Confirm with context message.
	if action.ConfirmMsg != "" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n\n", action.ConfirmMsg)
		confirmed, err := prompter.Confirm(ctx, fmt.Sprintf("Would you like to continue? (%s on %s)", action.Label, inst.Hostname))
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

	// Poll until expected status or show immediate result.
	if action.ExpectStatus != "" {
		return pollInstanceStatus(ctx, ioStreams.ErrOut, client, inst.ID, action.ExpectStatus)
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

// runDeleteFlow handles the delete action with volume selection.
func runDeleteFlow(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, inst *verda.Instance) error {
	prompter := f.Prompter()
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)  // red
	noteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))             // yellow
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s %s\n\n",
		warnStyle.Render("Deleting compute"),
		inst.Hostname)

	// Fetch attached volumes.
	volumes := fetchInstanceVolumes(ctx, client, inst)

	var volumeIDs []string
	if len(volumes) > 0 {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Choose storage to delete\n")
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n", dimStyle.Render("Deleted storage can be restored within 96 hours"))

		labels := make([]string, len(volumes))
		for i, v := range volumes {
			volType := "Block"
			if v.IsOSVolume {
				volType = "OS"
			}
			labels[i] = fmt.Sprintf("%s  %s  %dGB %s", v.Name, volType, v.Size, v.Type)
		}

		indices, err := prompter.MultiSelect(ctx, "Select volumes to delete (optional)", labels)
		if err != nil {
			return nil //nolint:nilerr
		}
		for _, idx := range indices {
			volumeIDs = append(volumeIDs, volumes[idx].ID)
		}
	}

	// Warning about undeleted volumes.
	if len(volumes) > 0 && len(volumeIDs) < len(volumes) {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n\n",
			noteStyle.Render("Storage not marked for deletion will continue to charge your account\n  and can be attached to other compute."))
	}

	// Final confirmation.
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n",
		warnStyle.Render("This action cannot be undone."))

	confirmed, err := prompter.Confirm(ctx, fmt.Sprintf("Delete %s?", inst.Hostname))
	if err != nil || !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Cancelled.")
		return nil
	}

	// Execute delete with spinner.
	deleteCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(deleteCtx, fmt.Sprintf("Deleting %s...", inst.Hostname))
	}
	err = client.Instances.Delete(deleteCtx, []string{inst.ID}, volumeIDs, false)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted: %s (%s)\n", inst.Hostname, inst.ID)
	if len(volumeIDs) > 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "Deleted %d volume(s)\n", len(volumeIDs))
	}
	return nil
}
