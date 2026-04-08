package vm

import (
	"context"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// actionNameMap maps CLI flag values to action labels.
var actionNameMap = map[string]string{
	"start":          "Start",
	"shutdown":       "Shutdown",
	"force_shutdown": "Force shutdown",
	"force-shutdown": "Force shutdown",
	"hibernate":      "Hibernate",
	"delete":         "Delete instance",
}

// instanceAction defines a supported action with its display label and executor.
type instanceAction struct {
	Label        string
	ConfirmMsg   string   // descriptive text shown before confirmation
	WarningMsg   string   // highlighted in red bold
	ValidFrom    []string // instance statuses where this action is available; empty = always
	ExpectStatus string   // poll until this status; empty = no polling
	Execute      func(ctx context.Context, client *verda.Client, inst *verda.Instance) error
}

var allActions = []instanceAction{
	{
		Label:        "Start",
		ValidFrom:    []string{verda.StatusOffline},
		ExpectStatus: verda.StatusRunning,
		Execute: func(ctx context.Context, c *verda.Client, inst *verda.Instance) error {
			return c.Instances.Start(ctx, inst.ID)
		},
	},
	{
		Label:     "Shutdown",
		ValidFrom: []string{verda.StatusRunning},
		ConfirmMsg: "Shutting down the instance temporarily pauses it so technical\n" +
			"  processes can occur, such as attaching or detaching volumes.",
		WarningMsg:   "Shutdown instances continue to charge your account.",
		ExpectStatus: verda.StatusOffline,
		Execute: func(ctx context.Context, c *verda.Client, inst *verda.Instance) error {
			return c.Instances.Shutdown(ctx, inst.ID)
		},
	},
	{
		Label:        "Force shutdown",
		ValidFrom:    []string{verda.StatusRunning},
		ConfirmMsg:   "Force shutdown immediately stops the instance without graceful shutdown.",
		WarningMsg:   "This may cause data loss.",
		ExpectStatus: verda.StatusOffline,
		Execute: func(ctx context.Context, c *verda.Client, inst *verda.Instance) error {
			return c.Instances.ForceShutdown(ctx, inst.ID)
		},
	},
	{
		Label:     "Hibernate",
		ValidFrom: []string{verda.StatusRunning},
		ConfirmMsg: "Hibernating the instance saves its state and stops billing.\n" +
			"  You can resume it later.",
		ExpectStatus: verda.StatusOffline,
		Execute: func(ctx context.Context, c *verda.Client, inst *verda.Instance) error {
			return c.Instances.Hibernate(ctx, inst.ID)
		},
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

type actionOptions struct {
	InstanceID  string
	Action      string
	Yes         bool
	All         bool
	Status      string
	Hostname    string
	WithVolumes bool
	Wait        cmdutil.WaitOptions
}

// NewCmdAction creates the vm action cobra command.
func NewCmdAction(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &actionOptions{}

	cmd := &cobra.Command{
		Use:   "action",
		Short: "Perform actions on a VM instance",
		Long: cmdutil.LongDesc(`
			Select a VM instance and perform an action: start, shutdown,
			force shutdown, hibernate, or delete.
		`),
		Example: cmdutil.Examples(`
			# Interactive: select instance then action
			verda vm action

			# Specify instance ID
			verda vm action --id abc-123

			# Run action and wait for completion
			verda vm action --id abc-123 --wait

			# Non-interactive (agent mode)
			verda --agent vm action --id abc-123 --action shutdown
			verda --agent vm action --id abc-123 --action delete --yes
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAction(cmd, f, ioStreams, opts)
		},
	}

	cmd.Flags().StringVar(&opts.InstanceID, "id", "", "Instance ID to act on")
	cmd.Flags().StringVar(&opts.Action, "action", "", "Action to perform: start, shutdown, force_shutdown, hibernate, delete")
	cmd.Flags().BoolVar(&opts.Yes, "yes", false, "Skip confirmation for destructive actions (required in agent mode)")
	opts.Wait.AddFlags(cmd.Flags(), true) // --wait defaults to true to preserve existing behavior

	return cmd
}

//nolint:gocyclo // Interactive CLI command with prompts, confirmation, and spinner — inherently complex.
func runAction(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *actionOptions) error {
	// In agent mode, --id and --action are required.
	if f.AgentMode() {
		var missing []string
		if opts.InstanceID == "" && !opts.All && opts.Hostname == "" {
			missing = append(missing, "--id")
		}
		if opts.Action == "" {
			missing = append(missing, "--action")
		}
		if len(missing) > 0 {
			return cmdutil.NewMissingFlagsError(missing)
		}
	}

	// Batch mode: --all or --hostname routes to batch execution.
	if opts.All || opts.Hostname != "" {
		return runBatchAction(cmd, f, ioStreams, opts)
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	prompter := f.Prompter()
	ctx := cmd.Context()

	// Select instance(s) if not provided.
	if opts.InstanceID == "" {
		id, batchErr := resolveInstanceInteractive(cmd, f, ioStreams, client, opts)
		if batchErr != nil {
			return batchErr
		}
		if id == "" {
			return nil // canceled or routed to batch
		}
		opts.InstanceID = id
	}

	// Fetch instance details.
	inst, err := client.Instances.GetByID(ctx, opts.InstanceID)
	if err != nil {
		return fmt.Errorf("fetching instance: %w", err)
	}

	// Filter actions by current status.
	validActions := availableActions(inst.Status)
	if len(validActions) == 0 {
		if f.AgentMode() {
			return &cmdutil.AgentError{
				Code:     "NO_ACTIONS_AVAILABLE",
				Message:  fmt.Sprintf("no actions available for status %q", inst.Status),
				Details:  map[string]any{"status": inst.Status},
				ExitCode: cmdutil.ExitBadArgs,
			}
		}
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "No actions available for status %q.\n", inst.Status)
		return nil
	}

	var action instanceAction

	if opts.Action != "" {
		var resolveErr error
		action, resolveErr = resolveAction(opts.Action, validActions)
		if resolveErr != nil {
			return resolveErr
		}
	} else {
		// Interactive: show instance summary and prompt for action.
		_, _ = fmt.Fprint(ioStreams.Out, renderInstanceCard(inst))

		actionLabels := make([]string, 0, len(validActions)+1)
		for _, a := range validActions {
			actionLabels = append(actionLabels, a.Label)
		}
		actionLabels = append(actionLabels, "Cancel")

		actionIdx, err := prompter.Select(ctx, "Select action", actionLabels)
		if err != nil {
			return nil
		}
		if actionIdx == len(validActions) { // Cancel
			return nil
		}
		action = validActions[actionIdx]
	}

	// Special handling for delete — needs volume selection sub-flow.
	if action.Execute == nil {
		if f.AgentMode() {
			return runDeleteAgent(ctx, f, ioStreams, client, inst, opts.Yes)
		}
		return runDeleteFlow(ctx, f, ioStreams, client, inst)
	}

	// Confirm with context message and warning.
	isDestructive := action.ConfirmMsg != "" || action.WarningMsg != ""
	if isDestructive && f.AgentMode() && !opts.Yes {
		return cmdutil.NewConfirmationRequiredError(opts.Action)
	}
	if isDestructive && !f.AgentMode() {
		confirmed, err := confirmDestructive(ctx, ioStreams, prompter, &action, inst)
		if err != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("Action: %s on instance:", action.Label), map[string]string{
		"instance_id": inst.ID,
		"hostname":    inst.Hostname,
		"status":      inst.Status,
	})

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

	// Structured output for agent mode.
	if f.AgentMode() {
		result := map[string]string{
			"id":     inst.ID,
			"action": opts.Action,
			"status": "completed",
		}
		_, _ = cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result)
		return nil
	}

	// Poll until expected status or show immediate result.
	if action.ExpectStatus != "" && opts.Wait.Wait {
		result, err := cmdutil.PollInstanceStatus(ctx, ioStreams.ErrOut, client, inst.ID, opts.Wait, action.ExpectStatus)
		if err != nil {
			return err
		}
		if result != nil {
			_, _ = fmt.Fprint(ioStreams.Out, renderInstanceCard(result))
		}
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Done: %s on %s (%s)\n", action.Label, inst.Hostname, inst.ID)
	return nil
}

// runDeleteAgent handles delete in agent mode: requires --yes, deletes all volumes.
func runDeleteAgent(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, inst *verda.Instance, yes bool) error {
	if !yes {
		return cmdutil.NewConfirmationRequiredError("delete")
	}

	// In agent mode, delete the instance and all attached volumes.
	volumes := fetchInstanceVolumes(ctx, client, inst)
	volumeIDs := make([]string, 0, len(volumes))
	for i := range volumes {
		volumeIDs = append(volumeIDs, volumes[i].ID)
	}

	deleteCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	err := client.Instances.Delete(deleteCtx, []string{inst.ID}, volumeIDs, false)
	if err != nil {
		return err
	}

	result := map[string]any{
		"id":              inst.ID,
		"action":          "delete",
		"status":          "completed",
		"volumes_deleted": len(volumeIDs),
	}
	_, _ = cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result)
	return nil
}

// resolveInstanceInteractive handles interactive instance selection.
// For shortcut commands (action pre-set), uses multi-select so users can pick
// multiple instances. If multiple are selected, routes to batch and returns "".
// Returns the selected instance ID, or "" if canceled/routed to batch.
func resolveInstanceInteractive(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, opts *actionOptions) (string, error) {
	ctx := cmd.Context()

	var statusFilter []string
	if opts.Status != "" {
		statusFilter = []string{opts.Status}
	} else if opts.Action != "" {
		statusFilter = validFromForAction(opts.Action)
	}

	// Shortcut commands use multi-select; generic "vm action" uses single-select.
	if opts.Action == "" {
		return selectInstance(ctx, f, ioStreams, client, statusFilter...)
	}

	selected, err := selectInstances(ctx, f, ioStreams, client, statusFilter...)
	if err != nil {
		return "", err
	}
	if len(selected) == 0 {
		return "", nil
	}
	if len(selected) > 1 {
		return "", runBatchWithInstances(cmd, f, ioStreams, client, selected, opts)
	}
	return selected[0].ID, nil
}

func selectInstance(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, statusFilter ...string) (string, error) {
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

	// Filter by status when the caller restricts to specific statuses.
	if len(statusFilter) > 0 {
		filtered := instances[:0]
		for i := range instances {
			for _, s := range statusFilter {
				if instances[i].Status == s {
					filtered = append(filtered, instances[i])
					break
				}
			}
		}
		instances = filtered
	}

	if len(instances) == 0 {
		if len(statusFilter) > 0 {
			_, _ = fmt.Fprintf(ioStreams.Out, "No instances with status %s found.\n", strings.Join(statusFilter, ", "))
		} else {
			_, _ = fmt.Fprintln(ioStreams.Out, "No instances found.")
		}
		return "", nil
	}

	labels := make([]string, 0, len(instances)+1)
	for i := range instances {
		labels = append(labels, formatInstanceRow(&instances[i]))
	}
	labels = append(labels, "Cancel")

	idx, err := f.Prompter().Select(ctx, "Select instance (type to filter)", labels)
	if err != nil {
		return "", nil //nolint:nilerr // User pressed Esc/Ctrl+C during prompt.
	}
	if idx == len(instances) {
		return "", nil
	}

	return instances[idx].ID, nil
}

// runDeleteFlow handles the delete action with volume selection.
func runDeleteFlow(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, inst *verda.Instance) error {
	prompter := f.Prompter()
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true) // red
	noteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))            // yellow
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
		for i := range volumes {
			volType := "Block"
			if volumes[i].IsOSVolume {
				volType = "OS"
			}
			labels[i] = fmt.Sprintf("%s  %s  %dGB %s", volumes[i].Name, volType, volumes[i].Size, volumes[i].Type)
		}

		indices, err := prompter.MultiSelect(ctx, "Select volumes to delete (optional)", labels)
		if err != nil {
			return nil
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
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return nil
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete instance:", map[string]any{
		"instance_id": inst.ID,
		"hostname":    inst.Hostname,
		"volume_ids":  volumeIDs,
	})

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

// validFromForAction returns the ValidFrom statuses for a given action name.
// Returns nil (no filter) if the action is unknown or has no status restriction (e.g. delete).
func validFromForAction(actionName string) []string {
	label, ok := actionNameMap[strings.ToLower(actionName)]
	if !ok {
		return nil
	}
	for _, a := range allActions {
		if a.Label == label {
			return a.ValidFrom
		}
	}
	return nil
}

// resolveAction maps a CLI --action flag value to an instanceAction from the valid set.
func resolveAction(actionName string, validActions []instanceAction) (instanceAction, error) {
	label, ok := actionNameMap[strings.ToLower(actionName)]
	if !ok {
		return instanceAction{}, fmt.Errorf("unknown --action %q: valid actions are start, shutdown, force_shutdown, hibernate, delete", actionName)
	}
	for _, a := range validActions {
		if a.Label == label {
			return a, nil
		}
	}
	return instanceAction{}, fmt.Errorf("action %q is not valid for current instance status", actionName)
}

// confirmDestructive shows warning/confirm messages and prompts the user.
func confirmDestructive(ctx context.Context, ioStreams cmdutil.IOStreams, prompter tui.Prompter, action *instanceAction, inst *verda.Instance) (bool, error) {
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	if action.ConfirmMsg != "" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n", action.ConfirmMsg)
	}
	if action.WarningMsg != "" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n", warnStyle.Render(action.WarningMsg))
	}
	_, _ = fmt.Fprintln(ioStreams.ErrOut)
	return prompter.Confirm(ctx, fmt.Sprintf("Would you like to continue? (%s on %s)", action.Label, inst.Hostname))
}
