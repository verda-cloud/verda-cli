package instancetypes

import (
	"context"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type listOptions struct {
	GPU  bool
	CPU  bool
	Spot bool
}

// NewCmdInstanceTypes creates the instance-types command.
func NewCmdInstanceTypes(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &listOptions{}

	cmd := &cobra.Command{
		Use:     "instance-types",
		Aliases: []string{"types"},
		Short:   "List available instance types with specs and pricing",
		Long: cmdutil.LongDesc(`
			List all available instance types with their specifications
			and pricing. Filter by GPU or CPU to narrow results.
		`),
		Example: cmdutil.Examples(`
			# All instance types
			verda instance-types

			# GPU instances only
			verda instance-types --gpu

			# CPU instances only
			verda instance-types --cpu

			# Show spot pricing
			verda instance-types --spot

			# JSON output
			verda instance-types -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstanceTypes(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&opts.GPU, "gpu", false, "Show only GPU instance types")
	flags.BoolVar(&opts.CPU, "cpu", false, "Show only CPU instance types")
	flags.BoolVar(&opts.Spot, "spot", false, "Show spot pricing instead of on-demand")

	return cmd
}

func runInstanceTypes(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *listOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading instance types...")
	}
	types, err := client.InstanceTypes.Get(ctx, "usd")
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	// Filter.
	filtered := types[:0]
	for i := range types {
		isGPU := types[i].GPU.NumberOfGPUs > 0
		if opts.GPU && !isGPU {
			continue
		}
		if opts.CPU && isGPU {
			continue
		}
		filtered = append(filtered, types[i])
	}
	types = filtered

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), fmt.Sprintf("API response: %d type(s):", len(types)), types)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), types); wrote {
		return err
	}

	if len(types) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No instance types found.")
		return nil
	}

	renderTypes(ioStreams.Out, types, opts.Spot)
	return nil
}

func renderTypes(w interface{ Write([]byte) (int, error) }, types []verda.InstanceTypeInfo, showSpot bool) {
	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	price := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	sep := dim.Render(strings.Repeat("─", 90))

	// Group by GPU vs CPU.
	var gpuTypes, cpuTypes []verda.InstanceTypeInfo
	for i := range types {
		if types[i].GPU.NumberOfGPUs > 0 {
			gpuTypes = append(gpuTypes, types[i])
		} else {
			cpuTypes = append(cpuTypes, types[i])
		}
	}

	priceLabel := "$/hr"
	if showSpot {
		priceLabel = "$/hr (spot)"
	}

	_, _ = fmt.Fprintf(w, "\n  %-20s  %-22s  %5s  %6s  %6s  %10s\n",
		"TYPE", "COMPUTE", "vCPU", "RAM", "VRAM", priceLabel)
	_, _ = fmt.Fprintf(w, "  %s\n", sep)

	if len(gpuTypes) > 0 {
		_, _ = fmt.Fprintf(w, "\n  %s\n\n", bold.Render("GPU Instances"))
		for i := range gpuTypes {
			renderTypeRow(w, &gpuTypes[i], showSpot, price, dim)
		}
	}

	if len(cpuTypes) > 0 {
		_, _ = fmt.Fprintf(w, "\n  %s\n\n", bold.Render("CPU Instances"))
		for i := range cpuTypes {
			renderTypeRow(w, &cpuTypes[i], showSpot, price, dim)
		}
	}

	_, _ = fmt.Fprintln(w)
}

func renderTypeRow(w interface{ Write([]byte) (int, error) }, t *verda.InstanceTypeInfo, showSpot bool, priceStyle, dimStyle lipgloss.Style) {
	compute := t.CPU.Description
	if t.GPU.NumberOfGPUs > 0 {
		compute = fmt.Sprintf("%dx %s", t.GPU.NumberOfGPUs, t.GPU.Description)
	}

	vram := ""
	if t.GPUMemory.SizeInGigabytes > 0 {
		vram = fmt.Sprintf("%dGB", t.GPUMemory.SizeInGigabytes)
	}

	p := float64(t.PricePerHour)
	if showSpot && float64(t.SpotPrice) > 0 {
		p = float64(t.SpotPrice)
	}

	priceStr := priceStyle.Render(fmt.Sprintf("$%.4f", p))
	if p == 0 {
		priceStr = dimStyle.Render("n/a")
	}

	_, _ = fmt.Fprintf(w, "  %-20s  %-22s  %5d  %4dGB  %6s  %10s\n",
		t.InstanceType,
		compute,
		t.CPU.NumberOfCores,
		t.Memory.SizeInGigabytes,
		vram,
		priceStr)
}
