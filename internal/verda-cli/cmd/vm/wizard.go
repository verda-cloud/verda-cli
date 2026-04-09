package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	"github.com/verda-cloud/verdagostack/pkg/tui/bubbletea"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const (
	billingTypeSpot = "spot"
	kindGPU         = "gpu"

	contractPayAsYouGo = "PAY_AS_YOU_GO"
	contractSpot       = "SPOT"
	contractLongTerm   = "LONG_TERM"

	unitLabelGPU  = "GPU"
	unitLabelVCPU = "vCPU"

	billingTypeOnDemand = "on-demand"
)

// clientFunc lazily resolves a Verda API client. This allows the wizard
// to start without credentials — steps that don't need API (billing-type,
// kind, text inputs) run first, and the client is only resolved when an
// API-dependent loader fires.
type clientFunc func() (*verda.Client, error)

// WizardMode controls which steps are included in the wizard flow.
type WizardMode int

const (
	// WizardModeDeploy includes all steps: config + hostname + description + confirm deploy.
	WizardModeDeploy WizardMode = iota
	// WizardModeTemplate includes config steps + hostname pattern + template description (no deploy hostname, deploy description, or confirm).
	WizardModeTemplate
)

// TemplateResult holds the wizard output in a form suitable for template saving.
// It maps internal IDs to human-readable names for SSH keys and startup scripts.
type TemplateResult struct {
	BillingType       string
	Contract          string
	Kind              string
	InstanceType      string
	Location          string
	Image             string
	OSVolumeSize      int
	SSHKeyNames       []string
	StartupScriptName string
	StorageSize       int
	StorageType       string
	StorageSkip       bool // user explicitly chose "None (skip)"
	StartupScriptSkip bool // user explicitly chose "None (skip)"
	HostnamePattern   string
	Description       string
}

// RunTemplateWizard runs the VM create wizard in template mode. Includes config
// steps plus hostname-pattern and template description, but no deploy hostname,
// deploy description, or confirm-deploy steps.
func RunTemplateWizard(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams) (*TemplateResult, error) {
	return runTemplateWizardWithOpts(ctx, f, ioStreams, &createOptions{
		LocationCode: verda.LocationFIN01,
		StorageType:  verda.VolumeTypeNVMe,
	})
}

func runTemplateWizardWithOpts(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *createOptions) (*TemplateResult, error) {
	flow := buildCreateFlow(ctx, f.VerdaClient, opts, WizardModeTemplate, ioStreams.ErrOut)
	engine := wizard.NewEngine(f.Prompter(), f.Status(), wizard.WithOutput(ioStreams.ErrOut))
	if err := engine.Run(ctx, flow); err != nil {
		return nil, err
	}

	return optsToTemplateResult(opts), nil
}

func optsToTemplateResult(opts *createOptions) *TemplateResult {
	result := &TemplateResult{
		Contract:          opts.Contract,
		Kind:              opts.Kind,
		InstanceType:      opts.InstanceType,
		Location:          opts.LocationCode,
		Image:             opts.imageName,
		OSVolumeSize:      opts.OSVolumeSize,
		SSHKeyNames:       opts.sshKeyNames,
		StartupScriptName: opts.startupScriptName,
		StorageSize:       opts.StorageSize,
		StorageType:       opts.StorageType,
		StorageSkip:       opts.StorageSize == 0 && len(opts.VolumeSpecs) == 0,
		StartupScriptSkip: opts.StartupScriptID == "" && opts.startupScriptName == "",
		HostnamePattern:   opts.hostnamePattern,
		Description:       opts.templateDescription,
	}
	if opts.IsSpot {
		result.BillingType = "spot"
	} else {
		result.BillingType = billingTypeOnDemand
	}
	return result
}

