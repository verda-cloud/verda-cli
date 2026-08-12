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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/template"
)

// applyContainerTemplateFrom loads the --from template, applies it to opts,
// and prints a summary. A bare --from opens a picker (agent mode: error).
// Precedence is exactly: explicit flag > template value > built-in default.
func applyContainerTemplateFrom(ctx context.Context, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *containerCreateOptions, ref string, changed func(string) bool) error {
	ref = strings.TrimSpace(ref)
	if ref == "" && f.AgentMode() {
		return errors.New("--from requires a template name in agent mode")
	}

	baseDir, err := cmdutil.TemplatesBaseDir()
	if err != nil {
		return err
	}

	tmpl, err := loadContainerTemplateRef(ctx, f, baseDir, ref)
	if err != nil {
		return err
	}
	if tmpl == nil {
		return nil // user canceled picker
	}

	if err := applyContainerTemplate(tmpl, opts, changed); err != nil {
		return fmt.Errorf("template %q: %w", ref, err)
	}
	printContainerTemplateSummary(ioStreams, tmpl)
	return nil
}

// loadContainerTemplateRef loads a container template by name/path, or shows a
// picker when ref is empty. Returns nil if the picker is canceled.
func loadContainerTemplateRef(ctx context.Context, f cmdutil.Factory, baseDir, ref string) (*template.Template, error) {
	if ref == "" {
		return pickContainerTemplate(ctx, f, baseDir)
	}
	path, err := template.Resolve(baseDir, "container", ref)
	if err != nil {
		return nil, err
	}
	return template.LoadFromPath(path)
}

func pickContainerTemplate(ctx context.Context, f cmdutil.Factory, baseDir string) (*template.Template, error) {
	entries, err := template.List(baseDir, "container")
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no container templates found in %s — create one with \"verda template create\"", filepath.Join(baseDir, "container"))
	}

	labels := make([]string, len(entries))
	for i, e := range entries {
		labels[i] = fmt.Sprintf("%-20s  %s", e.Name, e.Description)
	}

	idx, err := f.Prompter().Select(ctx, "Select a template", labels, tui.WithShowHints(true))
	if err != nil {
		if cmdutil.IsPromptCancel(err) {
			return nil, nil
		}
		return nil, err
	}
	return template.LoadFromPath(entries[idx].Path)
}

// applyContainerTemplate pre-fills opts from a container template. Any flag
// the user passed explicitly (changed) owns its field — the template only
// fills what the user did not type.
func applyContainerTemplate(tmpl *template.Template, opts *containerCreateOptions, changed func(string) bool) error {
	spec := tmpl.Container // Validate guarantees non-nil for resource=container
	applyContainerBasics(spec, opts, changed)
	applyContainerEnvAndSlices(spec, opts, changed)
	applyContainerScaling(spec, opts, changed)
	return applyContainerDurations(spec, opts, changed)
}

func applyContainerBasics(spec *template.ContainerSpec, opts *containerCreateOptions, changed func(string) bool) {
	if spec.Spot && !changed("spot") {
		opts.Spot = true
	}
	if spec.Compute != "" && !changed("compute") {
		opts.Compute = spec.Compute
	}
	if spec.ComputeSize > 0 && !changed("compute-size") {
		opts.ComputeSize = spec.ComputeSize
	}
	if spec.Image != "" && !changed("image") {
		opts.Image = spec.Image
	}
	if spec.RegistryCreds != "" && !changed("registry-creds") {
		opts.RegistryCreds = spec.RegistryCreds
		opts.RegistryPublic = false
	}
	if spec.Port != 0 && !changed("port") {
		opts.Port = spec.Port
	}
	if spec.HealthcheckOff && !changed("healthcheck-off") {
		opts.HealthcheckOff = true
	}
	if spec.HealthcheckPort != 0 && !changed("healthcheck-port") {
		opts.HealthcheckPort = spec.HealthcheckPort
	}
	if spec.HealthcheckPath != "" && !changed("healthcheck-path") {
		opts.HealthcheckPath = spec.HealthcheckPath
	}
}

