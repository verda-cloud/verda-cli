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
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
)

// gatewayAPI adds a second eligible gateway and an ineligible one in another location.
func gatewayAPI() *fakeAPI {
	api := seededAPI()
	api.addInstance("i-gw2", "gw-2", "FIN-03", "pn_a", "sn_a1", "9.9.9.9")
	api.addInstance("i-us", "us-box", "US-SOUTHWEST-1", "pn_a", "sn_a2", "8.8.8.8")
	api.addInstance("i-other", "other-net", "FIN-03", "pn_b", "", "7.7.7.7")
	api.addInstance("i-dead", "dead-gw", "FIN-03", "pn_a", "sn_a1", "6.6.6.6")
	api.instances[len(api.instances)-1].Status = verda.StatusDiscontinued
	return api
}

func TestRouteCreateGatewayChecks(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"classic":   "not on network alpha",
		"us-box":    "it is in US-SOUTHWEST-1, the subnet is in FIN-03",
		"worker":    "no public IP",
		"other-net": "not on network alpha",
		"i-nobody":  "",
		"dead-gw":   "",
	}
	for ref, want := range tests {
		api := gatewayAPI()
		client := newFakeClient(t, api)
		_, _, err := runNetwork(t, agentFactory(client), "route", "create", "--subnet", "sn_a1", "--instance", ref)
		if err == nil {
			t.Fatalf("%s: want an error", ref)
		}
		if want == "" {
			if code := agentErrorCode(err); code != "NOT_FOUND" {
				t.Errorf("%s: code = %q, want NOT_FOUND", ref, code)
			}
		} else if code := agentErrorCode(err); code != "VALIDATION_ERROR" || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want VALIDATION_ERROR with %q", ref, err, want)
		}
		if len(postBodies(api)) != 0 {
			t.Errorf("%s: no POST expected", ref)
		}
	}
}

func TestRouteCreateNewRouteNeedsNoConfirmAndPrintsNATNote(t *testing.T) {
	t.Parallel()
	api := gatewayAPI()
	client := newFakeClient(t, api)
	// No confirm is queued: a confirm prompt would fail the command.
	out, errOut, err := runNetwork(t, tableFactory(client, tuitest.New()), "route", "create", "--subnet", "sn_a1", "--instance", "gw-2")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b := postBodies(api); len(b) != 1 || b[0] != `{"destination":"0.0.0.0/0","instance_id":"i-gw2"}` {
		t.Errorf("POST = %q", b)
	}
	if !strings.Contains(out, "goes through gw-2") {
		t.Errorf("out = %q", out)
	}
	if !strings.Contains(errOut, "enable IP forwarding and NAT (MASQUERADE)") || strings.Contains(out, "MASQUERADE") {
		t.Errorf("NAT note must be on stderr only: out=%q err=%q", out, errOut)
	}
}

func TestRouteCreateJSONHasNoNATNote(t *testing.T) {
	t.Parallel()
	api := gatewayAPI()
	client := newFakeClient(t, api)
	out, errOut, err := runNetwork(t, jsonFactory(client), "route", "create", "--subnet", "sn_a1", "--instance", "i-gw2")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if strings.Contains(errOut+out, "MASQUERADE") {
		t.Errorf("NAT note is for table mode only: out=%q err=%q", out, errOut)
	}
	if !strings.Contains(out, `"destination": "0.0.0.0/0"`) {
		t.Errorf("out = %s", out)
	}
}

func movingAPI() *fakeAPI {
	api := gatewayAPI()
	pub := "1.2.3.4"
	api.addRoute("sn_a1", "rt_live", "0.0.0.0/0", &verda.AttachedInstance{ID: "i-gw", Hostname: "gw-1", PublicIP: &pub})
	return api
}

func TestRouteCreateMoveNeedsConfirmation(t *testing.T) {
	t.Parallel()
	api := movingAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "route", "create", "--subnet", "sn_a1", "--instance", "gw-2")
	if code := agentErrorCode(err); code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("code = %q (err %v)", code, err)
	}
	if len(postBodies(api)) != 0 {
		t.Error("no POST before --yes")
	}

	_, _, err = runNetwork(t, agentFactory(client), "route", "create", "--subnet", "sn_a1", "--instance", "gw-2", "--yes")
	if err != nil || len(postBodies(api)) != 1 {
		t.Errorf("with --yes: err = %v, posts = %d", err, len(postBodies(api)))
	}
}

