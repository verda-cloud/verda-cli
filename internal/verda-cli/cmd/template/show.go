package template

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	clioptions "github/verda-cloud/verda-cli/internal/verda-cli/options"
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
		Use:   "show <resource/name>",
		Short: "Show details of a saved template",
		Long: cmdutil.LongDesc(`
			Display the full configuration of a saved template.
			The argument must be in resource/name format (e.g. vm/gpu-training).
		`),
		Example: cmdutil.Examples(`
			# Show a VM template
			verda template show vm/gpu-training

			# Output as JSON
			verda template show vm/gpu-training -o json
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(f, ioStreams, args[0])
		},
	}

	return cmd
}

func runShow(f cmdutil.Factory, ioStreams cmdutil.IOStreams, ref string) error {
	resource, name, err := parseRef(ref)
	if err != nil {
		return err
	}

	verdaDir, err := clioptions.VerdaDir()
	if err != nil {
		return err
	}
	baseDir := filepath.Join(verdaDir, "templates")

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

	printField := func(label, value string) {
		if value != "" {
			_, _ = fmt.Fprintf(ioStreams.Out, "  %-14s %s\n", label, value)
		}
	}

	printField("Resource:", tmpl.Resource)
	printField("Billing:", tmpl.BillingType)
	printField("Contract:", tmpl.Contract)
	printField("Kind:", tmpl.Kind)
	printField("Type:", tmpl.InstanceType)
	printField("Location:", tmpl.Location)
	printField("Image:", tmpl.Image)

	if tmpl.OSVolumeSize > 0 {
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-14s %d GiB\n", "OS Volume:", tmpl.OSVolumeSize)
	}

	if len(tmpl.Storage) > 0 {
		for _, s := range tmpl.Storage {
			_, _ = fmt.Fprintf(ioStreams.Out, "  %-14s %s %d GiB\n", "Storage:", s.Type, s.Size)
		}
	}

	if len(tmpl.SSHKeys) > 0 {
		printField("SSH Keys:", strings.Join(tmpl.SSHKeys, ", "))
	}

	printField("Startup:", tmpl.StartupScript)

	return nil
}
