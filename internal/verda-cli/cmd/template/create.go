package template

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github/verda-cloud/verda-cli/internal/verda-cli/cmd/vm"
	clioptions "github/verda-cloud/verda-cli/internal/verda-cli/options"
)

var resourceTypes = []string{"Instance (VM)"}
var resourceMap = map[int]string{0: "vm"}

// NewCmdCreate creates the template create command.
func NewCmdCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new resource template interactively",
		Long: cmdutil.LongDesc(`
			Create a new resource configuration template by running
			the resource creation wizard. The template captures all
			settings so you can reuse them later with "verda vm create --from".
		`),
		Example: cmdutil.Examples(`
			# Create a template interactively (prompts for name)
			verda template create

			# Create a template with a specific name
			verda template create gpu-training

			# Short alias
			verda tmpl create my-template
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

	// 2. Prompt for template name if not provided as positional arg.
	if name == "" {
		name, err = prompter.TextInput(ctx, "Template name")
		if err != nil {
			return nil //nolint:nilerr // user cancellation (Ctrl+C) is not an error
		}
	}

	// 3. Validate name.
	if err := ValidateName(name); err != nil {
		return err
	}

	// 4. Check uniqueness.
	verdaDir, err := clioptions.VerdaDir()
	if err != nil {
		return err
	}
	baseDir := filepath.Join(verdaDir, "templates")

	if _, err := Load(baseDir, resource, name); err == nil {
		return fmt.Errorf("template %s/%s already exists", resource, name)
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
		Resource:      "vm",
		BillingType:   r.BillingType,
		Contract:      r.Contract,
		Kind:          r.Kind,
		InstanceType:  r.InstanceType,
		Location:      r.Location,
		Image:         r.Image,
		OSVolumeSize:  r.OSVolumeSize,
		SSHKeys:       r.SSHKeyNames,
		StartupScript: r.StartupScriptName,
	}
	if r.StorageSize > 0 {
		tmpl.Storage = []StorageSpec{{
			Type: r.StorageType,
			Size: r.StorageSize,
		}}
	}
	return tmpl
}
