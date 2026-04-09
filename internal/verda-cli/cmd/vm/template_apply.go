package vm

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	clioptions "github/verda-cloud/verda-cli/internal/verda-cli/options"
	"github/verda-cloud/verda-cli/internal/verda-cli/template"
)

// resolveCreateInputs handles template loading and wizard invocation.
// Returns (true, nil) if the caller should stop (user canceled), or (false, nil)
// to continue with the create flow.
func resolveCreateInputs(
	cmd *cobra.Command,
	f cmdutil.Factory,
	ioStreams cmdutil.IOStreams,
	client *verda.Client,
	opts *createOptions,
) (done bool, err error) {
	// Load template when --from is provided.
	if opts.From != "" {
		if err := applyTemplateFrom(cmd.Context(), f, ioStreams, client, opts); err != nil {
			return true, err
		}
	}

	// Run wizard for any remaining missing fields.
	if opts.InstanceType == "" || opts.Image == "" || opts.Hostname == "" {
		if err := runWizard(cmd.Context(), f, ioStreams, opts); err != nil {
			return true, err
		}
	}

	return false, nil
}

// applyTemplateFrom loads a template by name or path, applies its values
// to opts, resolves SSH key / startup script names to IDs, and prints a summary.
func applyTemplateFrom(
	ctx context.Context,
	f cmdutil.Factory,
	ioStreams cmdutil.IOStreams,
	client *verda.Client,
	opts *createOptions,
) error {
	verdaDir, err := clioptions.VerdaDir()
	if err != nil {
		return err
	}
	baseDir := filepath.Join(verdaDir, "templates")

	path, err := template.Resolve(baseDir, "vm", opts.From)
	if err != nil {
		return err
	}
	tmpl, err := template.LoadFromPath(path)
	if err != nil {
		return err
	}

	applyTemplate(tmpl, opts)

	resolveCtx, resolveCancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer resolveCancel()
	warnings := resolveTemplateNames(resolveCtx, client, tmpl, opts)

	printTemplateSummary(ioStreams, tmpl)
	for _, w := range warnings {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Warning: %s -- will prompt during wizard\n", w)
	}

	return nil
}

// applyTemplate pre-fills createOptions from a template.
func applyTemplate(tmpl *template.Template, opts *createOptions) {
	if tmpl.BillingType == billingTypeSpot {
		opts.IsSpot = true
	}
	if tmpl.Contract != "" {
		opts.Contract = tmpl.Contract
	}
	if tmpl.Kind != "" {
		opts.Kind = tmpl.Kind
	}
	if tmpl.InstanceType != "" {
		opts.InstanceType = tmpl.InstanceType
	}
	if tmpl.Location != "" {
		opts.LocationCode = tmpl.Location
	}
	if tmpl.Image != "" {
		opts.Image = tmpl.Image
	}
	if tmpl.OSVolumeSize != 0 {
		opts.OSVolumeSize = tmpl.OSVolumeSize
	}
	if len(tmpl.Storage) > 0 {
		// Only the first storage entry is applied — the wizard's convenience
		// flags (StorageSize/StorageType) support a single additional volume.
		opts.StorageSize = tmpl.Storage[0].Size
		opts.StorageType = tmpl.Storage[0].Type
	}
	// SSH keys and startup script are handled by resolveTemplateNames, not here.
}

// resolveTemplateNames resolves SSH key names and startup script name to IDs.
// Returns warnings for any names that couldn't be resolved.
func resolveTemplateNames(ctx context.Context, client *verda.Client, tmpl *template.Template, opts *createOptions) []string {
	sshWarnings := resolveSSHKeyNames(ctx, client, tmpl.SSHKeys, opts)
	scriptWarnings := resolveStartupScriptName(ctx, client, tmpl.StartupScript, opts)
	warnings := make([]string, 0, len(sshWarnings)+len(scriptWarnings))
	warnings = append(warnings, sshWarnings...)
	warnings = append(warnings, scriptWarnings...)
	return warnings
}

// resolveSSHKeyNames resolves SSH key names to IDs via the API.
// On API errors, returns no warnings (wizard will prompt later).
func resolveSSHKeyNames(ctx context.Context, client *verda.Client, names []string, opts *createOptions) []string {
	if len(names) == 0 {
		return nil
	}
	keys, err := client.SSHKeys.GetAllSSHKeys(ctx)
	if err != nil {
		return nil // silently skip; wizard will prompt later
	}

	nameToID := make(map[string]string, len(keys))
	for _, k := range keys {
		nameToID[k.Name] = k.ID
	}

	var warnings []string
	for _, name := range names {
		id, ok := nameToID[name]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("SSH key %q not found", name))
			continue
		}
		opts.SSHKeyIDs = append(opts.SSHKeyIDs, id)
		opts.sshKeyNames = append(opts.sshKeyNames, name)
	}
	return warnings
}

// resolveStartupScriptName resolves a startup script name to its ID via the API.
// On API errors, returns no warnings (wizard will prompt later).
func resolveStartupScriptName(ctx context.Context, client *verda.Client, name string, opts *createOptions) []string {
	if name == "" {
		return nil
	}
	scripts, err := client.StartupScripts.GetAllStartupScripts(ctx)
	if err != nil {
		return nil // silently skip; wizard will prompt later
	}

	for _, s := range scripts {
		if s.Name == name {
			opts.StartupScriptID = s.ID
			opts.startupScriptName = s.Name
			return nil
		}
	}
	return []string{fmt.Sprintf("startup script %q not found", name)}
}

// printTemplateSummary displays the template values being used.
func printTemplateSummary(ioStreams cmdutil.IOStreams, tmpl *template.Template) {
	w := ioStreams.ErrOut

	_, _ = fmt.Fprintln(w, "  Using template:")
	_, _ = fmt.Fprintln(w)

	if tmpl.BillingType != "" {
		_, _ = fmt.Fprintf(w, "    %-14s %s\n", "Billing:", tmpl.BillingType)
	}
	if tmpl.Kind != "" {
		_, _ = fmt.Fprintf(w, "    %-14s %s\n", "Kind:", strings.ToUpper(tmpl.Kind))
	}
	if tmpl.InstanceType != "" {
		_, _ = fmt.Fprintf(w, "    %-14s %s\n", "Type:", tmpl.InstanceType)
	}
	if tmpl.Location != "" {
		_, _ = fmt.Fprintf(w, "    %-14s %s\n", "Location:", tmpl.Location)
	}
	if tmpl.Image != "" {
		_, _ = fmt.Fprintf(w, "    %-14s %s\n", "Image:", tmpl.Image)
	}
	if tmpl.OSVolumeSize != 0 {
		_, _ = fmt.Fprintf(w, "    %-14s %d GiB\n", "OS Volume:", tmpl.OSVolumeSize)
	}
	if len(tmpl.Storage) > 0 {
		s := tmpl.Storage[0]
		_, _ = fmt.Fprintf(w, "    %-14s %s %d GiB\n", "Storage:", s.Type, s.Size)
	}
	if len(tmpl.SSHKeys) > 0 {
		_, _ = fmt.Fprintf(w, "    %-14s %s\n", "SSH Keys:", strings.Join(tmpl.SSHKeys, ", "))
	}
	if tmpl.StartupScript != "" {
		_, _ = fmt.Fprintf(w, "    %-14s %s\n", "Startup:", tmpl.StartupScript)
	}

	_, _ = fmt.Fprintln(w)
}
