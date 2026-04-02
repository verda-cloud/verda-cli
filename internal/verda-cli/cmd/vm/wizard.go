package vm

import (
	"context"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// clientFunc lazily resolves a Verda API client. This allows the wizard
// to start without credentials — steps that don't need API (billing-type,
// kind, text inputs) run first, and the client is only resolved when an
// API-dependent loader fires.
type clientFunc func() (*verda.Client, error)

// apiCache holds data fetched from the API, shared across wizard steps
// to avoid redundant calls within a single wizard session.
type apiCache struct {
	avail         []verda.LocationAvailability
	locations     map[string]verda.Location
	cachedSpot    bool // tracks which isSpot value was cached
	loaded        bool
	instanceTypes map[string]verda.InstanceTypeInfo // keyed by instance type name
	volumeTypes   map[string]verda.VolumeType       // keyed by volume type name
}

// fetchAvailability loads availability and location data, caching the result.
// Cache is invalidated if isSpot changes (user switched billing type).
func (c *apiCache) fetchAvailability(ctx context.Context, getClient clientFunc, isSpot bool) ([]verda.LocationAvailability, map[string]verda.Location, error) {
	if c.loaded && c.cachedSpot == isSpot {
		return c.avail, c.locations, nil
	}
	client, err := getClient()
	if err != nil {
		return nil, nil, err
	}
	avail, err := client.InstanceAvailability.GetAllAvailabilities(ctx, isSpot, "")
	if err != nil {
		return nil, nil, fmt.Errorf("fetching availability: %w", err)
	}
	locations, err := client.Locations.Get(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching locations: %w", err)
	}
	c.locations = make(map[string]verda.Location, len(locations))
	for _, loc := range locations {
		c.locations[loc.Code] = loc
	}
	c.avail = avail
	c.cachedSpot = isSpot
	c.loaded = true
	return c.avail, c.locations, nil
}

// buildCreateFlow builds the interactive wizard flow for vm create.
// It mirrors the web UI step order:
//
//	billing-type → contract → kind → instance-type → location →
//	image → os-volume-size → storage-size → ssh-keys →
//	startup-script → hostname → description
func buildCreateFlow(getClient clientFunc, opts *createOptions) *wizard.Flow {
	cache := &apiCache{}
	return &wizard.Flow{
		Name: "vm-create",
		Layout: []wizard.ViewDef{
			{ID: "progress", View: wizard.NewProgressView(wizard.WithProgressPercent())},
			{ID: "hints", View: wizard.NewHintBarView()},
		},
		Steps: []wizard.Step{
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
			stepHostname(opts),
			stepDescription(opts),
			stepConfirmDeploy(getClient, cache, opts),
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
		Loader: func(ctx context.Context, _ tui.Prompter, status tui.Status, store *wizard.Store) ([]wizard.Choice, error) {
			choices := []wizard.Choice{
				{Label: "Pay as you go", Value: "PAY_AS_YOU_GO"},
			}
			client, err := getClient()
			if err != nil {
				return choices, nil //nolint:nilerr // Non-fatal: just offer pay-as-you-go.
			}
			periods, err := withSpinner(ctx, status, "Loading contract options...", func() ([]verda.LongTermPeriod, error) {
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
			isSpot := c["billing-type"] == "spot"

			types, err := withSpinner(ctx, status, "Loading instance types...", func() ([]verda.InstanceTypeInfo, error) {
				return client.InstanceTypes.Get(ctx, "usd")
			})
			if err != nil {
				return nil, fmt.Errorf("fetching instance types: %w", err)
			}

			// Cache instance types for deployment summary.
			if cache.instanceTypes == nil {
				cache.instanceTypes = make(map[string]verda.InstanceTypeInfo, len(types))
			}
			for _, t := range types {
				cache.instanceTypes[t.InstanceType] = t
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
					unitLabel := "GPU"
					if t.GPU.NumberOfGPUs == 0 {
						unitLabel = "vCPU"
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
			isSpot := c["billing-type"] == "spot"
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
		Loader: func(ctx context.Context, _ tui.Prompter, status tui.Status, _ *wizard.Store) ([]wizard.Choice, error) {
			client, err := getClient()
			if err != nil {
				return nil, err
			}
			images, err := withSpinner(ctx, status, "Loading OS images...", func() ([]verda.Image, error) {
				return client.Images.Get(ctx)
			})
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
		Setter:   func(v any) {},  // Set directly in Loader.
		Resetter: func() {},       // Don't clear — Loader manages the value.
		IsSet:    func() bool { return opts.StorageSize > 0 || len(opts.VolumeSpecs) > 0 || len(opts.ExistingVolumes) > 0 },
		Value:    func() any { return "" },
	}
}

func buildStorageChoices(volumes []verda.VolumeCreateRequest, existingIDs []string) []wizard.Choice {
	choices := []wizard.Choice{
		{Label: "None (skip)", Value: ""},
		{Label: "+ Add new block volume", Value: addNewVolumeValue},
		{Label: "+ Attach existing volume", Value: "__attach_existing__"},
	}

	// Show already-added volumes.
	if len(volumes) > 0 || len(existingIDs) > 0 {
		for _, v := range volumes {
			choices = append(choices, wizard.Choice{
				Label: fmt.Sprintf("  New: %s (%dGB %s)", v.Name, v.Size, v.Type),
				Value: "__info__",
			})
		}
		for _, id := range existingIDs {
			choices = append(choices, wizard.Choice{
				Label: fmt.Sprintf("  Existing: %s", id),
				Value: "__info__",
			})
		}
		choices = append(choices, wizard.Choice{
			Label: "Done — continue with above storage",
			Value: "__done__",
		})
	}

	return choices
}

func promptAddVolume(ctx context.Context, prompter tui.Prompter, store *wizard.Store, cache *apiCache) (*verda.VolumeCreateRequest, error) {
	// Volume type with prices.
	nvmeLabel := "NVMe (fast SSD)"
	hddLabel := "HDD (large capacity)"
	if cache != nil && cache.volumeTypes != nil {
		if vt, ok := cache.volumeTypes[verda.VolumeTypeNVMe]; ok && vt.Price.MonthlyPerGB > 0 {
			nvmeLabel = fmt.Sprintf("NVMe (fast SSD)  $%.2f/GB/mo", vt.Price.MonthlyPerGB)
		}
		if vt, ok := cache.volumeTypes[verda.VolumeTypeHDD]; ok && vt.Price.MonthlyPerGB > 0 {
			hddLabel = fmt.Sprintf("HDD (large capacity)  $%.2f/GB/mo", vt.Price.MonthlyPerGB)
		}
	}
	typeIdx, err := prompter.Select(ctx, "Volume type", []string{
		nvmeLabel,
		hddLabel,
		"← Back",
	})
	if err != nil {
		return nil, nil //nolint:nilerr
	}
	if typeIdx == 2 { // "← Back"
		return nil, nil
	}
	volType := verda.VolumeTypeNVMe
	if typeIdx == 1 {
		volType = verda.VolumeTypeHDD
	}

	// Name
	c := store.Collected()
	hostname, _ := c["hostname"].(string)
	defaultName := ""
	if hostname != "" {
		defaultName = hostname + "-storage"
	}
	name, err := prompter.TextInput(ctx, "Volume name", tui.WithDefault(defaultName))
	if err != nil || strings.TrimSpace(name) == "" {
		return nil, nil //nolint:nilerr
	}

	// Size
	sizeStr, err := prompter.TextInput(ctx, "Size in GiB", tui.WithDefault("100"))
	if err != nil || strings.TrimSpace(sizeStr) == "" {
		return nil, nil //nolint:nilerr
	}
	size, err := strconv.Atoi(strings.TrimSpace(sizeStr))
	if err != nil || size <= 0 {
		_, _ = prompter.Confirm(ctx, "Error: size must be a positive integer. Press Enter to continue.", tui.WithConfirmDefault(true))
		return nil, nil
	}

	return &verda.VolumeCreateRequest{
		Name: strings.TrimSpace(name),
		Size: size,
		Type: volType,
	}, nil
}

func promptAttachExisting(ctx context.Context, prompter tui.Prompter, status tui.Status, client *verda.Client) (string, error) {
	volumes, err := withSpinner(ctx, status, "Loading volumes...", func() ([]verda.Volume, error) {
		return client.Volumes.ListVolumes(ctx)
	})
	if err != nil {
		return "", fmt.Errorf("fetching volumes: %w", err)
	}

	// Filter to detached volumes only.
	var detached []verda.Volume
	for _, v := range volumes {
		if v.InstanceID == nil || *v.InstanceID == "" {
			detached = append(detached, v)
		}
	}

	if len(detached) == 0 {
		_, _ = prompter.Confirm(ctx, "No detached volumes available. Press Enter to continue.", tui.WithConfirmDefault(true))
		return "", nil
	}

	labels := make([]string, len(detached))
	for i, v := range detached {
		labels[i] = fmt.Sprintf("%s (%dGB %s, %s)", v.Name, v.Size, v.Type, v.Location)
	}
	labels = append(labels, "← Back")

	idx, err := prompter.Select(ctx, "Select volume to attach", labels)
	if err != nil {
		return "", nil //nolint:nilerr
	}
	if idx == len(detached) { // "← Back"
		return "", nil
	}
	return detached[idx].ID, nil
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
			keys, err := withSpinner(ctx, status, "Loading SSH keys...", func() ([]verda.SSHKey, error) {
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
				indices, err := prompter.MultiSelect(ctx, "SSH keys to inject", labels)
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
					for i, c := range selected {
						opts.SSHKeyIDs[i] = c.Value
					}
					return nil, nil
				}

				newKey, err := promptAddSSHKey(ctx, prompter, client)
				if err != nil {
					return nil, err
				}
				if newKey != nil {
					keys = append(keys, *newKey)
					choices = buildSSHKeyChoices(keys)
				}
			}
		},
		Setter:   func(v any) {},                        // Set directly in Loader.
		Resetter: func() {},                               // Don't clear — Loader manages the value.
		IsSet:    func() bool { return len(opts.SSHKeyIDs) > 0 },
		Value:    func() any { return opts.SSHKeyIDs },
	}
}

func buildSSHKeyChoices(keys []verda.SSHKey) []wizard.Choice {
	choices := []wizard.Choice{
		{Label: "+ Add new SSH key", Value: addNewSSHKeyValue},
	}
	for _, k := range keys {
		choices = append(choices, wizard.Choice{
			Label:       k.Name,
			Value:       k.ID,
			Description: k.Fingerprint,
		})
	}
	return choices
}

func promptAddSSHKey(ctx context.Context, prompter tui.Prompter, client *verda.Client) (*verda.SSHKey, error) {
	name, err := prompter.TextInput(ctx, "SSH key name")
	if err != nil || strings.TrimSpace(name) == "" {
		return nil, nil //nolint:nilerr
	}
	pubKey, err := prompter.TextInput(ctx, "Public key (paste)")
	if err != nil || strings.TrimSpace(pubKey) == "" {
		return nil, nil //nolint:nilerr
	}
	created, err := client.SSHKeys.AddSSHKey(ctx, &verda.CreateSSHKeyRequest{
		Name:     strings.TrimSpace(name),
		PublicKey: strings.TrimSpace(pubKey),
	})
	if err != nil {
		_, _ = prompter.Confirm(ctx, fmt.Sprintf("Error: %v. Press Enter to continue.", err), tui.WithConfirmDefault(true))
		return nil, nil //nolint:nilerr
	}
	return created, nil
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
			scripts, err := withSpinner(ctx, status, "Loading startup scripts...", func() ([]verda.StartupScript, error) {
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
		Setter:   func(v any) {},  // Set directly in Loader.
		Resetter: func() {},       // Don't clear — Loader manages the value.
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

// --- Startup script helpers ---

func buildStartupScriptChoices(scripts []verda.StartupScript) []wizard.Choice {
	choices := []wizard.Choice{
		{Label: "None (skip)", Value: ""},
		{Label: "+ Add new startup script", Value: addNewScriptValue},
	}
	for _, s := range scripts {
		choices = append(choices, wizard.Choice{
			Label: s.Name,
			Value: s.ID,
		})
	}
	return choices
}

func promptAddStartupScript(ctx context.Context, prompter tui.Prompter, client *verda.Client) (*verda.StartupScript, error) {
	name, err := prompter.TextInput(ctx, "Script name")
	if err != nil || strings.TrimSpace(name) == "" {
		return nil, nil //nolint:nilerr
	}

	// Ask for source: paste or load from file.
	sourceIdx, err := prompter.Select(ctx, "Script source", []string{
		"Load from file",
		"Paste content",
	})
	if err != nil {
		return nil, nil //nolint:nilerr
	}

	var content string
	switch sourceIdx {
	case 0: // Load from file
		path, err := prompter.TextInput(ctx, "File path")
		if err != nil || strings.TrimSpace(path) == "" {
			return nil, nil //nolint:nilerr
		}
		data, err := os.ReadFile(strings.TrimSpace(path))
		if err != nil {
			_, _ = prompter.Confirm(ctx, fmt.Sprintf("Error: %v. Press Enter to continue.", err), tui.WithConfirmDefault(true))
			return nil, nil //nolint:nilerr
		}
		content = string(data)
	case 1: // Paste content
		content, err = prompter.Editor(ctx, "Script content (Ctrl+D to finish)",
			tui.WithEditorDefault("#!/bin/bash\n\n# Your startup script here\n"),
			tui.WithFileExt(".sh"))
		if err != nil {
			return nil, nil //nolint:nilerr
		}
	}

	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	created, err := client.StartupScripts.AddStartupScript(ctx, &verda.CreateStartupScriptRequest{
		Name:   strings.TrimSpace(name),
		Script: content,
	})
	if err != nil {
		// Show error and return to menu instead of crashing.
		_, _ = prompter.Confirm(ctx, fmt.Sprintf("Error: %v. Press Enter to continue.", err), tui.WithConfirmDefault(true))
		return nil, nil //nolint:nilerr
	}
	return created, nil
}

// --- Spinner helper ---

// withSpinner runs fn while showing a spinner. If status is nil, runs fn directly.
func withSpinner[T any](ctx context.Context, status tui.Status, msg string, fn func() (T, error)) (T, error) {
	if status == nil {
		return fn()
	}
	sp, err := status.Spinner(ctx, msg)
	if err != nil {
		return fn() // fallback: run without spinner
	}
	result, fnErr := fn()
	if fnErr != nil {
		sp.Stop("")
	} else {
		sp.Stop("")
	}
	return result, fnErr
}

// --- Step 13: Deployment Summary & Confirm ---

func stepConfirmDeploy(getClient clientFunc, cache *apiCache, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "confirm-deploy",
		Description: "Deploy now?",
		Prompt:      wizard.ConfirmPrompt,
		Required:    true,
		Default: func(_ map[string]any) any {
			// Fetch volume type pricing (best effort).
			if cache.volumeTypes == nil && len(opts.VolumeSpecs) > 0 {
				if client, err := getClient(); err == nil {
					if vTypes, err := client.VolumeTypes.GetAllVolumeTypes(context.Background()); err == nil {
						cache.volumeTypes = make(map[string]verda.VolumeType, len(vTypes))
						for _, vt := range vTypes {
							cache.volumeTypes[vt.Type] = vt
						}
					}
				}
			}
			renderDeploymentSummary(opts, cache)
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

// hoursInMonth is 365*24/12 = 730, matching the web frontend.
const hoursInMonth = 730

// volumeHourlyPrice calculates hourly price: monthlyPerGB * size / 730, rounded up to 4 decimals.
func volumeHourlyPrice(monthlyPerGB float64, sizeGB int) float64 {
	return math.Ceil(monthlyPerGB*float64(sizeGB)/hoursInMonth*10000) / 10000
}

func renderDeploymentSummary(opts *createOptions, cache *apiCache) {
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
			instUnitLabel = "GPU"
		} else {
			instLabel = fmt.Sprintf("%s — %d CPU, %dGB RAM", info.InstanceType, info.CPU.NumberOfCores, info.Memory.SizeInGigabytes)
			instUnitLabel = "vCPU"
		}
	}

	// OS volume pricing (NVMe).
	var osVolPrice float64
	var osVolUnitPrice float64
	if opts.OSVolumeSize > 0 {
		if vt, ok := cache.volumeTypes[verda.VolumeTypeNVMe]; ok {
			osVolUnitPrice = vt.Price.MonthlyPerGB
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
	var volDetails []volDetail
	for _, spec := range opts.VolumeSpecs {
		parts := strings.SplitN(spec, ":", 3)
		if len(parts) < 3 {
			continue
		}
		name, sizeStr, vType := parts[0], parts[1], parts[2]
		size, _ := strconv.Atoi(sizeStr)
		var hourly, unitP float64
		if vt, ok := cache.volumeTypes[vType]; ok {
			unitP = vt.Price.MonthlyPerGB
			hourly = volumeHourlyPrice(unitP, size)
			storageHourly += hourly
		}
		volDetails = append(volDetails, volDetail{name, vType, size, unitP, hourly})
	}

	// Print summary.
	_, _ = fmt.Fprintf(os.Stderr, "\n  %s\n", bold.Render("Deployment Summary"))

	billing := "On-Demand"
	if opts.IsSpot {
		billing = "Spot Instance"
	}
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n", dim.Render(billing))
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n\n", dim.Render(strings.Repeat("─", 50)))

	// Instance.
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n", accent.Render("Instance"))
	var computePriceStr string
	if instUnits > 1 {
		perUnit := computeHourly / float64(instUnits)
		computePriceStr = fmt.Sprintf("$%.4f/%s/hr  $%.4f/hr", perUnit, instUnitLabel, computeHourly)
	} else {
		computePriceStr = fmt.Sprintf("$%.4f/hr", computeHourly)
	}
	_, _ = fmt.Fprintf(os.Stderr, "    %-40s %s\n", instLabel, priceStyle.Render(computePriceStr))
	_, _ = fmt.Fprintf(os.Stderr, "    %s\n\n", dim.Render(opts.LocationCode))

	// OS.
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n", accent.Render("Operating System"))
	osLine := fmt.Sprintf("%s  %dGB NVMe", opts.Image, opts.OSVolumeSize)
	osPrice := fmt.Sprintf("($%.2f/GB/mo)  $%.4f/hr", osVolUnitPrice, osVolPrice)
	_, _ = fmt.Fprintf(os.Stderr, "    %-40s %s\n\n", osLine, priceStyle.Render(osPrice))

	// Storage volumes.
	if len(volDetails) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "  %s\n", accent.Render("Storage"))
		for _, v := range volDetails {
			line := fmt.Sprintf("%s  %dGB %s", v.name, v.size, v.volType)
			vPrice := fmt.Sprintf("($%.2f/GB/mo)  $%.4f/hr", v.unitPrice, v.hourly)
			_, _ = fmt.Fprintf(os.Stderr, "    %-40s %s\n", line, priceStyle.Render(vPrice))
		}
		_, _ = fmt.Fprintln(os.Stderr)
	}

	// SSH keys.
	if len(opts.SSHKeyIDs) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "  %s  %d key(s)\n\n", accent.Render("SSH Keys"), len(opts.SSHKeyIDs))
	}

	// Cost breakdown.
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n", dim.Render(strings.Repeat("─", 50)))
	_, _ = fmt.Fprintf(os.Stderr, "  %-40s %s\n", "Compute total", fmt.Sprintf("$%.4f/hr", computeHourly))
	_, _ = fmt.Fprintf(os.Stderr, "  %-40s %s\n", "Storage total", fmt.Sprintf("$%.4f/hr", storageHourly))
	total := computeHourly + storageHourly
	_, _ = fmt.Fprintf(os.Stderr, "  %s  %s\n", bold.Render(fmt.Sprintf("%-40s", "Total")), bold.Render(fmt.Sprintf("$%.4f/hr", total)))
	_, _ = fmt.Fprintf(os.Stderr, "  %s\n\n", dim.Render(strings.Repeat("─", 50)))
}

// --- Helpers ---

// instanceUnits returns the number of billable units (GPUs or vCPUs).
func instanceUnits(t *verda.InstanceTypeInfo) int {
	if t.GPU.NumberOfGPUs > 0 {
		return t.GPU.NumberOfGPUs
	}
	return t.CPU.NumberOfCores
}

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
