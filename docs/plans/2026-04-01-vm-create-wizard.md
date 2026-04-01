# VM Create Wizard Flow — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn `verda vm create` into an interactive wizard that walks users through VM creation following the web UI flow, while preserving full flag-based non-interactive usage.

**Architecture:** A new `wizard.go` file in the `vm` package defines the wizard `Flow` with 12 steps matching the web UI order. Each step's `Loader` calls the Verda SDK to fetch real data (instance types, locations, images, SSH keys, etc.). The existing `create.go` delegates to the wizard when required flags are missing. Flag values act as pre-filled answers via `IsSet`/`Value`.

**Tech Stack:** verdagostack `pkg/tui/wizard` engine, verdacloud-sdk-go v1.4.0, Cobra flags

---

### Task 1: Point go.mod at local verdagostack

The published `verdagostack v1.0.0` doesn't include the wizard engine. Add a `replace` directive to use the local checkout on `feature/tui-wizard-engine`.

**Files:**
- Modify: `go.mod`

**Step 1: Add replace directive**

Add to the end of `go.mod`:

```
replace github.com/verda-cloud/verdagostack => ../verdagostack
```

**Step 2: Tidy modules**

Run: `go mod tidy`
Expected: success, go.sum updated

**Step 3: Verify build**

Run: `go build ./...`
Expected: compiles without errors

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: point verdagostack at local checkout for wizard engine dev"
```

---

### Task 2: Create the wizard flow definition

Define the 12-step wizard flow in a new file. This task only defines the `Flow` struct and step definitions — no wiring to the command yet. Each step binds to fields on `createOptions` via `Setter`/`Resetter`/`IsSet`/`Value`.

**Files:**
- Create: `internal/verda-cli/cmd/vm/wizard.go`

**Step 1: Write the wizard flow file**

```go
package vm

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

