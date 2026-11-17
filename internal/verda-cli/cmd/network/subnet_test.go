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

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
)

func TestSubnetListTableAllNetworks(t *testing.T) {
	t.Parallel()
	api := emptyNetworkAPI()
	client := newFakeClient(t, api)
	out, _, err := runNetwork(t, tableFactory(client, nil), "subnet", "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"NETWORK", "IPS USED/TOTAL", "alpha", "beta", "fin-03-1", "us-1", "db", "10.1.0.0/24", "1/251", "gw-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestSubnetListFilters(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, emptyNetworkAPI())
	out, _, err := runNetwork(t, agentFactory(client), "subnets", "ls", "--network", "alpha", "-l", "fin-03")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(rows) != 1 || rows[0]["id"] != "sn_a1" || rows[0]["network_id"] != "pn_a" {
		t.Errorf("rows = %v", rows)
	}
}

func TestSubnetListEmpty(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, newFakeAPI())
	out, _, err := runNetwork(t, agentFactory(client), "subnet", "list")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

func TestSubnetDescribeByIDWithoutNetwork(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, tableFactory(client, nil), "subnet", "describe", "sn_a1")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	for _, want := range []string{"fin-03-1", "alpha (pn_a)", "FIN-03", "Route:      gw-1 (i-gw)", "Instances (2)", "worker", "1.2.3.4"} {
		if !strings.Contains(out, want) {
			t.Errorf("describe lacks %q:\n%s", want, out)
		}
	}
}

func TestSubnetDescribeAgentJSON(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, agentFactory(client), "subnet", "show", "us-1", "--network", "alpha")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if v["id"] != "sn_a2" || v["network_name"] != "alpha" || v["location"] != "US-SOUTHWEST-1" {
		t.Errorf("view = %v", v)
	}
}

func TestSubnetDescribeAmbiguousName(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	api.addSubnet("pn_b", "sn_b9", "fin-03-1", "FIN-03", "10.9.0.0/24", true)
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "describe", "fin-03-1")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" || !strings.Contains(err.Error(), "--network") {
		t.Errorf("err = %v, want VALIDATION_ERROR naming --network", err)
	}
}

func TestSubnetDescribeMissingArg(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "describe")
	if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code = %q, want MISSING_REQUIRED_FLAGS", code)
	}
}

func TestSubnetDescribePicker(t *testing.T) {
	withTerminal(t)
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddSelect(1)), "subnet", "describe")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !strings.Contains(out, "sn_a2") {
		t.Errorf("picker choice 1 should be sn_a2:\n%s", out)
	}
}

// An empty subnet prints "instances": [] so agents can tell it from an unknown list (absent).
func TestSubnetDescribeInstancesEmptyVersusUnknown(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, agentFactory(client), "subnet", "describe", "sn_a2")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if !strings.Contains(out, `"instances": []`) {
		t.Errorf("empty subnet must carry \"instances\": []:\n%s", out)
	}

	api := seededAPI()
	api.failOn("GET /private-networks/pn_a/subnets/sn_a2/workloads", 500, "internal_error", "boom")
	client = newFakeClient(t, api)
	out, errOut, err := runNetwork(t, agentFactory(client), "subnet", "describe", "sn_a2")
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	if strings.Contains(out, `"instances"`) || !strings.Contains(errOut, "Warning") {
		t.Errorf("unknown instance list must be absent with a warning: out=%s err=%q", out, errOut)
	}
}
