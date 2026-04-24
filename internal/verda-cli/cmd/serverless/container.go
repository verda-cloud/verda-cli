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

package serverless

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// newCmdContainer creates the `verda serverless container` subcommand tree.
func newCmdContainer(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "container",
		Short: "Manage serverless container deployments (always-on endpoints)",
		Long: cmdutil.LongDesc(`
			Create and manage always-on serverless container deployments. Each
			deployment exposes an HTTPS endpoint that auto-scales based on queue
			load, CPU/GPU utilization, or manual replica limits.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		newCmdContainerCreate(f, ioStreams),
		newCmdContainerList(f, ioStreams),
		newCmdContainerDescribe(f, ioStreams),
		newCmdContainerDelete(f, ioStreams),
		newCmdContainerPause(f, ioStreams),
		newCmdContainerResume(f, ioStreams),
		newCmdContainerRestart(f, ioStreams),
		newCmdContainerPurgeQueue(f, ioStreams),
	)
	return cmd
}
