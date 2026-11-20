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

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdClusterDelete creates `verda mk8s cluster delete`.
func NewCmdClusterDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var location string
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete [<cluster>]",
		Aliases: []string{"rm"},
		Short:   "Delete a cluster with its node pools and kubeconfigs",
		Long: longDesc(`
			Delete a cluster. After the API accepts the request, it deletes the
			node pools, their worker VMs and the kubeconfigs in the background:
			the cluster is gone from cluster list at once, but deleting the VMs
			takes longer. Kubeconfigs that you downloaded stop working. The
			private network and the subnet are kept. The CLI does not check for
			node pools first, and has no --wait.

			Without <cluster>, a terminal session shows a picker.

			Asks for confirmation on a terminal. --yes skips it and is required
			in scripts, agent mode and -o json|yaml.
		`),
		Example: cmdutil.Examples(`
			verda mk8s cluster delete prod

			# Without prompts
			verda mk8s cluster delete prod -l FIN-03 --yes
			verda --agent mk8s cluster delete prod --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterDelete(cmd, f, ioStreams, args, location, yes)
		},
	}
	addLocationFlag(cmd, &location)
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, yesUsage)
	return cmd
}

func runClusterDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string, location string, yes bool) error {
	name := argOrEmpty(args)
	if err := checkClusterArg(f, name, len(args) > 0, argCluster); err != nil {
		return err
	}
	if err := checkConfirmed(f, yes, "delete"); err != nil {
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

	if !yes {
		ok, err := confirmWarning(cmd.Context(), ioStreams, f.Prompter(),
			fmt.Sprintf("Deletes cluster %s, all its node pools, their worker VMs and its kubeconfigs.", target.Name),
			[]string{"Downloaded kubeconfigs stop working.", "This action cannot be undone."},
			fmt.Sprintf("Delete cluster %s (%s)?", target.Name, target.Location))
		if err != nil || !ok {
			return err
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete cluster:", map[string]string{"name": target.Name, "location": target.Location})
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	err = cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting cluster...", func(ctx context.Context) error {
		return client.ManagedKubernetes.Delete(ctx, target.Location, target.Name)
	})
	if err != nil {
		return mapErr(err)
	}

	result := ActionResult{Resource: kindCluster, Name: target.Name, Location: target.Location, Action: "delete", Status: statusAccepted}
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result); wrote {
		return err
	}
	_, _ = fmt.Fprintln(ioStreams.Out, "Deletion accepted. Node pools and worker VMs are deleted in the background.")
	return nil
}
