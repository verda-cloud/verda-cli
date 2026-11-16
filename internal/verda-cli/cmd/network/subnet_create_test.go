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
	"io"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
)

func postBodies(api *fakeAPI) []string {
	posts := api.requestsMatching("POST")
	out := make([]string, 0, len(posts))
	for _, r := range posts {
		out = append(out, r.Body)
	}
	return out
}

func TestSubnetCreateSuggestsCIDRAndName(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	out, errOut, err := runNetwork(t, tableFactory(client, nil), "subnet", "create", "--network", "alpha", "-l", "fin-03")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(errOut, "Using CIDR 10.0.2.0/24") {
		t.Errorf("the picked CIDR must be printed: %q", errOut)
	}
	bodies := postBodies(api)
	want := `{"location_code":"FIN-03","name":"fin-03-2","cidr":"10.0.2.0/24"}`
	if len(bodies) != 1 || bodies[0] != want {
		t.Errorf("POST = %q, want %s", bodies, want)
	}
	if !strings.Contains(out, "default: no") {
		t.Errorf("out = %q", out)
	}
}

func TestSubnetCreateFirstInLocationReportsDefault(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	out, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "beta", "-l", "FIN-03", "--cidr", "10.0.0.0/24")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if body := postBodies(api)[0]; strings.Contains(body, "is_default") {
		t.Errorf("is_default must be absent unless --default: %s", body)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil || v["is_default"] != true || v["network_id"] != "pn_b" {
		t.Errorf("view = %v (err %v)", v, err)
	}
}

func TestSubnetCreateDefaultFlagIsSent(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "alpha", "-l", "FIN-03", "--name", "web", "--default")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if body := postBodies(api)[0]; !strings.Contains(body, `"is_default":true`) || !strings.Contains(body, `"name":"web"`) {
		t.Errorf("body = %s", body)
	}
}

func TestSubnetCreateRejectsOverlapInAnyLocation(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	// 10.0.1.0/24 is the US-SOUTHWEST-1 subnet; creating in FIN-03 must still be refused.
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "alpha", "-l", "FIN-03", "--cidr", "10.0.0.0/16")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" || !strings.Contains(err.Error(), "overlaps subnet") {
		t.Fatalf("err = %v, want an overlap VALIDATION_ERROR", err)
	}
	if len(postBodies(api)) != 0 {
		t.Error("no POST may be sent")
	}
}

func TestSubnetCreateRejectsInvalidCIDR(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "alpha", "-l", "FIN-03", "--cidr", "10.0.9.0/30")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q, want VALIDATION_ERROR", code)
	}
}

func TestSubnetCreateLocationWithoutPrivateNetworks(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "alpha", "-l", "FIN-01")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q, want VALIDATION_ERROR", code)
	}
	for _, want := range []string{"FIN-01", "FIN-03", "US-SOUTHWEST-1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

func TestSubnetCreateMissingFlags(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "alpha")
	if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" || !strings.Contains(err.Error(), "MISSING") {
		t.Errorf("err = %v", err)
	}
}

func TestSubnetCreateNoNetworks(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, newFakeAPI())
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "x", "-l", "FIN-03")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" || !strings.Contains(err.Error(), "verda network create") {
		t.Errorf("err = %v", err)
	}
}

func subnetData(api *fakeAPI) *subnetCreateData {
	d := &subnetCreateData{networks: api.networks, subnets: api.subnets}
	for _, l := range api.locations {
		if l.IsPrivateNetworksEnabled {
			d.locations = append(d.locations, l)
		}
	}
	return d
}

func TestSubnetCreateFlowDefaults(t *testing.T) {
	t.Parallel()
	opts := &subnetCreateOptions{}
	engine := wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(
		wizard.SelectResult(1), // beta
		wizard.SelectResult(0), // FIN-03
		wizard.TextResult(""),  // name: default
		wizard.TextResult(""),  // CIDR: suggestion
		// default step is skipped: first subnet in the location
	))
	if err := engine.Run(context.Background(), buildSubnetCreateFlow(opts, subnetData(seededAPI()))); err != nil {
		t.Fatalf("run: %v", err)
	}
	if opts.Network != "pn_b" || opts.Location != "FIN-03" || opts.Name != "fin-03-1" || opts.CIDR != "10.0.0.0/24" || opts.Default {
		t.Errorf("opts = %+v", opts)
	}
}

func TestSubnetCreateFlowEditedCIDRAndDefault(t *testing.T) {
	t.Parallel()
	opts := &subnetCreateOptions{}
	engine := wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(
		wizard.SelectResult(0), // alpha (has FIN-03 already)
		wizard.SelectResult(0), // FIN-03
		wizard.TextResult("web"),
		wizard.TextResult("10.0.0.0/24"), // overlaps fin-03-1: re-prompted
		wizard.TextResult("172.16.4.0/24"),
		wizard.ConfirmResult(true),
	))
	if err := engine.Run(context.Background(), buildSubnetCreateFlow(opts, subnetData(seededAPI()))); err != nil {
		t.Fatalf("run: %v", err)
	}
	if opts.Network != "pn_a" || opts.Name != "web" || opts.CIDR != "172.16.4.0/24" || !opts.Default {
		t.Errorf("opts = %+v", opts)
	}
}

func TestSubnetCreateFlowSkipsFlagSteps(t *testing.T) {
	t.Parallel()
	opts := &subnetCreateOptions{Network: "pn_a", Location: "US-SOUTHWEST-1", Name: "edge"}
	engine := wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(
		wizard.TextResult(""),
		wizard.ConfirmResult(false),
	))
	if err := engine.Run(context.Background(), buildSubnetCreateFlow(opts, subnetData(seededAPI()))); err != nil {
		t.Fatalf("run: %v", err)
	}
	if opts.Name != "edge" || opts.CIDR != "10.0.2.0/24" {
		t.Errorf("opts = %+v", opts)
	}
}

func TestSubnetCreateRejectsDuplicateNameAndDefaultFalse(t *testing.T) {
	t.Parallel()
	for _, extra := range [][]string{{"--name", "fin-03-1"}, {"--default=false"}} {
		api := seededAPI()
		client := newFakeClient(t, api)
		args := append([]string{"subnet", "create", "--network", "alpha", "-l", "FIN-03"}, extra...)
		_, _, err := runNetwork(t, agentFactory(client), args...)
		if code := agentErrorCode(err); code != "VALIDATION_ERROR" {
			t.Errorf("%v: code = %q (err %v), want VALIDATION_ERROR", extra, code, err)
		}
		if len(postBodies(api)) != 0 {
			t.Errorf("%v: no POST expected", extra)
		}
	}
}
