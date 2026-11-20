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

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type clusterUpgradeOptions struct {
	Location string
	Version  string
	Yes      bool
}

// ClusterUpgradeResult is the structured output of `cluster upgrade`.
type ClusterUpgradeResult struct {
	Status          string      `json:"status"           yaml:"status"`
	PreviousVersion string      `json:"previous_version" yaml:"previous_version"`
	Cluster         ClusterView `json:"cluster"          yaml:"cluster"`
}

// NewCmdClusterUpgrade creates `verda mk8s cluster upgrade`.
func NewCmdClusterUpgrade(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &clusterUpgradeOptions{}
	cmd := &cobra.Command{
		Use:   "upgrade [<cluster>]",
		Short: "Upgrade a cluster to a new Kubernetes version",
		Long: longDesc(`
			Change the Kubernetes version of a cluster. The API runs the change
			as a rolling update. The command returns once the API accepts the
			request. It has no --wait: the API reports the requested version,
			not the running one, so the CLI cannot tell when the rollout ends.
			Follow the rollout with cluster describe.

			Without <cluster>, a terminal session shows a picker. Without
			--kubernetes-version, it shows the current version and asks for
			the new one; in scripts, agent mode and -o json|yaml the flag is
			required.

			Asks for confirmation on a terminal. --yes skips it and is required
			in scripts, agent mode and -o json|yaml.
		`),
		Example: cmdutil.Examples(`
			# Pick the cluster and type the version (terminal only)
			verda mk8s cluster upgrade

			verda mk8s cluster upgrade prod --kubernetes-version 1.38.0

			# Without prompts
			verda mk8s cluster upgrade prod -l FIN-03 --kubernetes-version 1.38.0 --yes
			verda --agent mk8s cluster upgrade prod --kubernetes-version 1.38.0 --yes
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterUpgrade(cmd, f, ioStreams, args, opts)
		},
	}
	addLocationFlag(cmd, &opts.Location)
	cmd.Flags().StringVar(&opts.Version, flagKubernetesVersion, "", "New Kubernetes version, for example 1.38.0 (the API checks it)")
	cmd.Flags().BoolVarP(&opts.Yes, flagYes, "y", false, yesUsage)
	return cmd
}

func runClusterUpgrade(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, args []string, opts *clusterUpgradeOptions) error {
	name := argOrEmpty(args)
	opts.Version = strings.TrimSpace(opts.Version)
	if missing := upgradeMissing(f, name, len(args) > 0, opts.Version); len(missing) > 0 {
		return cmdutil.NewMissingFlagsError(missing)
	}
	if err := checkConfirmed(f, opts.Yes, "upgrade"); err != nil {
		return err
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	target, err := resolveCluster(cmd, f, ioStreams, client, name, opts.Location)
	if err != nil || target == nil {
		return err
	}

	current, err := getCluster(cmd.Context(), f, client, target)
	if err != nil {
		return err
	}

	if opts.Version == "" {
		version, ok, err := askVersion(cmd.Context(), f, ioStreams, current)
		if err != nil || !ok {
			return err
		}
		opts.Version = version
	}
	if !opts.Yes {
		ok, err := confirmWarning(cmd.Context(), ioStreams, f.Prompter(),
			fmt.Sprintf("Rolling upgrade from %s to %s", current.KubernetesVersion, opts.Version),
			[]string{fmt.Sprintf("The API upgrades cluster %s in %s as a rolling update.", current.Name, current.Location)},
			fmt.Sprintf("Upgrade cluster %s to %s?", current.Name, opts.Version))
		if err != nil || !ok {
			return err
		}
	}

	req := verda.UpdateManagedKubernetesClusterRequest{KubernetesVersion: opts.Version}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)
	updated, err := apiCall(cmd.Context(), f, "Upgrading cluster...", func(ctx context.Context) (*verda.ManagedKubernetesCluster, error) {
		return client.ManagedKubernetes.Update(ctx, current.Location, current.Name, req)
	})
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", newClusterView(updated))

	result := ClusterUpgradeResult{Status: statusAccepted, PreviousVersion: current.KubernetesVersion, Cluster: newClusterView(updated)}
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Upgrade of cluster %s from %s to %s accepted (%s).\n",
		result.Cluster.Name, result.PreviousVersion, opts.Version, result.Cluster.Location)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "Follow the rollout with: %s\n", describeCmd(result.Cluster.Name, result.Cluster.Location))
	_, _ = fmt.Fprintln(ioStreams.ErrOut, "`describe` may show `degraded` while the rollout runs.")
	return nil
}

func upgradeMissing(f cmdutil.Factory, name string, given bool, version string) []string {
	var missing []string
	if clusterArgMissing(f, name, given) {
		missing = append(missing, argCluster)
	}
	if version == "" && !interactiveTTY(f) {
		missing = append(missing, "--"+flagKubernetesVersion)
	}
	return missing
}

// askVersion: ok is false when the user canceled.
func askVersion(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, current *verda.ManagedKubernetesCluster) (version string, ok bool, err error) {
	v, err := f.Prompter().TextInput(ctx, fmt.Sprintf("New Kubernetes version (cluster %s runs %s)", current.Name, current.KubernetesVersion),
		tui.WithPlaceholder("1.38.0"),
		tui.WithValidation(validateVersion))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			printCanceled(ioStreams)
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(v), true, nil
}
