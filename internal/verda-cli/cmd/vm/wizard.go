package vm

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

// clientFunc lazily resolves a Verda API client. This allows the wizard
// to start without credentials — steps that don't need API (billing-type,
// kind, text inputs) run first, and the client is only resolved when an
// API-dependent loader fires.
type clientFunc func() (*verda.Client, error)

// buildCreateFlow builds the interactive wizard flow for vm create.
// It mirrors the web UI step order:
//
//	billing-type → contract → kind → instance-type → location →
//	image → os-volume-size → storage-size → ssh-keys →
//	startup-script → hostname → description
func buildCreateFlow(getClient clientFunc, opts *createOptions) *wizard.Flow {
	return &wizard.Flow{
		Name: "vm-create",
		Steps: []wizard.Step{
			stepBillingType(opts),
			stepContract(getClient, opts),
			stepKind(opts),
			stepInstanceType(getClient, opts),
			stepLocation(getClient, opts),
			stepImage(getClient, opts),
			stepOSVolumeSize(opts),
			stepStorageSize(opts),
			stepSSHKeys(getClient, opts),
			stepStartupScript(getClient, opts),
			stepHostname(opts),
			stepDescription(opts),
		},
	}
}

// --- Step 1: Billing Type ---

func stepBillingType(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "billing-type",
		Description: "Billing type",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: wizard.StaticChoices(
			wizard.Choice{Label: "On-Demand", Value: "on-demand", Description: "Pay as you go or long-term contract"},
			wizard.Choice{Label: "Spot Instance", Value: "spot", Description: "Lower price, may be interrupted"},
		),
		Setter: func(v any) {
			opts.IsSpot = v.(string) == "spot"
		},
		Resetter: func() {
			opts.IsSpot = false
		},
		IsSet: func() bool { return opts.IsSpot },
		Value: func() any {
			if opts.IsSpot {
				return "spot"
			}
			return "on-demand"
		},
	}
}

// --- Step 2: Contract ---

func stepContract(getClient clientFunc, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "contract",
		Description: "Contract period",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		DependsOn:   []string{"billing-type"},
		ShouldSkip: func(c map[string]any) bool {
			return c["billing-type"] == "spot"
		},
		Loader: func(ctx context.Context, _ tui.Prompter, _ map[string]any) ([]wizard.Choice, error) {
			choices := []wizard.Choice{
				{Label: "Pay as you go", Value: "PAY_AS_YOU_GO"},
			}
			client, err := getClient()
			if err != nil {
				return choices, nil //nolint:nilerr // Non-fatal: just offer pay-as-you-go.
			}
			periods, err := client.LongTerm.GetInstancePeriods(ctx)
			if err != nil {
				return choices, nil //nolint:nilerr // Non-fatal: just offer pay-as-you-go.
			}
			for _, p := range periods {
				if p.IsEnabled {
					desc := ""
					if p.DiscountPercentage > 0 {
						desc = fmt.Sprintf("%.0f%% discount", p.DiscountPercentage)
					}
					choices = append(choices, wizard.Choice{
						Label:       p.Name,
						Value:       p.Code,
						Description: desc,
					})
				}
			}
			return choices, nil
		},
		Setter: func(v any) {
			opts.Contract = v.(string)
		},
		Resetter: func() { opts.Contract = "" },
		IsSet:    func() bool { return opts.Contract != "" && !opts.IsSpot },
		Value:    func() any { return opts.Contract },
	}
}

// --- Step 3: Kind (GPU / CPU) ---

func stepKind(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "kind",
		Description: "Compute type",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: wizard.StaticChoices(
			wizard.Choice{Label: "GPU", Value: "gpu", Description: "GPU-accelerated instances"},
			wizard.Choice{Label: "CPU", Value: "cpu", Description: "CPU-only instances"},
		),
		Setter:   func(v any) { opts.Kind = v.(string) },
		Resetter: func() { opts.Kind = "" },
		IsSet:    func() bool { return opts.Kind != "" },
		Value:    func() any { return opts.Kind },
	}
}

// --- Step 4: Instance Type ---

