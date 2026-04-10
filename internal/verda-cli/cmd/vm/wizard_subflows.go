package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func buildSSHKeyChoices(keys []verda.SSHKey) []wizard.Choice {
	choices := make([]wizard.Choice, 0, 1+len(keys))
	choices = append(choices, wizard.Choice{Label: "+ Add new SSH key", Value: addNewSSHKeyValue})
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
		return nil, nil //nolint:nilerr // User canceled or left input blank.
	}

	// Ask for source: load from file or paste.
	sourceIdx, err := prompter.Select(ctx, "Public key source", []string{
		"Load from file",
		"Paste content",
	})
	if err != nil {
		return nil, nil //nolint:nilerr // User canceled.
	}

	var pubKey string
	switch sourceIdx {
	case 0: // Load from file
		filePath, err := promptSSHKeyFilePath(ctx, prompter)
		if err != nil || filePath == "" {
			return nil, nil //nolint:nilerr // User canceled.
		}
		data, err := os.ReadFile(filePath) //nolint:gosec // User-provided path from interactive prompt, validated by validateFilePath.
		if err != nil {
			_, _ = prompter.Confirm(ctx, fmt.Sprintf("Error: %v. Press Enter to continue.", err), tui.WithConfirmDefault(true))
			return nil, nil
		}
		pubKey = string(data)
	case 1: // Paste content
		pubKey, err = prompter.TextInput(ctx, "Public key (paste)")
		if err != nil || strings.TrimSpace(pubKey) == "" {
			return nil, nil //nolint:nilerr // User canceled or left input blank.
		}
	}

	if strings.TrimSpace(pubKey) == "" {
		return nil, nil
	}

	created, err := client.SSHKeys.AddSSHKey(ctx, &verda.CreateSSHKeyRequest{
		Name:      strings.TrimSpace(name),
		PublicKey: strings.TrimSpace(pubKey),
	})
	if err != nil {
		// Show error and return to menu instead of crashing.
		_, _ = prompter.Confirm(ctx, fmt.Sprintf("Error: %v. Press Enter to continue.", err), tui.WithConfirmDefault(true))
		return nil, nil
	}
	return created, nil
}

// validateFilePath checks that the input is a non-empty path to an existing file.
var validateFilePath = func(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("file path is required")
	}
	if _, err := os.Stat(s); err != nil {
		return fmt.Errorf("file not found: %s", s)
	}
	return nil
}

// promptSSHKeyFilePath discovers .pub files in ~/.ssh/ and lets the user pick
// one, or enter a path manually. Returns "" if the user cancels.
func promptSSHKeyFilePath(ctx context.Context, prompter tui.Prompter) (string, error) {
	pubFiles := discoverSSHPubKeys()

	if len(pubFiles) == 0 {
		p, err := prompter.TextInput(ctx, "Public key file path",
			tui.WithPlaceholder("~/.ssh/id_ed25519.pub"),
			tui.WithValidation(validateFilePath),
		)
		if err != nil || strings.TrimSpace(p) == "" {
			return "", err
		}
		return strings.TrimSpace(p), nil
	}

	labels := make([]string, len(pubFiles)+1)
	copy(labels, pubFiles)
	labels[len(pubFiles)] = "Enter path manually..."

	idx, err := prompter.Select(ctx, "Select public key file", labels)
	if err != nil {
		return "", err
	}
	if idx < len(pubFiles) {
		return pubFiles[idx], nil
	}

	// Manual path entry.
	p, err := prompter.TextInput(ctx, "Public key file path",
		tui.WithValidation(validateFilePath),
	)
	if err != nil || strings.TrimSpace(p) == "" {
		return "", err
	}
	return strings.TrimSpace(p), nil
}

// discoverSSHPubKeys returns all .pub files found in ~/.ssh/, with well-known
// key types (id_ed25519, id_rsa, id_ecdsa) sorted first.
func discoverSSHPubKeys() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	sshDir := filepath.Join(home, ".ssh")

	matches, _ := filepath.Glob(filepath.Join(sshDir, "*.pub"))
	if len(matches) == 0 {
		return nil
	}

	// Sort well-known key types to the front.
	preferred := map[string]int{
		"id_ed25519.pub": 0,
		"id_rsa.pub":     1,
		"id_ecdsa.pub":   2,
	}
	slices.SortFunc(matches, func(a, b string) int {
		pa, oka := preferred[filepath.Base(a)]
		pb, okb := preferred[filepath.Base(b)]
		if oka && okb {
			return pa - pb
		}
		if oka {
			return -1
		}
		if okb {
			return 1
		}
		return strings.Compare(a, b)
	})

	return matches
}

