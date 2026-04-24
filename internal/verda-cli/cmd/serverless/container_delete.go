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

package serverless

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newCmdContainerDelete(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	var yes bool
	var timeoutMs int

	cmd := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm", "del"},
		Short:   "Delete a container deployment",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveContainerName(cmd, f, ioStreams, args)
			if err != nil || name == "" {
				return err
			}
			return runContainerDelete(cmd, f, ioStreams, name, yes, timeoutMs)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation (required in agent mode)")
	cmd.Flags().IntVar(&timeoutMs, "timeout-ms", -1, "Server-side wait timeout in ms (0 to skip wait; negative uses API default)")
	return cmd
}

func runContainerDelete(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, name string, yes bool, timeoutMs int) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	if f.AgentMode() && !yes {
		return cmdutil.NewConfirmationRequiredError("delete")
	}
	if !yes {
		confirmed, err := confirmDestructive(
			cmd.Context(), ioStreams, f.Prompter(),
			"Delete container deployment",
			fmt.Sprintf("Deployment %q will stop serving requests immediately.", name),
			fmt.Sprintf("Delete %s?", name),
		)
		if err != nil || !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	err = cmdutil.RunWithSpinner(ctx, f.Status(), fmt.Sprintf("Deleting %s...", name), func() error {
		return client.ContainerDeployments.DeleteDeployment(ctx, name, timeoutMs)
	})
	if err != nil {
		return err
	}

	if f.AgentMode() {
		result := map[string]string{"name": name, "action": "delete", "status": "completed"}
		_, _ = cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result)
		return nil
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Deleted deployment %q\n", name)
	return nil
}
