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

package sshkey

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdList creates the ssh-key list cobra command.
func NewCmdList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List SSH keys",
		Long: cmdutil.LongDesc(`
			List all SSH keys in your account, showing Name, ID, and Fingerprint.
		`),
		Example: cmdutil.Examples(`
			verda ssh-key list
			verda ssh-key ls
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, f, ioStreams)
		},
	}
	return cmd
}

func runList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading SSH keys...")
	}
	keys, err := client.SSHKeys.GetAllSSHKeys(ctx)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d SSH key(s):", len(keys)), keys)

	views := cmdutil.NewSSHKeyViews(keys)

	// Structured output: emit JSON/YAML and return.
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), views); wrote {
		return err
	}

	if len(views) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No SSH keys found.")
		return nil
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "  %d SSH key(s) found\n\n", len(views))
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-36s  %s\n", "NAME", "ID", "FINGERPRINT")
	_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-36s  %s\n", "----", "--", "-----------")
	for i := range views {
		v := &views[i]
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-20s  %-36s  %s\n", v.Name, v.ID, cmdutil.TextColumn(v.Fingerprint))
	}
	return nil
}
