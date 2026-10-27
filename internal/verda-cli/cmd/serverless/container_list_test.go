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
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"empty", "", 10, ""},
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"longer", "hello world", 8, "hello..."},
		{"min truncation", "abcdef", 4, "a..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.s, tc.n)
			if got != tc.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
		})
	}
}

func TestStatusOrDash(t *testing.T) {
	if got := statusOrDash(""); got != containerStatusUnknown {
		t.Fatalf("statusOrDash(\"\") = %q, want %q", got, containerStatusUnknown)
	}
	if got := statusOrDash("running"); got != "running" {
		t.Fatalf("statusOrDash(\"running\") = %q, want \"running\"", got)
	}
}

func TestFormatContainerCompute(t *testing.T) {
	cases := []struct {
		name   string
		deploy verda.ContainerDeployment
		want   string
	}{
		{"nil compute", verda.ContainerDeployment{}, "-"},
		{"with compute", verda.ContainerDeployment{
			Compute: &verda.ContainerCompute{Name: "RTX4500Ada", Size: 2},
		}, "2x RTX4500Ada"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatContainerCompute(&tc.deploy)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatContainerBilling(t *testing.T) {
	spot := verda.ContainerDeployment{IsSpot: true}
	if got := formatContainerBilling(&spot); got != computeTypeSpot {
		t.Fatalf("spot: got %q, want %q", got, computeTypeSpot)
	}
	onDemand := verda.ContainerDeployment{IsSpot: false}
	if got := formatContainerBilling(&onDemand); got != computeTypeOnDemand {
		t.Fatalf("on-demand: got %q, want %q", got, computeTypeOnDemand)
	}
}

func TestFormatContainerRow(t *testing.T) {
	d := &verda.ContainerDeployment{
		Name:      "my-api",
		IsSpot:    false,
		Compute:   &verda.ContainerCompute{Name: "RTX4500Ada", Size: 1},
		CreatedAt: time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
	}
	got := formatContainerRow(d, "running")
	if !strings.Contains(got, "my-api") {
		t.Errorf("expected row to contain deployment name, got %q", got)
	}
	if !strings.Contains(got, "running") {
		t.Errorf("expected row to contain status, got %q", got)
	}
	if !strings.Contains(got, "1x RTX4500Ada") {
		t.Errorf("expected row to contain compute, got %q", got)
	}
	if !strings.Contains(got, "on-demand") {
		t.Errorf("expected row to contain billing type, got %q", got)
	}
	if !strings.Contains(got, "2026-01-15") {
		t.Errorf("expected row to contain date, got %q", got)
	}
}

func TestPrintContainerTable(t *testing.T) {
	deployments := []verda.ContainerDeployment{
		{
			Name:            "my-api",
			EndpointBaseURL: "https://my-api.run.verda.cloud",
			IsSpot:          false,
			Compute:         &verda.ContainerCompute{Name: "RTX4500Ada", Size: 1},
			CreatedAt:       time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			Name:            "worker",
			EndpointBaseURL: "https://worker.run.verda.cloud",
			IsSpot:          true,
			Compute:         &verda.ContainerCompute{Name: "CPUNode", Size: 4},
			CreatedAt:       time.Date(2026, 2, 20, 8, 0, 0, 0, time.UTC),
		},
	}

	cache := newContainerStatusCache(time.Hour)
	cache.set("my-api", "running")
	cache.set("worker", "error")

	var buf bytes.Buffer
	if err := printContainerTable(&buf, deployments, cache); err != nil {
		t.Fatalf("printContainerTable: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "NAME") {
		t.Errorf("expected header NAME, got:\n%s", out)
	}
	if !strings.Contains(out, "STATUS") {
		t.Errorf("expected header STATUS, got:\n%s", out)
	}
	if !strings.Contains(out, "my-api") {
		t.Errorf("expected my-api in output, got:\n%s", out)
	}
	if !strings.Contains(out, "worker") {
		t.Errorf("expected worker in output, got:\n%s", out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("expected running status, got:\n%s", out)
	}
	if !strings.Contains(out, "error") {
		t.Errorf("expected error status, got:\n%s", out)
	}
}
