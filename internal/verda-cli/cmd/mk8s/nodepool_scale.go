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

package mk8s

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type nodePoolScaleOptions struct {
	poolScope
	Replicas int
	Yes      bool
	Wait     cmdutil.WaitOptions

	replicasSet bool
}

// PoolScaleResult is the structured output of `node-pool scale`.
type PoolScaleResult struct {
	Status           string   `json:"status"               yaml:"status"`
	PreviousReplicas int      `json:"previous_replicas"    yaml:"previous_replicas"`
	Pool             PoolView `json:"pool"                 yaml:"pool"`
	WaitError        string   `json:"wait_error,omitempty" yaml:"wait_error,omitempty"`
}

// NewCmdNodePoolScale creates `verda mk8s node-pool scale`.
func NewCmdNodePoolScale(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &nodePoolScaleOptions{}
	cmd := &cobra.Command{
		Use:   "scale [<pool>]",
		Short: "Change the number of workers of a node pool",
		Long: longDesc(`
			Set the number of workers of a node pool. Scaling down, to 0 too,
			removes workers and deletes their VMs: it asks for confirmation on
			a terminal, and --yes is required in scripts, agent mode and
			-o json|yaml. Scaling up never needs --yes; on a terminal it shows
			a summary and asks.

			Without <pool> or --cluster, a terminal session shows a picker.
			Without --replicas, it shows the current number and asks for the
			new one; in scripts, agent mode and -o json|yaml the flag is
			required.

			The command returns once the API accepts the request (status
			"accepted"). With --wait it polls until the pool is active with the
			new number of workers, all ready (status "completed"). After a
			scale-down the VMs of the removed workers can still be deleting in
			the background when the wait ends. Scaling to the current number
			sends nothing; with --wait it still waits when the pool is not yet
			active with all of them ready, so a retry after a timeout works.
		`),
		Example: cmdutil.Examples(`
			# Pick the node pool and type the number (terminal only)
			verda mk8s node-pool scale

			# Scale up and wait until the new workers are ready
			verda mk8s node-pool scale workers --cluster prod --replicas 5 --wait

			# Scale down to zero without prompts (deletes the worker VMs)
			verda mk8s node-pool scale workers --cluster prod --replicas 0 --yes
			verda --agent mk8s node-pool scale workers --cluster prod --replicas 0 --yes --wait
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.replicasSet = cmd.Flags().Changed(flagReplicas)
			return runNodePoolScale(cmd, f, ioStreams, opts, args)
		},
	}
	opts.addFlags(cmd)
	flags := cmd.Flags()
	flags.IntVar(&opts.Replicas, flagReplicas, 0, "New number of workers, 0 or more (a lower number deletes workers)")
	flags.BoolVarP(&opts.Yes, flagYes, "y", false, "Skip confirmation (required to scale down in scripts, agent mode and -o json|yaml)")
	flags.BoolVar(&opts.Wait.Wait, flagWait, false, "Wait until the pool is active with the new number of workers ready")
	flags.DurationVar(&opts.Wait.Timeout, flagWaitTimeout, defaultPoolWaitTimeout, waitTimeUsage)
	return cmd
}

func runNodePoolScale(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *nodePoolScaleOptions, args []string) error {
	if err := opts.validate(f, cmd, args); err != nil {
		return err
	}
	client, target, err := opts.connect(cmd, f, ioStreams)
	if err != nil || target == nil {
		return err
	}
	pool, err := resolvePool(cmd, f, ioStreams, client, target, args)
	if err != nil || pool == nil {
		return err
	}
	if !opts.replicasSet {
		n, ok, err := askReplicas(cmd.Context(), f, ioStreams, pool)
		if err != nil || !ok {
			return err
		}
		opts.Replicas = n
	}
	if opts.Replicas == pool.Replicas && (!opts.Wait.Wait || poolSettled(pool, opts.Replicas)) {
		return reportScaleUnchanged(f, ioStreams, pool)
	}

	result := PoolScaleResult{Status: statusAccepted, PreviousReplicas: pool.Replicas, Pool: newPoolView(pool)}
	accepted := pool
	if opts.Replicas != pool.Replicas {
		updated, err := opts.applyScale(cmd, f, ioStreams, client, target, pool)
		if err != nil || updated == nil {
			return err
		}
		accepted = updated
		result.Pool = newPoolView(updated)
	}
	var waitErr error
	if opts.Wait.Wait {
		waitErr = opts.wait(cmd.Context(), f, ioStreams, client, target, accepted, &result)
	}
	if written, err := writeResult(f, ioStreams, result, waitErr); written {
		return err
	}
	printPoolScaled(ioStreams, target, &result, opts.Replicas < pool.Replicas)
	return waitErr
}

func (o *nodePoolScaleOptions) validate(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if err := checkPoolFlags(cmd, o.Wait, o.Replicas); err != nil {
		return err
	}
	if missing := o.scaleMissing(f, cmd, args); len(missing) > 0 {
		return cmdutil.NewMissingFlagsError(missing)
	}
	return nil
}

func poolSettled(pool *verda.ManagedKubernetesNodePool, want int) bool {
	_, done := poolScaled(want)(pool)
	return done
}

// applyScale returns nil without an error when the user declined.
func (o *nodePoolScaleOptions) applyScale(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, target *clusterTarget, pool *verda.ManagedKubernetesNodePool) (*verda.ManagedKubernetesNodePool, error) {
	if ok, err := o.confirm(cmd.Context(), f, ioStreams, target, pool); err != nil || !ok {
		return nil, err
	}
	replicas := o.Replicas
	req := verda.UpdateManagedKubernetesNodePoolRequest{Replicas: &replicas}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)
	updated, err := apiCall(cmd.Context(), f, "Scaling node pool...", func(ctx context.Context) (*verda.ManagedKubernetesNodePool, error) {
		return client.ManagedKubernetes.UpdateNodePool(ctx, target.Location, target.Name, pool.Name, req)
	})
	if err != nil {
		return nil, err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", newPoolView(updated))
	return updated, nil
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func reportScaleUnchanged(f cmdutil.Factory, ioStreams cmdutil.IOStreams, pool *verda.ManagedKubernetesNodePool) error {
	result := PoolScaleResult{Status: statusUnchanged, PreviousReplicas: pool.Replicas, Pool: newPoolView(pool)}
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Node pool %s already has %s; nothing to do.\n", pool.Name, pluralize(pool.Replicas, "replica"))
	return nil
}

func (o *nodePoolScaleOptions) scaleMissing(f cmdutil.Factory, cmd *cobra.Command, args []string) []string {
	missing := o.missing(f, cmd, args, argPool)
	if !o.replicasSet && !interactiveTTY(f) {
		missing = append(missing, "--"+flagReplicas)
	}
	return missing
}

// confirm: only a scale-down is destructive, and only now is the current count known.
func (o *nodePoolScaleOptions) confirm(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, t *clusterTarget, pool *verda.ManagedKubernetesNodePool) (bool, error) {
	if o.Yes {
		return true, nil
	}
	down := o.Replicas < pool.Replicas
	if !interactiveTTY(f) {
		if down {
			return false, confirmationRequired("scale")
		}
		return true, nil
	}
	return confirmScale(ctx, f, ioStreams, t, pool, o.Replicas, down)
}

func (o *nodePoolScaleOptions) wait(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, t *clusterTarget, updated *verda.ManagedKubernetesNodePool, result *PoolScaleResult) error {
	last, err := waitForPool(ctx, f, ioStreams, client, t, updated.Name, updated, poolScaled(o.Replicas), o.Wait.Timeout)
	if last != nil {
		result.Pool = newPoolView(last)
	}
	if err == nil {
		result.Status = statusCompleted
		return nil
	}
	kind := poolWaitScaleUp
	switch {
	case o.Replicas < result.PreviousReplicas:
		kind = poolWaitScaleDown
	case o.Replicas == result.PreviousReplicas:
		kind = poolWaitSettle
	}
	err = poolWaitFailure(err, kind, t, updated, last, o.Wait.Timeout, result.PreviousReplicas)
	result.WaitError = err.Error()
	return wholeError(f, err)
}

// askReplicas: ok is false when the user canceled.
func askReplicas(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, pool *verda.ManagedKubernetesNodePool) (n int, ok bool, err error) {
	v, err := f.Prompter().TextInput(ctx, fmt.Sprintf("New number of workers (pool %s has %d)", pool.Name, pool.Replicas),
		tui.WithDefault(strconv.Itoa(pool.Replicas)), tui.WithValidation(validateReplicas))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			printCanceled(ioStreams)
			return 0, false, nil
		}
		return 0, false, err
	}
	n, _ = strconv.Atoi(strings.TrimSpace(v))
	return n, true, nil
}

func confirmScale(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, t *clusterTarget, pool *verda.ManagedKubernetesNodePool, want int, down bool) (bool, error) {
	prompt := fmt.Sprintf("Scale node pool %s to %s?", pool.Name, pluralize(want, "worker"))
	if down {
		return confirmWarning(ctx, ioStreams, f.Prompter(),
			fmt.Sprintf("Scales node pool %s down from %d to %s.", pool.Name, pool.Replicas, pluralize(want, "worker")),
			[]string{"The removed workers and their VMs are deleted.", "This action cannot be undone."}, prompt)
	}
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\nScale node pool %s\n  Cluster:  %s (%s)\n  Workers:  %d -> %d\n\n", pool.Name, t.Name, t.Location, pool.Replicas, want)
	return askConfirm(ctx, ioStreams, f.Prompter(), prompt)
}

func printPoolScaled(ioStreams cmdutil.IOStreams, t *clusterTarget, r *PoolScaleResult, down bool) {
	p := &r.Pool
	if r.Status == statusCompleted {
		_, _ = fmt.Fprintf(ioStreams.Out, "Node pool %s scaled from %d to %s (cluster %s, %s).", p.Name, r.PreviousReplicas, pluralize(p.Replicas, "worker"), t.Name, t.Location)
		if down {
			_, _ = fmt.Fprint(ioStreams.Out, " The removed workers' VMs are still being deleted in the background.")
		}
		_, _ = fmt.Fprintln(ioStreams.Out)
		return
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Scaling of node pool %s from %d to %s accepted (cluster %s, %s).\n", p.Name, r.PreviousReplicas, pluralize(p.Replicas, "worker"), t.Name, t.Location)
	if r.WaitError == "" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Check progress with: %s\n", poolDescribeCmd(t, p.Name))
	}
}
