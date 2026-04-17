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

package auth

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdAuth creates the parent auth command.
func NewCmdAuth(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage shared credentials and profiles",
		Long: cmdutil.LongDesc(`
			Manage Verda shared credentials and the active auth profile.

			Configuration is resolved from multiple sources in order of precedence:
			  1. Command-line flags (highest priority)
			  2. Environment variables with VERDA_ prefix
			  3. Credentials file: ~/.verda/credentials or --credentials-file path
			  4. Built-in defaults
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdLogin(f, ioStreams),
		NewCmdUse(f, ioStreams),
		NewCmdShow(f, ioStreams),
	)

	return cmd
}
