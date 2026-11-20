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
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdKubeconfig creates the `verda mk8s kubeconfig` parent command.
func NewCmdKubeconfig(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Manage the kubeconfigs of a cluster",
		Long: longDesc(`
			Create, save, list, show and delete the kubeconfigs of a cluster.
			A kubeconfig holds a client certificate that kubectl uses to reach
			the cluster. Its YAML holds a private key: the CLI writes it only to
			a file with mode 0600, or to stdout with --file -, and never prints
			it in structured output or --debug.

			Kubeconfigs are named by an ID that the API chooses (see kubeconfig
			list). Deleting a kubeconfig does not revoke the copies already
			downloaded: they keep working until they expire.
		`),
		Example: cmdutil.Examples(`
			# Admin access for alice, then use it with kubectl
			verda mk8s kubeconfig create --cluster prod --common-name alice --admin
			export KUBECONFIG=~/.kube/verda-prod.alice.yaml
			kubectl get nodes

			verda mk8s kubeconfig list --cluster prod
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		NewCmdKubeconfigList(f, ioStreams),
		NewCmdKubeconfigDescribe(f, ioStreams),
		NewCmdKubeconfigCreate(f, ioStreams),
		NewCmdKubeconfigSave(f, ioStreams),
		NewCmdKubeconfigDelete(f, ioStreams),
	)
	return cmd
}