func applyContainerEnvAndSlices(spec *template.ContainerSpec, opts *containerCreateOptions, changed func(string) bool) {
	if len(spec.Env) > 0 && !changed("env") {
		opts.Env = append(opts.Env, mapToPairs(spec.Env)...)
	}
	if len(spec.EnvSecret) > 0 && !changed("env-secret") {
		opts.EnvSecret = append(opts.EnvSecret, mapToPairs(spec.EnvSecret)...)
	}
	if len(spec.Entrypoint) > 0 && !changed("entrypoint") {
		opts.Entrypoint = append([]string(nil), spec.Entrypoint...)
	}
	if len(spec.Cmd) > 0 && !changed("cmd") {
		opts.Cmd = append([]string(nil), spec.Cmd...)
	}
	if len(spec.SecretMounts) > 0 && !changed("secret-mount") {
		opts.SecretMounts = append([]string(nil), spec.SecretMounts...)
	}
}

func applyContainerScaling(spec *template.ContainerSpec, opts *containerCreateOptions, changed func(string) bool) {
	if spec.MinReplicas != nil && !changed("min-replicas") {
		opts.MinReplicas = *spec.MinReplicas
	}
	if spec.MaxReplicas > 0 && !changed("max-replicas") {
		opts.MaxReplicas = spec.MaxReplicas
	}
	if spec.Concurrency > 0 && !changed("concurrency") {
		opts.Concurrency = spec.Concurrency
	}
	if spec.QueuePreset != "" && !changed("queue-preset") && !changed("queue-load") {
		opts.QueuePreset = spec.QueuePreset
	}
	if spec.QueueLoad > 0 && !changed("queue-load") {
		opts.QueueLoad = spec.QueueLoad
	}
	if spec.CPUUtil > 0 && !changed("cpu-util") {
		opts.CPUUtil = spec.CPUUtil
	}
	if spec.GPUUtil > 0 && !changed("gpu-util") {
		opts.GPUUtil = spec.GPUUtil
	}
}

func applyContainerDurations(spec *template.ContainerSpec, opts *containerCreateOptions, changed func(string) bool) error {
	dur, err := parseTemplateDuration("scale_up_delay", spec.ScaleUpDelay, changed("scale-up-delay"))
	if err != nil {
		return err
	}
	if dur != nil {
		opts.ScaleUpDelay = *dur
	}
	dur, err = parseTemplateDuration("scale_down_delay", spec.ScaleDownDelay, changed("scale-down-delay"))
	if err != nil {
		return err
	}
	if dur != nil {
		opts.ScaleDownDelay = *dur
	}
	dur, err = parseTemplateDuration("request_ttl", spec.RequestTTL, changed("request-ttl"))
	if err != nil {
		return err
	}
	if dur != nil {
		opts.RequestTTL = *dur
	}
	return nil
}

// parseTemplateDuration parses a duration string from a template. nil result
// means "leave opts alone" (field unset in the template or owned by a flag).
func parseTemplateDuration(field, value string, flagOwned bool) (*time.Duration, error) {
	if value == "" || flagOwned {
		return nil, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s %q: %w", field, value, err)
	}
	return &d, nil
}

// mapToPairs flattens a map to "K=V" pairs, sorted for determinism.
func mapToPairs(m map[string]string) []string {
	pairs := make([]string, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return pairs
}

// printContainerTemplateSummary shows which values the template supplied.
func printContainerTemplateSummary(ioStreams cmdutil.IOStreams, tmpl *template.Template) {
	w := ioStreams.ErrOut
	spec := tmpl.Container
	_, _ = fmt.Fprintln(w, "  Using template:")
	_, _ = fmt.Fprintln(w)

	row := func(label, value string) {
		_, _ = fmt.Fprintf(w, "    %-14s %s\n", label, value)
	}
	if spec.Compute != "" {
		row("Compute:", spec.Compute+" x"+strconv.Itoa(spec.ComputeSize))
	}
	if spec.Spot {
		row("Billing:", "spot")
	}
	if spec.Image != "" {
		row("Image:", spec.Image)
	}
	if spec.Port != 0 {
		row("Port:", strconv.Itoa(spec.Port))
	}
	if len(spec.Env)+len(spec.EnvSecret) > 0 {
		row("Env vars:", strconv.Itoa(len(spec.Env)+len(spec.EnvSecret))+" set")
	}
	if spec.QueuePreset != "" {
		row("Queue:", spec.QueuePreset)
	}
	if tmpl.Description != "" {
		row("Description:", tmpl.Description)
	}

	_, _ = fmt.Fprintln(w)
}
