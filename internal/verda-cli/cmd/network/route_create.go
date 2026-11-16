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
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	flagInstance       = "instance"
	flagDestination    = "destination"
	defaultDestination = "0.0.0.0/0"
)

type routeCreateOptions struct {
	target      routeTarget
	instance    string
	destination string
	yes         bool
}

// NewCmdRouteCreate creates the route create command.
func NewCmdRouteCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &routeCreateOptions{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Route a subnet's traffic through an instance (NAT gateway)",
		Long: cmdutil.LongDesc(`
			Send a subnet's traffic for --destination (default 0.0.0.0/0,
			the internet) through a gateway instance. The instance must be
			on the subnet's network, in the subnet's location, and have a
			public IP. It must also forward and NAT the traffic itself:
			enable IP forwarding and NAT (MASQUERADE) on it.

			A subnet has one route per destination. If the destination
			already has a route, create moves it to the new instance after
			a confirmation (--yes in agent mode); traffic switches at once.
		`),
		Example: cmdutil.Examples(`
			verda network route create --subnet sn_82gp8jck --instance nat-1
			verda --agent network route create --subnet web --network prod --instance 7c1e0a2b-... --yes
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRouteCreate(cmd, f, ioStreams, opts)
		},
	}
	opts.target.addFlags(cmd)
	cmd.Flags().StringVar(&opts.instance, flagInstance, "", "Gateway instance ID or hostname")
	cmd.Flags().StringVar(&opts.destination, flagDestination, defaultDestination, "Destination IPv4 CIDR")
	cmd.Flags().BoolVarP(&opts.yes, flagYes, "y", false, "Skip the confirmation when moving an existing route (required in agent mode)")
	return cmd
}

func runRouteCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *routeCreateOptions) error {
	if err := checkRouteCreateFlags(f, opts); err != nil {
		return err
	}
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	network, subnet, err := opts.target.resolve(cmd, f, ioStreams, client)
	if err != nil || subnet == nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	stop := spinner(ctx, f, "Loading routes and instances...")
	routes, instances, err := loadRoutesAndInstances(ctx, client, network.ID, subnet.ID)
	stop()
	cancel()
	if err != nil {
		return err
	}

	gw, err := chooseGateway(cmd.Context(), f, ioStreams, opts.instance, instances, network, subnet)
	if err != nil || gw == nil {
		return err
	}

	existing := routeFor(routes, opts.destination)
	if existing != nil && existing.Instance != nil && existing.Instance.ID == gw.ID {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "The route for %s already goes through %s.\n", opts.destination, gw.Hostname)
		_, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), cmdutil.NewRouteView(existing))
		return err
	}
	if existing != nil {
		ok, err := confirmRouteMove(cmd.Context(), f, ioStreams, opts, subnet, existing, gw)
		if err != nil || !ok {
			return err
		}
	}

	req := verda.CreateRouteRequest{Destination: opts.destination, InstanceID: gw.ID}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)
	ctx, cancel = context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	route, err := cmdutil.WithSpinner(ctx, f.Status(), "Creating route...", func(ctx context.Context) (*verda.Route, error) {
		return client.PrivateNetworks.CreateRoute(ctx, network.ID, subnet.ID, req)
	})
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "API response:", route)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), cmdutil.NewRouteView(route)); wrote {
		return err
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "Route %s: %s of subnet %s goes through %s (%s)\n", route.ID, route.Destination, subnet.Name, gw.Hostname, gw.ID)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "Note: traffic is dropped unless %s forwards it: enable IP forwarding and NAT (MASQUERADE) on it.\n", gw.Hostname)
	return nil
}

// checkRouteCreateFlags validates --destination and, off a terminal, the required flags.
func checkRouteCreateFlags(f cmdutil.Factory, opts *routeCreateOptions) error {
	if p, err := netip.ParsePrefix(opts.destination); err != nil || !p.Addr().Is4() {
		return cmdutil.NewValidationError(flagDestination, "must be an IPv4 CIDR, for example 0.0.0.0/0")
	}
	if interactiveTTY(f) {
		return nil
	}
	var missing []string
	if opts.target.subnet == "" {
		missing = append(missing, "--"+flagSubnet)
	}
	if opts.instance == "" {
		missing = append(missing, "--"+flagInstance)
	}
	if len(missing) > 0 {
		return cmdutil.NewMissingFlagsError(missing)
	}
	return nil
}

func loadRoutesAndInstances(ctx context.Context, client *verda.Client, networkID, subnetID string) ([]verda.Route, []verda.Instance, error) {
	routes, err := client.PrivateNetworks.GetRoutes(ctx, networkID, subnetID)
	if err != nil {
		return nil, nil, err
	}
	instances, err := client.Instances.List(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	return routes, instances, nil
}

func routeFor(routes []verda.Route, destination string) *verda.Route {
	for i := range routes {
		if routes[i].Destination == destination {
			return &routes[i]
		}
	}
	return nil
}

// gatewayProblem says why an instance cannot be the subnet's gateway, or "" if it can.
func gatewayProblem(inst *verda.Instance, network *verda.PrivateNetwork, subnet *verda.Subnet) string {
	switch {
	case inst.PrivateNetworkID == nil || *inst.PrivateNetworkID != network.ID:
		return "it is not on network " + network.Name
	case !strings.EqualFold(inst.Location, subnet.Location.Code):
		return fmt.Sprintf("it is in %s, the subnet is in %s", inst.Location, subnet.Location.Code)
	case inst.IP == nil || *inst.IP == "":
		return "it has no public IP"
	}
	return ""
}

// chooseGateway checks --instance, or offers the eligible instances on a TTY. nil, nil = canceled.
func chooseGateway(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, ref string, instances []verda.Instance, network *verda.PrivateNetwork, subnet *verda.Subnet) (*verda.Instance, error) {
	live := make([]verda.Instance, 0, len(instances))
	for i := range instances {
		if instances[i].Status != verda.StatusDiscontinued {
			live = append(live, instances[i])
		}
	}
	if ref != "" {
		inst, err := findInstance(live, ref)
		if err != nil {
			return nil, err
		}
		if problem := gatewayProblem(inst, network, subnet); problem != "" {
			return nil, cmdutil.NewValidationError(flagInstance, fmt.Sprintf("%s cannot be the gateway of subnet %s: %s", inst.Hostname, subnet.Name, problem))
		}
		return inst, nil
	}

	var eligible []*verda.Instance
	for i := range live {
		if gatewayProblem(&live[i], network, subnet) == "" {
			eligible = append(eligible, &live[i])
		}
	}
	if len(eligible) == 0 {
		return nil, cmdutil.NewValidationError(flagInstance, fmt.Sprintf(
			"no instance can be the gateway of subnet %s: it must be on network %s, in %s, and have a public IP",
			subnet.Name, network.Name, subnet.Location.Code))
	}
	labels := make([]string, 0, len(eligible)+1)
	for _, inst := range eligible {
		labels = append(labels, fmt.Sprintf("%s  (%s, %s)", inst.Hostname, inst.ID, *inst.IP))
	}
	labels = append(labels, cancelLabel)
	idx, err := f.Prompter().Select(ctx, "Select the gateway instance", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil, nil
		}
		return nil, err
	}
	if idx < 0 || idx >= len(eligible) {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, canceledMsg)
		return nil, nil
	}
	return eligible[idx], nil
}

// findInstance matches an ID, else a hostname. Hostnames are not unique: several matches are an error.
func findInstance(instances []verda.Instance, ref string) (*verda.Instance, error) {
	for i := range instances {
		if instances[i].ID == ref {
			return &instances[i], nil
		}
	}
	var matches []*verda.Instance
	for i := range instances {
		if instances[i].Hostname == ref {
			matches = append(matches, &instances[i])
		}
	}
	switch len(matches) {
	case 0:
		return nil, cmdutil.NewNotFoundError("instance", ref)
	case 1:
		return matches[0], nil
	}
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.ID
	}
	return nil, cmdutil.NewValidationError(flagInstance,
		fmt.Sprintf("%d instances are named %q (%s); pass the instance ID", len(matches), ref, strings.Join(ids, ", ")))
}

func confirmRouteMove(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *routeCreateOptions, subnet *verda.Subnet, existing *verda.Route, gw *verda.Instance) (bool, error) {
	if opts.yes {
		return true, nil
	}
	if f.AgentMode() {
		return false, cmdutil.NewConfirmationRequiredError("move route")
	}
	warn := warnStyle()
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s %s\n\n", warn.Render("⚠"),
		warn.Render(fmt.Sprintf("Moves the route for %s of %s from %s to %s; traffic switches at once.",
			existing.Destination, subnet.Name, gatewayName(existing.Instance), gw.Hostname)))
	ok, err := f.Prompter().Confirm(ctx, "Move the route?")
	if err != nil && !cmdutil.IsPromptCancel(err) {
		return false, err
	}
	if err != nil || !ok {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, canceledMsg)
		return false, nil //nolint:nilerr // intentional: prompter cancel is a clean exit
	}
	return true, nil
}
