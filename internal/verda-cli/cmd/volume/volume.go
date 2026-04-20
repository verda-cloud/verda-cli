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

package volume

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdVolume creates the parent volume command.
func NewCmdVolume(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "volume",
		Aliases: []string{"vol"},
		Short:   "Manage volumes",
		Long: cmdutil.LongDesc(`
			List and manage Verda block storage volumes.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdCreate(f, ioStreams),
		NewCmdList(f, ioStreams),
		NewCmdDescribe(f, ioStreams),
		NewCmdAction(f, ioStreams),
		NewCmdTrash(f, ioStreams),
		NewCmdDelete(f, ioStreams),
	)
	return cmd
}
