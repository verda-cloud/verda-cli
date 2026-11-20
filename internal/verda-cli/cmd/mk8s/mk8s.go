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

// Package mk8s implements `verda mk8s`: managed Kubernetes clusters, node
// pools and kubeconfigs (experimental).
package mk8s

import (
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	outputTable = "table"

	statusAccepted  = "accepted"
	statusCompleted = "completed"
	statusUnchanged = "unchanged" // nothing was sent

	timeLayout = "2006-01-02 15:04"

	// experimentalCaution starts every Long help; the line break keeps 80 columns.
	experimentalCaution = "Experimental: the managed Kubernetes API may change in incompatible ways.\n" +
		"Commands, flags and output may change without a deprecation period."

	// Help text shared by several commands, so the same rule reads the same everywhere.
	yesUsage      = "Skip confirmation (required in scripts, agent mode and -o json|yaml)"
	clusterUsage  = "Cluster name (required in scripts, agent mode and -o json|yaml)"
	locationUsage = "Location of the cluster (found from the cluster name when omitted)"
	waitTimeUsage = "Maximum time to wait with --wait"
)

func longDesc(s string) string {
	return experimentalCaution + "\n\n" + cmdutil.LongDesc(s)
}

// NewCmdMK8s creates the `verda mk8s` parent command.
func NewCmdMK8s(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "mk8s",
		Aliases:     []string{"k8s", "kubernetes"},
		Short:       "Manage Kubernetes clusters, node pools and kubeconfigs",
		Annotations: map[string]string{cmdutil.TagAnnotation: "experimental"},
		Long: longDesc(`
			Create and manage managed Kubernetes clusters, their node pools
			(the worker VMs) and their kubeconfigs (client certificates for
			kubectl).

			Before you create a cluster, create a private network and a subnet
			in the cluster's location with verda network. mk8s takes their IDs
			and never creates them.

			Cluster and node pool names are unique in the project, so commands
			find a cluster's location from its name; -l gives the location and
			skips that lookup. Kubeconfigs are named by an ID that the API
			chooses.

			On a terminal with table output, a command asks for each value that
			you do not give, and asks for confirmation before it creates,
			changes or deletes anything. In scripts, agent mode and
			-o json|yaml it never asks: give the required flags, and --yes to
			delete, to scale a node pool down or to upgrade a cluster.

			The API does the work in the background after it accepts a
			request. Cluster and node pool create and node pool scale can wait
			for the result with --wait; kubeconfig create always waits.
			Deletes and cluster upgrade do not wait.
		`),
		Example: cmdutil.Examples(`
			# Locations where this project can create clusters
			verda mk8s locations

			# A cluster with one worker, then a kubeconfig for kubectl
			verda mk8s cluster create --name prod -l FIN-03 --kubernetes-version 1.37.0 --network $NETWORK_ID --subnet $SUBNET_ID --wait
			verda mk8s node-pool create --cluster prod --name workers --instance-type 1V100.6V --ssh-key $KEY_ID --wait
			verda mk8s kubeconfig create --cluster prod --common-name alice --admin
			export KUBECONFIG=~/.kube/verda-prod.alice.yaml
			kubectl get nodes
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdLocations(f, ioStreams),
		NewCmdCluster(f, ioStreams),
		NewCmdNodePool(f, ioStreams),
		NewCmdKubeconfig(f, ioStreams),
	)
	return cmd
}

// isInteractiveTerminal: stdin, stdout and stderr on a TTY; a var so tests can fake one.
var isInteractiveTerminal = func() bool {
	return term.IsTerminal(os.Stdin.Fd()) && cmdutil.IsStdoutTerminal() && cmdutil.IsStderrTerminal()
}

func interactiveTTY(f cmdutil.Factory) bool {
	return isInteractiveTerminal() && !f.AgentMode() && f.OutputFormat() == outputTable
}
