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

// routeAPI: sn_a1 has a live route via gw-1, a route via a deleted instance, and one with no instance.
func routeAPI() *fakeAPI {
	api := seededAPI()
	pub := "1.2.3.4"
	api.addRoute("sn_a1", "rt_live", "0.0.0.0/0", &verda.AttachedInstance{ID: "i-gw", Hostname: "gw-1", PublicIP: &pub})
	api.addRoute("sn_a1", "rt_gone", "192.168.50.0/24", &verda.AttachedInstance{ID: "i-old", Hostname: "old-gw"})
	api.addRoute("sn_a1", "rt_null", "192.168.60.0/24", nil)
	return api
}

func TestRouteListTable(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, routeAPI())
	out, _, err := runNetwork(t, tableFactory(client, nil), "route", "list", "--subnet", "sn_a1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	lines := strings.Split(out, "\n")
	find := func(id string) string {
		for _, l := range lines {
			if strings.HasPrefix(l, id) {
				return l
			}
		}
		return ""
	}
	if l := find("rt_live"); !strings.Contains(l, "gw-1") || !strings.Contains(l, "1.2.3.4") {
		t.Errorf("live route row = %q", l)
	}
	for _, id := range []string{"rt_gone", "rt_null"} {
		if l := find(id); !strings.Contains(l, deletedMark) {
			t.Errorf("%s row should show %s: %q", id, deletedMark, l)
		}
	}
}

func TestRouteListAgentJSON(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, routeAPI())
	out, _, err := runNetwork(t, agentFactory(client), "route", "ls", "--subnet", "fin-03-1", "--network", "alpha")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 3 {
		t.Fatalf("rows = %v (err %v)\n%s", rows, err, out)
	}
	want := map[string]bool{"rt_live": false, "rt_gone": true, "rt_null": true}
	for _, r := range rows {
		if r["instance_deleted"] != want[r["id"].(string)] {
			t.Errorf("%v: instance_deleted = %v", r["id"], r["instance_deleted"])
		}
	}
}

func TestRouteListMissingSubnet(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, routeAPI())
	_, _, err := runNetwork(t, agentFactory(client), "route", "list")
	if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code = %q", code)
	}
}

func TestRouteListEmpty(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	out, _, err := runNetwork(t, tableFactory(client, nil), "route", "list", "--subnet", "sn_a2")
	if err != nil || !strings.Contains(out, "No routes in subnet us-1.") {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

func TestRouteDeleteAgentRequiresYes(t *testing.T) {
	t.Parallel()
	api := routeAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "route", "delete", "rt_live", "--subnet", "sn_a1")
	if code := agentErrorCode(err); code != "CONFIRMATION_REQUIRED" {
		t.Errorf("code = %q", code)
	}
	if len(api.requests) != 0 {
		t.Errorf("no API call expected: %v", api.requests)
	}
}

func TestRouteDeleteConfirmed(t *testing.T) {
	t.Parallel()
	api := routeAPI()
	client := newFakeClient(t, api)
	out, errOut, err := runNetwork(t, tableFactory(client, tuitest.New().AddConfirm(true)), "route", "delete", "rt_live", "--subnet", "sn_a1")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(errOut, "Instances in fin-03-1 lose internet access through gw-1.") {
		t.Errorf("warning = %q", errOut)
	}
	if d := api.requestsMatching("DELETE"); len(d) != 1 || d[0].Path != "/private-networks/pn_a/subnets/sn_a1/routes/rt_live" {
		t.Errorf("DELETE = %v", d)
	}
	if !strings.Contains(out, "Deleted route") {
		t.Errorf("out = %q", out)
	}
}

func TestRouteDeleteDeclined(t *testing.T) {
	t.Parallel()
	api := routeAPI()
	client := newFakeClient(t, api)
	_, errOut, err := runNetwork(t, tableFactory(client, tuitest.New().AddConfirm(false)), "route", "delete", "rt_gone", "--subnet", "sn_a1")
	if err != nil || !strings.Contains(errOut, "a deleted instance") || deletes(api) != 0 {
		t.Errorf("err = %v, deletes = %d, stderr = %q", err, deletes(api), errOut)
	}
}

func TestRouteDeleteUnknownRoute(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, routeAPI())
	_, _, err := runNetwork(t, agentFactory(client), "route", "delete", "rt_nope", "--subnet", "sn_a1", "--yes")
	if code := agentErrorCode(err); code != "NOT_FOUND" {
		t.Errorf("code = %q", code)
	}
}

func TestRouteDeleteMissingArgs(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, routeAPI())
	_, _, err := runNetwork(t, agentFactory(client), "route", "delete", "--yes")
	if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" || !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("err = %v", err)
	}
}

func TestRouteDeletePicker(t *testing.T) {
	withTerminal(t)
	api := routeAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddSelect(2).AddConfirm(true)), "route", "delete", "--subnet", "sn_a1")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if d := api.requestsMatching("DELETE"); len(d) != 1 || !strings.HasSuffix(d[0].Path, "/rt_null") {
		t.Errorf("DELETE = %v", d)
	}
}
