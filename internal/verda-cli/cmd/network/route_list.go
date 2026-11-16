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
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// routeListView adds whether the gateway instance is gone to the route.
type routeListView struct {
	cmdutil.RouteView `yaml:",inline"`
	InstanceDeleted   bool `json:"instance_deleted" yaml:"instance_deleted"`
}

// NewCmdRouteList creates the route list command.
func NewCmdRouteList(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	target := &routeTarget{}
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the routes of a subnet",
		Long: cmdutil.LongDesc(`
			List the routes of a subnet. A route whose gateway instance was
			deleted stays until you delete it, and shows as (deleted).
		`),
		Example: cmdutil.Examples(`
			verda network route list --subnet sn_82gp8jck
			verda network route ls --subnet web --network prod -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRouteList(cmd, f, ioStreams, target)
		},
	}
	target.addFlags(cmd)
	return cmd
}

func runRouteList(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, target *routeTarget) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	network, subnet, err := target.resolve(cmd, f, ioStreams, client)
	if err != nil || subnet == nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()
	stop := spinner(ctx, f, "Loading routes...")
	routes, live, err := loadRoutes(ctx, client, network.ID, subnet.ID)
	stop()
	if err != nil {
		return err
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d route(s):", len(routes)), routes)

	views := make([]routeListView, len(routes))
	for i := range routes {
		views[i] = routeListView{RouteView: cmdutil.NewRouteView(&routes[i]), InstanceDeleted: gatewayGone(&routes[i], live)}
	}
	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), views); wrote {
		return err
	}
	if len(views) == 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "No routes in subnet %s.\n", subnet.Name)
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Create one with: verda network route create --subnet "+subnet.ID)
		return nil
	}
	tw := tabwriter.NewWriter(ioStreams.Out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tDESTINATION\tINSTANCE\tPUBLIC IP\tCREATED")
	for i := range views {
		v := &views[i]
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", v.ID, v.Destination, routeInstanceColumn(&routes[i], v.InstanceDeleted),
			routePublicIP(&routes[i], live), cmdutil.TimeColumn(v.CreatedAt, timeLayout))
	}
	return tw.Flush()
}

func routeInstanceColumn(r *verda.Route, gone bool) string {
	if gone {
		return deletedMark
	}
	return gatewayName(r.Instance)
}

func routePublicIP(r *verda.Route, live map[string]*verda.Instance) string {
	if r.Instance == nil {
		return noneMark
	}
	if r.Instance.PublicIP != nil && *r.Instance.PublicIP != "" {
		return *r.Instance.PublicIP
	}
	if inst, ok := live[r.Instance.ID]; ok && inst.IP != nil {
		return *inst.IP
	}
	return noneMark
}
