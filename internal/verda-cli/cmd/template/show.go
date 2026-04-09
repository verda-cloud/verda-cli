package template

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// parseRef splits a "resource/name" reference into its two parts.
func parseRef(ref string) (resource, name string, err error) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid template reference %q: expected resource/name (e.g. vm/gpu-training)", ref)
	}
	return parts[0], parts[1], nil
}

// NewCmdShow creates the template show command.
func NewCmdShow(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show [resource/name]",
		Short: "Show details of a saved template",
		Long: cmdutil.LongDesc(`
			Display the full configuration of a saved template.
			Without arguments, shows an interactive picker.
			The argument must be in resource/name format (e.g. vm/gpu-training).
		`),
		Example: cmdutil.Examples(`
			# Interactive picker
			verda template show

			# Show a VM template
			verda template show vm/gpu-training

			# Output as JSON
			verda template show vm/gpu-training -o json
		`),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return runShow(f, ioStreams, args[0])
			}
			return runShowInteractive(cmd, f, ioStreams)
		},
	}

	return cmd
}

func runShow(f cmdutil.Factory, ioStreams cmdutil.IOStreams, ref string) error {
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

	// Structured output: emit JSON/YAML and return.
	if wrote, wErr := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), tmpl); wrote {
		return wErr
	}

	templatePath := filepath.Join(baseDir, resource, name+".yaml")
	_, _ = fmt.Fprintf(ioStreams.Out, "  Template: %s/%s\n", resource, name)
	_, _ = fmt.Fprintf(ioStreams.Out, "  File:     %s\n", templatePath)
	_, _ = fmt.Fprintln(ioStreams.Out)

	pf := func(label, value string) {
		if value == "" {
			value = "-"
		}
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-18s %s\n", label, value)
	}

	pf("Resource:", tmpl.Resource)
	pf("Billing:", tmpl.BillingType)
	pf("Contract:", tmpl.Contract)
	pf("Kind:", tmpl.Kind)
	pf("Type:", tmpl.InstanceType)
	pf("Location:", tmpl.Location)
	pf("Image:", tmpl.Image)

	if tmpl.OSVolumeSize > 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-18s %d GiB\n", "OS Volume:", tmpl.OSVolumeSize)
	} else {
		pf("OS Volume:", "-")
	}

	switch {
	case tmpl.StorageSkip:
		pf("Storage:", "None (skipped)")
	case len(tmpl.Storage) > 0:
		for _, s := range tmpl.Storage {
			_, _ = fmt.Fprintf(ioStreams.Out, "  %-18s %s %d GiB\n", "Storage:", s.Type, s.Size)
		}
	default:
		pf("Storage:", "-")
	}

	if len(tmpl.SSHKeys) > 0 {
		pf("SSH Keys:", strings.Join(tmpl.SSHKeys, ", "))
	} else {
		pf("SSH Keys:", "-")
	}

	if tmpl.StartupScriptSkip {
		pf("Startup Script:", "None (skipped)")
	} else {
		pf("Startup Script:", tmpl.StartupScript)
	}

	pf("Hostname Pattern:", tmpl.HostnamePattern)
	pf("Description:", tmpl.Description)

	return nil
}

func runShowInteractive(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	entry, err := pickTemplateEntry(cmd, f)
	if err != nil {
		return err
	}
	if entry == nil {
		return nil // user canceled
	}
	return runShow(f, ioStreams, entry.Resource+"/"+entry.Name)
}

// pickTemplateEntry shows an interactive picker of all saved templates.
// Returns nil if the user cancels or no templates exist.
func pickTemplateEntry(cmd *cobra.Command, f cmdutil.Factory) (*Entry, error) {
	baseDir, err := cmdutil.TemplatesBaseDir()
	if err != nil {
		return nil, err
	}
	entries, err := ListAll(baseDir)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("no templates found")
	}

	labels := make([]string, len(entries))
	for i, e := range entries {
		labels[i] = fmt.Sprintf("%-25s  %s", e.Resource+"/"+e.Name, e.Description)
	}

	idx, err := f.Prompter().Select(cmd.Context(), "Select a template", labels)
	if err != nil {
		return nil, nil //nolint:nilerr // user canceled
	}
	return &entries[idx], nil
}
