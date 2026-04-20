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

package template

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdTemplate creates the parent template command.
func NewCmdTemplate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "template",
		Aliases: []string{"tmpl"},
		Short:   "Manage reusable resource templates",
		Long: cmdutil.LongDesc(`
			Save, list, show, and delete reusable resource configuration templates.
			Templates pre-fill the create wizard so you don't repeat the same settings.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdCreate(f, ioStreams),
		NewCmdEdit(f, ioStreams),
		NewCmdList(f, ioStreams),
		NewCmdShow(f, ioStreams),
		NewCmdDelete(f, ioStreams),
	)
	return cmd
}
