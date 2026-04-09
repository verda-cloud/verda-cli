package status

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// Dashboard is the structured output for the status command.
type Dashboard struct {
	Instances  InstanceSummary  `json:"instances" yaml:"instances"`
	Volumes    VolumeSummary    `json:"volumes" yaml:"volumes"`
	Financials FinancialSummary `json:"financials" yaml:"financials"`
	Locations  []LocationInfo   `json:"locations" yaml:"locations"`
}

// InstanceSummary holds instance counts grouped by status.
type InstanceSummary struct {
	Total        int `json:"total" yaml:"total"`
	Running      int `json:"running" yaml:"running"`
	Offline      int `json:"offline" yaml:"offline"`
	Provisioning int `json:"provisioning,omitempty" yaml:"provisioning,omitempty"`
	Error        int `json:"error,omitempty" yaml:"error,omitempty"`
	Other        int `json:"other,omitempty" yaml:"other,omitempty"`
	SpotRunning  int `json:"spot_running" yaml:"spot_running"`
}

// VolumeSummary holds volume counts and total size.
type VolumeSummary struct {
	Total       int `json:"total" yaml:"total"`
	Attached    int `json:"attached" yaml:"attached"`
	Detached    int `json:"detached" yaml:"detached"`
	TotalSizeGB int `json:"total_size_gb" yaml:"total_size_gb"`
}

// FinancialSummary holds burn rate, balance, and runway.
type FinancialSummary struct {
	BurnRateHourly float64 `json:"burn_rate_hourly" yaml:"burn_rate_hourly"`
	BurnRateDaily  float64 `json:"burn_rate_daily" yaml:"burn_rate_daily"`
	Balance        float64 `json:"balance" yaml:"balance"`
	RunwayDays     int     `json:"runway_days" yaml:"runway_days"`
	Currency       string  `json:"currency" yaml:"currency"`
}

// LocationInfo holds per-location instance counts.
type LocationInfo struct {
	Code      string `json:"code" yaml:"code"`
	Instances int    `json:"instances" yaml:"instances"`
	Running   int    `json:"running" yaml:"running"`
	Offline   int    `json:"offline" yaml:"offline"`
}

// NewCmdStatus creates the status command.
func NewCmdStatus(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show dashboard overview of your Verda Cloud resources",
		Long: cmdutil.LongDesc(`
			Display a dashboard overview including instance and volume
			counts, burn rate, account balance, runway estimate, and
			resource distribution across locations.
		`),
		Example: cmdutil.Examples(`
			verda status
			verda status -o json
		`),
		Aliases: []string{"dash"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, f, ioStreams)
		},
	}
}

func runStatus(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading dashboard...")
	}

	// Fetch all data off the main goroutine (sequential to keep error handling simple).
	type result struct {
		instances []verda.Instance
		volumes   []verda.Volume
		balance   *verda.Balance
		err       error
	}

	ch := make(chan result, 1)
	go func() {
		var r result
		// Instances
		r.instances, r.err = client.Instances.Get(ctx, "")
		if r.err != nil {
			ch <- r
			return
		}
		// Volumes
		r.volumes, r.err = client.Volumes.ListVolumes(ctx)
		if r.err != nil {
			ch <- r
			return
		}
		// Balance
		r.balance, r.err = client.Balance.Get(ctx)
		ch <- r
	}()

	res := <-ch
	if sp != nil {
		sp.Stop("")
	}
	if res.err != nil {
		return res.err
	}

	dashboard := buildDashboard(res.instances, res.volumes, res.balance)

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Status dashboard:", dashboard)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), dashboard); wrote {
		return err
	}

	renderDashboard(ioStreams.Out, &dashboard)
	return nil
}

func buildDashboard(instances []verda.Instance, volumes []verda.Volume, balance *verda.Balance) Dashboard {
	d := Dashboard{}

	// Instance aggregation.
	locMap := make(map[string]*LocationInfo)
	for i := range instances {
		inst := &instances[i]
		d.Instances.Total++

		switch inst.Status {
		case verda.StatusRunning:
			d.Instances.Running++
			if inst.IsSpot {
				d.Instances.SpotRunning++
			}
		case verda.StatusOffline:
			d.Instances.Offline++
		case verda.StatusProvisioning, verda.StatusValidating, verda.StatusOrdered, verda.StatusNew:
			d.Instances.Provisioning++
		case verda.StatusError:
			d.Instances.Error++
		default:
			d.Instances.Other++
		}

		// Offline instances still charge — include all non-terminated instances in burn rate.
		if inst.Status == verda.StatusRunning || inst.Status == verda.StatusOffline {
			d.Financials.BurnRateHourly += cmdutil.InstanceTotalHourlyCost(inst)
		}

		// Location tracking.
		loc, ok := locMap[inst.Location]
		if !ok {
			loc = &LocationInfo{Code: inst.Location}
			locMap[inst.Location] = loc
		}
		loc.Instances++
		switch inst.Status {
		case verda.StatusRunning:
			loc.Running++
		case verda.StatusOffline:
			loc.Offline++
		}
	}

	// Volume aggregation — both attached and detached volumes incur charges.
	for i := range volumes {
		vol := &volumes[i]
		d.Volumes.Total++
		d.Volumes.TotalSizeGB += vol.Size

		switch vol.Status {
		case verda.VolumeStatusAttached:
			d.Volumes.Attached++
		case verda.VolumeStatusDetached:
			d.Volumes.Detached++
		}

		d.Financials.BurnRateHourly += vol.BaseHourlyCost
	}

	// Financials.
	d.Financials.BurnRateDaily = d.Financials.BurnRateHourly * 24
	if balance != nil {
		d.Financials.Balance = balance.Amount
		d.Financials.Currency = balance.Currency
	}
	if d.Financials.BurnRateDaily > 0 {
		d.Financials.RunwayDays = int(math.Floor(d.Financials.Balance / d.Financials.BurnRateDaily))
	}

	// Locations sorted by instance count descending, then code ascending for stability.
	for _, loc := range locMap {
		d.Locations = append(d.Locations, *loc)
	}
	sort.Slice(d.Locations, func(i, j int) bool {
		if d.Locations[i].Instances != d.Locations[j].Instances {
			return d.Locations[i].Instances > d.Locations[j].Instances
		}
		return d.Locations[i].Code < d.Locations[j].Code
	})

	return d
}

