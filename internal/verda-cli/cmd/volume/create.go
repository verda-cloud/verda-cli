package volume

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type createOptions struct {
	Name     string
	Size     int
	Type     string
	Location string
}

// NewCmdCreate creates the volume create command.
func NewCmdCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &createOptions{}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new volume",
		Long: cmdutil.LongDesc(`
			Create a new block storage volume. If flags are omitted,
			an interactive prompt guides you through the options.
		`),
		Example: cmdutil.Examples(`
			# Interactive
			verda volume create

			# Non-interactive
			verda volume create --name my-vol --size 100 --type NVMe --location FIN-01
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "Volume name")
	flags.IntVar(&opts.Size, "size", 0, "Volume size in GiB")
	flags.StringVar(&opts.Type, "type", "", "Volume type: NVMe or HDD")
	flags.StringVar(&opts.Location, "location", "", "Location code, e.g. FIN-01")

	return cmd
}

func runCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *createOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	prompter := f.Prompter()
	ctx := cmd.Context()

	// Fetch volume types for pricing display.
	var volumeTypes []verda.VolumeType
	if status := f.Status(); status != nil {
		var sp interface{ Stop(string) }
		sp, _ = status.Spinner(ctx, "Loading volume types...")
		volumeTypes, err = client.VolumeTypes.GetAllVolumeTypes(ctx)
		sp.Stop("")
	} else {
		volumeTypes, err = client.VolumeTypes.GetAllVolumeTypes(ctx)
	}
	if err != nil {
		return fmt.Errorf("fetching volume types: %w", err)
	}
	vtMap := make(map[string]verda.VolumeType, len(volumeTypes))
	for _, vt := range volumeTypes {
		vtMap[vt.Type] = vt
	}

	// Volume type.
	if opts.Type == "" {
		nvmeLabel := "NVMe (fast SSD)"
		hddLabel := "HDD (large capacity)"
		if vt, ok := vtMap[verda.VolumeTypeNVMe]; ok && vt.Price.PricePerMonthPerGB > 0 {
			nvmeLabel = fmt.Sprintf("NVMe (fast SSD)  $%.2f/GB/mo", vt.Price.PricePerMonthPerGB)
		}
		if vt, ok := vtMap[verda.VolumeTypeHDD]; ok && vt.Price.PricePerMonthPerGB > 0 {
			hddLabel = fmt.Sprintf("HDD (large capacity)  $%.2f/GB/mo", vt.Price.PricePerMonthPerGB)
		}
		idx, err := prompter.Select(ctx, "Volume type", []string{nvmeLabel, hddLabel})
		if err != nil {
			return nil //nolint:nilerr
		}
		if idx == 0 {
			opts.Type = verda.VolumeTypeNVMe
		} else {
			opts.Type = verda.VolumeTypeHDD
		}
	}

	// Name.
	if opts.Name == "" {
		name, err := prompter.TextInput(ctx, "Volume name")
		if err != nil || strings.TrimSpace(name) == "" {
			return nil //nolint:nilerr
		}
		opts.Name = strings.TrimSpace(name)
	}

	// Size.
	if opts.Size == 0 {
		sizeStr, err := prompter.TextInput(ctx, "Size in GiB", tui.WithDefault("100"))
		if err != nil || strings.TrimSpace(sizeStr) == "" {
			return nil //nolint:nilerr
		}
		size, err := strconv.Atoi(strings.TrimSpace(sizeStr))
		if err != nil || size <= 0 {
			return fmt.Errorf("size must be a positive integer")
		}
		opts.Size = size
	}

	// Location.
	if opts.Location == "" {
		var sp interface{ Stop(string) }
		if status := f.Status(); status != nil {
			sp, _ = status.Spinner(ctx, "Loading locations...")
		}
		locations, err := client.Locations.Get(ctx)
		if sp != nil {
			sp.Stop("")
		}
		if err != nil {
			return fmt.Errorf("fetching locations: %w", err)
		}

		labels := make([]string, len(locations))
		for i, loc := range locations {
			labels[i] = fmt.Sprintf("%s (%s)", loc.Code, loc.Name)
		}
		idx, err := prompter.Select(ctx, "Location", labels)
		if err != nil {
			return nil //nolint:nilerr
		}
		opts.Location = locations[idx].Code
	}

	// Summary with pricing.
	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	priceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

	var monthlyPerGB float64
	if vt, ok := vtMap[opts.Type]; ok {
		monthlyPerGB = vt.Price.PricePerMonthPerGB
	}
	const hoursInMonth = 730 // 365*24/12, matching web frontend
	hourly := math.Ceil(monthlyPerGB*float64(opts.Size)/hoursInMonth*10000) / 10000
	monthly := monthlyPerGB * float64(opts.Size)

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n", bold.Render("Volume Summary"))
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n", dim.Render(strings.Repeat("─", 45)))
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s  %s\n", dim.Render("Name:    "), opts.Name)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s  %dGB\n", dim.Render("Size:    "), opts.Size)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s  %s\n", dim.Render("Type:    "), opts.Type)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s  %s\n", dim.Render("Location:"), opts.Location)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s\n", dim.Render(strings.Repeat("─", 45)))
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %-30s %s\n", "Unit price", priceStyle.Render(fmt.Sprintf("$%.2f/GB/mo", monthlyPerGB)))
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %-30s %s\n", "Monthly", priceStyle.Render(fmt.Sprintf("$%.2f/mo", monthly)))
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s  %s\n", bold.Render(fmt.Sprintf("%-30s", "Hourly")), bold.Render(priceStyle.Render(fmt.Sprintf("$%.4f/hr", hourly))))
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n", dim.Render(strings.Repeat("─", 45)))

	confirmed, err := prompter.Confirm(ctx, "Create volume?", tui.WithConfirmDefault(true))
	if err != nil || !confirmed {
		_, _ = fmt.Fprintln(ioStreams.ErrOut, "Cancelled.")
		return nil
	}

	// Create.
	req := verda.VolumeCreateRequest{
		Name:         opts.Name,
		Size:         opts.Size,
		Type:         opts.Type,
		LocationCode: opts.Location,
	}
	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Request payload:", req)

	createCtx, cancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(createCtx, fmt.Sprintf("Creating volume %s...", opts.Name))
	}
	volID, err := client.Volumes.CreateVolume(createCtx, req)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Created volume: %s (%s)\n", opts.Name, volID)
	return nil
}
