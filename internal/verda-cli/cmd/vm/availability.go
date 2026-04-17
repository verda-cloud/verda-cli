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
	Kind         string
}

// NewCmdAvailability creates the vm availability subcommand.
func NewCmdAvailability(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &availabilityOptions{}

	cmd := &cobra.Command{
		Use:     "availability",
		Aliases: []string{"avail"},
		Short:   "Show available instance types with specs and pricing",
		Long: cmdutil.LongDesc(`
			Show currently available instance types with full specs, pricing,
			and datacenter locations. This helps you find what you can launch
			right now and how much it costs.
		`),
		Example: cmdutil.Examples(`
			# Show all available instance types
			verda vm availability

			# Filter by location
			verda vm availability --location FIN-01

			# Check a specific instance type
			verda vm availability --type 1V100.6V

			# GPU instances only
			verda vm availability --kind gpu

			# Spot pricing
			verda vm availability --spot

			# JSON output for scripting
			verda vm availability -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAvailability(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Location, "location", "", "Filter by location code (e.g., FIN-01)")
	flags.StringVar(&opts.InstanceType, "type", "", "Filter by instance type (e.g., 1V100.6V)")
	flags.BoolVar(&opts.IsSpot, "spot", false, "Show spot pricing and availability")
	flags.StringVar(&opts.Kind, "kind", "", "Filter by kind: gpu or cpu")

	return cmd
}

// availableInstance is a single row in the availability table.
type availableInstance struct {
	Location     string  `json:"location" yaml:"location"`
	InstanceType string  `json:"instance_type" yaml:"instance_type"`
	GPU          string  `json:"gpu" yaml:"gpu"`
	VRAM         string  `json:"vram,omitempty" yaml:"vram,omitempty"`
	RAM          string  `json:"ram" yaml:"ram"`
	CPUCores     int     `json:"cpu_cores" yaml:"cpu_cores"`
	PricePerHour float64 `json:"price_per_hour" yaml:"price_per_hour"`
	SpotPrice    float64 `json:"spot_price,omitempty" yaml:"spot_price,omitempty"`
}

func runAvailability(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *availabilityOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	type availData struct {
		types []verda.InstanceTypeInfo
		avail []verda.LocationAvailability
	}
	data, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading availability and pricing...", func() (availData, error) {
		types, typesErr := client.InstanceTypes.Get(ctx, "usd")
		if typesErr != nil {
			return availData{}, fmt.Errorf("fetching instance types: %w", typesErr)
		}
		avail, availErr := client.InstanceAvailability.GetAllAvailabilities(ctx, opts.IsSpot, opts.Location)
		if availErr != nil {
			return availData{}, fmt.Errorf("fetching availability: %w", availErr)
		}
		return availData{types: types, avail: avail}, nil
	})
	if err != nil {
		return err
	}
	types, avail := data.types, data.avail

	// Index instance types by name.
	typeMap := make(map[string]*verda.InstanceTypeInfo, len(types))
	for i := range types {
		typeMap[types[i].InstanceType] = &types[i]
	}

	// Build joined rows: one per (location, instance type) pair.
	var rows []availableInstance
	for _, la := range avail {
		for _, instType := range la.Availabilities {
			t, ok := typeMap[instType]
			if !ok {
				continue
			}
			// Filter by --type.
			if opts.InstanceType != "" && !strings.EqualFold(instType, opts.InstanceType) {
				continue
			}
			// Filter by --kind.
			if opts.Kind != "" && !matchesKind(instType, opts.Kind) {
				continue
			}

			gpu := "—"
			vram := ""
			if t.GPU.NumberOfGPUs > 0 {
				gpuDesc := cleanGPUDescription(t.GPU.NumberOfGPUs, t.GPU.Description)
				gpu = fmt.Sprintf("%dx %s", t.GPU.NumberOfGPUs, gpuDesc)
				vram = fmt.Sprintf("%dGB", t.GPUMemory.SizeInGigabytes)
			}

			price := float64(t.PricePerHour)
			spotPrice := float64(t.SpotPrice)

			rows = append(rows, availableInstance{
				Location:     la.LocationCode,
				InstanceType: instType,
				GPU:          gpu,
				VRAM:         vram,
				RAM:          fmt.Sprintf("%dGB", t.Memory.SizeInGigabytes),
				CPUCores:     t.CPU.NumberOfCores,
				PricePerHour: price,
				SpotPrice:    spotPrice,
			})
		}
	}

	// Sort by price ascending.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].PricePerHour != rows[j].PricePerHour {
			return rows[i].PricePerHour < rows[j].PricePerHour
		}
		return rows[i].Location < rows[j].Location
	})

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("Available: %d instance(s):", len(rows)), rows)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), rows); wrote {
		return err
	}

	if len(rows) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No available instances found.")
		return nil
	}

	renderAvailabilityTable(ioStreams, rows, opts.IsSpot)
	return nil
}

func renderAvailabilityTable(ioStreams cmdutil.IOStreams, rows []availableInstance, showSpot bool) {
	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	price := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

	_, _ = fmt.Fprintf(ioStreams.Out, "\n  %s  %s\n\n",
		bold.Render("Available Instances"),
		dim.Render(fmt.Sprintf("(%d)", len(rows))))

	// Header.
	if showSpot {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-8s  %-18s  %-22s  %6s  %6s  %10s  %10s\n",
			"LOCATION", "TYPE", "GPU", "VRAM", "RAM", "PRICE/HR", "SPOT/HR")
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-8s  %-18s  %-22s  %6s  %6s  %10s  %10s\n",
			"--------", "----", "---", "----", "---", "--------", "-------")
	} else {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-8s  %-18s  %-22s  %6s  %6s  %10s\n",
			"LOCATION", "TYPE", "GPU", "VRAM", "RAM", "PRICE/HR")
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-8s  %-18s  %-22s  %6s  %6s  %10s\n",
			"--------", "----", "---", "----", "---", "--------")
	}

	for i := range rows {
		r := &rows[i]
		vram := r.VRAM
		if vram == "" {
			vram = "—"
		}
		priceStr := price.Render(fmt.Sprintf("$%.3f", r.PricePerHour))

		if showSpot {
			spotStr := dim.Render("—")
			if r.SpotPrice > 0 {
				spotStr = price.Render(fmt.Sprintf("$%.3f", r.SpotPrice))
			}
			_, _ = fmt.Fprintf(ioStreams.Out, "  %s  %-18s  %-22s  %6s  %6s  %10s  %10s\n",
				green.Render(fmt.Sprintf("%-8s", r.Location)),
				r.InstanceType, r.GPU, vram, r.RAM, priceStr, spotStr)
		} else {
			_, _ = fmt.Fprintf(ioStreams.Out, "  %s  %-18s  %-22s  %6s  %6s  %10s\n",
				green.Render(fmt.Sprintf("%-8s", r.Location)),
				r.InstanceType, r.GPU, vram, r.RAM, priceStr)
		}
	}
	_, _ = fmt.Fprintln(ioStreams.Out)
}