func TestRouteCreateMoveConfirmPrompt(t *testing.T) {
	t.Parallel()
	for _, accept := range []bool{true, false} {
		api := movingAPI()
		client := newFakeClient(t, api)
		_, errOut, err := runNetwork(t, tableFactory(client, tuitest.New().AddConfirm(accept)), "route", "create", "--subnet", "sn_a1", "--instance", "gw-2")
		if err != nil {
			t.Fatalf("accept=%v: %v", accept, err)
		}
		if !strings.Contains(errOut, "Moves the route for 0.0.0.0/0 of fin-03-1 from gw-1 to gw-2; traffic switches at once.") {
			t.Errorf("accept=%v: warning = %q", accept, errOut)
		}
		want := 0
		if accept {
			want = 1
		}
		if got := len(postBodies(api)); got != want {
			t.Errorf("accept=%v: posts = %d, want %d", accept, got, want)
		}
	}
}

func TestRouteCreateSameGatewayIsANoOp(t *testing.T) {
	t.Parallel()
	api := movingAPI()
	client := newFakeClient(t, api)
	_, errOut, err := runNetwork(t, tableFactory(client, tuitest.New()), "route", "create", "--subnet", "sn_a1", "--instance", "gw-1")
	if err != nil || !strings.Contains(errOut, "already goes through gw-1") || len(postBodies(api)) != 0 {
		t.Errorf("err = %v, stderr = %q, posts = %d", err, errOut, len(postBodies(api)))
	}
}

func TestRouteCreateRejectsBadDestination(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, gatewayAPI())
	for _, d := range []string{"::/0", "not-a-cidr", "10.0.0.1"} {
		_, _, err := runNetwork(t, agentFactory(client), "route", "create", "--subnet", "sn_a1", "--instance", "gw-2", "--destination", d)
		if code := agentErrorCode(err); code != "VALIDATION_ERROR" {
			t.Errorf("%s: code = %q", d, code)
		}
	}
}

func TestRouteCreateMissingFlags(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, gatewayAPI())
	_, _, err := runNetwork(t, agentFactory(client), "route", "create")
	if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code = %q", code)
	}
}

// The picker offers only eligible gateways: gw-1 and gw-2, in list order.
func TestRouteCreatePickerListsEligibleOnly(t *testing.T) {
	withTerminal(t)
	api := gatewayAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddSelect(1)), "route", "create", "--subnet", "sn_a1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b := postBodies(api); len(b) != 1 || !strings.Contains(b[0], "i-gw2") {
		t.Errorf("POST = %q", b)
	}

	api = gatewayAPI()
	client = newFakeClient(t, api)
	_, _, err = runNetwork(t, tableFactory(client, tuitest.New().AddSelect(2)), "route", "create", "--subnet", "sn_a1")
	if err != nil || len(postBodies(api)) != 0 {
		t.Errorf("index 2 is Cancel: err = %v, posts = %d", err, len(postBodies(api)))
	}
}

// Hostnames are not unique: two matches are refused and the IDs are listed.
func TestRouteCreateAmbiguousHostname(t *testing.T) {
	t.Parallel()
	api := gatewayAPI()
	api.addInstance("i-gw3", "gw-2", "FIN-03", "pn_a", "sn_a1", "5.5.5.5")
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "route", "create", "--subnet", "sn_a1", "--instance", "gw-2")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" || !strings.Contains(err.Error(), "i-gw2, i-gw3") {
		t.Fatalf("err = %v, want VALIDATION_ERROR listing both IDs", err)
	}
	if len(postBodies(api)) != 0 {
		t.Error("no POST expected")
	}

	_, _, err = runNetwork(t, agentFactory(client), "route", "create", "--subnet", "sn_a1", "--instance", "i-gw3")
	if err != nil {
		t.Errorf("by ID: %v", err)
	}
}
