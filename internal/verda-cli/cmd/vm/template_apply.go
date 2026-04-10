package vm

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
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
	// Load template when --from is used.
	if cmd.Flags().Changed("from") {
		ref := strings.TrimSpace(opts.From)
		if err := applyTemplateFrom(cmd.Context(), f, ioStreams, client, opts, ref); err != nil {
			return true, err
		}
	}

	// Run wizard for any remaining missing fields.
	// When a template was used but didn't specify a location, prompt the user
	// so they can pick where to deploy instead of silently defaulting to FIN-01.
	templateWithoutLocation := cmd.Flags().Changed("from") && !opts.locationSet
	if opts.InstanceType == "" || opts.Image == "" || opts.Hostname == "" || templateWithoutLocation {
		if err := runWizard(cmd.Context(), f, ioStreams, opts); err != nil {
			return true, err
		}
	}

	return false, nil
}

// applyTemplateFrom loads a template, applies its values to opts, resolves
// SSH key / startup script names to IDs, and prints a summary.
// If ref is empty, shows an interactive picker; otherwise loads by name or path.
func applyTemplateFrom(
	ctx context.Context,
	f cmdutil.Factory,
	ioStreams cmdutil.IOStreams,
	client *verda.Client,
	opts *createOptions,
	ref string,
) error {
	baseDir, err := cmdutil.TemplatesBaseDir()
	if err != nil {
		return err
	}

	tmpl, err := loadTemplateRef(ctx, f, baseDir, ref)
	if err != nil {
		return err
	}
	if tmpl == nil {
		return nil // user canceled picker
	}

	applyTemplate(tmpl, opts)

	resolveCtx, resolveCancel := context.WithTimeout(ctx, f.Options().Timeout)
	defer resolveCancel()
	resolveTemplateNames(resolveCtx, ioStreams, client, tmpl, opts)

	printTemplateSummary(ioStreams, tmpl)

	return nil
}

// loadTemplateRef loads a template by name/path, or shows a picker when ref is empty.
func loadTemplateRef(ctx context.Context, f cmdutil.Factory, baseDir, ref string) (*template.Template, error) {
	if ref == "" {
		return pickTemplate(ctx, f, baseDir)
	}
	path, err := template.Resolve(baseDir, "vm", ref)
	if err != nil {
		return nil, err
	}
	return template.LoadFromPath(path)
}

// pickTemplate shows an interactive picker of saved VM templates.
// Returns nil if no templates exist or the user cancels.
func pickTemplate(ctx context.Context, f cmdutil.Factory, baseDir string) (*template.Template, error) {
	entries, err := template.List(baseDir, "vm")
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no VM templates found in %s", filepath.Join(baseDir, "vm"))
	}

	labels := make([]string, len(entries))
	for i, e := range entries {
		labels[i] = fmt.Sprintf("%-20s  %s", e.Name, e.Description)
	}

	idx, err := f.Prompter().Select(ctx, "Select a template", labels)
	if err != nil {
		return nil, nil //nolint:nilerr // user canceled
	}

	return template.LoadFromPath(entries[idx].Path)
}

// applyTemplate pre-fills createOptions from a template.
func applyTemplate(tmpl *template.Template, opts *createOptions) {
	if tmpl.BillingType != "" {
		opts.IsSpot = tmpl.BillingType == billingTypeSpot
		opts.billingTypeSet = true
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
		opts.locationSet = true
	}
	// Image name is resolved to ID by resolveTemplateNames, not here.
	if tmpl.OSVolumeSize != 0 {
		opts.OSVolumeSize = tmpl.OSVolumeSize
	}
	if len(tmpl.Storage) > 0 {
		// Only the first storage entry is applied — the wizard's convenience
		// flags (StorageSize/StorageType) support a single additional volume.
		opts.StorageSize = tmpl.Storage[0].Size
		opts.StorageType = tmpl.Storage[0].Type
	}
	if tmpl.StorageSkip {
		opts.storageSkip = true
	}
	if tmpl.StartupScriptSkip {
		opts.startupScriptSkip = true
	}
	// Hostname pattern: expand {random} and {location} placeholders.
	if tmpl.HostnamePattern != "" && opts.Hostname == "" {
		opts.Hostname = template.ExpandHostnamePattern(tmpl.HostnamePattern, opts.LocationCode)
	}
	// SSH keys and startup script are handled by resolveTemplateNames, not here.
}

