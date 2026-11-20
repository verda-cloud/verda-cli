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

// NewCmdNodePoolDelete creates `verda mk8s node-pool delete`.
func NewCmdNodePoolDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	scope := &poolScope{}
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete [<pool>]",
		Aliases: []string{"rm"},
		Short:   "Delete a node pool",
		Long: longDesc(`
			Delete a node pool. After the API accepts the request, it deletes
			the workers and their VMs in the background: the node pool is gone
			from node-pool list at once, but deleting the VMs takes longer. The
			command has no --wait.

			Without <pool> or --cluster, a terminal session shows a picker.

			Asks for confirmation on a terminal. --yes skips it and is required
			in scripts, agent mode and -o json|yaml.
		`),
		Example: cmdutil.Examples(`
			verda mk8s node-pool delete workers --cluster prod

			# Without prompts
			verda mk8s node-pool delete workers --cluster prod -l FIN-03 --yes
			verda --agent mk8s node-pool delete workers --cluster prod --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNodePoolDelete(cmd, f, ioStreams, scope, args, yes)
		},
	}
	scope.addFlags(cmd)
	cmd.Flags().BoolVarP(&yes, flagYes, "y", false, yesUsage)
	return cmd
}

func runNodePoolDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, scope *poolScope, args []string, yes bool) error {
	if err := checkPoolTarget(f, cmd, scope, args); err != nil {
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
	// A named pool is deleted without reading it first: a pool whose read fails must stay deletable.
	name, err := poolToDelete(cmd, f, ioStreams, client, target, args)
	if err != nil || name == "" {
		return err
	}

	if !yes {
		ok, err := confirmWarning(cmd.Context(), ioStreams, f.Prompter(),
			fmt.Sprintf("Deletes node pool %s and its worker VMs.", name),
			[]string{"This action cannot be undone."},
			fmt.Sprintf("Delete node pool %s (cluster %s)?", name, target.Name))
		if err != nil || !ok {
			return err
		}
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Delete node pool:", map[string]string{"name": name, "cluster": target.Name, "location": target.Location})
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	err = cmdutil.RunWithSpinner(ctx, f.Status(), "Deleting node pool...", func(ctx context.Context) error {
		return client.ManagedKubernetes.DeleteNodePool(ctx, target.Location, target.Name, name)
	})
	if err != nil {
		return mapErr(err)
	}

	result := ActionResult{Resource: kindNodePool, Name: name, Location: target.Location, Cluster: target.Name, Action: "delete", Status: statusAccepted}
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result); wrote {
		return err
	}
	_, _ = fmt.Fprintln(ioStreams.Out, "Deletion accepted. Worker VMs are deleted in the background.")
	return nil
}

// poolToDelete is args[0], or the pool the user picks; "" means canceled or no pool.
func poolToDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, t *clusterTarget, args []string) (string, error) {
	if len(args) > 0 {
		return strings.TrimSpace(args[0]), nil
	}
	pool, err := resolvePool(cmd, f, ioStreams, client, t, nil)
	if err != nil || pool == nil {
		return "", err
	}
	return pool.Name, nil
}
