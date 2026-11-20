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
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdClusterDescribe creates `verda mk8s cluster describe`.
func NewCmdClusterDescribe(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var location string
	cmd := &cobra.Command{
		Use:     "describe [<cluster>]",
		Aliases: []string{"get", "show"},
		Short:   "Show a cluster and its node pools",
		Long: longDesc(`
			Show a cluster: its version, phase, endpoint, network, conditions
			and a summary of its node pools. If the node pools cannot be
			loaded, the cluster is still shown, with a warning; in JSON and
			YAML, node_pools is then null and node_pools_error says why.

			Without <cluster>, a terminal session shows a picker.
		`),
		Example: cmdutil.Examples(`
			# Pick a cluster from a list (terminal only)
			verda mk8s cluster describe

			verda mk8s cluster describe prod

			# Give the location to skip the lookup; JSON for scripts
			verda mk8s cluster describe prod -l FIN-03 -o json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterDescribe(cmd, f, ioStreams, args, location)
		},
	}
	addLocationFlag(cmd, &location)
	return cmd
}

func runClusterDescribe(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string, location string) error {
	name := argOrEmpty(args)
	if err := checkClusterArg(f, name, len(args) > 0, argCluster); err != nil {
		return err
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	target, err := resolveCluster(cmd, f, ioStreams, client, name, location)
	if err != nil || target == nil {
		return err
	}

	cluster, err := getCluster(cmd.Context(), f, client, target)
	if err != nil {
		return err
	}
	view := ClusterDescribeView{ClusterView: newClusterView(cluster)}

	pools, poolErr := listPools(cmd.Context(), f, client, target)
	if poolErr != nil {
		view.NodePoolsError = poolErr.Error()
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Warning: could not load the node pools of cluster %s: %s\n", target.Name, view.NodePoolsError)
	} else {
		views := newPoolSummaryViews(pools)
		view.NodePools = &views
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response: cluster:", view)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), view); wrote {
		return err
	}
	return printClusterDescribe(ioStreams.Out, &view)
}

func printClusterDescribe(w io.Writer, v *ClusterDescribeView) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	rows := [][2]string{
		{"Name", v.Name},
		{"Location", v.Location},
		{"Version", v.KubernetesVersion},
		{"Phase", v.Phase},
		{"Endpoint", cmdutil.TextColumn(v.ExternalEndpoint)},
		{"DNS suffix", cmdutil.TextColumn(v.DNSSuffix)},
		{"Network", v.NetworkID},
		{"Subnet", v.SubnetID},
		{"Pod CIDR", v.PodCIDR},
		{"Service CIDR", v.ServiceCIDR},
		{"Created", timeColumn(v.CreatedAt)},
	}
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s:\t%s\n", r[0], r[1])
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(v.Conditions) > 0 {
		_, _ = fmt.Fprintln(w, "\nConditions:")
		ctw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(ctw, "  TYPE\tSTATUS\tREASON\tMESSAGE")
		for i := range v.Conditions {
			c := &v.Conditions[i]
			_, _ = fmt.Fprintf(ctw, "  %s\t%s\t%s\t%s\n", c.Type, c.Status, cmdutil.TextColumn(c.Reason), cmdutil.TextColumn(c.Message))
		}
		if err := ctw.Flush(); err != nil {
			return err
		}
	}

	switch {
	case v.NodePools == nil:
		_, _ = fmt.Fprintln(w, "\nNode pools: unavailable (see the warning).")
	case len(*v.NodePools) == 0:
		_, _ = fmt.Fprintln(w, "\nNode pools: none. Add one with: verda mk8s node-pool create --cluster "+v.Name)
	default:
		_, _ = fmt.Fprintln(w, "\nNode pools:")
		ptw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(ptw, "  NAME\tINSTANCE TYPE\tREADY/DESIRED\tSTATE")
		for i := range *v.NodePools {
			p := &(*v.NodePools)[i]
			_, _ = fmt.Fprintf(ptw, "  %s\t%s\t%d/%d\t%s\n", p.Name, p.InstanceType, p.ReadyReplicas, p.Replicas, p.State)
		}
		return ptw.Flush()
	}
	return nil
}
