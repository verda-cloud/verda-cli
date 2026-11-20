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
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	flagInstanceType = "instance-type"
	flagSSHKey       = "ssh-key"
	flagReplicas     = "replicas"

	defaultPoolReplicas     = 1
	defaultPoolWaitTimeout  = 15 * time.Minute
	poolBackendKeepsTrying  = "The backend keeps trying to create workers. Workers that already exist keep billing, even if they have not joined the cluster."
	nodePoolCreateWaitUsage = "Wait until the pool is active with all workers ready"
)

type nodePoolCreateOptions struct {
	poolScope
	Name         string
	InstanceType string
	SSHKeys      []string
	Replicas     int
	Wait         cmdutil.WaitOptions

	replicasSet bool
}

// PoolResult is the structured output of `node-pool create`.
type PoolResult struct {
	Status    string   `json:"status"               yaml:"status"`
	Pool      PoolView `json:"pool"                 yaml:"pool"`
	WaitError string   `json:"wait_error,omitempty" yaml:"wait_error,omitempty"`
}

// NewCmdNodePoolCreate creates `verda mk8s node-pool create`.
func NewCmdNodePoolCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &nodePoolCreateOptions{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a node pool",
		Long: longDesc(`
			Create a node pool: workers of one instance type that accept the
			SSH keys you name by ID. Node pool names are unique in the project,
			not only in their cluster.

			On a terminal, the CLI asks for each value that you do not give,
			then shows a summary and asks for confirmation. In scripts, agent
			mode and -o json|yaml, --cluster, --name, --instance-type and at
			least one --ssh-key are required and the command never asks.

			The CLI does not check the instance type: a type the API cannot
			provide gives a pool whose workers never become ready, and
			node-pool describe shows the reason. The wizard lists the types
			that the availability API reports for the cluster's location; that
			list does not take private-network capacity into account.

			The command returns once the API accepts the request (status
			"accepted"). With --wait it polls until the pool is active with all
			workers ready (status "completed"). If the wait times out, the
			backend keeps trying to create workers, and workers that already
			exist keep billing: delete the pool to stop.

			A create is sent once and is never retried: if it fails with a
			server or network error, the node pool may exist anyway, so check
			node-pool list before you run it again.
		`),
		Example: cmdutil.Examples(`
			# Ask for each value (terminal only)
			verda mk8s node-pool create

			# Find SSH key IDs and the instance types of a location
			verda ssh-key list
			verda availability --location FIN-03

			# One worker, without prompts
			verda mk8s node-pool create --cluster prod --name workers --instance-type 1V100.6V --ssh-key $KEY_ID

			# Three workers with two SSH keys; wait up to 30 minutes until all are ready
			verda mk8s node-pool create --cluster prod --name gpu --instance-type 1V100.6V --ssh-key $KEY_ID --ssh-key $OTHER_KEY_ID --replicas 3 --wait --wait-timeout 30m

			# Agent mode: JSON result and structured errors
			verda --agent mk8s node-pool create --cluster prod -l FIN-03 --name workers --instance-type 1V100.6V --ssh-key $KEY_ID --wait
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.replicasSet = cmd.Flags().Changed(flagReplicas)
			return runNodePoolCreate(cmd, f, ioStreams, opts)
		},
	}
	opts.addFlags(cmd)
	flags := cmd.Flags()
	flags.StringVar(&opts.Name, flagName, "", "Node pool name: a DNS label of at most 30 characters, unique in the project")
	flags.StringVar(&opts.InstanceType, flagInstanceType, "", "Instance type of the workers, for example 1V100.6V")
	flags.StringArrayVar(&opts.SSHKeys, flagSSHKey, nil, "ID of an SSH key for the workers (see verda ssh-key list); repeat the flag for more keys")
	flags.IntVar(&opts.Replicas, flagReplicas, defaultPoolReplicas, "Number of workers, 0 or more")
	flags.BoolVar(&opts.Wait.Wait, flagWait, false, nodePoolCreateWaitUsage)
	flags.DurationVar(&opts.Wait.Timeout, flagWaitTimeout, defaultPoolWaitTimeout, waitTimeUsage)
	return cmd
}

func (o *nodePoolCreateOptions) trim() {
	o.Cluster, o.Location = strings.TrimSpace(o.Cluster), strings.TrimSpace(o.Location)
	o.Name, o.InstanceType = strings.TrimSpace(o.Name), strings.TrimSpace(o.InstanceType)
	o.SSHKeys = cleanGroups(o.SSHKeys)
}

// checkPoolFlags runs before any prompt or call. The errors are UsageErrors:
// main prints an AgentError as JSON even for a person.
func checkPoolFlags(cmd *cobra.Command, w cmdutil.WaitOptions, replicas int) error {
	if w.Wait && w.Timeout <= 0 {
		return cmdutil.UsageErrorf(cmd, "--%s must be greater than 0", flagWaitTimeout)
	}
	if replicas < 0 {
		return cmdutil.UsageErrorf(cmd, "--%s must be 0 or more", flagReplicas)
	}
	return nil
}

func (o *nodePoolCreateOptions) missingValues() []string {
	var m []string
	if o.Cluster == "" {
		m = append(m, "--"+flagCluster)
	}
	if o.Name == "" {
		m = append(m, "--"+flagName)
	}
	if o.InstanceType == "" {
		m = append(m, "--"+flagInstanceType)
	}
	if len(o.SSHKeys) == 0 {
		m = append(m, "--"+flagSSHKey)
	}
	return m
}

func (o *nodePoolCreateOptions) request() verda.CreateManagedKubernetesNodePoolRequest {
	replicas := o.Replicas
	return verda.CreateManagedKubernetesNodePoolRequest{
		Name:         o.Name,
		Replicas:     &replicas,
		InstanceType: o.InstanceType,
		SSHKeyIDs:    o.SSHKeys,
	}
}

func runNodePoolCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *nodePoolCreateOptions) error {
	opts.trim()
	if err := checkPoolFlags(cmd, opts.Wait, opts.Replicas); err != nil {
		return err
	}
	missing := opts.missingValues()
	interactive := interactiveTTY(f)
	if len(missing) > 0 && !interactive {
		return cmdutil.NewMissingFlagsError(missing)
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	// A cluster given by flag is found first, so a wrong name fails before any prompt.
	if opts.Cluster != "" {
		t, err := resolveCluster(cmd, f, ioStreams, client, opts.Cluster, opts.Location)
		if err != nil {
			return err
		}
		opts.Location = t.Location
	}
	if len(missing) > 0 {
		flow := buildNodePoolCreateFlow(opts, newWizardCache(f, client))
		if err := runWizard(cmd.Context(), f, ioStreams, flow); err != nil {
			return err
		}
		opts.trim()
	}
	target := &clusterTarget{Name: opts.Cluster, Location: opts.Location}
	req := opts.request()

	if interactive {
		ok, err := confirmNodePoolCreate(cmd.Context(), f, ioStreams, target, &req)
		if err != nil || !ok {
			return err
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)
	pool, err := apiCall(cmd.Context(), f, "Creating node pool...", func(ctx context.Context) (*verda.ManagedKubernetesNodePool, error) {
		return client.ManagedKubernetes.CreateNodePool(ctx, target.Location, target.Name, req)
	})
	if err != nil {
		listCmd := fmt.Sprintf("verda mk8s node-pool list --cluster %s -l %s", target.Name, target.Location)
		return createFailed(f, ioStreams, kindNodePool, listCmd, err)
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", newPoolView(pool))

	result := PoolResult{Status: statusAccepted, Pool: newPoolView(pool)}
	var waitErr error
	if opts.Wait.Wait {
		last, err := waitForPool(cmd.Context(), f, ioStreams, client, target, pool.Name, poolCreated(opts.Replicas), opts.Wait.Timeout)
		if last != nil {
			result.Pool = newPoolView(last)
		}
		if err == nil {
			result.Status = statusCompleted
		} else {
			waitErr = poolWaitFailure(err, poolWaitCreate, target, pool, last, opts.Wait.Timeout, 0)
			result.WaitError = waitErr.Error()
		}
	}
	if written, err := writeResult(f, ioStreams, result, waitErr); written {
		return err
	}
	printPoolCreated(ioStreams, target, &result)
	return waitErr
}

func confirmNodePoolCreate(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, t *clusterTarget, req *verda.CreateManagedKubernetesNodePoolRequest) (bool, error) {
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\nCreate node pool %s\n  Cluster:        %s (%s)\n  Instance type:  %s\n  Workers:        %d\n  SSH keys:       %s\n\n",
		req.Name, t.Name, t.Location, req.InstanceType, *req.Replicas, strings.Join(req.SSHKeyIDs, ", "))
	return askConfirm(ctx, ioStreams, f.Prompter(), "Create node pool "+req.Name+"?")
}

func printPoolCreated(ioStreams cmdutil.IOStreams, t *clusterTarget, r *PoolResult) {
	p := &r.Pool
	if r.Status == statusCompleted {
		_, _ = fmt.Fprintf(ioStreams.Out, "Node pool %s completed (cluster %s, %s): %d/%d workers ready\n", p.Name, t.Name, t.Location, p.ReadyReplicas, p.Replicas)
		return
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Node pool %s accepted (cluster %s, %s), state %s.\n", p.Name, t.Name, t.Location, p.State)
	if r.WaitError == "" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Check progress with: %s\n", poolDescribeCmd(t, p.Name))
	}
}