func stepInstanceType(getClient clientFunc, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "instance-type",
		Description: "Instance type",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		DependsOn:   []string{"kind", "billing-type"},
		Loader: func(ctx context.Context, _ tui.Prompter, c map[string]any) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}
			types, err := client.InstanceTypes.Get(ctx, "usd")
			if err != nil {
				return nil, fmt.Errorf("fetching instance types: %w", err)
			}

			kind := c["kind"].(string)
			isSpot := c["billing-type"] == "spot"

			var choices []wizard.Choice
			for i := range types {
				t := &types[i]
				if !matchesKind(t.InstanceType, kind) {
					continue
				}
				price := t.PricePerHour
				if isSpot {
					price = t.SpotPrice
				}
				label := fmt.Sprintf("%s — %s, %s",
					t.InstanceType, formatGPU(t), formatMemory(t))
				desc := fmt.Sprintf("$%.2f/hr", float64(price))
				choices = append(choices, wizard.Choice{
					Label:       label,
					Value:       t.InstanceType,
					Description: desc,
				})
			}
			return choices, nil
		},
		Setter:   func(v any) { opts.InstanceType = v.(string) },
		Resetter: func() { opts.InstanceType = "" },
		IsSet:    func() bool { return opts.InstanceType != "" },
		Value:    func() any { return opts.InstanceType },
	}
}

// --- Step 5: Location ---

func stepLocation(getClient clientFunc, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "location",
		Description: "Datacenter location",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		DependsOn:   []string{"instance-type", "billing-type"},
		Loader: func(ctx context.Context, _ tui.Prompter, c map[string]any) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}
			isSpot := c["billing-type"] == "spot"
			instType := c["instance-type"].(string)

			avail, err := client.InstanceAvailability.GetAllAvailabilities(ctx, isSpot, "")
			if err != nil {
				return nil, fmt.Errorf("fetching availability: %w", err)
			}

			locations, err := client.Locations.Get(ctx)
			if err != nil {
				return nil, fmt.Errorf("fetching locations: %w", err)
			}
			locMap := make(map[string]verda.Location, len(locations))
			for _, loc := range locations {
				locMap[loc.Code] = loc
			}

			var choices []wizard.Choice
			for _, la := range avail {
				if slices.Contains(la.Availabilities, instType) {
					loc := locMap[la.LocationCode]
					label := fmt.Sprintf("%s (%s)", loc.Code, loc.Name)
					choices = append(choices, wizard.Choice{
						Label: label,
						Value: loc.Code,
					})
				}
			}
			return choices, nil
		},
		Setter:   func(v any) { opts.LocationCode = v.(string) },
		Resetter: func() { opts.LocationCode = verda.LocationFIN01 },
		IsSet:    func() bool { return opts.LocationCode != "" && opts.LocationCode != verda.LocationFIN01 },
		Value:    func() any { return opts.LocationCode },
		Default:  func(_ map[string]any) any { return verda.LocationFIN01 },
	}
}

// --- Step 6: OS Image ---

func stepImage(getClient clientFunc, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "image",
		Description: "Operating system image",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: func(ctx context.Context, _ tui.Prompter, _ map[string]any) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}
			images, err := client.Images.Get(ctx)
			if err != nil {
				return nil, fmt.Errorf("fetching images: %w", err)
			}
			var choices []wizard.Choice
			for _, img := range images {
				if img.IsCluster {
					continue
				}
				desc := ""
				if len(img.Details) > 0 {
					desc = strings.Join(img.Details, ", ")
				}
				choices = append(choices, wizard.Choice{
					Label:       img.Name,
					Value:       img.ID,
					Description: desc,
				})
			}
			return choices, nil
		},
		Setter:   func(v any) { opts.Image = v.(string) },
		Resetter: func() { opts.Image = "" },
		IsSet:    func() bool { return opts.Image != "" },
		Value:    func() any { return opts.Image },
	}
}

// --- Step 7: OS Volume Size ---

func stepOSVolumeSize(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "os-volume-size",
		Description: "OS volume size (GiB)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		Default:     func(_ map[string]any) any { return "50" },
		Validate: func(v any) error {
			s := v.(string)
			if s == "" {
				return nil
			}
			n, err := strconv.Atoi(s)
			if err != nil || n <= 0 {
				return fmt.Errorf("must be a positive integer")
			}
			return nil
		},
		Setter: func(v any) {
			if s := v.(string); s != "" {
				n, _ := strconv.Atoi(s)
				opts.OSVolumeSize = n
			}
		},
		Resetter: func() { opts.OSVolumeSize = 0 },
		IsSet:    func() bool { return opts.OSVolumeSize > 0 },
		Value:    func() any { return strconv.Itoa(opts.OSVolumeSize) },
	}
}

// --- Step 8: Storage Size ---

