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
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
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
	out, errOut, err := runNetwork(t, tableFactory(client, nil), "subnet", "create", "--network", "pn_a", "-l", "fin-03")
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

func TestSubnetCreateDefaultIsSentOnlyWhenSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		extra []string
		want  string // "" means is_default is absent
	}{
		{name: "not set", want: ""},
		{name: "--default", extra: []string{"--default"}, want: `"is_default":true`},
		{name: "--default=false", extra: []string{"--default=false"}, want: `"is_default":false`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := seededAPI()
			client := newFakeClient(t, api)
			args := append([]string{"subnet", "create", "--network", "pn_a", "-l", "FIN-03", "--name", "web"}, tt.extra...)
			if _, _, err := runNetwork(t, agentFactory(client), args...); err != nil {
				t.Fatalf("create: %v", err)
			}
			bodies := postBodies(api)
			if len(bodies) != 1 {
				t.Fatalf("POST bodies = %q, want one", bodies)
			}
			if tt.want == "" && strings.Contains(bodies[0], "is_default") {
				t.Errorf("is_default must be absent: %s", bodies[0])
			}
			if tt.want != "" && !strings.Contains(bodies[0], tt.want) {
				t.Errorf("body = %s, want %s", bodies[0], tt.want)
			}
		})
	}
}

func TestSubnetCreateRejectsOverlapInAnyLocation(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	// 10.0.1.0/24 is the US-SOUTHWEST-1 subnet; a FIN-03 subnet must not overlap it either.
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "pn_a", "-l", "FIN-03", "--cidr", "10.0.0.0/16")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" || !strings.Contains(err.Error(), "overlaps subnet") {
		t.Fatalf("err = %v, want an overlap VALIDATION_ERROR", err)
	}
	if len(postBodies(api)) != 0 {
		t.Error("no POST may be sent")
	}
}

func TestSubnetCreateSendsTrimmedCIDR(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	if _, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "pn_a", "-l", "FIN-03", "--cidr", " 10.0.8.0/24 "); err != nil {
		t.Fatalf("create: %v", err)
	}
	if bodies := postBodies(api); len(bodies) != 1 || !strings.Contains(bodies[0], `"cidr":"10.0.8.0/24"`) {
		t.Fatalf("POST bodies = %q, want one with the trimmed CIDR", bodies)
	}
}

func TestSubnetCreateRejectsInvalidCIDR(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "pn_a", "-l", "FIN-03", "--cidr", "10.0.9.0/30")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q, want VALIDATION_ERROR", code)
	}
	if len(postBodies(api)) != 0 {
		t.Error("no POST may be sent")
	}
}

func TestSubnetCreateRejectsDuplicateName(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "pn_a", "-l", "FIN-03", "--name", " fin-03-1 ")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" || !strings.Contains(err.Error(), errNameTaken.Error()) {
		t.Errorf("code = %q (err %v), want VALIDATION_ERROR", code, err)
	}
	if len(postBodies(api)) != 0 {
		t.Error("no POST may be sent")
	}
}

func TestSubnetCreateLocationWithoutPrivateNetworks(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "pn_a", "-l", "FIN-01")
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
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create")
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v, want MISSING_REQUIRED_FLAGS", err)
	}
	if got := fmt.Sprint(ae.Details["missing"]); got != "[--network --location]" {
		t.Errorf("missing = %s, want [--network --location]", got)
	}
	if len(api.requestsMatching("GET")) != 0 {
		t.Error("no API call expected before the flag check")
	}
}

func TestSubnetCreateUnknownNetwork(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "nope", "-l", "FIN-03")
	if code := agentErrorCode(err); code != "NOT_FOUND" {
		t.Errorf("code = %q (err %v), want NOT_FOUND", code, err)
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

func TestSubnetCreateReadsOnlyTheChosenNetwork(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	if _, _, err := runNetwork(t, agentFactory(client), "subnet", "create", "--network", "pn_a", "-l", "FIN-03"); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, r := range api.requestsMatching("GET") {
		if r.Path == "/private-networks/pn_b/subnets" {
			t.Errorf("read the subnets of an unrelated network: %s", r.Path)
		}
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

func runSubnetFlow(t *testing.T, opts *subnetCreateOptions, results ...wizard.TestResult) {
	t.Helper()
	engine := wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(results...))
	if err := engine.Run(context.Background(), buildSubnetCreateFlow(opts, subnetData(seededAPI()))); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestSubnetCreateFlowDefaults(t *testing.T) {
	t.Parallel()
	opts := &subnetCreateOptions{}
	runSubnetFlow(t, opts,
		wizard.SelectResult(1), // beta
		wizard.SelectResult(0), // FIN-03
		wizard.TextResult(""),  // name: default
		wizard.TextResult(""),  // CIDR: suggestion
		// The default step is skipped: first subnet in the location.
	)
	if opts.Network != "pn_b" || opts.Location != "FIN-03" || opts.Name != "fin-03-1" || opts.CIDR != "10.0.0.0/24" || opts.Default || opts.defaultSet {
		t.Errorf("opts = %+v", opts)
	}
}

func TestSubnetCreateFlowEditedCIDRAndDefault(t *testing.T) {
	t.Parallel()
	opts := &subnetCreateOptions{}
	runSubnetFlow(t, opts,
		wizard.SelectResult(0), // alpha (has FIN-03 already)
		wizard.SelectResult(0), // FIN-03
		wizard.TextResult("web"),
		wizard.TextResult("10.0.0.0/24"), // overlaps fin-03-1: asked again
		wizard.TextResult("172.16.4.0/24"),
		wizard.ConfirmResult(true),
	)
	if opts.Network != "pn_a" || opts.Name != "web" || opts.CIDR != "172.16.4.0/24" || !opts.Default || !opts.defaultSet {
		t.Errorf("opts = %+v", opts)
	}
}

func TestSubnetCreateFlowSkipsFlagStepsAndRecordsAnsweredFalse(t *testing.T) {
	t.Parallel()
	opts := &subnetCreateOptions{Network: "pn_a", Location: "US-SOUTHWEST-1", Name: "edge"}
	runSubnetFlow(t, opts,
		wizard.TextResult(""),
		wizard.ConfirmResult(false),
	)
	if opts.Name != "edge" || opts.CIDR != "10.0.2.0/24" || opts.Default || !opts.defaultSet {
		t.Errorf("opts = %+v", opts)
	}
}
