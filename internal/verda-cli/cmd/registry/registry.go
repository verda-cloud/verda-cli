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

package registry

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdRegistry creates the parent `verda registry` command.
func NewCmdRegistry(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "registry",
		Aliases: []string{"vcr"},
		Short:   "Manage Verda Container Registry credentials and images",
		Hidden:  true, // pre-GA; also gated in cmd/cmd.go via VERDA_REGISTRY_ENABLED
		Long: cmdutil.LongDesc(`
			Manage Verda Container Registry (vccr.io) credentials, browse
			repositories, push local Docker images, and copy images between
			registries.

			Credentials are stored separately from API credentials in
			~/.verda/credentials using verda_registry_ prefixed keys.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}
	cmd.AddCommand(
		NewCmdConfigure(f, ioStreams),
		NewCmdShow(f, ioStreams),
		NewCmdLogin(f, ioStreams),
	)
	return cmd
}
