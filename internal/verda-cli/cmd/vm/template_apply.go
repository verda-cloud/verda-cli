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
	fromFlagSet := cmd.Flags().Changed("from")
	needsWizard := opts.InstanceType == "" || opts.Image == "" || opts.Hostname == ""

	if !fromFlagSet && !needsWizard {
		return false, nil
	}

	allEmpty := opts.InstanceType == "" && opts.Image == "" && opts.Hostname == ""
	applied, err := handleTemplateFrom(cmd.Context(), f, ioStreams, client, opts, fromFlagSet, allEmpty)
	if err != nil {
		return true, err
	}
	// If --from was explicitly set and the picker was canceled, stop.
	if fromFlagSet && !applied {
		return true, nil
	}

	// Run wizard for any remaining missing fields.
	if opts.InstanceType == "" || opts.Image == "" || opts.Hostname == "" {
		if err := runWizard(cmd.Context(), f, ioStreams, opts); err != nil {
			return true, err
		}
	}

	return false, nil
}

// handleTemplateFrom loads a template (by ref or picker), applies it to opts,
// resolves SSH key / startup script names to IDs, and prints the summary.
//
// Parameters:
//   - fromFlagSet: true when --from was explicitly provided on the command line
//   - ref: the --from value (may be empty when the flag is set without a value)
//   - allEmpty: true when no required flags were set at all (instance-type, os, hostname)
//
// Returns true if a template was applied, false otherwise.
func handleTemplateFrom(
	ctx context.Context,
	f cmdutil.Factory,
	ioStreams cmdutil.IOStreams,
	client *verda.Client,
	opts *createOptions,
	fromFlagSet bool,
	allEmpty bool,
) (applied bool, err error) {
	var tmpl *template.Template

	if fromFlagSet {
		// --from explicitly used (with or without value)
		tmpl, err = loadTemplate(ctx, f, opts.From, false)
		if err != nil {
			return false, err
		}
		if tmpl == nil {
			return false, nil // user canceled picker
		}
	} else if allEmpty {
		// No flags at all: offer template picker with "Start from scratch"
		tmpl, err = loadTemplate(ctx, f, "", true)
		if err != nil {
			return false, err
		}
		// tmpl may be nil if "Start from scratch" selected or no templates
	}

	if tmpl == nil {
		return false, nil
	}

	applyTemplate(tmpl, opts)
	warnings := resolveTemplateNames(ctx, client, tmpl, opts)
	printTemplateSummary(ioStreams, tmpl)
	for _, w := range warnings {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Warning: %s -- will prompt during wizard\n", w)
	}

	return true, nil
}

// loadTemplate resolves and loads a template.
// - If ref is non-empty: resolve by name or path using template.Resolve()
// - If ref is empty: show a picker of available VM templates
//   - If allowScratch is true: add "Start from scratch" option at the end
//   - If allowScratch is false and no templates exist: return error
//
// Returns nil if user cancels or selects "Start from scratch".
func loadTemplate(ctx context.Context, f cmdutil.Factory, ref string, allowScratch bool) (*template.Template, error) {
	verdaDir, err := clioptions.VerdaDir()
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Join(verdaDir, "templates")

	if ref != "" {
		path, err := template.Resolve(baseDir, "vm", ref)
		if err != nil {
			return nil, err
		}
		return template.LoadFromPath(path)
	}

	// No ref: show a picker.
	entries, err := template.List(baseDir, "vm")
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		if !allowScratch {
			return nil, fmt.Errorf("no VM templates found in %s", filepath.Join(baseDir, "vm"))
		}
		// No templates and scratch allowed: skip picker, treat as scratch.
		return nil, nil
	}

	// Build labels for the picker.
	labels := make([]string, 0, len(entries)+1)
	for _, e := range entries {
		labels = append(labels, fmt.Sprintf("%-20s  %s", e.Name, e.Description))
	}
	if allowScratch {
		labels = append(labels, "Start from scratch")
	}

	idx, err := f.Prompter().Select(ctx, "Select a template:", labels)
	if err != nil {
		return nil, err
	}

	// "Start from scratch" is the last entry when allowScratch is true.
	if allowScratch && idx == len(entries) {
		return nil, nil
	}

	return template.LoadFromPath(entries[idx].Path)
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
