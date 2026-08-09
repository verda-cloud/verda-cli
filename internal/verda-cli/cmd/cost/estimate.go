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

package cost

import (
	"context"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type estimateOptions struct {
	InstanceType string
	IsSpot       bool
	OSVolumeSize int
	StorageSize  int
	StorageType  string
}

func newCmdEstimate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &estimateOptions{StorageType: "NVMe"}

	cmd := &cobra.Command{
		Use:   "estimate",
		Short: "Estimate costs for an instance configuration",
		Long: cmdutil.LongDesc(`
			Calculate estimated costs for an instance type with optional
			OS volume and additional storage. Shows hourly, daily, and
			monthly breakdowns.
		`),
		Example: cmdutil.Examples(`
			# Basic estimate
			verda cost estimate --type 1V100.6V

			# With storage
			verda cost estimate --type 1V100.6V --os-volume 100 --storage 500

			# Spot pricing
			verda cost estimate --type 1V100.6V --spot

			# JSON output
			verda cost estimate --type 1V100.6V --storage 500 -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.InstanceType == "" {
				return cmdutil.UsageErrorf(cmd, "--type is required")
			}
			return runEstimate(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.InstanceType, "type", "", "Instance type (required)")
	flags.BoolVar(&opts.IsSpot, "spot", false, "Use spot pricing")
	flags.IntVar(&opts.OSVolumeSize, "os-volume", 0, "OS volume size in GiB")
	flags.IntVar(&opts.StorageSize, "storage", 0, "Additional storage size in GiB")
	flags.StringVar(&opts.StorageType, "storage-type", opts.StorageType, "Storage type: NVMe or HDD")

	return cmd
}

// Estimate is the structured output for cost estimation.
type Estimate struct {
	InstanceType string    `json:"instance_type" yaml:"instance_type"`
	Spot         bool      `json:"spot" yaml:"spot"`
	Instance     LineItem  `json:"instance" yaml:"instance"`
	OSVolume     *LineItem `json:"os_volume,omitempty" yaml:"os_volume,omitempty"`
	Storage      *LineItem `json:"storage,omitempty" yaml:"storage,omitempty"`
	Total        TotalItem `json:"total" yaml:"total"`
}

// LineItem represents a single cost component.
type LineItem struct {
	Description string  `json:"description" yaml:"description"`
	Hourly      float64 `json:"hourly" yaml:"hourly"`
	Daily       float64 `json:"daily" yaml:"daily"`
	Monthly     float64 `json:"monthly" yaml:"monthly"`
}

// TotalItem represents the total cost.
type TotalItem struct {
	Hourly  float64 `json:"hourly" yaml:"hourly"`
	Daily   float64 `json:"daily" yaml:"daily"`
	Monthly float64 `json:"monthly" yaml:"monthly"`
}

func runEstimate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *estimateOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading pricing...")
	}

	// Fetch all instance types and find the matching one.
	allTypes, err := client.InstanceTypes.Get(ctx, "usd")
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return fmt.Errorf("fetching instance type pricing: %w", err)
	}

	instType := findInstanceType(allTypes, opts.InstanceType)
	if instType == nil {
		return fmt.Errorf("instance type %q not found", opts.InstanceType)
	}

	// Instance hourly rate.
	instanceHourly := float64(instType.PricePerHour)
	if opts.IsSpot && float64(instType.SpotPrice) > 0 {
		instanceHourly = float64(instType.SpotPrice)
	}

	estimate := Estimate{
		InstanceType: opts.InstanceType,
		Spot:         opts.IsSpot,
		Instance: LineItem{
			Description: instanceDescription(instType),
			Hourly:      instanceHourly,
			Daily:       instanceHourly * 24,
			Monthly:     instanceHourly * cmdutil.HoursInMonth,
		},
	}

	// Volume pricing (if needed).
	if opts.OSVolumeSize > 0 || opts.StorageSize > 0 {
		if err := addVolumeItems(ctx, cmd, client, opts, &estimate); err != nil {
			return err
		}
	}

	estimate.computeTotals()

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Cost estimate:", estimate)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), estimate); wrote {
		return err
	}

	renderEstimate(ioStreams.Out, &estimate)
	return nil
}

// addVolumeItems fills in the OS volume and storage line items.
func addVolumeItems(ctx context.Context, cmd *cobra.Command, client *verda.Client, opts *estimateOptions, estimate *Estimate) error {
	volTypes, err := client.VolumeTypes.GetAllVolumeTypes(ctx)
	if err != nil {
		return fmt.Errorf("fetching volume pricing: %w", err)
	}
	vtMap := make(map[string]verda.VolumeType, len(volTypes))
	for _, vt := range volTypes {
		vtMap[vt.Type] = vt
	}

	if opts.OSVolumeSize > 0 {
		// OS volumes are always NVMe; a missing catalog entry is a bug, not user input.
		item, err := volumeCostItem(verda.VolumeTypeNVMe, opts.OSVolumeSize, vtMap)
		if err != nil {
			return err
		}
		estimate.OSVolume = &item
	}
	if opts.StorageSize > 0 {
		item, err := volumeCostItem(opts.StorageType, opts.StorageSize, vtMap)
		if err != nil {
			return cmdutil.UsageErrorf(cmd, "invalid --storage-type: %v", err)
		}
		estimate.Storage = &item
	}
	return nil
}

func findInstanceType(types []verda.InstanceTypeInfo, name string) *verda.InstanceTypeInfo {
	for i := range types {
		if strings.EqualFold(types[i].InstanceType, name) {
			return &types[i]
		}
	}
	return nil
}

// volumeCostItem prices a volume; an unknown type is an error (was: silent $0).
func volumeCostItem(volType string, sizeGB int, vtMap map[string]verda.VolumeType) (LineItem, error) {
	vt, ok := vtMap[volType]
	if !ok {
		return LineItem{}, fmt.Errorf("unknown volume type %q (valid types: %s)",
			volType, strings.Join(cmdutil.ValidVolumeTypeNames(vtMap), ", "))
	}
	monthlyPerGB := vt.Price.PricePerMonthPerGB
	hourly := cmdutil.VolumeHourlyPrice(monthlyPerGB, sizeGB)
	return LineItem{
		Description: fmt.Sprintf("%dGB %s", sizeGB, volType),
		Hourly:      hourly,
		Daily:       hourly * 24,
		Monthly:     cmdutil.VolumeMonthlyPrice(monthlyPerGB, sizeGB),
	}, nil
}

// computeTotals recomputes e.Total from the line items. The money path is
// pinned by TestEstimateTotals.
func (e *Estimate) computeTotals() {
	e.Total = TotalItem{
		Hourly:  e.Instance.Hourly,
		Daily:   e.Instance.Daily,
		Monthly: e.Instance.Monthly,
	}
	for _, item := range []*LineItem{e.OSVolume, e.Storage} {
		if item == nil {
			continue
		}
		e.Total.Hourly += item.Hourly
		e.Total.Daily += item.Daily
		e.Total.Monthly += item.Monthly
	}
}

func instanceDescription(info *verda.InstanceTypeInfo) string {
	if info.GPU.NumberOfGPUs > 0 {
		return fmt.Sprintf("%dx %s, %dGB VRAM, %dGB RAM",
			info.GPU.NumberOfGPUs, info.GPU.Description,
			info.GPUMemory.SizeInGigabytes, info.Memory.SizeInGigabytes)
	}
	return fmt.Sprintf("%d CPU, %dGB RAM",
		info.CPU.NumberOfCores, info.Memory.SizeInGigabytes)
}

func renderEstimate(w interface{ Write([]byte) (int, error) }, e *Estimate) {
	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	price := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	sep := dim.Render(strings.Repeat("─", 52))

	_, _ = fmt.Fprintf(w, "\n  %s\n", bold.Render("Cost Estimate"))
	_, _ = fmt.Fprintf(w, "  %s\n\n", sep)

	pricingMode := "on-demand"
	if e.Spot {
		pricingMode = "spot"
	}
	_, _ = fmt.Fprintf(w, "  %s  %s (%s)\n", dim.Render("Instance:"), e.InstanceType, pricingMode)
	_, _ = fmt.Fprintf(w, "  %s  %s\n", dim.Render("Specs:   "), e.Instance.Description)
	_, _ = fmt.Fprintf(w, "\n  %s\n", sep)

	_, _ = fmt.Fprintf(w, "  %-30s %10s %10s %12s\n", "", "Hourly", "Daily", "Monthly")
	_, _ = fmt.Fprintf(w, "  %s\n", sep)

	renderLine(w, "Instance", e.Instance, &price)
	if e.OSVolume != nil {
		renderLine(w, "OS Volume", *e.OSVolume, &price)
	}
	if e.Storage != nil {
		renderLine(w, "Storage", *e.Storage, &price)
	}

	_, _ = fmt.Fprintf(w, "  %s\n", sep)
	_, _ = fmt.Fprintf(w, "  %s %s %s %s\n",
		bold.Render(fmt.Sprintf("%-30s", "Total")),
		bold.Render(price.Render(fmt.Sprintf("%10s", formatPrice(e.Total.Hourly)))),
		bold.Render(price.Render(fmt.Sprintf("%10s", formatPrice(e.Total.Daily)))),
		bold.Render(price.Render(fmt.Sprintf("%12s", formatPrice(e.Total.Monthly)))))
	_, _ = fmt.Fprintf(w, "  %s\n\n", sep)
}

func renderLine(w interface{ Write([]byte) (int, error) }, label string, item LineItem, priceStyle *lipgloss.Style) {
	_, _ = fmt.Fprintf(w, "  %-30s %s %s %s\n",
		label,
		priceStyle.Render(fmt.Sprintf("%10s", formatPrice(item.Hourly))),
		priceStyle.Render(fmt.Sprintf("%10s", formatPrice(item.Daily))),
		priceStyle.Render(fmt.Sprintf("%12s", formatPrice(item.Monthly))))
}

func formatPrice(v float64) string {
	if v < 0.01 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}
