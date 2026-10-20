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

package inferno

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdInferno creates the top-level `verda inferno` command tree.
func NewCmdInferno(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inferno",
		Short: "Manage Verda Inferno inference deployments",
		Long: cmdutil.LongDesc(`
			Build and deploy ML inference workloads using Verda Inferno. Inferno
			provides a high-performance inference server with automatic worker
			orchestration, health management, and queue-based scaling.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		newCmdInfernoInit(f, ioStreams),
		newCmdInfernoBuild(f, ioStreams),
		newCmdInfernoDeploy(f, ioStreams),
	)
	return cmd
}
