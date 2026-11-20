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
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdNodePoolList creates `verda mk8s node-pool list`.
func NewCmdNodePoolList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	scope := &poolScope{}
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the node pools of a cluster",
		Long: longDesc(`
			List the node pools of a cluster. READY/DESIRED is the number of
			workers that joined the cluster over the number you asked for.

			Without --cluster, a terminal session shows a picker.
		`),
		Example: cmdutil.Examples(`
			# Pick the cluster from a list (terminal only)
			verda mk8s node-pool list

			verda mk8s node-pool list --cluster prod

			# Short aliases, JSON for scripts
			verda mk8s np ls --cluster prod -l FIN-03 -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runNodePoolList(cmd, f, ioStreams, scope)
		},
	}
	scope.addFlags(cmd)
	return cmd
}

func runNodePoolList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, scope *poolScope) error {
	if err := scope.check(f, cmd, nil, ""); err != nil {
		return err
	}
	client, target, err := scope.connect(cmd, f, ioStreams)
	if err != nil || target == nil {
		return err
	}
	pools, err := listPools(cmd.Context(), f, client, target)
	if err != nil {
		return err
	}
	views := newPoolViews(pools)
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d node pool(s):", len(views)), views)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), views); wrote {
		return err
	}
	if len(views) == 0 {
		printNoPools(ioStreams, target.Name)
		return nil
	}

	now := time.Now()
	tw := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tINSTANCE TYPE\tREADY/DESIRED\tSTATE\tAGE")
	for i := range views {
		v := &views[i]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d/%d\t%s\t%s\n", v.Name, v.InstanceType, v.ReadyReplicas, v.Replicas, v.State, ageColumn(v.CreatedAt, now))
	}
	return tw.Flush()
}
