// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package serverless

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// containerListExitKey is the synthetic LiveRow key for the trailing Exit row (_ cannot appear in deployment names).
const containerListExitKey = "__exit__"

type containerListOptions struct {
	Status string
}

func newCmdContainerList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &containerListOptions{}
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List serverless container deployments",
		Long: cmdutil.LongDesc(`
			List container deployments. On a terminal, you can type to filter,
			select a deployment to view details, and return to the list.
		`),
		Example: cmdutil.Examples(`
			verda container list
			verda container ls
			verda container list --status healthy
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runContainerList(cmd, f, ioStreams, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Status, "status", "", "Filter by status substring (e.g., healthy, initializing, error)")
	return cmd
}

func runContainerList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *containerListOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	statuses := newContainerStatusCache(containerStatusCacheTTL)
	deployments, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading container deployments...", func() ([]verda.ContainerDeployment, error) {
		return client.ContainerDeployments.GetDeployments(ctx)
	})
	if err != nil {
		return fmt.Errorf("fetching deployments: %w", err)
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Deployments:", deployments)

	interactive := cmdutil.IsStdoutTerminal() && !f.AgentMode() && f.OutputFormat() == "table"

	// List response omits status; prefetch when filtering/structured/non-interactive,
	// otherwise LiveList fills rows lazily.
	if opts.Status != "" || !interactive {
		_ = cmdutil.RunWithSpinner(ctx, f.Status(), "Loading statuses...", func() error {
			statuses.refresh(ctx, client, deployments)
			return nil
		})
	}

	if opts.Status != "" {
		needle := strings.ToLower(opts.Status)
		filtered := deployments[:0]
		for i := range deployments {
			if strings.Contains(strings.ToLower(statuses.get(deployments[i].Name)), needle) {
				filtered = append(filtered, deployments[i])
			}
		}
		deployments = filtered
	}

	if f.OutputFormat() != "table" {
		type row struct {
			*verda.ContainerDeployment
			Status string `json:"status,omitempty"`
		}
		rows := make([]row, len(deployments))
		for i := range deployments {
			rows[i] = row{&deployments[i], statuses.get(deployments[i].Name)}
		}
		if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), rows); wrote {
			return werr
		}
	}

	if len(deployments) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No container deployments found.")
		return nil
	}

	if !interactive {
		return printContainerTable(ioStreams.Out, deployments, statuses)
	}

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %d deployment(s) found\n\n", len(deployments))
	return runContainerListInteractive(cmd, f, ioStreams, client, deployments, statuses)
}

func runContainerListInteractive(
	cmd *cobra.Command,
	f cmdutil.Factory,
	ioStreams cmdutil.IOStreams,
	client *verda.Client,
	deployments []verda.ContainerDeployment,
	statuses *containerStatusCache,
) error {
	prompter := f.Prompter()
	// LiveLister is optional on Prompter; fall back to eager status fetch + Select.
	if liveLister, ok := prompter.(tui.LiveLister); ok {
		return runContainerListLive(cmd, f, ioStreams, client, deployments, statuses, prompter, liveLister)
	}
	return runContainerListEager(cmd, f, ioStreams, client, deployments, statuses, prompter)
}

// runContainerListLive: LiveList paints rows immediately; status RPCs refine labels asynchronously.
func runContainerListLive(
	cmd *cobra.Command,
	f cmdutil.Factory,
	ioStreams cmdutil.IOStreams,
	client *verda.Client,
	deployments []verda.ContainerDeployment,
	statuses *containerStatusCache,
	prompter tui.Prompter,
	liveLister tui.LiveLister,
) error {
	for {
		rows := buildContainerLiveRows(deployments, statuses)
		updates := make(chan tui.LiveListUpdate, len(deployments))
		updateCtx, updateCancel := context.WithCancel(cmd.Context())
		go pushContainerStatusUpdates(updateCtx, client, deployments, statuses, updates)

		idx, err := liveLister.LiveList(cmd.Context(),
			"Select deployment (type to filter)",
			rows, updates,
			tui.WithLiveListShowHints(true),
		)
		updateCancel() // abort in-flight status fetches once the user picks/exits
		if err != nil {
			if cmdutil.IsPromptCancel(err) {
				return nil
			}
			return err
		}
		if idx == len(deployments) {
			return nil
		}

		if derr := runContainerDescribe(cmd, f, ioStreams, deployments[idx].Name); derr != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "Error: %v\n", derr)
		}

		exit, perr := cmdutil.PromptBackOrExit(cmd.Context(), prompter)
		if perr != nil {
			return perr
		}
		if exit {
			return nil
		}
	}
}

// runContainerListEager: no LiveLister—prefetch statuses so Select sees full labels.
func runContainerListEager(
	cmd *cobra.Command,
	f cmdutil.Factory,
	ioStreams cmdutil.IOStreams,
	client *verda.Client,
	deployments []verda.ContainerDeployment,
	statuses *containerStatusCache,
	prompter tui.Prompter,
) error {
	for {
		if statuses.anyStale(deployments) {
			_ = cmdutil.RunWithSpinner(cmd.Context(), f.Status(), "Loading statuses...", func() error {
				refreshCtx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
				defer cancel()
				statuses.refresh(refreshCtx, client, deployments)
				return nil
			})
		}

		labels := make([]string, 0, len(deployments)+1)
		for i := range deployments {
			labels = append(labels, formatContainerRow(&deployments[i], statuses.get(deployments[i].Name)))
		}
		labels = append(labels, "Exit")

		idx, err := prompter.Select(cmd.Context(), "Select deployment (type to filter)", labels, tui.WithShowHints(true))
		if err != nil {
			if cmdutil.IsPromptCancel(err) {
				return nil
			}
			return err
		}
		if idx == len(deployments) {
			return nil
		}

		if derr := runContainerDescribe(cmd, f, ioStreams, deployments[idx].Name); derr != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "Error: %v\n", derr)
		}

		exit, perr := cmdutil.PromptBackOrExit(cmd.Context(), prompter)
		if perr != nil {
			return perr
		}
		if exit {
			return nil
		}
	}
}

// buildContainerLiveRows builds deployment rows plus Exit; stale/missing status shows "..." until pushed.
func buildContainerLiveRows(deployments []verda.ContainerDeployment, statuses *containerStatusCache) []tui.LiveRow {
	rows := make([]tui.LiveRow, 0, len(deployments)+1)
	for i := range deployments {
		d := &deployments[i]
		label := statuses.get(d.Name)
		if label == "" || statuses.stale(d.Name) {
			label = containerStatusLoading
		}
		rows = append(rows, tui.LiveRow{
			Key:   d.Name,
			Label: formatContainerRow(d, label),
		})
	}
	rows = append(rows, tui.LiveRow{Key: containerListExitKey, Label: "Exit"})
	return rows
}

// pushContainerStatusUpdates refreshes stale cache entries with bounded concurrency,
// pushes LiveListUpdate per deployment, closes updates when done.
func pushContainerStatusUpdates(
	ctx context.Context,
	client *verda.Client,
	deployments []verda.ContainerDeployment,
	statuses *containerStatusCache,
	updates chan<- tui.LiveListUpdate,
) {
	defer close(updates)
	var wg sync.WaitGroup
	sem := make(chan struct{}, containerStatusFetchConcurrency)
	for i := range deployments {
		d := &deployments[i]
		if !statuses.stale(d.Name) {
			continue
		}
		wg.Add(1)
		go func(d *verda.ContainerDeployment) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			s, fetchErr := client.ContainerDeployments.GetDeploymentStatus(ctx, d.Name)
			var status string
			var liveErr error
			switch {
			case fetchErr != nil:
				status = containerStatusUnknown
				liveErr = fetchErr
			case s == nil:
				status = containerStatusUnknown
			default:
				status = s.Status
			}
			statuses.set(d.Name, status)
			select {
			case updates <- tui.LiveListUpdate{
				Key:   d.Name,
				Label: formatContainerRow(d, status),
				Err:   liveErr,
			}:
			case <-ctx.Done():
			}
		}(d)
	}
	wg.Wait()
}

func printContainerTable(out io.Writer, deployments []verda.ContainerDeployment, statuses *containerStatusCache) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tSTATUS\tCOMPUTE\tBILLING\tENDPOINT\tCREATED")
	for i := range deployments {
		d := &deployments[i]
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			d.Name,
			statusOrDash(statuses.get(d.Name)),
			formatContainerCompute(d),
			formatContainerBilling(d),
			d.EndpointBaseURL,
			d.CreatedAt.Format("2006-01-02 15:04"),
		)
	}
	return w.Flush()
}

func formatContainerRow(d *verda.ContainerDeployment, status string) string {
	return fmt.Sprintf("%-32s  ● %-14s  %-22s  %-10s  %s",
		truncate(d.Name, 32),
		statusOrDash(status),
		formatContainerCompute(d),
		formatContainerBilling(d),
		d.CreatedAt.Format("2006-01-02 15:04"),
	)
}

func formatContainerCompute(d *verda.ContainerDeployment) string {
	if d.Compute == nil {
		return "-"
	}
	return fmt.Sprintf("%dx %s", d.Compute.Size, d.Compute.Name)
}

func formatContainerBilling(d *verda.ContainerDeployment) string {
	if d.IsSpot {
		return computeTypeSpot
	}
	return computeTypeOnDemand
}

func statusOrDash(s string) string {
	if s == "" {
		return containerStatusUnknown
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
