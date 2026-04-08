package vm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// runBatchAction validates batch flags and executes the action against all
// matching instances. It is called from runAction when --all is set.
func runBatchAction(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *actionOptions) error {
	// Validation: --all cannot be combined with --id or a positional instance ID.
	if opts.InstanceID != "" {
		return errors.New("cannot combine --all with --id or positional instance ID")
	}

	// Validation: --with-volumes is only valid for delete.
	if opts.WithVolumes && opts.Action != verda.ActionDelete {
		return errors.New("--with-volumes is only valid with the delete action")
	}

	// Agent mode requires --yes for batch operations (always destructive at scale).
	if f.AgentMode() && !opts.Yes {
		return cmdutil.NewConfirmationRequiredError(opts.Action + " --all")
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx := cmd.Context()

	instances, err := fetchBatchInstances(ctx, f, ioStreams, client, opts)
	if err != nil {
		return err
	}

	if len(instances) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No matching instances found.")
		return nil
	}

	// Resolve action metadata (use allActions since instances are already filtered).
	action, err := resolveAction(opts.Action, allActions)
	if err != nil {
		return err
	}

	// Delete has special handling (volume selection sub-flow).
	if action.Execute == nil {
		return runBatchDelete(cmd, f, ioStreams, client, instances, opts)
	}

	// Interactive confirmation.
	if !f.AgentMode() {
		_, _ = fmt.Fprint(ioStreams.ErrOut, formatBatchConfirmation(action.Label, instances))

		if action.WarningMsg != "" {
			warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n\n", warnStyle.Render(action.WarningMsg))
		}

		confirmed, confirmErr := f.Prompter().Confirm(ctx, fmt.Sprintf("Continue? (%s %d instances)", action.Label, len(instances)))
		if confirmErr != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	// Collect instance IDs.
	ids := make([]string, len(instances))
	for i := range instances {
		ids[i] = instances[i].ID
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("Batch %s on %d instances:", action.Label, len(instances)), map[string]any{
		"action":       opts.Action,
		"instance_ids": ids,
	})

	// Execute with spinner.
	actionCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(actionCtx, fmt.Sprintf("%s %d instances...", action.Label, len(instances)))
	}
	results, err := client.Instances.Action(actionCtx, verda.InstanceActionRequest{
		Action: actionNameToAPI(opts.Action),
		ID:     ids,
	})
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	if f.AgentMode() {
		return writeBatchAgentOutput(ioStreams, f.OutputFormat(), opts.Action, instances, results)
	}

	_, _ = fmt.Fprint(ioStreams.Out, formatBatchResults(action.Label, instances, results))
	return nil
}

// runBatchDelete handles delete with optional volume deletion for batch mode.
func runBatchDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, instances []verda.Instance, opts *actionOptions) error {
	ctx := cmd.Context()

	deleteVolumes := opts.WithVolumes

	// Interactive mode: prompt for volume deletion and confirm.
	if !f.AgentMode() {
		confirmed, withVols, err := confirmBatchDelete(ctx, f, ioStreams, instances, opts.WithVolumes)
		if err != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
		deleteVolumes = withVols
	} else {
		// Agent mode: just show the decision for debug.
		cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Batch delete:", map[string]any{
			"count":        len(instances),
			"with_volumes": deleteVolumes,
		})
	}

	// Collect instance IDs and volume IDs.
	ids := make([]string, len(instances))
	for i := range instances {
		ids[i] = instances[i].ID
	}

	// VolumeIDs: empty slice = no volumes deleted; populated = those volumes deleted.
	// IMPORTANT: nil means API default (OS volume deleted), so we always pass an explicit slice.
	var volumeIDs []string
	if deleteVolumes {
		seen := make(map[string]bool)
		for i := range instances {
			for _, vid := range cmdutil.UniqueVolumeIDs(&instances[i]) {
				if !seen[vid] {
					volumeIDs = append(volumeIDs, vid)
					seen[vid] = true
				}
			}
		}
	} else {
		volumeIDs = []string{} // explicit empty = no volumes deleted
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("Batch delete %d instances:", len(instances)), map[string]any{
		"instance_ids":   ids,
		"volume_ids":     volumeIDs,
		"delete_volumes": deleteVolumes,
	})

	// Execute with spinner.
	deleteCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(deleteCtx, fmt.Sprintf("Deleting %d instances...", len(instances)))
	}
	results, err := client.Instances.Action(deleteCtx, verda.InstanceActionRequest{
		Action:    verda.ActionDelete,
		ID:        ids,
		VolumeIDs: volumeIDs,
	})
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	if f.AgentMode() {
		return writeBatchAgentOutput(ioStreams, f.OutputFormat(), "delete", instances, results)
	}

	_, _ = fmt.Fprint(ioStreams.Out, formatBatchResults("Delete", instances, results))
	if deleteVolumes && len(volumeIDs) > 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "Deleted %d volume(s)\n", len(volumeIDs))
	}
	return nil
}