// buildCreateFlow builds the interactive wizard flow for vm create.
// It mirrors the web UI step order:
//
//	billing-type → contract → kind → instance-type → location →
//	image → os-volume-size → storage-size → ssh-keys →
//	startup-script → hostname → description
func buildCreateFlow(ctx context.Context, getClient clientFunc, opts *createOptions, mode WizardMode, errOut io.Writer) *wizard.Flow {
	cache := &apiCache{}

	steps := []wizard.Step{
		stepBillingType(opts),
		stepContract(getClient, opts),
		stepKind(opts),
		stepInstanceType(getClient, cache, opts),
		stepLocation(getClient, cache, opts),
		stepImage(getClient, opts),
		stepOSVolumeSize(opts),
		stepStorage(getClient, cache, opts),
		stepSSHKeys(getClient, opts),
		stepStartupScript(getClient, opts),
	}
	if mode == WizardModeDeploy {
		steps = append(steps,
			stepHostname(opts),
			stepDescription(opts),
			stepConfirmDeploy(ctx, errOut, getClient, cache, opts),
		)
	}
	if mode == WizardModeTemplate {
		steps = append(steps,
			stepHostnamePattern(opts),
			stepTemplateDescription(opts),
		)
	}

	layout := []wizard.ViewDef{
		{ID: "hints", View: wizard.NewHintBarView(wizard.WithHintStyle(bubbletea.HintStyle()))},
	}
	if mode == WizardModeDeploy {
		// Prepend progress bar for deploy mode only
		layout = append([]wizard.ViewDef{
			{ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
		}, layout...)
	}

	return &wizard.Flow{
		Name:   "vm-create",
		Layout: layout,
		Steps:  steps,
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
			wizard.Choice{Label: "On-Demand", Value: billingTypeOnDemand, Description: "Pay as you go or long-term contract"},
			wizard.Choice{Label: "Spot Instance", Value: billingTypeSpot, Description: "Lower price, may be interrupted"},
		),
		Default: func(_ map[string]any) any {
			if opts.IsSpot {
				return billingTypeSpot
			}
			return billingTypeOnDemand
		},
		Setter: func(v any) {
			opts.IsSpot = v.(string) == billingTypeSpot
		},
		Resetter: func() {
			opts.IsSpot = false
		},
		IsSet: func() bool { return opts.billingTypeSet || opts.IsSpot },
		Value: func() any {
			if opts.IsSpot {
				return billingTypeSpot
			}
			return billingTypeOnDemand
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
			return c["billing-type"] == billingTypeSpot
		},
		Loader: func(ctx context.Context, _ tui.Prompter, status tui.Status, store *wizard.Store) ([]wizard.Choice, error) {
			choices := []wizard.Choice{
				{Label: "Pay as you go", Value: contractPayAsYouGo},
			}
			client, err := getClient()
			if err != nil {
				return choices, nil //nolint:nilerr // Non-fatal: just offer pay-as-you-go.
			}
			periods, err := cmdutil.WithSpinner(ctx, status, "Loading contract options...", func() ([]verda.LongTermPeriod, error) {
				return client.LongTerm.GetInstancePeriods(ctx)
			})
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
		Default: func(_ map[string]any) any { return opts.Contract },
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
			wizard.Choice{Label: "GPU", Value: kindGPU, Description: "GPU-accelerated instances"},
			wizard.Choice{Label: "CPU", Value: "cpu", Description: "CPU-only instances"},
		),
		Default:  func(_ map[string]any) any { return opts.Kind },
		Setter:   func(v any) { opts.Kind = v.(string) },
		Resetter: func() { opts.Kind = "" },
		IsSet:    func() bool { return opts.Kind != "" },
		Value:    func() any { return opts.Kind },
	}
}

// --- Step 4: Instance Type ---

