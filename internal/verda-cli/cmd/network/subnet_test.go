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
	"context"
	"encoding/json"
	"strings"
	"testing"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
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

func TestSubnetDeleteAgentRequiresYes(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "delete", "sn_a2")
	if code := agentErrorCode(err); code != "CONFIRMATION_REQUIRED" {
		t.Errorf("code = %q", code)
	}
	if len(api.requests) != 0 {
		t.Errorf("no API call expected before --yes, got %v", api.requests)
	}
}

func TestSubnetDeleteConfirmed(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	out, errOut, err := runNetwork(t, tableFactory(client, tuitest.New().AddConfirm(true)), "subnet", "delete", "sn_a2", "--network", "pn_a")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, want := range []string{"Delete subnet us-1 (sn_a2, US-SOUTHWEST-1, 10.0.1.0/24) of network pn_a", "cannot be undone"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("warning lacks %q:\n%s", want, errOut)
		}
	}
	if d := api.requestsMatching("DELETE"); len(d) != 1 || d[0].Path != "/private-networks/pn_a/subnets/sn_a2" {
		t.Errorf("DELETE = %v", d)
	}
	if !strings.Contains(out, "Deleted subnet us-1 (sn_a2)") {
		t.Errorf("out = %q", out)
	}
}

func TestSubnetDeleteAgentYesWritesResult(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	out, _, err := runNetwork(t, agentFactory(client), "subnet", "rm", "sn_a1", "--network", "pn_a", "--yes")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	if got["resource"] != "subnet" || got["id"] != "sn_a1" || got["status"] != "completed" {
		t.Errorf("result = %v", got)
	}
	if d := api.requestsMatching("DELETE"); len(d) != 1 || d[0].Path != "/private-networks/pn_a/subnets/sn_a1" {
		t.Errorf("DELETE = %v", d)
	}
}

func TestSubnetDeleteMissingArgOffTerminal(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	for _, args := range [][]string{{"subnet", "delete"}, {"subnet", "delete", "sn_a1"}} {
		_, _, err := runNetwork(t, tableFactory(client, nil), args...)
		if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" {
			t.Errorf("%v: code = %q (err %v), want MISSING_REQUIRED_FLAGS", args, code, err)
		}
	}
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "delete", "sn_a1", "--yes")
	if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("agent: code = %q (err %v), want MISSING_REQUIRED_FLAGS", code, err)
	}
	if len(api.requests) != 0 {
		t.Errorf("no API call expected without --network, got %v", api.requests)
	}
}

func TestUnknownIDIsNotFound(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	for _, args := range [][]string{
		{"subnet", "delete", "sn_nope", "--network", "pn_a", "--yes"},
		{"subnet", "list", "--network", "alpha"},
	} {
		_, _, err := runNetwork(t, agentFactory(client), args...)
		if code := agentErrorCode(err); code != "NOT_FOUND" {
			t.Errorf("%v: code = %q (err %v), want NOT_FOUND", args, code, err)
		}
	}
}

func TestSubnetDeletePicker(t *testing.T) {
	withTerminal(t)
	api := seededAPI()
	client := newFakeClient(t, api)
	out, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddSelect(1).AddConfirm(true)), "subnet", "delete")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(out, "Deleted subnet us-1 (sn_a2)") {
		t.Errorf("picker choice 1 should delete sn_a2: %q", out)
	}
}

func TestSubnetDeletePickerCancel(t *testing.T) {
	withTerminal(t)
	for name, p := range map[string]*cancelPrompter{
		"cancel row": {Prompter: tuitest.New().AddSelect(2)},
		"esc":        {Prompter: tuitest.New(), selectErr: context.Canceled},
	} {
		api := seededAPI()
		client := newFakeClient(t, api)
		f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable, PrompterOverride: p}
		out, errOut, err := runNetwork(t, f, "subnet", "delete")
		if err != nil || out != "" || !strings.Contains(errOut, canceledMsg) || deletes(api) != 0 {
			t.Errorf("%s: want %q on stderr only; out=%q stderr=%q err=%v deletes=%d", name, canceledMsg, out, errOut, err, deletes(api))
		}
	}
}
