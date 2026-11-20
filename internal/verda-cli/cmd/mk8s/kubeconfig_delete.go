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
	"time"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdKubeconfigDelete creates `verda mk8s kubeconfig delete`.
func NewCmdKubeconfigDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	scope := &poolScope{}
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete [<id>]",
		Aliases: []string{"rm"},
		Short:   "Delete a kubeconfig",
		Long: longDesc(`
			Delete a kubeconfig. This does not revoke it: copies that were
			already downloaded keep working until they expire (EXPIRES in
			kubeconfig list, not_after in JSON). Deleting the cluster
			invalidates them; a copy bound to a group can also be cut off by
			removing that group's RBAC binding in the cluster.

			Without <id> or --cluster, a terminal session shows a picker.

			Asks for confirmation on a terminal. --yes skips it and is required
			in scripts, agent mode and -o json|yaml.
		`),
		Example: cmdutil.Examples(`
			verda mk8s kubeconfig delete $KUBECONFIG_ID --cluster prod

			# Without prompts
			verda mk8s kubeconfig delete $KUBECONFIG_ID --cluster prod -l FIN-03 --yes
			verda --agent mk8s kubeconfig delete $KUBECONFIG_ID --cluster prod --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKubeconfigDelete(cmd, f, ioStreams, scope, args, yes)
		},
	}
	scope.addFlags(cmd)
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, yesUsage)
	return cmd
}

func runKubeconfigDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, scope *poolScope, args []string, yes bool) error {
	if err := checkKubeconfigTarget(f, cmd, scope, args); err != nil {
		return err
	}
	if err := checkConfirmed(f, yes, "delete"); err != nil {
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
	// A named id is deleted without reading it first, like node pools: one that cannot be read must stay deletable.
	kc, err := resolveKubeconfig(cmd, f, ioStreams, client, target, args, false)
	if err != nil || kc == nil {
		return err
	}

	if !yes {
		ok, err := confirmWarning(cmd.Context(), ioStreams, f.Prompter(),
			fmt.Sprintf("Deletes kubeconfig %s.", kc.ID),
			[]string{
				"Copies already downloaded are NOT revoked: they keep working " + validUntil(kc.NotAfter) + ".",
				copiesNote,
				"This action cannot be undone.",
			},
			fmt.Sprintf("Delete kubeconfig %s (cluster %s)?", kc.ID, target.Name))
		if err != nil || !ok {
			return err
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete kubeconfig:", map[string]string{"id": kc.ID, "cluster": target.Name, "location": target.Location})
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	err = cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting kubeconfig...", func(ctx context.Context) error {
		return client.ManagedKubernetes.DeleteKubeconfig(ctx, target.Location, target.Name, kc.ID)
	})
	if err != nil {
		return mapErr(err)
	}

	result := ActionResult{Resource: kindKubeconfig, ID: kc.ID, Name: kc.ID, Location: target.Location, Cluster: target.Name, Action: "delete", Status: statusAccepted}
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Deletion accepted. Copies already downloaded are not revoked and still work %s. %s\n", validUntil(kc.NotAfter), copiesNote)
	return nil
}

// copiesNote: the CLI cannot revoke a certificate. system:masters is bound to cluster-admin by the control plane itself, so no RBAC change cuts that group off.
const copiesNote = "Deleting the cluster invalidates them. A copy that does not use system:masters can also be cut off by removing its group's RBAC binding in the cluster."

// validUntil: the date is known only when the id came from the picker; a named id is not read first.
func validUntil(notAfter *time.Time) string {
	if notAfter == nil {
		return "until they expire"
	}
	return "until " + utcTime(*notAfter)
}
