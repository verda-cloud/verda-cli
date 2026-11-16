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
	"net/netip"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const flagCIDR = "cidr"

type subnetCreateOptions struct {
	Network    string
	Location   string
	Name       string
	CIDR       string
	Default    bool
	defaultSet bool
}

// subnetCreateData is read once before the wizard so its steps need no API calls.
type subnetCreateData struct {
	networks  []verda.PrivateNetwork
	locations []verda.Location // only those with private networks
	subnets   map[string][]verda.Subnet
}

// NewCmdSubnetCreate creates the subnet create command.
func NewCmdSubnetCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &subnetCreateOptions{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a subnet in a private network",
		Long: cmdutil.LongDesc(`
			Create a subnet in one location of a private network. The
			location must support private networks.

			Without --cidr, the first free /24 in 10.0.0.0/16 that overlaps
			none of the network's subnets is used, and printed. A --cidr
			must be an IPv4 range of /29 or larger inside 10.0.0.0/8,
			172.16.0.0/12 or 192.168.0.0/16, and may not overlap another
			subnet of the network in any location. The name defaults to
			<location>-<n>. The first subnet in a location becomes its
			default; --default makes a later one the default.
		`),
		Example: cmdutil.Examples(`
			verda network subnet create
			verda network subnet create --network prod -l FIN-03
			verda network subnet create --network prod -l FIN-03 --name web --cidr 10.0.8.0/24 --default
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.defaultSet = cmd.Flags().Changed(flagDefault)
			return runSubnetCreate(cmd, f, ioStreams, opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.Network, flagNetwork, "", "Network ID or name")
	flags.StringVarP(&opts.Location, flagLocation, "l", "", "Location code, for example FIN-03")
	flags.StringVar(&opts.Name, flagName, "", "Subnet name (default: <location>-<n>)")
	flags.StringVar(&opts.CIDR, flagCIDR, "", "IPv4 range (default: the first free /24 in 10.0.0.0/16)")
	flags.BoolVar(&opts.Default, flagDefault, false, "Make this the default subnet of its location")
	return cmd
}

func runSubnetCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *subnetCreateOptions) error {
	interactive := interactiveTTY(f)
	if !interactive {
		if missing := missingSubnetCreateFlags(opts); len(missing) > 0 {
			return cmdutil.NewMissingFlagsError(missing)
		}
	}
	if opts.defaultSet && !opts.Default {
		return cmdutil.NewValidationError("default", "--default only sets; leave it out to keep the current default")
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	data, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading networks and locations...", func(ctx context.Context) (*subnetCreateData, error) {
		return loadSubnetCreateData(ctx, client, opts.Network)
	})
	cancel()
	if err != nil {
		return err
	}
	if err := data.normalize(opts); err != nil {
		return err
	}

	if interactive && (opts.Network == "" || opts.Location == "") {
		if err := newEngine(f.Prompter(), f.Status(), ioStreams.ErrOut).Run(cmd.Context(), buildSubnetCreateFlow(opts, data)); err != nil {
			return err
		}
	}
	autoCIDR := opts.CIDR == ""
	if err := data.fillDefaults(opts); err != nil {
		return err
	}
	if autoCIDR && !f.AgentMode() {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "Using CIDR %s (the first free /%d in %s).\n", opts.CIDR, cmdutil.DefaultSubnetBits, cmdutil.DefaultSubnetPool)
	}
	if err := data.validate(opts); err != nil {
		return err
	}

	req := verda.CreateSubnetRequest{LocationCode: opts.Location, Name: opts.Name, CIDR: opts.CIDR, IsDefault: opts.Default}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)
	ctx, cancel = context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	subnet, err := cmdutil.WithSpinner(ctx, f.Status(), "Creating subnet...", func(ctx context.Context) (*verda.Subnet, error) {
		return client.PrivateNetworks.CreateSubnet(ctx, opts.Network, req)
	})
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", subnet)

	view := cmdutil.NewSubnetView(opts.Network, subnet)
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), view); wrote {
		return err
	}
	isDefault := yesNo(view.IsDefault)
	_, _ = fmt.Fprintf(ioStreams.Out, "Created subnet %s (%s) in %s, %s, default: %s\n", view.Name, view.ID, view.Location, view.CIDR, isDefault)
	return nil
}

func missingSubnetCreateFlags(opts *subnetCreateOptions) []string {
	var missing []string
	if opts.Network == "" {
		missing = append(missing, "--"+flagNetwork)
	}
	if opts.Location == "" {
		missing = append(missing, "--"+flagLocation)
	}
	return missing
}

// loadSubnetCreateData reads networks, locations and subnets. With networkArg set, only that
// network's subnets are read (an unknown one is reported later by normalize).
func loadSubnetCreateData(ctx context.Context, client *verda.Client, networkArg string) (*subnetCreateData, error) {
	networks, err := client.PrivateNetworks.Get(ctx)
	if err != nil {
		return nil, err
	}
	locations, err := client.Locations.Get(ctx)
	if err != nil {
		return nil, err
	}
	data := &subnetCreateData{networks: networks, subnets: make(map[string][]verda.Subnet, len(networks))}
	for i := range locations {
		if locations[i].IsPrivateNetworksEnabled {
			data.locations = append(data.locations, locations[i])
		}
	}
	toRead := networks
	if networkArg != "" {
		toRead = nil
		if n := cmdutil.FindNetwork(networks, networkArg); n != nil {
			toRead = []verda.PrivateNetwork{*n}
		}
	}
	for i := range toRead {
		subnets, err := client.PrivateNetworks.GetSubnets(ctx, toRead[i].ID)
		if err != nil {
			return nil, err
		}
		data.subnets[toRead[i].ID] = subnets
	}
	return data, nil
}

// normalize resolves the --network flag to an ID and the --location flag to its canonical code.
func (d *subnetCreateData) normalize(opts *subnetCreateOptions) error {
	if len(d.networks) == 0 {
		return cmdutil.NewValidationError(flagNetwork, "no private network found; create one with \"verda network create\", then run this command again")
	}
	if opts.Network != "" {
		n := cmdutil.FindNetwork(d.networks, opts.Network)
		if n == nil {
			return cmdutil.NewNotFoundError("network", opts.Network)
		}
		opts.Network = n.ID
	}
	if opts.Location != "" {
		code, err := d.enabledLocation(opts.Location)
		if err != nil {
			return err
		}
		opts.Location = code
	}
	return nil
}

func (d *subnetCreateData) enabledLocation(loc string) (string, error) {
	codes := make([]string, len(d.locations))
	for i := range d.locations {
		if strings.EqualFold(d.locations[i].Code, loc) {
			return d.locations[i].Code, nil
		}
		codes[i] = d.locations[i].Code
	}
	return "", cmdutil.NewValidationError(flagLocation,
		fmt.Sprintf("private networks are not available in %s; use one of: %s", loc, strings.Join(codes, ", ")))
}

// fillDefaults sets the name and CIDR when they were not given.
func (d *subnetCreateData) fillDefaults(opts *subnetCreateOptions) error {
	if opts.Name == "" {
		opts.Name = d.defaultName(opts.Network, opts.Location)
	}
	if opts.CIDR == "" {
		p, err := d.suggestCIDR(opts.Network)
		if err != nil {
			return cmdutil.NewValidationError(flagCIDR, err.Error()+"; pass --cidr")
		}
		opts.CIDR = p.String()
	}
	return nil
}

// defaultName is <location>-<n> in lower case, with the first n not used in the network.
func (d *subnetCreateData) defaultName(networkID, location string) string {
	prefix := strings.ToLower(location) + "-"
	for n := 1; ; n++ {
		name := prefix + strconv.Itoa(n)
		if cmdutil.FindSubnet(d.subnets[networkID], name) == nil {
			return name
		}
	}
}

func (d *subnetCreateData) suggestCIDR(networkID string) (netip.Prefix, error) {
	return cmdutil.FreeSubnetCIDR(cmdutil.SubnetPrefixes(d.subnets[networkID]), cmdutil.DefaultSubnetPool, cmdutil.DefaultSubnetBits)
}

// checkCIDR validates the range and rejects an overlap with any subnet of the network, in any location.
func (d *subnetCreateData) checkCIDR(networkID, cidr string) error {
	p, err := cmdutil.ValidateSubnetCIDR(cidr)
	if err != nil {
		return err
	}
	subnets := d.subnets[networkID]
	for i := range subnets {
		taken, err := netip.ParsePrefix(subnets[i].CIDR)
		if err == nil && cmdutil.PrefixesOverlap(p, taken) {
			return fmt.Errorf("%s overlaps subnet %s (%s, %s)", p, subnets[i].Name, subnets[i].CIDR, subnets[i].Location.Code)
		}
	}
	return nil
}

func (d *subnetCreateData) validate(opts *subnetCreateOptions) error {
	if err := validateName(opts.Name); err != nil {
		return cmdutil.NewValidationError(flagName, err.Error())
	}
	if cmdutil.FindSubnet(d.subnets[opts.Network], opts.Name) != nil {
		return cmdutil.NewValidationError(flagName, fmt.Sprintf("the network already has a subnet named %q", opts.Name))
	}
	if err := d.checkCIDR(opts.Network, opts.CIDR); err != nil {
		return cmdutil.NewValidationError(flagCIDR, err.Error())
	}
	return nil
}

// firstInLocation: the API makes the first subnet of a location its default.
func (d *subnetCreateData) firstInLocation(networkID, location string) bool {
	subnets := d.subnets[networkID]
	for i := range subnets {
		if strings.EqualFold(subnets[i].Location.Code, location) {
			return false
		}
	}
	return true
}