func renderDashboard(w interface{ Write([]byte) (int, error) }, d *Dashboard) {
	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	price := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

	// Build content lines.
	var lines []string

	// Header.
	lines = append(lines,
		bold.Render("Verda Cloud Status"),
		dim.Render(strings.Repeat("─", 45)))

	// Instances.
	instParts := []string{green.Render(fmt.Sprintf("%d running", d.Instances.Running))}
	if d.Instances.Offline > 0 {
		instParts = append(instParts, yellow.Render(fmt.Sprintf("%d offline", d.Instances.Offline)))
	}
	if d.Instances.Provisioning > 0 {
		instParts = append(instParts, yellow.Render(fmt.Sprintf("%d provisioning", d.Instances.Provisioning)))
	}
	if d.Instances.Error > 0 {
		instParts = append(instParts, red.Render(fmt.Sprintf("%d error", d.Instances.Error)))
	}
	lines = append(lines, fmt.Sprintf("%-13s%s", bold.Render("Instances:"), strings.Join(instParts, "  ")))

	// Warning: offline instances still charge.
	if d.Instances.Offline > 0 {
		lines = append(lines, fmt.Sprintf("%-13s%s",
			"",
			yellow.Render(fmt.Sprintf("! %d offline instance(s) still charging", d.Instances.Offline))))
	}

	// Spot (only if any running spot instances).
	if d.Instances.SpotRunning > 0 {
		lines = append(lines, fmt.Sprintf("%-13s%s",
			bold.Render("Spot:"),
			yellow.Render(fmt.Sprintf("%d of %d running", d.Instances.SpotRunning, d.Instances.Running))))
	}

	// Volumes.
	if d.Volumes.Total > 0 {
		volParts := []string{green.Render(fmt.Sprintf("%d attached", d.Volumes.Attached))}
		if d.Volumes.Detached > 0 {
			volParts = append(volParts, dim.Render(fmt.Sprintf("%d detached", d.Volumes.Detached)))
		}
		volParts = append(volParts, dim.Render(fmt.Sprintf("%d GB", d.Volumes.TotalSizeGB)))
		lines = append(lines, fmt.Sprintf("%-13s%s", bold.Render("Volumes:"), strings.Join(volParts, "  ")))
	}

	// Burn rate and balance.
	lines = append(lines,
		"",
		fmt.Sprintf("%-13s%s  %s",
			bold.Render("Burn rate:"),
			price.Render(cmdutil.FormatPrice(d.Financials.BurnRateHourly)+"/hr"),
			dim.Render(fmt.Sprintf("(%s/day)", cmdutil.FormatPrice(d.Financials.BurnRateDaily)))),
		fmt.Sprintf("%-13s%s",
			bold.Render("Balance:"),
			price.Render(fmt.Sprintf("$%.2f", d.Financials.Balance))))

	// Runway.
	if d.Financials.RunwayDays > 0 {
		lines = append(lines, fmt.Sprintf("%-13s%s",
			bold.Render("Runway:"),
			dim.Render(fmt.Sprintf("~%d days at current rate", d.Financials.RunwayDays))))
	}

	// Locations.
	if len(d.Locations) > 0 {
		lines = append(lines, "")
		maxLocs := 5
		for i, loc := range d.Locations {
			if i >= maxLocs {
				lines = append(lines, fmt.Sprintf("%-13s%s",
					"",
					dim.Render(fmt.Sprintf("+%d more", len(d.Locations)-maxLocs))))
				break
			}
			label := ""
			if i == 0 {
				label = bold.Render("Locations:")
			}

			locDesc := fmt.Sprintf("%s (%d instance", loc.Code, loc.Instances)
			if loc.Instances != 1 {
				locDesc += "s"
			}
			if loc.Offline > 0 {
				locDesc += fmt.Sprintf(", %d offline", loc.Offline)
			}
			locDesc += ")"
			lines = append(lines, fmt.Sprintf("%-13s%s", label, locDesc))
		}
	}

	// Disclaimer.
	lines = append(lines,
		"",
		dim.Render("* Estimated. For details, see Billing & Settings at console.verda.com"))

	// Render box.
	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1, 2)

	_, _ = fmt.Fprintf(w, "\n%s\n\n", box.Render(content))
}