func stepInstanceType(getClient clientFunc, cache *apiCache, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "instance-type",
		Description: "Instance type",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		DependsOn:   []string{"kind", "billing-type"},
		Loader: func(ctx context.Context, _ tui.Prompter, status tui.Status, store *wizard.Store) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}
			c := store.Collected()
			kind := c["kind"].(string)
			isSpot := c["billing-type"] == billingTypeSpot

			types, err := cmdutil.WithSpinner(ctx, status, "Loading instance types...", func() ([]verda.InstanceTypeInfo, error) {
				return client.InstanceTypes.Get(ctx, "usd")
			})
			if err != nil {
				return nil, fmt.Errorf("fetching instance types: %w", err)
			}

			// Cache instance types for deployment summary.
			if cache.instanceTypes == nil {
				cache.instanceTypes = make(map[string]verda.InstanceTypeInfo, len(types))
			}
			for i := range types {
				cache.instanceTypes[types[i].InstanceType] = types[i]
			}

			avail, locMap, err := cache.fetchAvailability(ctx, getClient, isSpot)
			if err != nil {
				return nil, err
			}

			// Build a map: instance type → list of location codes where it's available.
			availLocs := make(map[string][]string)
			for _, la := range avail {
				for _, a := range la.Availabilities {
					availLocs[a] = append(availLocs[a], la.LocationCode)
				}
			}

			var choices []wizard.Choice
			for i := range types {
				t := &types[i]
				if !matchesKind(t.InstanceType, kind) {
					continue
				}
				locs := availLocs[t.InstanceType]
				if len(locs) == 0 {
					continue // skip unavailable instance types
				}
				totalPrice := float64(t.PricePerHour)
				if isSpot {
					totalPrice = float64(t.SpotPrice)
				}
				locNames := make([]string, len(locs))
				for j, code := range locs {
					if loc, ok := locMap[code]; ok {
						locNames[j] = loc.Code
					} else {
						locNames[j] = code
					}
				}
				units := instanceUnits(t)
				var priceStr string
				if units > 1 {
					unitLabel := unitLabelGPU
					if t.GPU.NumberOfGPUs == 0 {
						unitLabel = unitLabelVCPU
					}
					perUnit := totalPrice / float64(units)
					priceStr = fmt.Sprintf("$%.3f/%s/hr  $%.3f/hr", perUnit, unitLabel, totalPrice)
				} else {
					priceStr = fmt.Sprintf("$%.3f/hr", totalPrice)
				}
				label := fmt.Sprintf("%s — %s, %s  %s",
					t.InstanceType, formatGPU(t), formatMemory(t), priceStr)
				desc := fmt.Sprintf("[%s]", strings.Join(locNames, ", "))
				choices = append(choices, wizard.Choice{
					Label:       label,
					Value:       t.InstanceType,
					Description: desc,
				})
			}
			return choices, nil
		},
		Default:  func(_ map[string]any) any { return opts.InstanceType },
		Setter:   func(v any) { opts.InstanceType = v.(string) },
		Resetter: func() { opts.InstanceType = "" },
		IsSet:    func() bool { return opts.InstanceType != "" },
		Value:    func() any { return opts.InstanceType },
	}
}

// --- Step 5: Location ---

