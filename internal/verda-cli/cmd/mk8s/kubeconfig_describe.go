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

// NewCmdKubeconfigDescribe creates `verda mk8s kubeconfig describe`.
func NewCmdKubeconfigDescribe(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	scope := &poolScope{}
	cmd := &cobra.Command{
		Use:     "describe [<id>]",
		Aliases: []string{"get", "show"},
		Short:   "Show a kubeconfig",
		Long: longDesc(`
			Show a kubeconfig: who it is for, its groups, its status and when
			it expires. This never prints the kubeconfig file or its private
			key, in any output format. To write the file, use kubeconfig save.

			Without <id> or --cluster, a terminal session shows a picker.
		`),
		Example: cmdutil.Examples(`
			# Pick the cluster and the kubeconfig from lists (terminal only)
			verda mk8s kubeconfig describe

			verda mk8s kubeconfig describe $KUBECONFIG_ID --cluster prod
			verda mk8s kubeconfig describe $KUBECONFIG_ID --cluster prod -o json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKubeconfigDescribe(cmd, f, ioStreams, scope, args)
		},
	}
	scope.addFlags(cmd)
	return cmd
}

func runKubeconfigDescribe(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, scope *poolScope, args []string) error {
	if err := checkKubeconfigTarget(f, cmd, scope, args); err != nil {
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
	view, err := resolveKubeconfig(cmd, f, ioStreams, client, target, args, true)
	if err != nil || view == nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response: kubeconfig:", view)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), view); wrote {
		return err
	}
	return printKubeconfigDescribe(ioStreams.Out, target, view)
}

func printKubeconfigDescribe(w io.Writer, t *clusterTarget, v *KubeconfigView) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	rows := [][2]string{
		{"ID", v.ID},
		{"Display name", cmdutil.TextColumn(v.DisplayName)},
		{"Common name", v.CommonName},
		{"Groups", groupsColumn(v.Groups)},
		{"Status", v.Status},
		{"Expires", expiresColumn(v.NotAfter)},
		{"Cluster", t.Name},
		{"Location", t.Location},
	}
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s:\t%s\n", r[0], r[1])
	}
	return tw.Flush()
}
