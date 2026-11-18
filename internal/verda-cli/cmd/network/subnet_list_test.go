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
)

func TestSubnetListTableAllNetworks(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	api.addSubnet("pn_b", "sn_b1", "db", "FIN-03", "10.1.0.0/24", true)
	client := newFakeClient(t, api)
	out, _, err := runNetwork(t, tableFactory(client, nil), "subnet", "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"NETWORK", "IPS USED/TOTAL", "ROUTE", "alpha", "beta", "fin-03-1", "us-1", "db", "10.1.0.0/24", "1/251", "gw-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

func TestSubnetListFilters(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, agentFactory(client), "subnets", "ls", "--network", "pn_a", "-l", "fin-03")
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
		t.Errorf("agent: out = %q, err = %v", out, err)
	}
	out, errOut, err := runNetwork(t, tableFactory(client, nil), "subnet", "list")
	if err != nil || !strings.Contains(out, "No subnets found.") || !strings.Contains(errOut, "Create one with: verda network subnet create") {
		t.Errorf("table: out = %q, stderr = %q, err = %v", out, errOut, err)
	}
}
