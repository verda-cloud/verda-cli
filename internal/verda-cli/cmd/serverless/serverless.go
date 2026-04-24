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

// NewCmdServerless creates the parent `verda serverless` command.
func NewCmdServerless(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serverless",
		Short: "Manage serverless container and batch-job deployments",
		Long: cmdutil.LongDesc(`
			Deploy and manage serverless container endpoints and one-shot batch
			jobs on Verda Cloud. Container deployments run continuously and scale
			with demand; batch jobs run to completion on a deadline.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		newCmdContainer(f, ioStreams),
		newCmdBatchjob(f, ioStreams),
	)
	return cmd
}
