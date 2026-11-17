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
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const flagMode = "mode"

type createOptions struct {
	Name       string
	Mode       string
	Default    bool
	modeSet    bool
	defaultSet bool
}

// NewCmdCreate creates the network create command.
func NewCmdCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &createOptions{Mode: verda.NetworkModeCustom}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a private network",
		Long: cmdutil.LongDesc(`
			Create a private network. On a terminal, omitted values are
			asked for; in scripts and agent mode --name is required.

			Mode "custom" (the default) means you create the subnets.
			Mode "auto" matters only for the default network: a deploy
			that names no subnet gets one in its location, from that
			location's range. Otherwise, create the subnets yourself.
			--default makes the new network the one VMs join when they
			name no network.
		`),
		Example: cmdutil.Examples(`
			verda network create
			verda network create --name prod
			verda network create --name staging --mode auto --default
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.modeSet = cmd.Flags().Changed(flagMode)
			opts.defaultSet = cmd.Flags().Changed(flagDefault)
			return runCreate(cmd, f, ioStreams, opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.Name, flagName, "", "Network name (1-64 characters, unique in the project)")
	flags.StringVar(&opts.Mode, flagMode, verda.NetworkModeCustom, "Subnet mode: custom or auto")
	flags.BoolVar(&opts.Default, flagDefault, false, "Make this the default network")
	return cmd
}

func runCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *createOptions) error {
	opts.Name = strings.TrimSpace(opts.Name)
	if opts.Name == "" {
		if !interactiveTTY(f) {
			return cmdutil.NewMissingFlagsError([]string{"--" + flagName})
		}
		engine := wizard.NewEngine(f.Prompter(), f.Status(), wizard.WithOutput(ioStreams.ErrOut))
		if err := engine.Run(cmd.Context(), buildNetworkCreateFlow(opts)); err != nil {
			return err
		}
		opts.Name = strings.TrimSpace(opts.Name)
	}
	if err := validateName(opts.Name); err != nil {
		return cmdutil.NewValidationError("name", err.Error())
	}
	if opts.Mode != verda.NetworkModeCustom && opts.Mode != verda.NetworkModeAuto {
		return cmdutil.NewValidationError("mode", "must be custom or auto")
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	req := verda.CreatePrivateNetworkRequest{Name: opts.Name, Mode: opts.Mode, IsDefault: opts.Default}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	network, err := cmdutil.WithSpinner(ctx, f.Status(), "Creating private network...", func(ctx context.Context) (*verda.PrivateNetwork, error) {
		return client.PrivateNetworks.Create(ctx, req)
	})
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", network)

	view := newNetworkView(network)
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), view); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Created private network %s (%s), mode %s", view.Name, view.ID, view.Mode)
	if view.IsDefault {
		_, _ = fmt.Fprint(ioStreams.Out, ", default")
	}
	_, _ = fmt.Fprintln(ioStreams.Out)
	if view.Mode == verda.NetworkModeCustom {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Next: verda network subnet create --network "+view.ID)
	}
	return nil
}
