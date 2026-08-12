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

package template

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verda-cli/pkg/tui"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/serverless"
	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/vm"
	tpl "github.com/verda-cloud/verda-cli/internal/verda-cli/template"
)

var resourceTypes = []string{"Instance (VM)", "Serverless container"}
var resourceMap = map[int]string{0: "vm", 1: "container"}

// NewCmdCreate creates the template create command.
func NewCmdCreate(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new resource template interactively",
		Long: cmdutil.LongDesc(`
			Create a reusable resource configuration template by running
			the interactive wizard. Pick the resource type first:
			"Instance (VM)" collects instance type, image, location, SSH keys,
			storage and other VM settings; "Serverless container" collects
			compute, image, scaling and the rest of the container create
			parameters (no deployment name — that is chosen at deploy time).

			Templates are saved as YAML files under ~/.verda/templates/<resource>/.
			Names are auto-reformatted: "My GPU Setup" becomes "my-gpu-setup".

			After saving, use "verda vm create --from <name>" or
			"verda container create --from <name>" to create resources with
			pre-filled settings.

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

			# Then use it to create resources
			verda vm create --from gpu-training
			verda vm create --from gpu-training --hostname my-vm
			verda container create --from llm-api --name my-endpoint
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
	idx, err := prompter.Select(ctx, "Resource type", resourceTypes, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil // user cancellation (Ctrl+C/Esc) is not an error
		}
		return err
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
				if cmdutil.IsPromptCancel(err) {
					return nil // user cancellation (Ctrl+C/Esc) is not an error
				}
				return err
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

	// 5. Run resource wizard, then convert the result to a Template.
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
		tmpl = vmResultToTemplate(result)
	case "container":
		result, err := serverless.RunContainerTemplateWizard(ctx, f, ioStreams)
		if err != nil {
			return err
		}
		if result == nil {
			return nil // user canceled wizard
		}
		tmpl = containerResultToTemplate(result)
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

// containerResultToTemplate mirrors vmResultToTemplate: wizard result → YAML
// shape. Env pairs ("K=V") become maps; durations become Go duration strings.
// MinReplicas is always materialized as a pointer because the wizard asked for
// it explicitly — 0 (scale-to-zero) must survive the save.
func containerResultToTemplate(r *serverless.ContainerTemplateResult) *Template {
	minReplicas := r.MinReplicas
	return &Template{
		Resource: "container",
		Container: &tpl.ContainerSpec{
			Spot:            r.Spot,
			Compute:         r.Compute,
			ComputeSize:     r.ComputeSize,
			Image:           r.Image,
			RegistryCreds:   r.RegistryCreds,
			Port:            r.Port,
			HealthcheckOff:  r.HealthcheckOff,
			HealthcheckPort: r.HealthcheckPort,
			HealthcheckPath: r.HealthcheckPath,
			Env:             pairsToMap(r.Env),
			EnvSecret:       pairsToMap(r.EnvSecret),
			Entrypoint:      r.Entrypoint,
			Cmd:             r.Cmd,
			MinReplicas:     &minReplicas,
			MaxReplicas:     r.MaxReplicas,
			Concurrency:     r.Concurrency,
			QueuePreset:     r.QueuePreset,
			QueueLoad:       r.QueueLoad,
			CPUUtil:         r.CPUUtil,
			GPUUtil:         r.GPUUtil,
			ScaleUpDelay:    durationString(r.ScaleUpDelay),
			ScaleDownDelay:  durationString(r.ScaleDownDelay),
			RequestTTL:      durationString(r.RequestTTL),
			SecretMounts:    r.SecretMounts,
		},
		Description: r.Description,
	}
}

// pairsToMap splits "K=V" entries; entries without "=" keep the key with an
// empty value so nothing is silently dropped.
func pairsToMap(pairs []string) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, _ := strings.Cut(p, "=")
		m[k] = v
	}
	return m
}

// durationString renders d as a Go duration string; 0 means "unset" for the
// fields that carry it.
func durationString(d time.Duration) string {
	if d == 0 {
		return ""
	}
	return d.String()
}
