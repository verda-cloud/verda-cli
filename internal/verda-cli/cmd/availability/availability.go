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

package availability

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type availabilityOptions struct {
	Location     string
	InstanceType string
	IsSpot       bool
}

// NewCmdAvailability creates the availability command.
func NewCmdAvailability(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &availabilityOptions{}

	cmd := &cobra.Command{
		Use:     "availability",
		Aliases: []string{"avail"},
		Short:   "Check instance type availability",
		Long: cmdutil.LongDesc(`
			Check which instance types are available in each location.
			Optionally filter by location, instance type, or spot pricing.
		`),
		Example: cmdutil.Examples(`
			# Full availability matrix
			verda availability

			# Check a specific location
			verda availability --location FIN-01

			# Check a specific instance type
			verda availability --type 1V100.6V

			# Spot availability only
			verda availability --spot

			# JSON output
			verda availability -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAvailability(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Location, "location", "", "Filter by location code (e.g., FIN-01)")
	flags.StringVar(&opts.InstanceType, "type", "", "Check availability for a specific instance type")
	flags.BoolVar(&opts.IsSpot, "spot", false, "Check spot instance availability")

	return cmd
}

// availabilityResult is the structured output type.
type availabilityResult struct {
	LocationCode  string   `json:"location_code" yaml:"location_code"`
	InstanceTypes []string `json:"instance_types" yaml:"instance_types"`
	Count         int      `json:"count" yaml:"count"`
}

func runAvailability(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *availabilityOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	// If checking a specific instance type, use the boolean check.
	if opts.InstanceType != "" {
		return runSingleCheck(ctx, f, ioStreams, client, opts)
	}

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading availability...")
	}
	avails, err := client.InstanceAvailability.GetAllAvailabilities(ctx, opts.IsSpot, opts.Location)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Availability response:", avails)

	// Build structured result.
	results := make([]availabilityResult, 0, len(avails))
	for _, a := range avails {
		sorted := append([]string(nil), a.Availabilities...)
		sort.Strings(sorted)
		results = append(results, availabilityResult{
			LocationCode:  a.LocationCode,
			InstanceTypes: sorted,
			Count:         len(sorted),
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].LocationCode < results[j].LocationCode
	})

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), results); wrote {
		return err
	}

	if len(results) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No availability data found.")
		return nil
	}

	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

	pricingLabel := "on-demand"
	if opts.IsSpot {
		pricingLabel = "spot"
	}
	_, _ = fmt.Fprintf(ioStreams.Out, "  Instance availability (%s)\n\n", pricingLabel)

	for _, r := range results {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %s  %s\n",
			bold.Render(fmt.Sprintf("%-8s", r.LocationCode)),
			dim.Render(fmt.Sprintf("%d type(s)", r.Count)))

		for _, t := range r.InstanceTypes {
			_, _ = fmt.Fprintf(ioStreams.Out, "    %s %s\n",
				green.Render("●"),
				t)
		}
		_, _ = fmt.Fprintln(ioStreams.Out)
	}

	return nil
}

func runSingleCheck(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, client *verda.Client, opts *availabilityOptions) error {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, fmt.Sprintf("Checking %s...", opts.InstanceType))
	}
	available, err := client.InstanceAvailability.GetInstanceTypeAvailability(ctx, opts.InstanceType, opts.IsSpot, opts.Location)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	result := struct {
		InstanceType string `json:"instance_type" yaml:"instance_type"`
		Location     string `json:"location,omitempty" yaml:"location,omitempty"`
		Spot         bool   `json:"spot" yaml:"spot"`
		Available    bool   `json:"available" yaml:"available"`
	}{
		InstanceType: opts.InstanceType,
		Location:     opts.Location,
		Spot:         opts.IsSpot,
		Available:    available,
	}

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result); wrote {
		return err
	}

	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	location := opts.Location
	if location == "" {
		location = "all locations"
	}

	if available {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %s %s is available in %s\n",
			green.Render("●"),
			opts.InstanceType,
			location)
	} else {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %s %s is not available in %s\n",
			red.Render("●"),
			opts.InstanceType,
			location)
	}

	return nil
}

// FormatTypeList formats a list of instance types for display.
func FormatTypeList(types []string) string {
	if len(types) == 0 {
		return "none"
	}
	return strings.Join(types, ", ")
}
