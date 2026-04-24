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

// newCmdBatchjob creates the `verda serverless batchjob` subcommand tree.
func newCmdBatchjob(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batchjob",
		Short: "Manage serverless batch-job deployments (one-shot runs)",
		Long: cmdutil.LongDesc(`
			Create and manage one-shot batch-job deployments. Jobs accept queued
			requests, run each to completion within a deadline, and scale the
			worker pool up to a maximum replica count. Batch jobs cannot use
			spot compute.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		newCmdBatchjobCreate(f, ioStreams),
		newCmdBatchjobList(f, ioStreams),
		newCmdBatchjobDescribe(f, ioStreams),
		newCmdBatchjobDelete(f, ioStreams),
		newCmdBatchjobPause(f, ioStreams),
		newCmdBatchjobResume(f, ioStreams),
		newCmdBatchjobPurgeQueue(f, ioStreams),
	)
	return cmd
}
