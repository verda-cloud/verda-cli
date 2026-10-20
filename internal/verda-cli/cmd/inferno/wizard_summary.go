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

package inferno

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

func renderInfernoSummary(w io.Writer, opts *infernoDeployOptions) {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)

	_, _ = fmt.Fprintf(w, "\n  %s\n", header.Render("Inferno deployment summary"))

	kv := func(k, v string) {
		_, _ = fmt.Fprintf(w, "  %-22s %s\n", label.Render(k), v)
	}

	kv("Name", opts.Name)
	kv("Image", opts.Image)
	kv("Type", "Inferno inference")
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
	kv("Port", strconv.Itoa(infernoExposedPort))
	kv("Healthcheck", fmt.Sprintf("%s on :%d", infernoHealthcheckPath, infernoExposedPort))
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
	kv("Scale delays", fmt.Sprintf("up=%s  down=%s", opts.ScaleUpDelay, opts.ScaleDownDelay))
	kv("Request TTL", opts.RequestTTL.String())

	if len(opts.SecretMounts) > 0 {
		kv("Secret mounts", strconv.Itoa(len(opts.SecretMounts)))
	}
	kv("Storage", defaultGeneralStoragePath+"  (scratch, server-allocated)")

	_, _ = fmt.Fprintln(w)
}