func stepStorageSize(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "storage-size",
		Description: "Additional storage size in GiB (0 to skip)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		Default:     func(_ map[string]any) any { return "0" },
		Validate: func(v any) error {
			s := v.(string)
			if s == "" {
				return nil
			}
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 {
				return fmt.Errorf("must be zero or a positive integer")
			}
			return nil
		},
		Setter: func(v any) {
			if s := v.(string); s != "" {
				n, _ := strconv.Atoi(s)
				opts.StorageSize = n
			}
		},
		Resetter: func() { opts.StorageSize = 0 },
		IsSet:    func() bool { return opts.StorageSize > 0 },
		Value:    func() any { return strconv.Itoa(opts.StorageSize) },
	}
}

// --- Step 9: SSH Keys ---

func stepSSHKeys(getClient clientFunc, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "ssh-keys",
		Description: "SSH keys to inject",
		Prompt:      wizard.MultiSelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, _ tui.Prompter, _ map[string]any) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}
			keys, err := client.SSHKeys.GetAllSSHKeys(ctx)
			if err != nil {
				return nil, fmt.Errorf("fetching SSH keys: %w", err)
			}
			choices := make([]wizard.Choice, len(keys))
			for i, k := range keys {
				choices[i] = wizard.Choice{
					Label:       k.Name,
					Value:       k.ID,
					Description: k.Fingerprint,
				}
			}
			return choices, nil
		},
		Setter: func(v any) {
			opts.SSHKeyIDs = v.([]string)
		},
		Resetter: func() { opts.SSHKeyIDs = nil },
		IsSet:    func() bool { return len(opts.SSHKeyIDs) > 0 },
		Value:    func() any { return opts.SSHKeyIDs },
	}
}

// --- Step 10: Startup Script ---

func stepStartupScript(getClient clientFunc, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "startup-script",
		Description: "Startup script (optional)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, _ tui.Prompter, _ map[string]any) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}
			scripts, err := client.StartupScripts.GetAllStartupScripts(ctx)
			if err != nil {
				return nil, fmt.Errorf("fetching startup scripts: %w", err)
			}
			choices := []wizard.Choice{
				{Label: "None", Value: ""},
			}
			for _, s := range scripts {
				choices = append(choices, wizard.Choice{
					Label: s.Name,
					Value: s.ID,
				})
			}
			return choices, nil
		},
		Setter:   func(v any) { opts.StartupScriptID = v.(string) },
		Resetter: func() { opts.StartupScriptID = "" },
		IsSet:    func() bool { return opts.StartupScriptID != "" },
		Value:    func() any { return opts.StartupScriptID },
	}
}

// --- Step 11: Hostname ---

func stepHostname(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "hostname",
		Description: "Hostname",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		Setter:      func(v any) { opts.Hostname = v.(string) },
		Resetter:    func() { opts.Hostname = "" },
		IsSet:       func() bool { return opts.Hostname != "" },
		Value:       func() any { return opts.Hostname },
	}
}

// --- Step 12: Description ---

func stepDescription(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "description",
		Description: "Description",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		DependsOn:   []string{"hostname"},
		Default: func(c map[string]any) any {
			if h, ok := c["hostname"].(string); ok && h != "" {
				return h
			}
			return ""
		},
		Setter:   func(v any) { opts.Description = v.(string) },
		Resetter: func() { opts.Description = "" },
		IsSet:    func() bool { return opts.Description != "" },
		Value:    func() any { return opts.Description },
	}
}

// --- Helpers ---

func matchesKind(instanceType, kind string) bool {
	isCPU := strings.HasPrefix(strings.ToUpper(instanceType), "CPU.")
	switch strings.ToLower(kind) {
	case "cpu":
		return isCPU
	case "gpu":
		return !isCPU
	default:
		return true
	}
}

func formatGPU(t *verda.InstanceTypeInfo) string {
	if t.GPU.NumberOfGPUs > 0 {
		return fmt.Sprintf("%dx %s", t.GPU.NumberOfGPUs, t.GPU.Description)
	}
	return fmt.Sprintf("%d cores", t.CPU.NumberOfCores)
}

func formatMemory(t *verda.InstanceTypeInfo) string {
	if t.GPUMemory.SizeInGigabytes > 0 {
		return fmt.Sprintf("%dGB VRAM, %dGB RAM", t.GPUMemory.SizeInGigabytes, t.Memory.SizeInGigabytes)
	}
	return fmt.Sprintf("%dGB RAM", t.Memory.SizeInGigabytes)
}
