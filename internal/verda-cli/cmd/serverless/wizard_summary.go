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
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// renderContainerSummary prints a human-readable review card before the final
// deploy confirmation. The cost section shows a scale-to-zero range when min
// replicas is 0 — the lower bound is $0 regardless of compute choice.
func renderContainerSummary(w io.Writer, opts *containerCreateOptions) {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)

	_, _ = fmt.Fprintf(w, "\n  %s\n", header.Render("Deployment summary"))

	kv := func(k, v string) {
		_, _ = fmt.Fprintf(w, "  %-22s %s\n", label.Render(k), v)
	}

	kv("Name", opts.Name)
	kv("Image", opts.Image)
	billing := "on-demand"
	if opts.Spot {
		billing = "spot"
	}
	kv("Billing", billing)
	kv("Compute", fmt.Sprintf("%s x%d", opts.Compute, opts.ComputeSize))
	if opts.RegistryCreds != "" {
		kv("Registry creds", opts.RegistryCreds)
	} else {
		kv("Registry creds", dim.Render("public"))
	}
	kv("Port", strconv.Itoa(opts.Port))
	if opts.HealthcheckOff {
		kv("Healthcheck", dim.Render("disabled"))
	} else {
		port := opts.HealthcheckPort
		if port == 0 {
			port = opts.Port
		}
		kv("Healthcheck", fmt.Sprintf("%s on :%d", opts.HealthcheckPath, port))
	}
	if n := len(opts.Env) + len(opts.EnvSecret); n > 0 {
		kv("Env vars", strconv.Itoa(n))
	}
	kv("Replicas", fmt.Sprintf("%d..%d", opts.MinReplicas, opts.MaxReplicas))
	kv("Concurrency", fmt.Sprintf("%d requests/replica", opts.Concurrency))

	preset := strings.ToLower(opts.QueuePreset)
	if opts.QueueLoad > 0 {
		preset = fmt.Sprintf("custom (%d)", opts.QueueLoad)
	}
	kv("Queue-load preset", preset)

	if opts.CPUUtil > 0 {
		kv("CPU util trigger", fmt.Sprintf("%d%%", opts.CPUUtil))
	}
	if opts.GPUUtil > 0 {
		kv("GPU util trigger", fmt.Sprintf("%d%%", opts.GPUUtil))
	}
	kv("Scale delays", fmt.Sprintf("up=%s  down=%s", opts.ScaleUpDelay, opts.ScaleDownDelay))
	kv("Request TTL", opts.RequestTTL.String())

	if len(opts.SecretMounts) > 0 {
		kv("Secret mounts", strconv.Itoa(len(opts.SecretMounts)))
	}
	kv("General storage", fmt.Sprintf("%s  %d GiB (fixed)", defaultGeneralStoragePath, opts.GeneralStorageSize))
	kv("Shared memory", fmt.Sprintf("%s  %d MiB", defaultSHMPath, opts.SHMSize))

	_, _ = fmt.Fprintln(w)
}

// renderBatchjobSummary is the batchjob counterpart to renderContainerSummary.
// Smaller review card — no spot, no scaling triggers, no concurrency, no
// healthcheck — but calls out the deadline prominently since it's the one
// batchjob-only field.
func renderBatchjobSummary(w io.Writer, opts *batchjobCreateOptions) {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)

	_, _ = fmt.Fprintf(w, "\n  %s\n", header.Render("Batch-job summary"))

	kv := func(k, v string) {
		_, _ = fmt.Fprintf(w, "  %-22s %s\n", label.Render(k), v)
	}

	kv("Name", opts.Name)
	kv("Image", opts.Image)
	kv("Compute", fmt.Sprintf("%s x%d", opts.Compute, opts.ComputeSize))
	if opts.RegistryCreds != "" {
		kv("Registry creds", opts.RegistryCreds)
	} else {
		kv("Registry creds", dim.Render("public"))
	}
	kv("Port", strconv.Itoa(opts.Port))
	if n := len(opts.Env) + len(opts.EnvSecret); n > 0 {
		kv("Env vars", strconv.Itoa(n))
	}
	kv("Max replicas", strconv.Itoa(opts.MaxReplicas))
	kv("Deadline", opts.Deadline.String())
	kv("Request TTL", opts.RequestTTL.String())
	if len(opts.SecretMounts) > 0 {
		kv("Secret mounts", strconv.Itoa(len(opts.SecretMounts)))
	}
	kv("General storage", fmt.Sprintf("%s  %d GiB (fixed)", defaultGeneralStoragePath, opts.GeneralStorageSize))
	kv("Shared memory", fmt.Sprintf("%s  %d MiB", defaultSHMPath, opts.SHMSize))
	_, _ = fmt.Fprintln(w)
}
