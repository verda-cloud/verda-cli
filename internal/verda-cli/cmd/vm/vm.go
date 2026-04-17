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

package vm

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdVM creates the parent VM command.
func NewCmdVM(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "vm",
		Aliases: []string{"instance", "instances"},
		Short:   "Manage virtual machines",
		Long: cmdutil.LongDesc(`
			Create and manage Verda virtual machine instances.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdCreate(f, ioStreams),
		NewCmdList(f, ioStreams),
		NewCmdDescribe(f, ioStreams),
		NewCmdAction(f, ioStreams),
		NewCmdAvailability(f, ioStreams),
	)

	// Shortcut commands for common actions.
	for _, def := range shortcuts {
		cmd.AddCommand(newShortcutCmd(f, ioStreams, def))
	}
	return cmd
}