// buildCreateFlow builds the interactive wizard flow for vm create.
// It mirrors the web UI step order:
//
//	billing-type → contract → kind → instance-type → location →
//	image → os-volume-size → storage-size → ssh-keys →
//	startup-script → hostname → description
func buildCreateFlow(client *verda.Client, opts *createOptions) *wizard.Flow {
	return &wizard.Flow{
		Name: "vm-create",
		Steps: []wizard.Step{
			stepBillingType(opts),
			stepContract(client, opts),
			stepKind(opts),
			stepInstanceType(client, opts),
			stepLocation(client, opts),
			stepImage(client, opts),
			stepOSVolumeSize(opts),
			stepStorageSize(opts),
			stepSSHKeys(client, opts),
			stepStartupScript(client, opts),
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
			if v.(string) == "spot" {
				opts.IsSpot = true
				opts.Contract = "SPOT"
			} else {
				opts.IsSpot = false
			}
		},
		Resetter: func() {
			opts.IsSpot = false
			opts.Contract = ""
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

func stepContract(client *verda.Client, opts *createOptions) wizard.Step {
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
			periods, err := client.LongTerm.GetInstancePeriods(ctx)
			if err != nil {
				// Non-fatal: just offer pay-as-you-go.
				return choices, nil
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

func stepInstanceType(client *verda.Client, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "instance-type",
		Description: "Instance type",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		DependsOn:   []string{"kind", "billing-type"},
		Loader: func(ctx context.Context, _ tui.Prompter, c map[string]any) ([]wizard.Choice, error) {
			types, err := client.InstanceTypes.Get(ctx, "usd")
			if err != nil {
				return nil, fmt.Errorf("fetching instance types: %w", err)
			}

			kind := c["kind"].(string)
			isSpot := c["billing-type"] == "spot"

			var choices []wizard.Choice
			for _, t := range types {
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

func stepLocation(client *verda.Client, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "location",
		Description: "Datacenter location",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		DependsOn:   []string{"instance-type", "billing-type"},
		Loader: func(ctx context.Context, _ tui.Prompter, c map[string]any) ([]wizard.Choice, error) {
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
				for _, a := range la.Availabilities {
					if a == instType {
						loc := locMap[la.LocationCode]
						label := fmt.Sprintf("%s (%s)", loc.Code, loc.Name)
						choices = append(choices, wizard.Choice{
							Label: label,
							Value: loc.Code,
						})
						break
					}
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

func stepImage(client *verda.Client, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "image",
		Description: "Operating system image",
		Prompt:      wizard.SelectPrompt,
		Required:    true,
		Loader: func(ctx context.Context, _ tui.Prompter, _ map[string]any) ([]wizard.Choice, error) {
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

func stepSSHKeys(client *verda.Client, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "ssh-keys",
		Description: "SSH keys to inject",
		Prompt:      wizard.MultiSelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, _ tui.Prompter, _ map[string]any) ([]wizard.Choice, error) {
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

func stepStartupScript(client *verda.Client, opts *createOptions) wizard.Step {
	return wizard.Step{
		Name:        "startup-script",
		Description: "Startup script (optional)",
		Prompt:      wizard.SelectPrompt,
		Required:    false,
		Loader: func(ctx context.Context, _ tui.Prompter, _ map[string]any) ([]wizard.Choice, error) {
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

func formatGPU(t verda.InstanceTypeInfo) string {
	if t.GPU.NumberOfGPUs > 0 {
		return fmt.Sprintf("%dx %s", t.GPU.NumberOfGPUs, t.GPU.Description)
	}
	return fmt.Sprintf("%d cores", t.CPU.NumberOfCores)
}

func formatMemory(t verda.InstanceTypeInfo) string {
	if t.GPUMemory.SizeInGigabytes > 0 {
		return fmt.Sprintf("%dGB VRAM, %dGB RAM", t.GPUMemory.SizeInGigabytes, t.Memory.SizeInGigabytes)
	}
	return fmt.Sprintf("%dGB RAM", t.Memory.SizeInGigabytes)
}
```

**Step 2: Verify compilation**

Run: `go build ./internal/verda-cli/cmd/vm/...`
Expected: compiles (wizard.go references createOptions and SDK types)

**Step 3: Commit**

```bash
git add internal/verda-cli/cmd/vm/wizard.go
git commit -m "feat(vm): add wizard flow definition for vm create"
```

---

### Task 3: Wire the wizard into `vm create`

Modify `create.go` to run the wizard when required flags are missing. The wizard fills `createOptions`, then the existing `request()` + API call proceeds unchanged.

**Files:**
- Modify: `internal/verda-cli/cmd/vm/create.go`

**Step 1: Remove required flag marks and add wizard entry point**

In `NewCmdCreate`, remove the `MarkFlagRequired` calls (the wizard handles missing values):

```go
// DELETE these lines:
_ = cmd.MarkFlagRequired("instance-type")
_ = cmd.MarkFlagRequired("os")
_ = cmd.MarkFlagRequired("hostname")
```

**Step 2: Update `runCreate` to detect missing flags and launch wizard**

Replace the `runCreate` function:

```go
func runCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *createOptions) error {
	// If any required field is missing, run the interactive wizard.
	if opts.InstanceType == "" || opts.Image == "" || opts.Hostname == "" {
		if err := runWizard(cmd.Context(), f, ioStreams, opts); err != nil {
			return err
		}
	}

	req, err := opts.request()
	if err != nil {
		return cmdutil.UsageErrorf(cmd, "%v", err)
	}

	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	instance, err := client.Instances.Create(ctx, req)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(ioStreams.Out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(instance)
}

func runWizard(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *createOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	flow := buildCreateFlow(client, opts)
	engine := wizard.NewEngine(f.Prompter(), wizard.WithOutput(ioStreams.ErrOut))

	fmt.Fprintln(ioStreams.ErrOut, "=== Create VM Instance ===")
	fmt.Fprintln(ioStreams.ErrOut, "Navigate: ↑/↓ to move, Enter to select, Esc to go back")
	fmt.Fprintln(ioStreams.ErrOut)

	return engine.Run(ctx, flow)
}
```

Add the missing import for `wizard`:

```go
"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: compiles

**Step 4: Run existing tests**

Run: `go test ./internal/verda-cli/cmd/vm/...`
Expected: all existing tests pass (they set all fields, so wizard is never triggered)

**Step 5: Commit**

```bash
git add internal/verda-cli/cmd/vm/create.go
git commit -m "feat(vm): wire wizard flow into vm create for interactive mode"
```

---

### Task 4: Write wizard integration test

Write a test that uses the mock prompter from `verdagostack/pkg/tui/testing` to verify the wizard flow completes and populates `createOptions` correctly.

**Files:**
- Create: `internal/verda-cli/cmd/vm/wizard_test.go`

**Step 1: Write the test**

```go
package vm

import (
	"context"
	"testing"

	"github.com/verda-cloud/verdagostack/pkg/tui/testing/promptertest"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

func TestBuildCreateFlowHappyPath(t *testing.T) {
	t.Parallel()

	// We cannot use a real verda.Client in unit tests, so we test
	// the steps that use StaticChoices (billing-type, kind) and the
	// text-input steps (os-volume-size, storage-size, hostname, description).
	// API-dependent steps are tested via IsSet (simulating flag pre-fill).

	opts := &createOptions{
		// Pre-fill API-dependent steps as if flags were provided.
		Contract:        "PAY_AS_YOU_GO",
		InstanceType:    "1V100.6V",
		LocationCode:    "FIN-01",
		Image:           "ubuntu-24.04-cuda-12.8-open-docker",
		SSHKeyIDs:       []string{"key-1"},
		StartupScriptID: "script-1",
	}

	// The wizard will prompt: billing-type, kind, os-volume-size,
	// storage-size, hostname, description.
	mock := promptertest.New()
	mock.AddSelect(0)           // billing-type: On-Demand
	mock.AddSelect(0)           // kind: GPU
	mock.AddTextInput("100")    // os-volume-size
	mock.AddTextInput("500")    // storage-size
	mock.AddTextInput("my-gpu") // hostname
	mock.AddTextInput("")       // description (use default = hostname)

	flow := buildCreateFlow(nil, opts) // nil client OK — API steps skipped via IsSet
	engine := wizard.NewEngine(mock)

	if err := engine.Run(context.Background(), flow); err != nil {
		t.Fatalf("wizard Run failed: %v", err)
	}

	if opts.Kind != "gpu" {
		t.Errorf("expected kind=gpu, got %q", opts.Kind)
	}
	if opts.Hostname != "my-gpu" {
		t.Errorf("expected hostname=my-gpu, got %q", opts.Hostname)
	}
	if opts.OSVolumeSize != 100 {
		t.Errorf("expected os-volume-size=100, got %d", opts.OSVolumeSize)
	}
	if opts.StorageSize != 500 {
		t.Errorf("expected storage-size=500, got %d", opts.StorageSize)
	}
	if opts.IsSpot {
		t.Error("expected IsSpot=false for on-demand")
	}
}
```

Note: The exact mock import path (`promptertest`) depends on how verdagostack exports its test double. Verify by checking `pkg/tui/testing/prompter.go` for the package name. It may be `tuiTesting` or `testing` — adjust accordingly.

**Step 2: Run the test**

Run: `go test ./internal/verda-cli/cmd/vm/... -run TestBuildCreateFlowHappyPath -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/verda-cli/cmd/vm/wizard_test.go
git commit -m "test(vm): add wizard flow unit test with mock prompter"
```

---

### Task 5: Manual smoke test

Run the CLI interactively to verify the wizard launches and renders correctly.

**Step 1: Build the binary**

Run: `make build`

**Step 2: Run without flags (triggers wizard)**

Run: `./bin/verda vm create --auth.client-id <YOUR_ID> --auth.client-secret <YOUR_SECRET>`

Expected: The wizard launches with the Bubble Tea TUI, starting at "Billing type" step. Navigate through all steps, verify:
- Up/down arrows move selection
- Enter selects
- Esc goes back
- API data loads for instance types, locations, images, SSH keys
- After final step, the create request is sent

**Step 3: Run with all flags (skips wizard)**

Run: `./bin/verda vm create --instance-type 1V100.6V --os ubuntu-24.04-cuda-12.8-open-docker --hostname test-node --auth.client-id <YOUR_ID> --auth.client-secret <YOUR_SECRET>`

Expected: Wizard does NOT launch. Command executes directly as before.

---

## Notes for Iteration

Things to refine later (not in scope for this initial integration):

1. **Image filtering by instance type** — The `image` step should filter images based on whether the instance is GPU (show CUDA images) or CPU (show plain images). Add `DependsOn: []string{"kind"}` and filter in the loader.

2. **Rich instance type display** — Show a formatted table or columns for GPU count, VRAM, RAM, price. May need a custom renderer beyond simple labels.

3. **SSH key creation sub-flow** — When no SSH keys exist, offer to create one inline using the `Prompter` passed to the `LoaderFunc`.

4. **Location display with availability count** — Show how many units of the selected instance type are available at each location.

5. **Spot discontinue policies** — Add conditional steps for OS volume and storage volume spot discontinue policies when `billing-type == "spot"`.

6. **Non-interactive fallback** — Detect non-TTY stdin and skip the wizard entirely (error if required flags missing).