// confirmBatchDelete runs the interactive confirmation flow for batch delete.
// Returns (confirmed, deleteVolumes, error).
func confirmBatchDelete(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, instances []verda.Instance, withVolumesFlag bool) (confirmed, deleteVolumes bool, _ error) {
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	noteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	prompter := f.Prompter()

	_, _ = fmt.Fprint(ioStreams.ErrOut, formatBatchConfirmation("Delete", instances))

	deleteVolumes = withVolumesFlag

	// Only ask about volumes if --with-volumes was not already set.
	if !withVolumesFlag {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n\n",
			noteStyle.Render("Attached volumes will continue to charge your account if not deleted."))

		var volErr error
		deleteVolumes, volErr = prompter.Confirm(ctx, "Also delete all attached volumes?")
		if volErr != nil {
			return false, false, nil //nolint:nilerr // User pressed Esc/Ctrl+C during prompt.
		}
	}

	// Show warning based on volume decision.
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n\n", batchDeleteWarning(len(instances), deleteVolumes))

	// Final confirmation.
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n",
		warnStyle.Render("This action cannot be undone."))

	var confirmErr error
	confirmed, confirmErr = prompter.Confirm(ctx, fmt.Sprintf("Delete %d instances?", len(instances)))
	if confirmErr != nil {
		return false, false, nil //nolint:nilerr // User pressed Esc/Ctrl+C during prompt.
	}
	return confirmed, deleteVolumes, nil
}

// actionNameToAPI maps CLI action names to SDK action constants.
func actionNameToAPI(action string) string {
	switch strings.ToLower(action) {
	case "start":
		return verda.ActionStart
	case "shutdown", "stop":
		return verda.ActionShutdown
	case "force_shutdown", "force-shutdown":
		return verda.ActionForceShutdown
	case "hibernate":
		return verda.ActionHibernate
	case "delete":
		return verda.ActionDelete
	default:
		return action
	}
}

// batchDeleteWarning returns a styled warning message for batch delete.
func batchDeleteWarning(count int, withVolumes bool) string {
	if withVolumes {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
		return warnStyle.Render(fmt.Sprintf("%d instances and ALL attached volumes will be deleted.", count))
	}
	noteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	return noteStyle.Render(fmt.Sprintf("%d instances will be deleted. Attached volumes will NOT be deleted and will continue to charge your account.", count))
}

// writeBatchAgentOutput writes structured JSON output for batch actions in agent mode.
func writeBatchAgentOutput(ioStreams cmdutil.IOStreams, format, action string, instances []verda.Instance, results []verda.InstanceActionResult) error {
	hostnames := make(map[string]string, len(instances))
	for i := range instances {
		hostnames[instances[i].ID] = instances[i].Hostname
	}

	type resultEntry struct {
		InstanceID string `json:"instance_id"`
		Hostname   string `json:"hostname"`
		Status     string `json:"status"`
		Error      string `json:"error,omitempty"`
	}

	succeeded := 0
	entries := make([]resultEntry, 0, max(len(results), len(instances)))

	if len(results) == 0 {
		for i := range instances {
			entries = append(entries, resultEntry{
				InstanceID: instances[i].ID,
				Hostname:   instances[i].Hostname,
				Status:     "success",
			})
			succeeded++
		}
	} else {
		for _, r := range results {
			e := resultEntry{
				InstanceID: r.InstanceID,
				Hostname:   hostnames[r.InstanceID],
				Status:     r.Status,
				Error:      r.Error,
			}
			if r.Status == "success" {
				succeeded++
			}
			entries = append(entries, e)
		}
	}

	output := map[string]any{
		"action":    action,
		"total":     len(entries),
		"succeeded": succeeded,
		"failed":    len(entries) - succeeded,
		"results":   entries,
	}
	_, _ = cmdutil.WriteStructured(ioStreams.Out, format, output)
	return nil
}