func stepLocation(getClient clientFunc, cache *apiCache, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "location",
		Description: "Datacenter location",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		DependsOn:   []string{"instance-type", "billing-type"},
		Loader: func(ctx context.Context, _ tui.Prompter, _ tui.Status, store *wizard.Store) ([]wizard.Choice, error) {
			c := store.Collected()
			isSpot := c["billing-type"] == billingTypeSpot
			instType := c["instance-type"].(string)

			// Usually a cache hit — instance-type step already fetched this.
			avail, locMap, err := cache.fetchAvailability(ctx, getClient, isSpot)
			if err != nil {
				return nil, err
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
		IsSet: func() bool {
			return opts.locationSet || (opts.LocationCode != "" && opts.LocationCode != verda.LocationFIN01)
		},
		Value:   func() any { return opts.LocationCode },
		Default: func(_ map[string]any) any { return opts.LocationCode },
	}
}

// --- Step 6: OS Image ---

func stepImage(getClient clientFunc, opts *createOptions) wizard.Step {
	var imagesByID map[string]string // ID → Name lookup, built by Loader
	return wizard.Step{
		Name:        "image",
		Description: "Operating system image",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: func(ctx context.Context, _ tui.Prompter, status tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}
			images, err := cmdutil.WithSpinner(ctx, status, "Loading OS images...", func() ([]verda.Image, error) {
				return client.Images.Get(ctx)
			})
			if err != nil {
				return nil, fmt.Errorf("fetching images: %w", err)
			}
			imagesByID = make(map[string]string, len(images))
			var choices []wizard.Choice
			for _, img := range images {
				if img.IsCluster {
					continue
				}
				imagesByID[img.ID] = img.Name
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
		Default: func(_ map[string]any) any { return opts.Image },
		Setter: func(v any) {
			id := v.(string)
			opts.Image = id
			if imagesByID != nil {
				opts.imageName = imagesByID[id]
			}
		},
		Resetter: func() { opts.Image = ""; opts.imageName = "" },
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
		Default: func(_ map[string]any) any {
			if opts.OSVolumeSize > 0 {
				return strconv.Itoa(opts.OSVolumeSize)
			}
			return "50"
		},
		Validate: func(v any) error {
			s := v.(string)
			if s == "" {
				return nil
			}
			n, err := strconv.Atoi(s)
			if err != nil || n <= 0 {
				return errors.New("must be a positive integer")
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

// --- Step 8: Storage (optional) ---

const addNewVolumeValue = "__add_new_volume__"

func stepStorage(getClient clientFunc, cache *apiCache, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "storage",
		Description: "Storage (optional)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, prompter tui.Prompter, status tui.Status, store *wizard.Store) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}

			// Fetch volume type pricing for display.
			if cache.volumeTypes == nil {
				if vTypes, vtErr := client.VolumeTypes.GetAllVolumeTypes(ctx); vtErr == nil {
					cache.volumeTypes = make(map[string]verda.VolumeType, len(vTypes))
					for _, vt := range vTypes {
						cache.volumeTypes[vt.Type] = vt
					}
				}
			}

			// Reset volumes for fresh wizard pass.
			var volumes []verda.VolumeCreateRequest
			var existingIDs []string

			choices := buildStorageChoices(volumes, existingIDs)

			for {
				labels := make([]string, len(choices))
				for i, c := range choices {
					labels[i] = c.Label
				}
				idx, err := prompter.Select(ctx, "Storage", labels)
				if err != nil {
					return nil, err
				}

				selected := choices[idx].Value
				switch selected {
				case "": // None (skip)
					opts.VolumeSpecs = nil
					opts.ExistingVolumes = nil
					opts.StorageSize = 0
					return nil, nil

				case addNewVolumeValue:
					vol, err := promptAddVolume(ctx, prompter, store, cache)
					if err != nil {
						return nil, err
					}
					if vol != nil {
						volumes = append(volumes, *vol)
					}
					choices = buildStorageChoices(volumes, existingIDs)
					continue

				case "__attach_existing__":
					id, err := promptAttachExisting(ctx, prompter, status, client)
					if err != nil {
						return nil, err
					}
					if id != "" {
						existingIDs = append(existingIDs, id)
					}
					choices = buildStorageChoices(volumes, existingIDs)
					continue

				default: // "done"
					// Write the collected volumes to opts.
					specs := make([]string, len(volumes))
					for i, v := range volumes {
						specs[i] = fmt.Sprintf("%s:%d:%s", v.Name, v.Size, v.Type)
					}
					opts.VolumeSpecs = specs
					opts.ExistingVolumes = existingIDs
					opts.StorageSize = 0 // handled via VolumeSpecs now
					return nil, nil
				}
			}
		},
		Setter:   func(v any) {}, // Set directly in Loader.
		Resetter: func() {},      // Don't clear — Loader manages the value.
		IsSet: func() bool {
			return opts.storageSkip || opts.StorageSize > 0 || len(opts.VolumeSpecs) > 0 || len(opts.ExistingVolumes) > 0
		},
		Value: func() any { return "" },
	}
}

// --- Step 9: SSH Keys ---

const addNewSSHKeyValue = "__add_new__"

func stepSSHKeys(getClient clientFunc, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "ssh-keys",
		Description: "SSH keys to inject",
		Prompt:      wizard.MultiSelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, prompter tui.Prompter, status tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}
			keys, err := cmdutil.WithSpinner(ctx, status, "Loading SSH keys...", func() ([]verda.SSHKey, error) {
				return client.SSHKeys.GetAllSSHKeys(ctx)
			})
			if err != nil {
				return nil, fmt.Errorf("fetching SSH keys: %w", err)
			}

			choices := buildSSHKeyChoices(keys)

			// Loop: if user selects "Add new", run sub-flow and re-prompt.
			for {
				labels := make([]string, len(choices))
				for i, c := range choices {
					labels[i] = c.Label
				}
				indices, err := prompter.MultiSelect(ctx, "SSH keys to inject", labels, tui.WithMinSelections(1))
				if err != nil {
					return nil, err
				}

				addNew := false
				for _, idx := range indices {
					if choices[idx].Value == addNewSSHKeyValue {
						addNew = true
						break
					}
				}

				if !addNew {
					// Return the selected keys as choices. The engine will
					// show a "no prompt" since these are the final selections,
					// but we need the engine to call Setter with the values.
					var selected []wizard.Choice
					for _, idx := range indices {
						if choices[idx].Value != addNewSSHKeyValue {
							selected = append(selected, choices[idx])
						}
					}
					// Set directly — the engine auto-skips optional steps
					// with empty choices and calls Resetter, so we must
					// bypass by returning a sentinel.
					opts.SSHKeyIDs = make([]string, len(selected))
					opts.sshKeyNames = make([]string, len(selected))
					for i, c := range selected {
						opts.SSHKeyIDs[i] = c.Value
						opts.sshKeyNames[i] = c.Label
					}
					return nil, nil
				}

				newKey, err := promptAddSSHKey(ctx, prompter, client)
				if err != nil {
					return nil, err
				}
				if newKey != nil {
					keys = append([]verda.SSHKey{*newKey}, keys...)
					choices = buildSSHKeyChoices(keys)
				}
			}
		},
		Setter:   func(v any) {}, // Set directly in Loader.
		Resetter: func() {},      // Don't clear — Loader manages the value.
		IsSet:    func() bool { return len(opts.SSHKeyIDs) > 0 },
		Value:    func() any { return opts.SSHKeyIDs },
	}
}

// --- Step 10: Startup Script ---

const addNewScriptValue = "__add_new_script__"

func stepStartupScript(getClient clientFunc, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "startup-script",
		Description: "Startup script (optional)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, prompter tui.Prompter, status tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}
			scripts, err := cmdutil.WithSpinner(ctx, status, "Loading startup scripts...", func() ([]verda.StartupScript, error) {
				return client.StartupScripts.GetAllStartupScripts(ctx)
			})
			if err != nil {
				return nil, fmt.Errorf("fetching startup scripts: %w", err)
			}

			choices := buildStartupScriptChoices(scripts)

			// Loop: if user selects "Add new", run sub-flow and re-prompt.
			for {
				labels := make([]string, len(choices))
				for i, c := range choices {
					labels[i] = c.Label
				}
				idx, err := prompter.Select(ctx, "Startup script (optional)", labels)
				if err != nil {
					return nil, err
				}

				if choices[idx].Value != addNewScriptValue {
					// Set the value directly and return empty so the engine auto-skips.
					opts.StartupScriptID = choices[idx].Value
					if choices[idx].Value != "" {
						opts.startupScriptName = choices[idx].Label
					}
					return nil, nil
				}

				newScript, err := promptAddStartupScript(ctx, prompter, client)
				if err != nil {
					return nil, err
				}
				if newScript != nil {
					scripts = append(scripts, *newScript)
					choices = buildStartupScriptChoices(scripts)
				}
			}
		},
		Setter:   func(v any) {}, // Set directly in Loader.
		Resetter: func() {},      // Don't clear — Loader manages the value.
		IsSet:    func() bool { return opts.startupScriptSkip || opts.StartupScriptID != "" },
		Value:    func() any { return opts.StartupScriptID },
	}
}

