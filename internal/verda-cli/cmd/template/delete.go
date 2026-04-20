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

// NewCmdDelete creates the template delete command.
func NewCmdDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete [resource/name]",
		Aliases: []string{"rm"},
		Short:   "Delete a saved template",
		Long: cmdutil.LongDesc(`
			Delete a saved resource configuration template.
			Without arguments, shows an interactive picker with confirmation.
			The argument must be in resource/name format (e.g. vm/gpu-training).
		`),
		Example: cmdutil.Examples(`
			# Interactive picker
			verda template delete

			# Delete a VM template
			verda template delete vm/gpu-training

			# Short alias
			verda tmpl rm vm/gpu-training
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runDelete(cmd, f, ioStreams, args[0])
			}
			return runDeleteInteractive(cmd, f, ioStreams)
		},
	}

	return cmd
}

func runDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, ref string) error {
	resource, name, err := parseRef(ref)
	if err != nil {
		return err
	}

	baseDir, err := cmdutil.TemplatesBaseDir()
	if err != nil {
		return err
	}

	// Verify template exists by loading it first.
	if _, err := Load(baseDir, resource, name); err != nil {
		return err
	}

	// Confirm deletion.
	prompter := f.Prompter()
	confirmed, err := prompter.Confirm(cmd.Context(), fmt.Sprintf("Delete template %s/%s?", resource, name))
	if err != nil {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return nil //nolint:nilerr // user cancellation (Ctrl+C) is not an error
	}
	if !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
		return nil
	}

	if err := Delete(baseDir, resource, name); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted template: %s/%s\n", resource, name)
	return nil
}

func runDeleteInteractive(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	entry, err := pickTemplateEntry(cmd, f)
	if err != nil {
		return err
	}
	if entry == nil {
		return nil // user canceled
	}
	return runDelete(cmd, f, ioStreams, entry.Resource+"/"+entry.Name)
}