// resolveTemplateNames resolves image name, SSH key names, and startup script
// name to IDs. Prints each warning to ioStreams.ErrOut and returns the
// collected warnings.
func resolveTemplateNames(ctx context.Context, ioStreams cmdutil.IOStreams, client *verda.Client, tmpl *template.Template, opts *createOptions) []string {
	imageWarnings := resolveImageName(ctx, client, tmpl.Image, opts)
	_, sshWarnings := resolveSSHKeyNames(ctx, client, tmpl.SSHKeys, opts)
	scriptWarnings := resolveStartupScriptName(ctx, client, tmpl.StartupScript, opts)
	warnings := make([]string, 0, len(imageWarnings)+len(sshWarnings)+len(scriptWarnings))
	warnings = append(warnings, imageWarnings...)
	warnings = append(warnings, sshWarnings...)
	warnings = append(warnings, scriptWarnings...)
	for _, w := range warnings {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  Warning: %s\n", w)
	}
	return warnings
}

// resolveImageName resolves an image name to its image_type slug via the API.
// The API expects image_type (e.g. "ubuntu-24.04-cuda-13.0-open-docker"), not
// the image ID. Filters by instance type when available so incompatible images
// (e.g. CUDA on CPU) are rejected early with a clear warning.
func resolveImageName(ctx context.Context, client *verda.Client, name string, opts *createOptions) (warnings []string) {
	if name == "" {
		return nil
	}
	var images []verda.Image
	var err error
	if opts.InstanceType != "" {
		images, err = client.Images.GetImagesByInstanceType(ctx, opts.InstanceType)
	} else {
		images, err = client.Images.Get(ctx)
	}
	if err != nil {
		return []string{fmt.Sprintf("failed to resolve image name: %v", err)}
	}
	// Try matching by name first.
	for _, img := range images {
		if img.Name == name {
			opts.Image = img.ImageType
			opts.imageName = img.Name
			return nil
		}
	}
	// Match by image_type slug (templates may store this directly).
	for _, img := range images {
		if img.ImageType == name {
			opts.Image = img.ImageType
			opts.imageName = img.Name
			return nil
		}
	}
	// Backward compat: if the value matches an image ID, resolve to image_type.
	for _, img := range images {
		if img.ID == name {
			opts.Image = img.ImageType
			opts.imageName = img.Name
			return nil
		}
	}
	return []string{fmt.Sprintf("image %q not found", name)}
}

// resolveSSHKeyNames resolves SSH key names to IDs via the API.
// Returns (resolved IDs, warnings). On API error or name-not-found, a warning
// is returned so the caller can inform the user.
func resolveSSHKeyNames(ctx context.Context, client *verda.Client, names []string, opts *createOptions) (ids, warnings []string) {
	if len(names) == 0 {
		return nil, nil
	}
	keys, err := client.SSHKeys.GetAllSSHKeys(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to resolve SSH key names: %v", err)}
	}

	nameToID := make(map[string]string, len(keys))
	for _, k := range keys {
		nameToID[k.Name] = k.ID
	}

	for _, name := range names {
		id, ok := nameToID[name]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("SSH key %q not found", name))
			continue
		}
		ids = append(ids, id)
		opts.SSHKeyIDs = append(opts.SSHKeyIDs, id)
		opts.sshKeyNames = append(opts.sshKeyNames, name)
	}
	return ids, warnings
}

// resolveStartupScriptName resolves a startup script name to its ID via the API.
// Returns warnings on API error or name-not-found so the caller can inform the user.
func resolveStartupScriptName(ctx context.Context, client *verda.Client, name string, opts *createOptions) (warnings []string) {
	if name == "" {
		return nil
	}
	scripts, err := client.StartupScripts.GetAllStartupScripts(ctx)
	if err != nil {
		return []string{fmt.Sprintf("failed to resolve startup script name: %v", err)}
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
	if tmpl.HostnamePattern != "" {
		_, _ = fmt.Fprintf(w, "    %-14s %s\n", "Hostname:", tmpl.HostnamePattern)
	}

	_, _ = fmt.Fprintln(w)
}
