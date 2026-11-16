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

package network

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdSubnetUpdate creates the subnet update command.
func NewCmdSubnetUpdate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &updateOptions{}
	var networkArg string
	cmd := &cobra.Command{
		Use:   "update [<subnet>]",
		Short: "Rename a subnet or make it the default of its location",
		Long: cmdutil.LongDesc(`
			Rename a subnet and/or make it the default subnet of its
			location. The CIDR cannot change: create a new subnet instead.
			--default only sets (--default=false is rejected).

			On a terminal with no change flags, pick the changes from a
			list. In scripts and agent mode, pass --name and/or --default.
		`),
		Example: cmdutil.Examples(`
			verda network subnet update sn_82gp8jck --name web
			verda network subnet update web --network prod --default
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.nameSet = cmd.Flags().Changed(flagName)
			opts.defaultSet = cmd.Flags().Changed(flagDefault)
			return runSubnetUpdate(cmd, f, ioStreams, opts, networkArg, args)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&networkArg, flagNetwork, "", "Network ID or name (needed when the subnet name is not unique)")
	flags.StringVar(&opts.Name, flagName, "", "New name (1-64 characters)")
	flags.BoolVar(&opts.Default, flagDefault, false, "Make this the default subnet of its location")
	return cmd
}

func runSubnetUpdate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *updateOptions, networkArg string, args []string) error {
	if err := validateUpdateFlags(opts, "subnet"); err != nil {
		return err
	}
	if !interactiveTTY(f) {
		if err := missingUpdateFlags(opts, args, argSubnet); err != nil {
			return err
		}
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	network, subnet, err := resolveSubnetArg(cmd, f, ioStreams, client, networkArg, args)
	if err != nil || subnet == nil {
		return err
	}

	if !opts.hasChanges() {
		if err := runChangeFlow(cmd.Context(), f, ioStreams, opts, subnet.Name, subnet.IsDefault); err != nil {
			return err
		}
		ok, err := confirmUpdate(cmd.Context(), f, ioStreams, opts, "subnet", subnet.Name)
		if err != nil || !ok {
			return err
		}
	}

	req := verda.UpdateSubnetRequest{}
	if opts.nameSet {
		req.Name = &opts.Name
	}
	if opts.defaultSet {
		req.IsDefault = &opts.Default
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	updated, err := cmdutil.WithSpinner(ctx, f.Status(), "Updating subnet...", func(ctx context.Context) (*verda.Subnet, error) {
		return client.PrivateNetworks.UpdateSubnet(ctx, network.ID, subnet.ID, req)
	})
	if err != nil {
		return err
	}
	view := cmdutil.NewSubnetView(network.ID, updated)
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), view); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Updated subnet %s (%s) in %s%s\n", view.Name, view.ID, view.Location, defaultSuffix(view.IsDefault))
	return nil
}
