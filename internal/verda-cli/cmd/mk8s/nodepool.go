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

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	flagCluster = "cluster"
	argPool     = "<pool>"
)

// NewCmdNodePool creates the `verda mk8s node-pool` parent command.
func NewCmdNodePool(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "node-pool",
		Aliases: []string{"pool", "np"},
		Short:   "Manage the node pools of a cluster",
		Long: longDesc(`
			List, show, create, scale and delete the node pools of a cluster.
			A node pool is a set of workers: VMs of one instance type that join
			the cluster as nodes. Every command takes the cluster with
			--cluster and finds its location from the name; -l gives the
			location and skips that lookup.

			Node pool names are unique in the project, not only in their
			cluster.
		`),
		Example: cmdutil.Examples(`
			verda mk8s node-pool list --cluster prod
			verda mk8s node-pool create --cluster prod --name workers --instance-type 1V100.6V --ssh-key $KEY_ID
			verda mk8s node-pool scale workers --cluster prod --replicas 3
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		NewCmdNodePoolList(f, ioStreams),
		NewCmdNodePoolDescribe(f, ioStreams),
		NewCmdNodePoolCreate(f, ioStreams),
		NewCmdNodePoolScale(f, ioStreams),
		NewCmdNodePoolDelete(f, ioStreams),
	)
	return cmd
}

// poolScope is --cluster and -l, shared by the node-pool and kubeconfig commands.
type poolScope struct {
	Cluster  string
	Location string
}

func (s *poolScope) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&s.Cluster, flagCluster, "", clusterUsage)
	addLocationFlag(cmd, &s.Location)
}

// missing lists --cluster, and arg (<pool> or <id>) unless it is "", when a
// terminal cannot ask for them or they are blank.
func (s *poolScope) missing(f cmdutil.Factory, cmd *cobra.Command, args []string, arg string) []string {
	var m []string
	if clusterArgMissing(f, s.Cluster, cmd.Flags().Changed(flagCluster)) {
		m = append(m, "--"+flagCluster)
	}
	if arg != "" && clusterArgMissing(f, argOrEmpty(args), len(args) > 0) {
		m = append(m, arg)
	}
	return m
}

// check fails before any call, listing everything missing at once.
func (s *poolScope) check(f cmdutil.Factory, cmd *cobra.Command, args []string, arg string) error {
	if m := s.missing(f, cmd, args, arg); len(m) > 0 {
		return cmdutil.NewMissingFlagsError(m)
	}
	return nil
}

// connect returns a nil target with a nil error when the user canceled or there is nothing to pick.
func (s *poolScope) connect(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) (*verda.Client, *clusterTarget, error) {
	client, err := f.VerdaClient()
	if err != nil {
		return nil, nil, err
	}
	t, err := resolveCluster(cmd, f, ioStreams, client, s.Cluster, s.Location)
	return client, t, err
}

// resolvePool reads the pool named by args[0], or asks for one. A nil pool with
// a nil error means the user canceled or the cluster has no pool.
func resolvePool(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, t *clusterTarget, args []string) (*verda.ManagedKubernetesNodePool, error) {
	if len(args) > 0 {
		name := strings.TrimSpace(args[0])
		return apiCall(cmd.Context(), f, "Loading node pool...", func(ctx context.Context) (*verda.ManagedKubernetesNodePool, error) {
			return client.ManagedKubernetes.GetNodePool(ctx, t.Location, t.Name, name)
		})
	}
	pools, err := listPools(cmd.Context(), f, client, t)
	if err != nil {
		return nil, err
	}
	return pickPool(cmd.Context(), f, ioStreams, t, pools)
}

func listPools(ctx context.Context, f cmdutil.Factory, client *verda.Client, t *clusterTarget) ([]verda.ManagedKubernetesNodePool, error) {
	return apiCall(ctx, f, "Loading node pools...", func(ctx context.Context) ([]verda.ManagedKubernetesNodePool, error) {
		return client.ManagedKubernetes.ListNodePools(ctx, t.Location, t.Name)
	})
}

// poolCmd is a node-pool command for "check with" and "to stop" hints.
func poolCmd(verb string, t *clusterTarget, pool string) string {
	return fmt.Sprintf("verda mk8s node-pool %s %s --cluster %s -l %s", verb, pool, t.Name, t.Location)
}

func poolDescribeCmd(t *clusterTarget, pool string) string { return poolCmd("describe", t, pool) }

func pickPool(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, t *clusterTarget, pools []verda.ManagedKubernetesNodePool) (*verda.ManagedKubernetesNodePool, error) {
	if len(pools) == 0 {
		printNoPools(ioStreams, t.Name)
		return nil, nil
	}
	labels := make([]string, 0, len(pools)+1)
	for i := range pools {
		p := &pools[i]
		labels = append(labels, fmt.Sprintf("%s  (%s, %d/%d, %s)", p.Name, p.InstanceType, p.ReadyReplicas, p.Replicas, p.State))
	}
	labels = append(labels, cancelLabel)
	idx, err := f.Prompter().Select(ctx, "Select node pool", labels, pickerHints()...)
	switch {
	case err == nil && idx >= 0 && idx < len(pools):
		return &pools[idx], nil
	case err != nil && !cmdutil.IsPromptCancel(err):
		return nil, err
	}
	printCanceled(ioStreams)
	return nil, nil
}

func printNoPools(ioStreams cmdutil.IOStreams, cluster string) {
	_, _ = fmt.Fprintf(ioStreams.Out, "No node pools in cluster %s.\n", cluster)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "Create one with: verda mk8s node-pool create --cluster %s\n", cluster)
}
