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
	if opts.WithVolumes && opts.Action != "delete" {
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

	_ = instances // will be used by Tasks 6-7
	return errors.New("batch action not yet implemented")
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
