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
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdNodePoolDescribe creates `verda mk8s node-pool describe`.
func NewCmdNodePoolDescribe(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	scope := &poolScope{}
	cmd := &cobra.Command{
		Use:     "describe [<pool>]",
		Aliases: []string{"get", "show"},
		Short:   "Show a node pool",
		Long: longDesc(`
			Show a node pool: its instance type, worker counts, state, SSH key
			IDs and, when it is not healthy, the reasons the API reports.

			Without <pool> or --cluster, a terminal session shows a picker.
		`),
		Example: cmdutil.Examples(`
			# Pick the cluster and the node pool from lists (terminal only)
			verda mk8s node-pool describe

			verda mk8s node-pool describe workers --cluster prod

			# JSON for scripts: state, worker counts and errors[]
			verda mk8s node-pool describe workers --cluster prod -o json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNodePoolDescribe(cmd, f, ioStreams, scope, args)
		},
	}
	scope.addFlags(cmd)
	return cmd
}

func runNodePoolDescribe(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, scope *poolScope, args []string) error {
	if err := checkPoolTarget(f, cmd, scope, args); err != nil {
		return err
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	target, err := scope.resolve(cmd, f, ioStreams, client)
	if err != nil || target == nil {
		return err
	}
	pool, err := resolvePool(cmd, f, ioStreams, client, target, args)
	if err != nil || pool == nil {
		return err
	}
	view := newPoolView(pool)
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response: node pool:", view)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), view); wrote {
		return err
	}
	return printPoolDescribe(ioStreams.Out, target, &view)
}

func printPoolDescribe(w io.Writer, t *clusterTarget, v *PoolView) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	rows := [][2]string{
		{"Name", v.Name},
		{"Cluster", t.Name},
		{"Location", t.Location},
		{"Instance type", v.InstanceType},
		{"Workers", fmt.Sprintf("%d/%d ready", v.ReadyReplicas, v.Replicas)},
		{"State", v.State},
		{"SSH keys", cmdutil.TextColumn(strings.Join(v.SSHKeyIDs, ", "))},
		{"Created", timeColumn(v.CreatedAt)},
	}
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s:\t%s\n", r[0], r[1])
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(v.Errors) == 0 {
		return nil
	}
	_, _ = fmt.Fprintln(w, "\nErrors:")
	etw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(etw, "  REASON\tMESSAGE\tSINCE")
	for i := range v.Errors {
		e := &v.Errors[i]
		_, _ = fmt.Fprintf(etw, "  %s\t%s\t%s\n", e.Reason, cmdutil.TextColumn(e.Message), timeColumn(e.Since))
	}
	return etw.Flush()
}
