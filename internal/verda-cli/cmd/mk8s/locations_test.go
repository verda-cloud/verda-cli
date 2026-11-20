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

package mk8s

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestLocations_Table(t *testing.T) {
	api := newFakeAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "locations")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"LOCATION", "FIN-01", "FIN-03"} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
	if n := len(api.requestsMatching("GET")); n != 1 {
		t.Errorf("locations made %d GET calls, want 1", n)
	}
	if n := len(api.requestsMatching("POST")); n != 0 {
		t.Errorf("locations must not write: %d POST calls", n)
	}
}

func TestLocations_JSONAndAgent(t *testing.T) {
	for name, newFactory := range map[string]func(*verda.Client) *cmdutil.TestFactory{
		"json":  jsonFactory,
		"agent": agentFactory,
	} {
		t.Run(name, func(t *testing.T) {
			out, _, err := runMK8s(t, newFactory(newFakeClient(t, newFakeAPI())), "locations")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("output is not a JSON string array: %v\n%s", err, out)
			}
			if len(got) != 2 || got[0] != "FIN-01" {
				t.Errorf("locations = %v", got)
			}
		})
	}
}

func TestLocations_EnabledButEmpty(t *testing.T) {
	api := newFakeAPI()
	api.locations = nil

	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "locations")
	if err != nil {
		t.Fatalf("an empty list is not an error: %v", err)
	}
	if !strings.Contains(out, "no location is configured") {
		t.Errorf("table output = %q", out)
	}

	out, _, err = runMK8s(t, jsonFactory(newFakeClient(t, api)), "locations")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("JSON output = %q, want []", out)
	}
}

func TestLocations_FeatureNotEnabled(t *testing.T) {
	api := newFakeAPI()
	api.disabled = true

	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "locations")
	if err == nil {
		t.Fatal("want an error")
	}
	ae := cmdutil.ClassifyError(err)
	if ae.Code != "AUTH_ERROR" || ae.ExitCode != cmdutil.ExitAuth {
		t.Errorf("classified as %s exit %d, want AUTH_ERROR exit %d", ae.Code, ae.ExitCode, cmdutil.ExitAuth)
	}
	if !strings.Contains(ae.Message, "Managed Kubernetes is not enabled for this project") {
		t.Errorf("message = %q", ae.Message)
	}
	if cmdutil.IsAgentError(err) {
		t.Error("must stay a plain error: main prints AgentErrors as JSON even for humans")
	}
}

func TestLocations_Other403StaysPlain(t *testing.T) {
	api := newFakeAPI()
	api.failures["GET /mk8sclusters/enabled_locations"] = apiFailure{Status: 403, Body: `{"code":"forbidden","message":"no access","details":"some_other_reason"}`}

	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), "locations")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "AUTH_ERROR" {
		t.Fatalf("classified = %+v", ae)
	}
	if strings.Contains(ae.Message, "not enabled") {
		t.Errorf("only feature_not_enabled gets the not-enabled message: %q", ae.Message)
	}
}

func TestLocations_Debug(t *testing.T) {
	api := newFakeAPI()
	f := tableFactory(newFakeClient(t, api), nil)
	f.DebugOverride = true
	_, errOut, err := runMK8s(t, f, "locations")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "FIN-03") {
		t.Errorf("debug output misses the API response: %q", errOut)
	}
}

func TestMK8s_HelpStartsWithCaution(t *testing.T) {
	f := tableFactory(nil, nil)
	cmd := NewCmdMK8s(f, cmdutil.IOStreams{})
	if !strings.HasPrefix(cmd.Long, "Experimental: the managed Kubernetes API may change") {
		t.Errorf("Long = %q", cmd.Long)
	}
	if cmd.Annotations[cmdutil.TagAnnotation] != "experimental" {
		t.Errorf("tag = %q", cmd.Annotations[cmdutil.TagAnnotation])
	}
	for _, alias := range []string{"k8s", "kubernetes"} {
		found := false
		for _, a := range cmd.Aliases {
			found = found || a == alias
		}
		if !found {
			t.Errorf("missing alias %q", alias)
		}
	}
	for _, sub := range cmd.Commands() {
		if !strings.HasPrefix(sub.Long, "Experimental:") {
			t.Errorf("%s: Long must start with the caution", sub.Name())
		}
	}
}

func TestInteractiveTTY(t *testing.T) {
	// Not parallel: withTerminal swaps a package variable.
	client := newFakeClient(t, newFakeAPI())
	if interactiveTTY(tableFactory(client, nil)) {
		t.Error("no terminal: must not be interactive")
	}
	withTerminal(t)
	if !interactiveTTY(tableFactory(client, nil)) {
		t.Error("terminal + table output: want interactive")
	}
	if interactiveTTY(agentFactory(client)) {
		t.Error("agent mode must never be interactive")
	}
	if interactiveTTY(jsonFactory(client)) {
		t.Error("-o json must never be interactive")
	}
}
