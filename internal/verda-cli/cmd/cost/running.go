package cost

import (
	"context"
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newCmdRunning(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "running",
		Short: "Show costs of currently running instances",
		Long: cmdutil.LongDesc(`
			Calculate the cost of all currently running instances,
			including their attached volumes. Shows per-instance
			breakdown and total burn rate.
		`),
		Example: cmdutil.Examples(`
			verda cost running
			verda cost running -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunning(cmd, f, ioStreams)
		},
	}
}

// RunningInstanceCost represents the cost of a single running instance.
type RunningInstanceCost struct {
	ID           string  `json:"id" yaml:"id"`
	Hostname     string  `json:"hostname" yaml:"hostname"`
	InstanceType string  `json:"instance_type" yaml:"instance_type"`
	Location     string  `json:"location" yaml:"location"`
	Status       string  `json:"status" yaml:"status"`
	Hourly       float64 `json:"hourly" yaml:"hourly"`
	Daily        float64 `json:"daily" yaml:"daily"`
	Monthly      float64 `json:"monthly" yaml:"monthly"`
	VolumeCount  int     `json:"volume_count" yaml:"volume_count"`
	VolumeGB     int     `json:"volume_gb" yaml:"volume_gb"`
	VolumeHourly float64 `json:"volume_hourly" yaml:"volume_hourly"`
}

// RunningCostSummary is the structured output.
type RunningCostSummary struct {
	Instances []RunningInstanceCost `json:"instances" yaml:"instances"`
	Total     CostTotalItem         `json:"total" yaml:"total"`
}

func runRunning(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading instances and volumes...")
	}

	instances, err := client.Instances.Get(ctx, "")
	if err != nil {
		if sp != nil {
			sp.Stop("")
		}
		return err
	}

	// Fetch volume types for pricing.
	volTypes, err := client.VolumeTypes.GetAllVolumeTypes(ctx)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return fmt.Errorf("fetching volume pricing: %w", err)
	}

	vtMap := make(map[string]verda.VolumeType, len(volTypes))
	for _, vt := range volTypes {
		vtMap[vt.Type] = vt
	}

	summary := RunningCostSummary{}

	for i := range instances {
		inst := &instances[i]
		if inst.Status != verda.StatusRunning {
			continue
		}

		instanceHourly := float64(inst.PricePerHour)

		// Calculate volume costs.
		var volGB int
		var volHourly float64
		var volCount int

		volumeIDs := cmdutil.UniqueVolumeIDs(inst)
		for _, volID := range volumeIDs {
			vol, err := client.Volumes.GetVolume(ctx, volID)
			if err != nil {
				continue
			}
			volCount++
			volGB += vol.Size
			if vt, ok := vtMap[vol.Type]; ok {
				volHourly += math.Ceil(vt.Price.PricePerMonthPerGB*float64(vol.Size)/hoursInMonth*10000) / 10000
			}
		}

		totalHourly := instanceHourly + volHourly
		rc := RunningInstanceCost{
			ID:           inst.ID,
			Hostname:     inst.Hostname,
			InstanceType: inst.InstanceType,
			Location:     inst.Location,
			Status:       inst.Status,
			Hourly:       totalHourly,
			Daily:        totalHourly * 24,
			Monthly:      totalHourly * hoursInMonth,
			VolumeCount:  volCount,
			VolumeGB:     volGB,
			VolumeHourly: volHourly,
		}

		summary.Instances = append(summary.Instances, rc)
		summary.Total.Hourly += rc.Hourly
		summary.Total.Daily += rc.Daily
		summary.Total.Monthly += rc.Monthly
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Running costs:", summary)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), summary); wrote {
		return err
	}

	if len(summary.Instances) == 0 {
		_, _ = fmt.Fprintln(ioStreams.Out, "No running instances.")
		return nil
	}

	renderRunning(ioStreams.Out, &summary)
	return nil
}

func renderRunning(w interface{ Write([]byte) (int, error) }, s *RunningCostSummary) {
	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	price := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	sep := dim.Render(strings.Repeat("─", 60))

	_, _ = fmt.Fprintf(w, "\n  %s  %s\n",
		bold.Render("Running Instances"),
		dim.Render(fmt.Sprintf("(%d)", len(s.Instances))))
	_, _ = fmt.Fprintf(w, "  %s\n\n", sep)

	for _, inst := range s.Instances {
		_, _ = fmt.Fprintf(w, "  %s  %s  %s\n",
			bold.Render(fmt.Sprintf("%-25s", inst.Hostname)),
			dim.Render(inst.InstanceType),
			dim.Render(inst.Location))

		_, _ = fmt.Fprintf(w, "    Instance:  %s/hr\n",
			price.Render(formatPrice(inst.Hourly-inst.VolumeHourly)))

		if inst.VolumeCount > 0 {
			_, _ = fmt.Fprintf(w, "    Volumes:   %s/hr  %s\n",
				price.Render(formatPrice(inst.VolumeHourly)),
				dim.Render(fmt.Sprintf("(%d vol, %dGB)", inst.VolumeCount, inst.VolumeGB)))
		}

		_, _ = fmt.Fprintf(w, "    Total:     %s/hr  %s/day  %s/mo\n\n",
			price.Render(formatPrice(inst.Hourly)),
			price.Render(formatPrice(inst.Daily)),
			price.Render(formatPrice(inst.Monthly)))
	}

	_, _ = fmt.Fprintf(w, "  %s\n", sep)
	_, _ = fmt.Fprintf(w, "  %s     %s/hr  %s/day  %s/mo\n",
		bold.Render("Total Burn"),
		bold.Render(price.Render(formatPrice(s.Total.Hourly))),
		bold.Render(price.Render(formatPrice(s.Total.Daily))),
		bold.Render(price.Render(formatPrice(s.Total.Monthly))))
	_, _ = fmt.Fprintf(w, "  %s\n\n", sep)
}
