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

package s3

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdS3 creates the parent s3 command.
func NewCmdS3(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "s3",
		Short: "Manage S3 object storage",
		// Pre-release: hide from `verda --help`. Removal is a one-line change
		// when S3 ships GA. The env-var gate in cmd/cmd.go decides whether
		// the command is even registered; this Hidden flag covers the case
		// where it is registered (testers with VERDA_S3_ENABLED set).
		Hidden: true,
		Long: cmdutil.LongDesc(`
			Manage S3-compatible object storage credentials and operations.

			S3 credentials are stored separately from API credentials in the
			same credentials file (~/.verda/credentials) using verda_s3_ prefixed keys.

			Configure credentials:
			  verda s3 configure

			Show current S3 credential status:
			  verda s3 show
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdConfigure(f, ioStreams),
		NewCmdCp(f, ioStreams),
		NewCmdLs(f, ioStreams),
		NewCmdMb(f, ioStreams),
		NewCmdMv(f, ioStreams),
		NewCmdPresign(f, ioStreams),
		NewCmdRb(f, ioStreams),
		NewCmdRm(f, ioStreams),
		NewCmdShow(f, ioStreams),
		NewCmdSync(f, ioStreams),
	)

	return cmd
}
