package template

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/vm"
)

var resourceTypes = []string{"Instance (VM)"}
var resourceMap = map[int]string{0: "vm"}

// NewCmdCreate creates the template create command.
func NewCmdCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new resource template interactively",
		Long: cmdutil.LongDesc(`
			Create a reusable resource configuration template by running
			the interactive wizard. The wizard collects instance type,
			image, location, SSH keys, storage, and other settings.

			Templates are saved as YAML files under ~/.verda/templates/<resource>/.
			Names are auto-reformatted: "My GPU Setup" becomes "my-gpu-setup".

			After saving, use "verda vm create --from <name>" to create
			instances with pre-filled settings.

			You can manually edit the template YAML to add features like:
			  hostname_pattern: "gpu-{random}-{location}"
			  storage_skip: true
			  startup_script_skip: true
		`),
		Example: cmdutil.Examples(`
			# Create a template interactively
			verda template create

			# Create with a name (skips name prompt)
			verda template create gpu-training

			# Then use it to create VMs
			verda vm create --from gpu-training
			verda vm create --from gpu-training --hostname my-vm
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) > 0 {
				name = args[0]
			}
			return runCreate(cmd, f, ioStreams, name)
		},
	}

	return cmd
}

func runCreate(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, name string) error {
	ctx := cmd.Context()
	prompter := f.Prompter()

	// 1. Select resource type.
	idx, err := prompter.Select(ctx, "Resource type", resourceTypes)
	if err != nil {
		return nil //nolint:nilerr // user cancellation (Ctrl+C) is not an error
	}
	resource := resourceMap[idx]

	// 2. Resolve templates directory.
	baseDir, err := cmdutil.TemplatesBaseDir()
	if err != nil {
		return err
	}

	// 3. Get and validate template name (re-prompt on invalid input).
	for {
		if name == "" {
			name, err = prompter.TextInput(ctx, "Template name")
			if err != nil {
				return nil //nolint:nilerr // user cancellation (Ctrl+C) is not an error
			}
		}

		// Auto-format: lowercase, replace spaces/underscores with hyphens,
		// strip invalid characters.
		normalized := normalizeName(name)
		if normalized != name {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Reformatted: %s\n", normalized)
			name = normalized
		}

		if err := ValidateName(name); err != nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %v (e.g. gpu-training, cheap-dev-01)\n", err)
			name = "" // re-prompt
			continue
		}

		if _, loadErr := Load(baseDir, resource, name); loadErr == nil {
			_, _ = fmt.Fprintf(ioStreams.ErrOut, "  template %s/%s already exists\n", resource, name)
			name = "" // re-prompt
			continue
		}

		break
	}

	// 5. Run resource wizard.
	var tmpl *Template
	switch resource {
	case "vm":
		result, err := vm.RunTemplateWizard(ctx, f, ioStreams)
		if err != nil {
			return err
		}
		if result == nil {
			return nil // user canceled wizard
		}
		// 6. Convert result to Template.
		tmpl = vmResultToTemplate(result)
	default:
		return fmt.Errorf("unsupported resource type: %s", resource)
	}

	// 7. Save to disk.
	if err := Save(baseDir, resource, name, tmpl); err != nil {
		return err
	}

	// 8. Print confirmation.
	_, _ = fmt.Fprintf(ioStreams.Out, "Template %s/%s saved\n", resource, name)
	return nil
}

func vmResultToTemplate(r *vm.TemplateResult) *Template {
	tmpl := &Template{
		Resource:          "vm",
		BillingType:       r.BillingType,
		Contract:          r.Contract,
		Kind:              r.Kind,
		InstanceType:      r.InstanceType,
		Location:          r.Location,
		Image:             r.Image,
		OSVolumeSize:      r.OSVolumeSize,
		SSHKeys:           r.SSHKeyNames,
		StartupScript:     r.StartupScriptName,
		StorageSkip:       r.StorageSkip,
		StartupScriptSkip: r.StartupScriptSkip,
		HostnamePattern:   r.HostnamePattern,
		Description:       r.Description,
	}
	if r.StorageSize > 0 {
		tmpl.Storage = []StorageSpec{{
			Type: r.StorageType,
			Size: r.StorageSize,
		}}
	}
	return tmpl
}

// invalidChars matches anything that is not lowercase alphanumeric or hyphen.
var invalidChars = regexp.MustCompile(`[^a-z0-9-]+`)

// normalizeName auto-formats a template name: lowercases, replaces
// spaces and underscores with hyphens, strips other invalid characters,
// and collapses multiple hyphens.
func normalizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.NewReplacer(" ", "-", "_", "-").Replace(s)
	s = invalidChars.ReplaceAllString(s, "")
	// Collapse multiple hyphens.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	return s
}
