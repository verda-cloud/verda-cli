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
)

func seededAPI() *fakeAPI {
	api := newFakeAPI()
	api.addNetwork("pn_a", "alpha", true)
	api.addNetwork("pn_b", "beta", false)
	api.addSubnet("pn_a", "sn_a1", "fin-03-1", "FIN-03", "10.0.0.0/24", true)
	api.addSubnet("pn_a", "sn_a2", "us-1", "US-SOUTHWEST-1", "10.0.1.0/24", true)
	api.subnets["pn_a"][0].DefaultRouteInstance = &verda.AttachedInstance{ID: "i-gw", Hostname: "gw-1"}
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
