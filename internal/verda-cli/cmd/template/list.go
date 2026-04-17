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
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdList creates the template list command.
func NewCmdList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var resourceType string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List saved templates",
		Long: cmdutil.LongDesc(`
			List all saved resource configuration templates.
			Use --type to filter by resource type (e.g. "vm").
		`),
		Example: cmdutil.Examples(`
			# List all templates
			verda template list

			# List only VM templates
			verda template list --type vm

			# Short alias
			verda tmpl ls
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(f, ioStreams, resourceType)
		},
	}

	cmd.Flags().StringVar(&resourceType, "type", "", "Filter by resource type (e.g. vm)")
	return cmd
}

func runList(f cmdutil.Factory, ioStreams cmdutil.IOStreams, resourceType string) error {
	baseDir, err := cmdutil.TemplatesBaseDir()
	if err != nil {
		return err
	}

	var entries []Entry
	if resourceType != "" {
		entries, err = List(baseDir, resourceType)
	} else {
		entries, err = ListAll(baseDir)
	}
	if err != nil {
		return err
	}

	// Structured output: emit JSON/YAML and return.
	if wrote, wErr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), entries); wrote {
		return wErr
	}

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No templates found.")
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %-25s  %s\n", "NAME", "DESCRIPTION")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-25s  %s\n", "----", "-----------")
	for _, e := range entries {
		displayName := e.Resource + "/" + e.Name
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-25s  %s\n", displayName, e.Description)
	}
	return nil
}
