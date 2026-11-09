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
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

func TestContainerList_StatusFilterIsExact(t *testing.T) {
	t.Parallel()
	mock, client := newContainerMock(t)
	mock.SeedContainerDeployment("sick-api")
	mock.SetContainerStatus("sick-api", "unhealthy")

	for status, want := range map[string][]string{
		"healthy":   {testDeployment},
		"UNHEALTHY": {"sick-api"},
		"health":    nil,
	} {
		stdout, _, err := runContainer(t, agentFactory(client), "list", "--status", status)
		if err != nil {
			t.Fatalf("list --status %s: %v", status, err)
		}
		var rows []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
		}
		var got []string
		for _, r := range rows {
			got = append(got, r.Name)
		}
		if !slices.Equal(got, want) {
			t.Errorf("--status %s: got %v, want %v", status, got, want)
		}
	}
}

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

func TestContainerList_TableOutput(t *testing.T) {
	t.Parallel()
	mock, client := newContainerMock(t)
	mock.SetContainerStatus(testDeployment, "healthy")
	f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable}

	stdout, _, err := runContainer(t, f, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"NAME", "STATUS", "COMPUTE", testDeployment, "healthy"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table output lacks %q:\n%s", want, stdout)
		}
	}
}

func TestContainerList_Empty(t *testing.T) {
	t.Parallel()
	mock := mockapi.New()
	t.Cleanup(mock.Close)
	client, err := verda.NewClient(
		verda.WithBaseURL(mock.URL()),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("verda.NewClient: %v", err)
	}
	f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable}

	stdout, _, err := runContainer(t, f, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout, "No container deployments found.") {
		t.Errorf("expected empty message; got:\n%s", stdout)
	}
}

func TestContainerList_JSONWithStatus(t *testing.T) {
	t.Parallel()
	mock, client := newContainerMock(t)
	mock.SetContainerStatus(testDeployment, "running")
	f := agentFactory(client)

	stdout, _, err := runContainer(t, f, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Name != testDeployment {
		t.Errorf("name = %q, want %q", rows[0].Name, testDeployment)
	}
	if rows[0].Status != "running" {
		t.Errorf("status = %q, want running", rows[0].Status)
	}
}

func TestContainerList_StatusFilterNoMatch(t *testing.T) {
	t.Parallel()
	_, client := newContainerMock(t)
	f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable}

	stdout, _, err := runContainer(t, f, "list", "--status", "nonexistent")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout, "No container deployments found.") {
		t.Errorf("expected empty message for no matching status; got:\n%s", stdout)
	}
}
