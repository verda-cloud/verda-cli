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

// NewCmdCluster creates the `verda mk8s cluster` parent command.
func NewCmdCluster(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage clusters",
		Long: longDesc(`
			List, show, create, upgrade and delete managed Kubernetes clusters.
			Cluster names are unique in the project, so commands find a
			cluster's location from its name; -l gives the location and skips
			that lookup.
		`),
		Example: cmdutil.Examples(`
			verda mk8s cluster list
			verda mk8s cluster describe prod
			verda mk8s cluster create --name prod -l FIN-03 --kubernetes-version 1.37.0 --network $NETWORK_ID --subnet $SUBNET_ID
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		NewCmdClusterList(f, ioStreams),
		NewCmdClusterDescribe(f, ioStreams),
		NewCmdClusterCreate(f, ioStreams),
		NewCmdClusterUpgrade(f, ioStreams),
		NewCmdClusterDelete(f, ioStreams),
	)
	return cmd
}