// fetchBatchInstances loads instances from the API and filters them to those
// valid for the requested action.
func fetchBatchInstances(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, opts *actionOptions) ([]verda.Instance, error) {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading instances...")
	}

	instances, err := client.Instances.Get(ctx, opts.Status)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return nil, err
	}

	// When no explicit --status filter is set, filter to statuses valid for this action.
	if opts.Status == "" {
		instances = filterByValidFrom(instances, opts.Action)
	}

	return instances, nil
}

// filterByValidFrom keeps only instances whose status is valid for the given action.
// If the action has no status restriction (e.g. delete), all instances are returned.
func filterByValidFrom(instances []verda.Instance, action string) []verda.Instance {
	validStatuses := validFromForAction(action)
	if len(validStatuses) == 0 {
		return instances
	}

	filtered := make([]verda.Instance, 0, len(instances))
	for i := range instances {
		for _, s := range validStatuses {
			if instances[i].Status == s {
				filtered = append(filtered, instances[i])
				break
			}
		}
	}
	return filtered
}

// formatBatchConfirmation builds a human-readable confirmation prompt listing
// the instances that will be acted upon.
func formatBatchConfirmation(actionLabel string, instances []verda.Instance) string {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "\n  About to %s %d instances:\n", actionLabel, len(instances))
	for i := range instances {
		inst := &instances[i]
		ip := ""
		if inst.IP != nil && *inst.IP != "" {
			ip = "  " + *inst.IP
		}
		_, _ = fmt.Fprintf(&b, "    %-20s  %-13s  %-18s  %s%s\n",
			inst.Hostname,
			inst.Status,
			inst.InstanceType,
			inst.Location,
			ip)
	}
	return b.String()
}

// formatBatchResults builds a human-readable summary of batch action results.
// If results is nil (204 No Content), all instances are treated as successful.
func formatBatchResults(actionLabel string, instances []verda.Instance, results []verda.InstanceActionResult) string {
	greenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	// Build hostname lookup from instances.
	hostnames := make(map[string]string, len(instances))
	for i := range instances {
		hostnames[instances[i].ID] = instances[i].Hostname
	}

	var b strings.Builder

	// 204 No Content: treat all as success.
	if results == nil {
		_, _ = fmt.Fprintf(&b, "%s %d instances:\n", actionLabel, len(instances))
		for i := range instances {
			_, _ = fmt.Fprintf(&b, "  %s %s\n", greenStyle.Render("✓"), instances[i].Hostname)
		}
		return b.String()
	}

	// Count succeeded vs failed.
	var succeeded, failed int
	for i := range results {
		if results[i].Error != "" {
			failed++
		} else {
			succeeded++
		}
	}

	total := len(results)
	if failed > 0 {
		_, _ = fmt.Fprintf(&b, "%s %d of %d instances:\n", actionLabel, succeeded, total)
	} else {
		_, _ = fmt.Fprintf(&b, "%s %d instances:\n", actionLabel, total)
	}

	for i := range results {
		hostname := hostnames[results[i].InstanceID]
		if hostname == "" {
			hostname = results[i].InstanceID
		}
		if results[i].Error != "" {
			_, _ = fmt.Fprintf(&b, "  %s %s  %s\n", redStyle.Render("✗"), hostname, results[i].Error)
		} else {
			_, _ = fmt.Fprintf(&b, "  %s %s\n", greenStyle.Render("✓"), hostname)
		}
	}

	return b.String()
}