// --- Template Step: Hostname Pattern ---

func stepHostnamePattern(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "hostname-pattern",
		Description: "Hostname pattern (optional)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		Default: func(_ map[string]any) any {
			return "{random}-{location}"
		},
		Setter:   func(v any) { opts.hostnamePattern = v.(string) },
		Resetter: func() { opts.hostnamePattern = "" },
		IsSet:    func() bool { return opts.hostnamePattern != "" },
		Value:    func() any { return opts.hostnamePattern },
	}
}

// --- Template Step: Description ---

func stepTemplateDescription(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "template-description",
		Description: "Description (optional)",
		Prompt:      wizard.TextInputPrompt,
		Required:    false,
		Setter:      func(v any) { opts.templateDescription = v.(string) },
		Resetter:    func() { opts.templateDescription = "" },
		IsSet:       func() bool { return opts.templateDescription != "" },
		Value:       func() any { return opts.templateDescription },
	}
}

// --- Step 11: Hostname ---

func stepHostname(opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "hostname",
		Description: "Hostname",
		Prompt:      wizard.TextInputPrompt,
		Required:    true,
		DependsOn:   []string{"location"},
		Default: func(c map[string]any) any {
			loc, _ := c["location"].(string)
			if loc == "" {
				loc = "fin-01"
			}
			return cmdutil.GenerateHostname(loc)
		},
		Validate: func(v any) error {
			return cmdutil.ValidateHostname(v.(string))
		},
		Setter:   func(v any) { opts.Hostname = v.(string) },
		Resetter: func() { opts.Hostname = "" },
		IsSet:    func() bool { return opts.Hostname != "" },
		Value:    func() any { return opts.Hostname },
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

// --- Step 13: Deployment Summary & Confirm ---

func stepConfirmDeploy(ctx context.Context, errOut io.Writer, getClient clientFunc, cache *apiCache, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "confirm-deploy",
		Description: "Deploy now?",
		Prompt:      wizard.ConfirmPrompt,
		Required:    true,
		Default: func(_ map[string]any) any {
			// Ensure pricing data is available (may be missing when
			// steps were skipped via --from template pre-fill).
			ensurePricingCache(ctx, getClient, cache)
			renderDeploymentSummary(errOut, opts, cache)
			return true
		},
		Setter: func(v any) {
			if confirmed, ok := v.(bool); ok && !confirmed {
				opts.Hostname = ""
			}
		},
		Resetter: func() {},
		IsSet:    func() bool { return false },
		Value:    func() any { return true },
	}
}
