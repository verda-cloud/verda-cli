package template

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdEdit creates the template edit command.
func NewCmdEdit(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit [resource/name]",
		Short: "Edit fields of an existing template",
		Long: cmdutil.LongDesc(`
			Edit specific fields of an existing template. Shows a menu of
			all template fields with their current values — pick which ones
			to change.

			Without arguments, shows an interactive template picker first.
		`),
		Example: cmdutil.Examples(`
			# Interactive picker, then field menu
			verda template edit

			# Edit a specific template
			verda template edit vm/gpu-training
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runEdit(cmd, f, ioStreams, args[0])
			}
			return runEditInteractive(cmd, f, ioStreams)
		},
	}

	return cmd
}

func runEditInteractive(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	entry, err := pickTemplateEntry(cmd, f)
	if err != nil {
		return err
	}
	if entry == nil {
		return nil
	}
	return runEdit(cmd, f, ioStreams, entry.Resource+"/"+entry.Name)
}

func runEdit(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, ref string) error {
	resource, name, err := parseRef(ref)
	if err != nil {
		return err
	}

	baseDir, err := cmdutil.TemplatesBaseDir()
	if err != nil {
		return err
	}

	tmpl, err := Load(baseDir, resource, name)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	prompter := f.Prompter()

	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Editing template: %s/%s\n\n", resource, name)

	// Field menu loop.
	for {
		fields := buildFieldMenu(tmpl)
		labels := make([]string, len(fields)+1)
		for i, field := range fields {
			labels[i] = fmt.Sprintf("%-20s %s", field.label, field.display(tmpl))
		}
		labels[len(fields)] = "Save & exit"

		idx, selErr := prompter.Select(ctx, "Edit field", labels)
		if selErr != nil {
			// Ctrl+C — save what we have
			break
		}

		if idx == len(fields) {
			break // Save & exit
		}

		if err := fields[idx].edit(ctx, f, tmpl); err != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Error: %v\n", err)
		}
	}

	if err := Save(baseDir, resource, name, tmpl); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(ioStreams.Out, "Template %s/%s updated\n", resource, name)
	return nil
}

// editableField describes a template field that can be edited.
type editableField struct {
	label   string
	display func(t *Template) string
	edit    func(ctx context.Context, f cmdutil.Factory, t *Template) error
}

func buildFieldMenu(tmpl *Template) []editableField {
	fields := []editableField{
		{
			label:   "Billing Type",
			display: func(t *Template) string { return valueOrDash(t.BillingType) },
			edit: func(ctx context.Context, f cmdutil.Factory, t *Template) error {
				choices := []string{"on-demand", "spot"}
				idx, err := f.Prompter().Select(ctx, "Billing type", choices)
				if err != nil {
					return nil //nolint:nilerr // user canceled
				}
				t.BillingType = choices[idx]
				if t.BillingType == "spot" {
					t.Contract = ""
				}
				return nil
			},
		},
		{
			label:   "Kind",
			display: func(t *Template) string { return valueOrDash(t.Kind) },
			edit: func(ctx context.Context, f cmdutil.Factory, t *Template) error {
				choices := []string{"gpu", "cpu"}
				idx, err := f.Prompter().Select(ctx, "Kind", choices)
				if err != nil {
					return nil //nolint:nilerr // user canceled
				}
				t.Kind = choices[idx]
				return nil
			},
		},
		{
			label:   "Instance Type",
			display: func(t *Template) string { return valueOrDash(t.InstanceType) },
			edit:    editInstanceType,
		},
		{
			label:   "Location",
			display: func(t *Template) string { return valueOrDash(t.Location) },
			edit:    editLocation,
		},
		{
			label:   "Image",
			display: func(t *Template) string { return valueOrDash(t.Image) },
			edit:    editImage,
		},
		{
			label:   "OS Volume Size",
			display: func(t *Template) string { return intOrDash(t.OSVolumeSize) + " GiB" },
			edit: func(ctx context.Context, f cmdutil.Factory, t *Template) error {
				current := "50"
				if t.OSVolumeSize > 0 {
					current = strconv.Itoa(t.OSVolumeSize)
				}
				val, err := f.Prompter().TextInput(ctx, "OS volume size (GiB)", tui.WithDefault(current))
				if err != nil {
					return nil //nolint:nilerr // user canceled
				}
				if val != "" {
					n, parseErr := strconv.Atoi(val)
					if parseErr != nil || n <= 0 {
						return errors.New("must be a positive integer")
					}
					t.OSVolumeSize = n
				}
				return nil
			},
		},
		{
			label: "SSH Keys",
			display: func(t *Template) string {
				if len(t.SSHKeys) == 0 {
					return "-"
				}
				return strings.Join(t.SSHKeys, ", ")
			},
			edit: editSSHKeys,
		},
		{
			label:   "Startup Script",
			display: func(t *Template) string { return valueOrDash(t.StartupScript) },
			edit:    editStartupScript,
		},
		{
			label: "Hostname Pattern",
			display: func(t *Template) string {
				return valueOrDash(t.HostnamePattern)
			},
			edit: func(ctx context.Context, f cmdutil.Factory, t *Template) error {
				hint := t.HostnamePattern
				if hint == "" {
					hint = "{random}-{location}"
				}
				val, err := f.Prompter().TextInput(ctx, "Hostname pattern ({random}, {location})", tui.WithDefault(hint))
				if err != nil {
					return nil //nolint:nilerr // user canceled
				}
				t.HostnamePattern = val
				return nil
			},
		},
	}
	return fields
}

func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func intOrDash(n int) string {
	if n == 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

// --- API-backed field editors ---

func editInstanceType(ctx context.Context, f cmdutil.Factory, t *Template) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	isSpot := t.BillingType == "spot"
	types, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading instance types...", func() ([]verda.InstanceTypeInfo, error) {
		return client.InstanceTypes.Get(ctx, "usd")
	})
	if err != nil {
		return err
	}

	choices := make([]string, 0, len(types))
	values := make([]string, 0, len(types))
	for i := range types {
		it := &types[i]
		if t.Kind != "" && !matchKind(it.InstanceType, t.Kind) {
			continue
		}
		price := it.PricePerHour
		if isSpot {
			price = it.SpotPrice
		}
		label := fmt.Sprintf("%-18s $%.3f/hr", it.InstanceType, price)
		choices = append(choices, label)
		values = append(values, it.InstanceType)
	}
	if len(choices) == 0 {
		return errors.New("no instance types available")
	}

	idx, selErr := f.Prompter().Select(ctx, "Instance type", choices)
	if selErr != nil {
		return nil //nolint:nilerr // user canceled
	}
	t.InstanceType = values[idx]
	return nil
}

func editLocation(ctx context.Context, f cmdutil.Factory, t *Template) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	locations, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading locations...", func() ([]verda.Location, error) {
		return client.Locations.Get(ctx)
	})
	if err != nil {
		return err
	}

	choices := make([]string, len(locations))
	for i, loc := range locations {
		choices[i] = loc.Code
	}

	idx, selErr := f.Prompter().Select(ctx, "Location", choices)
	if selErr != nil {
		return nil //nolint:nilerr // user canceled
	}
	t.Location = locations[idx].Code
	return nil
}

func editImage(ctx context.Context, f cmdutil.Factory, t *Template) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	images, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading images...", func() ([]verda.Image, error) {
		return client.Images.Get(ctx)
	})
	if err != nil {
		return err
	}

	choices := make([]string, 0, len(images))
	values := make([]string, 0, len(images))
	for _, img := range images {
		if img.IsCluster {
			continue
		}
		choices = append(choices, img.ID)
		values = append(values, img.ID)
	}

	idx, selErr := f.Prompter().Select(ctx, "Image", choices)
	if selErr != nil {
		return nil //nolint:nilerr // user canceled
	}
	t.Image = values[idx]
	return nil
}

func editSSHKeys(ctx context.Context, f cmdutil.Factory, t *Template) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	keys, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading SSH keys...", func() ([]verda.SSHKey, error) {
		return client.SSHKeys.GetAllSSHKeys(ctx)
	})
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return errors.New("no SSH keys found — create one first with 'verda sshkey create'")
	}

	choices := make([]string, len(keys))
	// Pre-select keys that are already in the template.
	existing := make(map[string]bool, len(t.SSHKeys))
	for _, name := range t.SSHKeys {
		existing[name] = true
	}
	var defaults []int
	for i, k := range keys {
		choices[i] = k.Name
		if existing[k.Name] {
			defaults = append(defaults, i)
		}
	}

	selected, selErr := f.Prompter().MultiSelect(ctx, "SSH keys to inject", choices, tui.WithMultiSelectDefaults(defaults))
	if selErr != nil {
		return nil //nolint:nilerr // user canceled
	}

	t.SSHKeys = make([]string, len(selected))
	for i, idx := range selected {
		t.SSHKeys[i] = keys[idx].Name
	}
	return nil
}

func editStartupScript(ctx context.Context, f cmdutil.Factory, t *Template) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}
	scripts, err := cmdutil.WithSpinner(ctx, f.Status(), "Loading startup scripts...", func() ([]verda.StartupScript, error) {
		return client.StartupScripts.GetAllStartupScripts(ctx)
	})
	if err != nil {
		return err
	}

	choices := []string{"None (clear)"}
	for _, s := range scripts {
		choices = append(choices, s.Name)
	}

	idx, selErr := f.Prompter().Select(ctx, "Startup script", choices)
	if selErr != nil {
		return nil //nolint:nilerr // user canceled
	}

	if idx == 0 {
		t.StartupScript = ""
		t.StartupScriptSkip = true
	} else {
		t.StartupScript = scripts[idx-1].Name
		t.StartupScriptSkip = false
	}
	return nil
}

// matchKind returns true if the instance type matches the given kind.
func matchKind(instanceType, kind string) bool {
	isCPU := strings.HasPrefix(strings.ToUpper(instanceType), "CPU.")
	switch strings.ToLower(kind) {
	case "cpu":
		return isCPU
	case "gpu":
		return !isCPU
	}
	return true
}
