package vm

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

// summaryView implements wizard.View and renders the deployment summary
// when the wizard reaches the confirm-deploy step.
type summaryView struct {
	ctx       context.Context
	getClient clientFunc
	cache     *apiCache
	opts      *createOptions
	last      string
}

func newSummaryView(ctx context.Context, getClient clientFunc, cache *apiCache, opts *createOptions) *summaryView {
	return &summaryView{ctx: ctx, getClient: getClient, cache: cache, opts: opts}
}

func (v *summaryView) Update(msg any) (render string, publish []any) {
	if sc, ok := msg.(wizard.StepChangedMsg); ok {
		if sc.StepName == "confirm-deploy" {
			ensurePricingCache(v.ctx, v.getClient, v.cache)
			v.last = renderDeploymentSummary(v.opts, v.cache)
		} else {
			v.last = ""
		}
	}
	return v.last, nil
}

func (v *summaryView) Subscribe() []reflect.Type {
	return nil // receive all engine broadcasts
}

func renderDeploymentSummary(opts *createOptions, cache *apiCache) string {
	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	priceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green for prices

	var computeHourly float64
	var storageHourly float64

	// Instance pricing.
	instLabel := opts.InstanceType
	var instUnits int
	var instUnitLabel string
	if info, ok := cache.instanceTypes[opts.InstanceType]; ok {
		computeHourly = float64(info.PricePerHour)
		if opts.IsSpot {
			computeHourly = float64(info.SpotPrice)
		}
		instUnits = instanceUnits(&info)
		if info.GPU.NumberOfGPUs > 0 {
			instLabel = fmt.Sprintf("%s — %s", info.InstanceType, info.GPU.Description)
			instUnitLabel = unitLabelGPU
		} else {
			instLabel = fmt.Sprintf("%s — %d CPU, %dGB RAM", info.InstanceType, info.CPU.NumberOfCores, info.Memory.SizeInGigabytes)
			instUnitLabel = unitLabelVCPU
		}
	}

	// OS volume pricing (NVMe).
	var osVolPrice float64
	var osVolUnitPrice float64
	if opts.OSVolumeSize > 0 {
		if vt, ok := cache.volumeTypes[verda.VolumeTypeNVMe]; ok {
			osVolUnitPrice = vt.Price.PricePerMonthPerGB
			osVolPrice = volumeHourlyPrice(osVolUnitPrice, opts.OSVolumeSize)
			storageHourly += osVolPrice
		}
	}

	// Additional volume pricing.
	type volDetail struct {
		name, volType string
		size          int
		unitPrice     float64 // per GB per month
		hourly        float64
	}
	volDetails := make([]volDetail, 0, len(opts.VolumeSpecs))
	for _, spec := range opts.VolumeSpecs {
		parts := strings.SplitN(spec, ":", 3)
		if len(parts) < 3 {
			continue
		}
		name, sizeStr, vType := parts[0], parts[1], parts[2]
		size, _ := strconv.Atoi(sizeStr)
		var hourly, unitP float64
		if vt, ok := cache.volumeTypes[vType]; ok {
			unitP = vt.Price.PricePerMonthPerGB
			hourly = volumeHourlyPrice(unitP, size)
			storageHourly += hourly
		}
		volDetails = append(volDetails, volDetail{name, vType, size, unitP, hourly})
	}

	var b strings.Builder

	// Print summary.
	fmt.Fprintf(&b, "\n  %s\n", bold.Render("Deployment Summary"))

	billing := "On-Demand"
	if opts.IsSpot {
		billing = "Spot Instance"
	}
	fmt.Fprintf(&b, "  %s\n", dim.Render(billing))
	fmt.Fprintf(&b, "  %s\n\n", dim.Render(strings.Repeat("─", 50)))

	// Instance.
	fmt.Fprintf(&b, "  %s\n", accent.Render("Instance"))
	var computePriceStr string
	if instUnits > 1 {
		perUnit := computeHourly / float64(instUnits)
		computePriceStr = fmt.Sprintf("$%.4f/%s/hr  $%.4f/hr", perUnit, instUnitLabel, computeHourly)
	} else {
		computePriceStr = fmt.Sprintf("$%.4f/hr", computeHourly)
	}
	fmt.Fprintf(&b, "    %-40s %s\n", instLabel, priceStyle.Render(computePriceStr))
	fmt.Fprintf(&b, "    %s\n\n", dim.Render(opts.LocationCode))

	// OS.
	fmt.Fprintf(&b, "  %s\n", accent.Render("Operating System"))
	osLine := fmt.Sprintf("%s  %dGB NVMe", opts.Image, opts.OSVolumeSize)
	osPrice := fmt.Sprintf("($%.2f/GB/mo)  $%.4f/hr", osVolUnitPrice, osVolPrice)
	fmt.Fprintf(&b, "    %-40s %s\n\n", osLine, priceStyle.Render(osPrice))

	// Storage volumes.
	if len(volDetails) > 0 {
		fmt.Fprintf(&b, "  %s\n", accent.Render("Storage"))
		for _, v := range volDetails {
			line := fmt.Sprintf("%s  %dGB %s", v.name, v.size, v.volType)
			vPrice := fmt.Sprintf("($%.2f/GB/mo)  $%.4f/hr", v.unitPrice, v.hourly)
			fmt.Fprintf(&b, "    %-40s %s\n", line, priceStyle.Render(vPrice))
		}
		fmt.Fprintln(&b)
	}

	// SSH keys.
	if len(opts.SSHKeyIDs) > 0 {
		fmt.Fprintf(&b, "  %s  %d key(s)\n\n", accent.Render("SSH Keys"), len(opts.SSHKeyIDs))
	}

	// Cost breakdown.
	fmt.Fprintf(&b, "  %s\n", dim.Render(strings.Repeat("─", 50)))
	fmt.Fprintf(&b, "  %-40s %s\n", "Compute total", fmt.Sprintf("$%.4f/hr", computeHourly))
	fmt.Fprintf(&b, "  %-40s %s\n", "Storage total", fmt.Sprintf("$%.4f/hr", storageHourly))
	total := computeHourly + storageHourly
	fmt.Fprintf(&b, "  %s  %s\n", bold.Render(fmt.Sprintf("%-40s", "Total")), bold.Render(fmt.Sprintf("$%.4f/hr", total)))
	fmt.Fprintf(&b, "  %s\n", dim.Render(strings.Repeat("─", 50)))

	return b.String()
}
