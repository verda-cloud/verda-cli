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

package network

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
)

func seededAPI() *fakeAPI {
	api := newFakeAPI()
	api.addNetwork("pn_a", "alpha", true)
	api.addNetwork("pn_b", "beta", false)
	api.addSubnet("pn_a", "sn_a1", "fin-03-1", "FIN-03", "10.0.0.0/24", true)
	api.addSubnet("pn_a", "sn_a2", "us-1", "US-SOUTHWEST-1", "10.0.1.0/24", true)
	api.subnets["pn_a"][0].DefaultRouteInstance = &verda.AttachedInstance{ID: "i-gw", Hostname: "gw-1"}
	api.addInstance("i-gw", "gw-1", "FIN-03", "pn_a", "sn_a1", "1.2.3.4")
	api.addInstance("i-2", "worker", "FIN-03", "pn_a", "sn_a1", "")
	api.addInstance("i-3", "classic", "FIN-01", "", "", "5.6.7.8")
	return api
}

func TestListTable(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, tableFactory(client, nil), "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"NAME", "MODE", "DEFAULT", "alpha", "pn_a", "beta", "custom", "2026-11-16 09:30"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestListAgentJSON(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, agentFactory(client), "ls")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if len(rows) != 2 || rows[0]["id"] != "pn_a" || rows[0]["is_default"] != true {
		t.Errorf("rows = %v", rows)
	}
}

func TestListEmpty(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, newFakeAPI())
	out, errOut, err := runNetwork(t, tableFactory(client, nil), "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "No private networks found.") || !strings.Contains(errOut, "verda network create") {
		t.Errorf("want empty message and hint; out=%q err=%q", out, errOut)
	}

	out, _, err = runNetwork(t, agentFactory(client), "list")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Errorf("agent empty list = %q (err %v), want []", out, err)
	}
}

func TestDescribeTable(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, tableFactory(client, nil), "describe", "alpha")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	for _, want := range []string{"pn_a", "Default:    yes", "Instances:  2", "fin-03-1", "10.0.0.0/24", "FIN-03", "gw-1", "IPS USED/TOTAL", "1/251"} {
		if !strings.Contains(out, want) {
			t.Errorf("describe lacks %q:\n%s", want, out)
		}
	}
}

func TestDescribeAgentJSON(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, agentFactory(client), "get", "pn_a")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	var v struct {
		ID            string `json:"id"`
		InstanceCount *int   `json:"instance_count"`
		Subnets       []struct {
			ID        string `json:"id"`
			NetworkID string `json:"network_id"`
		} `json:"subnets"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if v.ID != "pn_a" || v.InstanceCount == nil || *v.InstanceCount != 2 || len(v.Subnets) != 2 || v.Subnets[0].NetworkID != "pn_a" {
		t.Errorf("unexpected view: %+v", v)
	}
}

// Describe still prints the network when the instance list fails.
func TestDescribeInstanceListFailureIsAWarning(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	api.failOn("GET /private-networks/pn_a/workloads", 500, "internal_error", "boom")
	client := newFakeClient(t, api)
	out, errOut, err := runNetwork(t, jsonFactory(client), "describe", "alpha")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if strings.Contains(out, "instance_count") {
		t.Errorf("instance_count must be absent when the count failed:\n%s", out)
	}
	if !strings.Contains(errOut, "Warning") {
		t.Errorf("want a warning on stderr, got %q", errOut)
	}
}

func TestDescribeNotFound(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	_, _, err := runNetwork(t, agentFactory(client), "describe", "nope")
	if code := agentErrorCode(err); code != "NOT_FOUND" {
		t.Errorf("code = %q (err %v), want NOT_FOUND", code, err)
	}
}

func TestDescribeMissingArgOffTerminal(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	_, _, err := runNetwork(t, tableFactory(client, nil), "describe")
	if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code = %q (err %v), want MISSING_REQUIRED_FLAGS", code, err)
	}
}

func TestDescribePickerOnTerminal(t *testing.T) {
	withTerminal(t)
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddSelect(1)), "describe")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !strings.Contains(out, "pn_b") {
		t.Errorf("picker choice 1 should describe beta:\n%s", out)
	}
}

func TestDescribePickerCancel(t *testing.T) {
	withTerminal(t)
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddSelect(2)), "describe")
	if err != nil || out != "" {
		t.Errorf("Cancel should print nothing and succeed: out=%q err=%v", out, err)
	}
}