func buildStartupScriptChoices(scripts []verda.StartupScript) []wizard.Choice {
	choices := make([]wizard.Choice, 0, 2+len(scripts))
	choices = append(choices,
		wizard.Choice{Label: "None (skip)", Value: ""},
		wizard.Choice{Label: "+ Add new startup script", Value: addNewScriptValue},
	)
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
		return nil, nil //nolint:nilerr // User canceled or left input blank.
	}

	// Ask for source: paste or load from file.
	sourceIdx, err := prompter.Select(ctx, "Script source", []string{
		"Load from file",
		"Paste content",
	})
	if err != nil {
		return nil, nil //nolint:nilerr // User canceled or left input blank.
	}

	var content string
	switch sourceIdx {
	case 0: // Load from file
		path, err := prompter.TextInput(ctx, "File path")
		if err != nil || strings.TrimSpace(path) == "" {
			return nil, nil //nolint:nilerr // User canceled or left input blank.
		}
		data, err := os.ReadFile(strings.TrimSpace(path))
		if err != nil {
			_, _ = prompter.Confirm(ctx, fmt.Sprintf("Error: %v. Press Enter to continue.", err), tui.WithConfirmDefault(true))
			return nil, nil
		}
		content = string(data)
	case 1: // Paste content
		content, err = prompter.Editor(ctx, "Script content (Ctrl+D to finish)",
			tui.WithEditorDefault("#!/bin/bash\n\n# Your startup script here\n"),
			tui.WithFileExt(".sh"))
		if err != nil {
			return nil, nil //nolint:nilerr // User canceled the editor; return to menu.
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
		return nil, nil
	}
	return created, nil
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
				Label: "  Existing: " + id,
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
		if vt, ok := cache.volumeTypes[verda.VolumeTypeNVMe]; ok && vt.Price.PricePerMonthPerGB > 0 {
			nvmeLabel = fmt.Sprintf("NVMe (fast SSD)  $%.2f/GB/mo", vt.Price.PricePerMonthPerGB)
		}
		if vt, ok := cache.volumeTypes[verda.VolumeTypeHDD]; ok && vt.Price.PricePerMonthPerGB > 0 {
			hddLabel = fmt.Sprintf("HDD (large capacity)  $%.2f/GB/mo", vt.Price.PricePerMonthPerGB)
		}
	}
	typeIdx, err := prompter.Select(ctx, "Volume type", []string{
		nvmeLabel,
		hddLabel,
		"← Back",
	})
	if err != nil {
		return nil, nil //nolint:nilerr // User pressed Esc/Ctrl+C during prompt.
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
		return nil, nil //nolint:nilerr // User pressed Esc/Ctrl+C or left input blank.
	}

	// Size
	sizeStr, err := prompter.TextInput(ctx, "Size in GiB", tui.WithDefault("100"))
	if err != nil || strings.TrimSpace(sizeStr) == "" {
		return nil, nil //nolint:nilerr // User pressed Esc/Ctrl+C or left input blank.
	}
	size, parseErr := strconv.Atoi(strings.TrimSpace(sizeStr))
	if parseErr != nil || size <= 0 {
		_, _ = prompter.Confirm(ctx, "Error: size must be a positive integer. Press Enter to continue.", tui.WithConfirmDefault(true))
		return nil, nil //nolint:nilerr // Invalid input is not a fatal error; show message and return to menu.
	}

	return &verda.VolumeCreateRequest{
		Name: strings.TrimSpace(name),
		Size: size,
		Type: volType,
	}, nil
}

func promptAttachExisting(ctx context.Context, prompter tui.Prompter, status tui.Status, client *verda.Client) (string, error) {
	volumes, err := cmdutil.WithSpinner(ctx, status, "Loading volumes...", func() ([]verda.Volume, error) {
		return client.Volumes.ListVolumes(ctx)
	})
	if err != nil {
		return "", fmt.Errorf("fetching volumes: %w", err)
	}

	// Filter to detached volumes only.
	var detached []verda.Volume
	for i := range volumes {
		if volumes[i].InstanceID == nil || *volumes[i].InstanceID == "" {
			detached = append(detached, volumes[i])
		}
	}

	if len(detached) == 0 {
		_, _ = prompter.Confirm(ctx, "No detached volumes available. Press Enter to continue.", tui.WithConfirmDefault(true))
		return "", nil
	}

	labels := make([]string, 0, len(detached)+1)
	for i := range detached {
		labels = append(labels, fmt.Sprintf("%s (%dGB %s, %s)", detached[i].Name, detached[i].Size, detached[i].Type, detached[i].Location))
	}
	labels = append(labels, "← Back")

	idx, err := prompter.Select(ctx, "Select volume to attach", labels)
	if err != nil {
		return "", nil //nolint:nilerr // User canceled or left input blank.
	}
	if idx == len(detached) { // "← Back"
		return "", nil
	}
	return detached[idx].ID, nil
}
