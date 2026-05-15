// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package serverless

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// deploymentNameRE: lowercase DNS-label subset (max 63) enforced by the API for URL slugs.
var deploymentNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// validateDeploymentName returns an error if the given name is not a valid
// deployment name. Empty is rejected; names > 63 chars are rejected.
func validateDeploymentName(name string) error {
	switch {
	case name == "":
		return errors.New("deployment name is required")
	case len(name) > 63:
		return fmt.Errorf("deployment name %q is longer than 63 characters", name)
	case !deploymentNameRE.MatchString(name):
		return fmt.Errorf("deployment name %q must be lowercase alphanumerics and hyphens, starting and ending with an alphanumeric", name)
	}
	return nil
}

// rejectLatestTag fails fast on :latest before the API returns a generic 400.
func rejectLatestTag(image string) error {
	if verda.IsLatestTag(image) {
		return fmt.Errorf("container image %q must use a specific tag, not ':latest'", image)
	}
	return nil
}

// envVarNameRE matches a conventional POSIX environment-variable name:
// uppercase letters, digits, and underscores, not leading with a digit.
var envVarNameRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// parseEnvFlag parses a KEY=VALUE env-var flag entry into a ContainerEnvVar.
// envType is "plain" for literal values or "secret" for secret-name references.
func parseEnvFlag(entry, envType string) (verda.ContainerEnvVar, error) {
	eq := strings.IndexByte(entry, '=')
	if eq < 1 {
		return verda.ContainerEnvVar{}, fmt.Errorf("invalid env entry %q: expected KEY=VALUE", entry)
	}
	name := entry[:eq]
	value := entry[eq+1:]
	if !envVarNameRE.MatchString(name) {
		return verda.ContainerEnvVar{}, fmt.Errorf("invalid env name %q: use uppercase letters, digits, and underscores, not leading with a digit", name)
	}
	return verda.ContainerEnvVar{
		Type:                     envType,
		Name:                     name,
		ValueOrReferenceToSecret: value,
	}, nil
}

// parseSecretMountFlag parses a SECRET:PATH flag entry into a ContainerVolumeMount.
func parseSecretMountFlag(entry string) (verda.ContainerVolumeMount, error) {
	colon := strings.IndexByte(entry, ':')
	if colon < 1 || colon == len(entry)-1 {
		return verda.ContainerVolumeMount{}, fmt.Errorf("invalid secret mount %q: expected SECRET:MOUNT_PATH", entry)
	}
	secretName := entry[:colon]
	mountPath := entry[colon+1:]
	if !strings.HasPrefix(mountPath, "/") {
		return verda.ContainerVolumeMount{}, fmt.Errorf("invalid secret mount %q: mount path must be absolute", entry)
	}
	return verda.ContainerVolumeMount{
		Type:       mountTypeSecret,
		MountPath:  mountPath,
		SecretName: secretName,
	}, nil
}

// Mount type constants match the server-side enum. "scratch" is the
// auto-allocated `/data` general-storage volume; the server sizes it.
// `shared` (named volume) is not exposed by the CLI yet — it requires a
// volume_id we have no API to pass.
const (
	mountTypeSecret  = "secret"
	mountTypeScratch = "scratch"
)

// Environment-variable type constants.
const (
	envTypePlain  = "plain"
	envTypeSecret = "secret"
)

// isPromptCancel reports whether err represents a clean prompter exit rather
// than a real failure. Ctrl+C surfaces as tui.ErrInterrupted, Esc as
// context.Canceled. Anything else (I/O errors, terminal disconnects, real
// context deadlines) should propagate so the failure isn't invisible.
func isPromptCancel(err error) bool {
	return errors.Is(err, tui.ErrInterrupted) || errors.Is(err, context.Canceled)
}

// confirmDestructive renders a red-bold warning line and prompts the user to
// confirm. Returns (true, nil) to proceed, (false, nil) on a clean prompter
// cancel (Ctrl+C / Esc), or (false, err) on a real prompter failure that
// callers must surface. In agent mode, callers must bypass this helper and
// enforce --yes themselves.
func confirmDestructive(ctx context.Context, ioStreams cmdutil.IOStreams, prompter tui.Prompter, heading, detail, prompt string) (bool, error) {
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s %s\n", warn.Render("⚠"), warn.Render(heading))
	if detail != "" {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n", detail)
	}
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n\n", warn.Render("This action cannot be undone."))
	confirmed, err := prompter.Confirm(ctx, prompt)
	if err != nil {
		if isPromptCancel(err) {
			return false, nil
		}
		return false, err
	}
	return confirmed, nil
}

// statusColor returns a lipgloss style that highlights a deployment status.
// Green: healthy/running; yellow: transitional; red: errored; dim: stopped.
func statusColor(status string) lipgloss.Style {
	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "running"), strings.Contains(s, "active"), strings.Contains(s, "healthy"):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true) // green
	case strings.Contains(s, "error"), strings.Contains(s, "failed"):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true) // red
	case strings.Contains(s, "paused"), strings.Contains(s, "stopped"), strings.Contains(s, "offline"):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // dim
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow for transitional
	}
}
