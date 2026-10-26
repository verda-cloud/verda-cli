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
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderInfernoSummary(t *testing.T) {
	opts := &infernoDeployOptions{
		Name:           "flux-schnell",
		Image:          "vccr.io/proj/flux-worker:v1.0",
		Compute:        "RTX PRO 6000",
		ComputeSize:    2,
		Spot:           true,
		RegistryCreds:  "my-ghcr",
		Env:            []string{"HF_HOME=/data/.hf"},
		EnvSecret:      []string{"TOKEN=secret"},
		MinReplicas:    0,
		MaxReplicas:    4,
		Concurrency:    2,
		QueuePreset:    presetBalanced,
		ScaleUpDelay:   10 * time.Second,
		ScaleDownDelay: 300 * time.Second,
		RequestTTL:     120 * time.Second,
		SecretMounts:   []string{"api-key:/etc/creds/token"},
	}

	var buf bytes.Buffer
	renderInfernoSummary(&buf, opts)
	out := buf.String()

	checks := []string{
		"flux-schnell",
		"vccr.io/proj/flux-worker:v1.0",
		"Inferno inference",
		"spot",
		"RTX PRO 6000",
		"x2",
		"my-ghcr",
		"5001",
		"/health",
		"0..4",
		"2 requests/replica",
		"balanced",
		"/data",
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q\ngot:\n%s", want, out)
		}
	}

	// Env vars: 1 plain + 1 secret = 2.
	if !strings.Contains(out, "2") {
		t.Errorf("summary should show env var count 2\ngot:\n%s", out)
	}
}

func TestRenderInfernoSummary_OnDemand(t *testing.T) {
	opts := &infernoDeployOptions{
		Name:           "my-model",
		Image:          "vccr.io/proj/model:v1",
		Compute:        "RTX PRO 6000",
		ComputeSize:    1,
		MinReplicas:    0,
		MaxReplicas:    1,
		Concurrency:    1,
		QueuePreset:    presetInstant,
		ScaleDownDelay: defaultInfernoScaleDownDelay,
		RequestTTL:     defaultInfernoRequestTTL,
	}

	var buf bytes.Buffer
	renderInfernoSummary(&buf, opts)
	out := buf.String()

	if !strings.Contains(out, "on-demand") {
		t.Errorf("expected on-demand billing, got:\n%s", out)
	}
	if strings.Contains(out, "spot") {
		t.Errorf("should not contain spot for on-demand:\n%s", out)
	}
	// Public registry → "public" should appear.
	if !strings.Contains(out, "public") {
		t.Errorf("expected public registry label, got:\n%s", out)
	}
}

func TestRenderInfernoSummary_CustomQueueLoad(t *testing.T) {
	opts := &infernoDeployOptions{
		Name:           "my-model",
		Image:          "vccr.io/proj/model:v1",
		Compute:        "RTX PRO 6000",
		ComputeSize:    1,
		MinReplicas:    0,
		MaxReplicas:    1,
		Concurrency:    1,
		QueuePreset:    presetCustom,
		QueueLoad:      42,
		ScaleDownDelay: defaultInfernoScaleDownDelay,
		RequestTTL:     defaultInfernoRequestTTL,
	}

	var buf bytes.Buffer
	renderInfernoSummary(&buf, opts)
	out := buf.String()

	if !strings.Contains(out, "custom (42)") {
		t.Errorf("expected custom queue-load display, got:\n%s", out)
	}
}
