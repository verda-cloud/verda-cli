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

//go:build integration

package integration

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// requireNetworks skips when the [test] project cannot use private networks.
func requireNetworks(t *testing.T) {
	t.Helper()
	r := runIsolated(t, "--agent", "network", "list")
	if r.ExitCode == 3 {
		t.Skipf("the [%s] profile is not working (no credentials or rejected): %s", isolatedProfile, r.Stderr)
	}
	if r.ExitCode != 0 {
		t.Skipf("private networks are not available to the [test] project (exit %d): %s", r.ExitCode, r.Stderr)
	}
}

func TestNetwork_ListJSONAndAgent(t *testing.T) {
	requireNetworks(t)
	for _, args := range [][]string{
		{"-o", "json", "network", "list"},
		{"--agent", "network", "list"},
		{"-o", "json", "network", "subnet", "list"},
		{"--agent", "network", "subnet", "list"},
	} {
		r := runIsolated(t, args...)
		if r.ExitCode != 0 {
			t.Fatalf("%v: exit %d\nstderr: %s", args, r.ExitCode, r.Stderr)
		}
		var rows []map[string]any
		parseJSON(t, r, &rows)
	}
}

func TestNetwork_DescribeMissingIsNotFound(t *testing.T) {
	requireNetworks(t)
	r := runIsolated(t, "--agent", "network", "describe", "pn_doesnotexist")
	if r.ExitCode != 5 {
		t.Fatalf("exit = %d, want 5\nstderr: %s", r.ExitCode, r.Stderr)
	}
	if env := parseAgentError(t, r); env.Error.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

// Without --yes nothing is deleted; the gate fires before any API call.
func TestNetwork_DeleteWithoutYesIsRefused(t *testing.T) {
	requireNetworks(t)
	r := runIsolated(t, "--agent", "network", "delete", "pn_doesnotexist")
	if r.ExitCode != 2 {
		t.Fatalf("exit = %d, want 2\nstderr: %s", r.ExitCode, r.Stderr)
	}
	if env := parseAgentError(t, r); env.Error.Code != "CONFIRMATION_REQUIRED" {
		t.Errorf("code = %q, want CONFIRMATION_REQUIRED", env.Error.Code)
	}
}

// TestNetwork_Lifecycle creates a network and an empty subnet, renames the
// subnet, then deletes the network while the subnet still exists: the API must
// remove the subnet with it. No instances are created, so nothing is billed.
func TestNetwork_Lifecycle(t *testing.T) {
	if os.Getenv("VERDA_NETWORK_INTEGRATION") != "1" {
		t.Skip("set VERDA_NETWORK_INTEGRATION=1 to create and delete a network")
	}
	requireNetworks(t)

	location := pickNetworkLocation(t)
	name := fmt.Sprintf("cli-it-%d", time.Now().UnixNano())

	var networkID string
	t.Cleanup(func() {
		if networkID == "" {
			return
		}
		if r := runIsolated(t, "--agent", "network", "describe", networkID); r.ExitCode == 5 {
			return
		}
		if r := runIsolated(t, "--agent", "network", "delete", networkID, "--yes"); r.ExitCode != 0 {
			t.Errorf("cleanup left network %s behind: %s", networkID, r.Stderr)
		}
	})

	r := runIsolated(t, "--agent", "network", "create", "--name", name)
	if r.ExitCode != 0 {
		t.Fatalf("network create: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
	var network struct {
		ID string `json:"id"`
	}
	parseJSON(t, r, &network)
	networkID = network.ID

	r = runIsolated(t, "--agent", "network", "subnet", "create", "--network", networkID, "-l", location)
	if r.ExitCode != 0 {
		t.Fatalf("subnet create: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}
	var subnet struct {
		ID   string `json:"id"`
		CIDR string `json:"cidr"`
	}
	parseJSON(t, r, &subnet)
	if subnet.CIDR != "10.0.0.0/24" {
		t.Errorf("first subnet CIDR = %s, want 10.0.0.0/24", subnet.CIDR)
	}

	r = runIsolated(t, "--agent", "network", "subnet", "update", subnet.ID, "--name", name+"-renamed")
	if r.ExitCode != 0 || !strings.Contains(r.Stdout, name+"-renamed") {
		t.Fatalf("subnet rename: exit %d\nstdout: %s\nstderr: %s", r.ExitCode, r.Stdout, r.Stderr)
	}

	r = runIsolated(t, "--agent", "network", "delete", networkID, "--yes")
	if r.ExitCode != 0 {
		t.Fatalf("network delete with an empty subnet: exit %d\nstderr: %s", r.ExitCode, r.Stderr)
	}

	if r := runIsolated(t, "--agent", "network", "describe", networkID); r.ExitCode != 5 {
		t.Errorf("network describe after delete: exit %d, want 5 (not found)\nstderr: %s", r.ExitCode, r.Stderr)
	}
	if r := runIsolated(t, "--agent", "network", "subnet", "describe", subnet.ID); r.ExitCode != 5 {
		t.Errorf("subnet describe after network delete: exit %d, want 5 (not found)\nstderr: %s", r.ExitCode, r.Stderr)
	}
}

// pickNetworkLocation returns the first location with private networks.
func pickNetworkLocation(t *testing.T) string {
	t.Helper()
	r := runIsolated(t, "-o", "json", "locations")
	if r.ExitCode != 0 {
		t.Fatalf("locations: %s", r.Stderr)
	}
	var locations []struct {
		Code    string `json:"code"`
		Enabled bool   `json:"is_private_networks_enabled"`
	}
	parseJSON(t, r, &locations)
	for _, l := range locations {
		if l.Enabled {
			return l.Code
		}
	}
	t.Skip("no location with private networks in the [test] project")
	return ""
}
