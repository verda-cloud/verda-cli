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

package volume

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type createOptions struct {
	Name     string
	Size     int
	Type     string
	Location string
	Yes      bool
	Wait     cmdutil.WaitOptions
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
			Agent mode requires all flags and --yes.
		`),
		Example: cmdutil.Examples(`
			# Interactive
			verda volume create

			# Non-interactive
			verda volume create --name my-vol --size 100 --type NVMe --location FIN-01 --yes
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&opts.Name, "name", "", "Volume name")
	flags.IntVar(&opts.Size, "size", 0, "Volume size in GiB")
	flags.StringVar(&opts.Type, "type", "", "Volume type (default: NVMe)")
	flags.StringVar(&opts.Location, "location", "", "Location code, e.g. FIN-01")
	flags.BoolVar(&opts.Yes, "yes", false, "Skip confirmation (required in agent mode)")
	opts.Wait.AddFlags(flags, false) // --wait defaults to false for volume create

	return cmd
}

//nolint:gocyclo // Interactive CLI command with multiple prompt steps — inherently complex.
func runCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *createOptions) error {
	// Agent mode never prompts: creating a billable volume without --yes is an explicit error.
	if f.AgentMode() && !opts.Yes {
		return cmdutil.NewConfirmationRequiredError("create volume")
	}

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

	// Volume type: NVMe is the only provisionable type (HDD deprecated), so we
	// default it rather than prompt. Pricing is still shown in the summary below.
	// An unknown type must fail loudly here — it would otherwise price at $0.
	if opts.Type == "" {
		opts.Type = verda.VolumeTypeNVMe
	}
	if _, ok := vtMap[opts.Type]; !ok {
		return cmdutil.UsageErrorf(cmd, "invalid --type %q (valid types: %s)",
			opts.Type, strings.Join(cmdutil.ValidVolumeTypeNames(vtMap), ", "))
	}

	// Name.
	if opts.Name == "" {
		name, err := prompter.TextInput(ctx, "Volume name")
		if err != nil {
			if cmdutil.IsPromptCancel(err) {
				return nil // User pressed Esc/Ctrl+C.
			}
			return err
		}
		if strings.TrimSpace(name) == "" {
			return nil // Blank input cancels.
		}
		opts.Name = strings.TrimSpace(name)
	}

	// Size.
	if opts.Size == 0 {
		size, err := promptSize(ctx, prompter)
		if err != nil {
			return err
		}
		if size == 0 {
			return nil // Canceled or blank input.
		}
		opts.Size = size
	}

	// Location.
	if opts.Location == "" {
		location, err := promptLocation(ctx, f, prompter, client)
		if err != nil {
			return err
		}
		if location == "" {
			return nil // Canceled.
		}
		opts.Location = location
	}

	// Summary with pricing.
	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	priceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

	monthlyPerGB := vtMap[opts.Type].Price.PricePerMonthPerGB
	hourly := cmdutil.VolumeHourlyPrice(monthlyPerGB, opts.Size)
	monthly := cmdutil.VolumeMonthlyPrice(monthlyPerGB, opts.Size)

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

	if !opts.Yes {
		confirmed, err := prompter.Confirm(ctx, "Create volume?", tui.WithConfirmDefault(true))
		if err != nil {
			if cmdutil.IsPromptCancel(err) {
				_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
				return nil
			}
			return err
		}
		if !confirmed {
			_, _ = fmt.Fprintln(ioStreams.ErrOut, "Canceled.")
			return nil
		}
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

	if f.AgentMode() {
		// Structured result. With --wait, the polled volume document below
		// is the single agent-mode payload instead.
		if !opts.Wait.Wait {
			result := map[string]any{
				"action":   "create",
				"id":       volID,
				"name":     opts.Name,
				"size_gb":  opts.Size,
				"type":     opts.Type,
				"location": opts.Location,
				"status":   "created",
			}
			_, _ = cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), result)
		}
	} else {
		_, _ = fmt.Fprintf(ioStreams.Out, "Created volume: %s (%s)\n", opts.Name, volID)
	}

	if opts.Wait.Wait {
		vol, err := cmdutil.PollVolumeStatus(ctx, ioStreams.ErrOut, client, volID, opts.Wait, "detached")
		if err != nil {
			return err
		}
		if vol != nil {
			if wrote, werr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), vol); wrote {
				return werr
			}
		}
	}
	return nil
}

// promptSize asks for the volume size in GiB. Returns (0, nil) when the user
// cancels or submits blank input — both abort the create flow quietly.
func promptSize(ctx context.Context, prompter tui.Prompter) (int, error) {
	sizeStr, err := prompter.TextInput(ctx, "Size in GiB", tui.WithDefault("100"))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return 0, nil // User pressed Esc/Ctrl+C.
		}
		return 0, err
	}
	if strings.TrimSpace(sizeStr) == "" {
		return 0, nil // Blank input cancels.
	}
	size, err := strconv.Atoi(strings.TrimSpace(sizeStr))
	if err != nil || size <= 0 {
		return 0, errors.New("size must be a positive integer")
	}
	return size, nil
}

// promptLocation fetches available locations and asks the user to pick one.
// Returns "" when the user cancels.
func promptLocation(ctx context.Context, f cmdutil.Factory, prompter tui.Prompter, client *verda.Client) (string, error) {
	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading locations...")
	}
	locations, err := client.Locations.Get(ctx)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return "", fmt.Errorf("fetching locations: %w", err)
	}

	labels := make([]string, len(locations))
	for i, loc := range locations {
		labels[i] = fmt.Sprintf("%s (%s)", loc.Code, loc.Name)
	}
	idx, err := prompter.Select(ctx, "Location", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return "", nil // User pressed Esc/Ctrl+C.
		}
		return "", err
	}
	return locations[idx].Code, nil
}
