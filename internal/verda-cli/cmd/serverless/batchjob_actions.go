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

type batchjobActionFn func(ctx context.Context, client *verda.Client, name string) error

func newBatchjobActionCmd(f cmdutil.Factory, ioStreams cmdutil.IOStreams, verb, short, spinner, successMsg string, destructive bool, fn batchjobActionFn) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   verb + " <name>",
		Short: short,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveBatchjobName(cmd, f, ioStreams, args)
			if err != nil || name == "" {
				return err
			}
			return runBatchjobAction(cmd, f, ioStreams, name, verb, spinner, successMsg, destructive, yes, fn)
		},
	}
	if destructive {
		cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation (required in agent mode)")
	}
	return cmd
}

func runBatchjobAction(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, name, verb, spinner, successMsg string, destructive, yes bool, fn batchjobActionFn) error {
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
				verb+" batch-job deployment",
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

func newCmdBatchjobPause(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return newBatchjobActionCmd(f, ioStreams,
		"pause", "Pause a batch-job deployment", "Pausing", "Paused deployment %q", false,
		func(ctx context.Context, c *verda.Client, name string) error {
			return c.ServerlessJobs.PauseJobDeployment(ctx, name)
		},
	)
}

func newCmdBatchjobResume(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return newBatchjobActionCmd(f, ioStreams,
		"resume", "Resume a paused batch-job deployment", "Resuming", "Resumed deployment %q", false,
		func(ctx context.Context, c *verda.Client, name string) error {
			return c.ServerlessJobs.ResumeJobDeployment(ctx, name)
		},
	)
}

func newCmdBatchjobPurgeQueue(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return newBatchjobActionCmd(f, ioStreams,
		"purge-queue", "Purge the pending-request queue for a batch-job deployment", "Purging queue for", "Purged queue for deployment %q", true,
		func(ctx context.Context, c *verda.Client, name string) error {
			return c.ServerlessJobs.PurgeJobDeploymentQueue(ctx, name)
		},
	)
}
