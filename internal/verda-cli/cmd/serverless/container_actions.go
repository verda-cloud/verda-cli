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
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// containerActionFn executes a lifecycle action on a named container deployment.
type containerActionFn func(ctx context.Context, client *verda.Client, name string) error

// newContainerActionCmd builds a `verda serverless container <verb>` command
// whose only behavior is to call the given SDK method with the resolved name.
// All four lifecycle commands (pause/resume/restart/purge-queue) share this shape.
func newContainerActionCmd(f cmdutil.Factory, ioStreams cmdutil.IOStreams, verb, short, spinner, successMsg string, destructive bool, fn containerActionFn) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   verb + " <name>",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveContainerName(cmd, f, ioStreams, args)
			if err != nil || name == "" {
				return err
			}
			return runContainerAction(cmd, f, ioStreams, name, verb, spinner, successMsg, destructive, yes, fn)
		},
	}
	if destructive {
		cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation (required in agent mode)")
	}
	return cmd
}

func runContainerAction(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, name, verb, spinner, successMsg string, destructive, yes bool, fn containerActionFn) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	if destructive {
		if f.AgentMode() && !yes {
			return cmdutil.NewConfirmationRequiredError(verb)
		}
		if !yes {
			confirmed, err := confirmDestructive(cmd.Context(), ioStreams, f.Prompter(),
				verb+" container deployment",
				fmt.Sprintf("Deployment %q will be %sd.", name, verb),
				fmt.Sprintf("%s %s?", verb, name),
			)
			if err != nil || !confirmed {
				_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
				return nil
			}
		}
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	err = cmdutil.RunWithSpinner(ctx, f.Status(), fmt.Sprintf("%s %s...", spinner, name), func() error {
		return fn(ctx, client, name)
	})
	if err != nil {
		return err
	}

	if f.AgentMode() {
		_, _ = cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), map[string]string{
			"name": name, "action": verb, "status": "completed",
		})
		return nil
	}
	_, _ = fmt.Fprintf(ioStreams.Out, successMsg+"\n", name)
	return nil
}

func newCmdContainerPause(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return newContainerActionCmd(f, ioStreams,
		"pause", "Pause a container deployment", "Pausing", "Paused deployment %q", false,
		func(ctx context.Context, c *verda.Client, name string) error {
			return c.ContainerDeployments.PauseDeployment(ctx, name)
		},
	)
}

func newCmdContainerResume(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return newContainerActionCmd(f, ioStreams,
		"resume", "Resume a paused container deployment", "Resuming", "Resumed deployment %q", false,
		func(ctx context.Context, c *verda.Client, name string) error {
			return c.ContainerDeployments.ResumeDeployment(ctx, name)
		},
	)
}

func newCmdContainerRestart(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return newContainerActionCmd(f, ioStreams,
		"restart", "Restart a container deployment", "Restarting", "Restarted deployment %q", true,
		func(ctx context.Context, c *verda.Client, name string) error {
			return c.ContainerDeployments.RestartDeployment(ctx, name)
		},
	)
}

func newCmdContainerPurgeQueue(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return newContainerActionCmd(f, ioStreams,
		"purge-queue", "Purge the pending-request queue for a container deployment", "Purging queue for", "Purged queue for deployment %q", true,
		func(ctx context.Context, c *verda.Client, name string) error {
			return c.ContainerDeployments.PurgeDeploymentQueue(ctx, name)
		},
	)
}
