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

package vm

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdDescribe creates the vm describe cobra command.
func NewCmdDescribe(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "describe <instance-id>",
		Aliases: []string{"get", "show"},
		Short:   "Show detailed information about a VM instance",
		Long: cmdutil.LongDesc(`
			Display detailed information about a single VM instance,
			including compute specs, networking, and attached volumes.
		`),
		Example: cmdutil.Examples(`
			verda vm describe abc-123-def
			verda vm show abc-123-def
			verda vm describe abc-123-def -o json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var instanceID string
			if len(args) > 0 {
				instanceID = args[0]
			}
			return runDescribe(cmd, f, ioStreams, instanceID)
		},
	}
	return cmd
}

func runDescribe(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, instanceID string) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	// Interactive picker when no ID specified.
	if instanceID == "" {
		selected, err := selectInstance(cmd.Context(), f, ioStreams, client)
		if err != nil {
			return err
		}
		if selected == "" {
			return nil
		}
		instanceID = selected
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	inst, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading instance...", func() (*verda.Instance, error) {
		return client.Instances.GetByID(ctx, instanceID)
	})
	if err != nil {
		return fmt.Errorf("fetching instance: %w", err)
	}

	volumes := fetchInstanceVolumes(ctx, client, inst)

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Instance details:", inst)

	// Structured output.
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), inst); wrote {
		return err
	}

	// Human-readable card.
	_, _ = fmt.Fprint(ioStreams.Out, renderInstanceCard(inst, volumes...))
	return nil
}
