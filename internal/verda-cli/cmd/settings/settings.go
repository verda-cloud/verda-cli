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

package settings

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdSettings creates the parent settings command.
func NewCmdSettings(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Manage CLI settings",
		Long: cmdutil.LongDesc(`
			View and update CLI settings stored in ~/.verda/config.yaml.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdTheme(f, ioStreams),
	)

	return cmd
}
