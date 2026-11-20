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
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func wizardAPI() *fakeAPI {
	api := newFakeAPI()
	api.addNetwork("pn-a", "alpha")
	api.addNetwork("pn-123", "beta")
	api.addSubnet("pn-123", "sn-1", "other-loc", "FIN-01", "10.0.1.0/24")
	api.addSubnet("pn-123", "sn-456", "fin-03", "FIN-03", "10.0.3.0/24")
	return api
}

// withWizardAnswers makes the wizard take canned answers instead of a terminal.
func withWizardAnswers(t *testing.T, results ...wizard.TestResult) {
	t.Helper()
	orig := newWizardEngine
	newWizardEngine = func(cmdutil.Factory, cmdutil.IOStreams) *wizard.Engine {
		return wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(results...))
	}
	t.Cleanup(func() { newWizardEngine = orig })
}

func postBody(t *testing.T, api *fakeAPI) map[string]string {
	t.Helper()
	reqs := api.requestsMatching("POST")
	if len(reqs) != 1 {
		t.Fatalf("want one POST, got %v", reqs)
	}
	return decodeJSON[map[string]string](t, reqs[0].Body)
}

func TestClusterCreateWizard_EndsInTheSameRequestAsFlags(t *testing.T) {
	withTerminal(t)

	flagsAPI := wizardAPI()
	if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, flagsAPI)), createArgs...); err != nil {
		t.Fatal(err)
	}

	wizAPI := wizardAPI()
	withWizardAnswers(t,
		wizard.TextResult("prod"),
		wizard.SelectResult(1), // FIN-03 (locations are FIN-01, FIN-03)
		wizard.TextResult("1.37.0"),
		wizard.SelectResult(1), // beta = pn-123
		wizard.SelectResult(0), // only FIN-03 subnet: sn-456
		wizard.TextResult(""),  // pod CIDR: default
		wizard.TextResult(""),  // service CIDR: default
	)
	f := tableFactory(newFakeClient(t, wizAPI), nil)
	f.PrompterOverride = confirmYes()
	if _, _, err := runMK8s(t, f, "cluster", "create"); err != nil {
		t.Fatal(err)
	}

	want, got := postBody(t, flagsAPI), postBody(t, wizAPI)
	for k, v := range want {
		if got[k] != v {
			t.Errorf("wizard body[%s] = %q, flags body has %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("wizard body = %v, flags body = %v", got, want)
	}
}

func TestClusterCreateWizard_FlagsSkipTheirSteps(t *testing.T) {
	withTerminal(t)
	api := wizardAPI()
	// The name and --service-cidr are not given: two answers. --pod-cidr and the
	// rest skip their steps.
	withWizardAnswers(t, wizard.TextResult("prod"), wizard.TextResult(""))
	f := tableFactory(newFakeClient(t, api), nil)
	f.PrompterOverride = confirmYes()
	args := []string{"cluster", "create", "-l", "FIN-03", "--kubernetes-version", "1.37.0", "--network", "pn-123", "--subnet", "sn-456", "--pod-cidr", "10.50.0.0/16"}
	if _, _, err := runMK8s(t, f, args...); err != nil {
		t.Fatal(err)
	}
	body := postBody(t, api)
	if body["name"] != "prod" || body["pod_cidr"] != "10.50.0.0/16" || body["service_cidr"] != defaultServiceCIDR {
		t.Errorf("body = %v", body)
	}
	// Flags need no lookups.
	if got := api.paths("GET"); len(got) != 0 {
		t.Errorf("unexpected reads: %v", got)
	}
}

func TestClusterCreateWizard_StopsBeforeCreateOnEmptyPickers(t *testing.T) {
	withTerminal(t)
	for name, tc := range map[string]struct {
		api     func() *fakeAPI
		answers []wizard.TestResult
		want    string // exactly as shown, with no wizard step prefix
	}{
		"no network": {
			api:     func() *fakeAPI { return newFakeAPI() },
			answers: []wizard.TestResult{wizard.TextResult("prod"), wizard.SelectResult(1), wizard.TextResult("1.37.0")},
			want:    "No private network found. Create one with `verda network create`.",
		},
		"no subnet in the location": {
			api: func() *fakeAPI {
				api := newFakeAPI()
				api.addNetwork("pn-123", "beta")
				api.addSubnet("pn-123", "sn-1", "other-loc", "FIN-01", "10.0.1.0/24")
				return api
			},
			answers: []wizard.TestResult{wizard.TextResult("prod"), wizard.SelectResult(1), wizard.TextResult("1.37.0"), wizard.SelectResult(0)},
			want:    "Network beta has no subnet in FIN-03. Create one with `verda network subnet create`.",
		},
		"no location": {
			api: func() *fakeAPI {
				api := newFakeAPI()
				api.locations = nil
				return api
			},
			answers: []wizard.TestResult{wizard.TextResult("prod")},
			want:    "Managed Kubernetes is enabled for this project, but no location is configured.",
		},
	} {
		api := tc.api()
		withWizardAnswers(t, tc.answers...)
		f := tableFactory(newFakeClient(t, api), nil)
		f.PrompterOverride = confirmYes()
		_, _, err := runMK8s(t, f, "cluster", "create")
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want exactly %q", name, err, tc.want)
		}
		if n := len(api.requestsMatching("POST")); n != 0 {
			t.Errorf("%s: %d POST(s) before the stop", name, n)
		}
	}
}

func TestClusterCreateWizard_DeclinedSummaryCreatesNothing(t *testing.T) {
	withTerminal(t)
	api := wizardAPI()
	withWizardAnswers(t, wizard.TextResult("prod"), wizard.TextResult(""), wizard.TextResult(""))
	f := tableFactory(newFakeClient(t, api), nil)
	f.PrompterOverride = confirmNo()
	args := []string{"cluster", "create", "-l", "FIN-03", "--kubernetes-version", "1.37.0", "--network", "pn-123", "--subnet", "sn-456"}
	_, errOut, err := runMK8s(t, f, args...)
	if err != nil || !strings.Contains(errOut, canceledMsg) {
		t.Fatalf("err=%v stderr=%q", err, errOut)
	}
	if n := len(api.requestsMatching("POST")); n != 0 {
		t.Errorf("%d POST(s)", n)
	}
}

func TestClusterCreateWizard_NotUsedOffTerminal(t *testing.T) {
	t.Parallel()
	// A wizard run here would hang the test: off a terminal the command fails first.
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, wizardAPI()), nil), "cluster", "create", "--name", "prod")
	if agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
}

func TestWizardCache_MemoizesLookups(t *testing.T) {
	t.Parallel()
	api := wizardAPI()
	f := tableFactory(newFakeClient(t, api), nil)
	cache := newWizardCache(f, newFakeClient(t, api))
	ctx := context.Background()
	for range 3 {
		if _, err := cache.enabledLocations(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := cache.privateNetworks(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := cache.networkSubnets(ctx, "pn-123"); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"/mk8sclusters/enabled_locations", "/private-networks", "/private-networks/pn-123/subnets"}
	got := api.paths("GET")
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("requests = %v, want one each: %v", got, want)
	}
	if cache.networkName("pn-123") != "beta" || cache.networkName("pn-x") != "pn-x" {
		t.Error("networkName falls back to the ID")
	}
}

func TestValidateClusterWizardInputs(t *testing.T) {
	t.Parallel()
	// Only input that cannot be sent is rejected; the SDK and the API own the real rules.
	for name, ok := range map[string]bool{"prod": true, "Prod": true, "a_b": true, "": false} {
		if err := validateClusterName(name); (err == nil) != ok {
			t.Errorf("validateClusterName(%q) = %v, ok=%v", name, err, ok)
		}
	}
	for cidr, ok := range map[string]bool{"10.100.0.0/16": true, "": false, "10.0.0.1": false, "x/16": false} {
		if err := validateCIDR(cidr, defaultPodCIDR); (err == nil) != ok {
			t.Errorf("validateCIDR(%q) = %v, ok=%v", cidr, err, ok)
		}
	}
	for example, want := range map[string]string{defaultPodCIDR: "for example 10.100.0.0/16", defaultServiceCIDR: "for example 10.200.0.0/16"} {
		if err := validateCIDR("x", example); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("validateCIDR error = %v, want %q", err, want)
		}
	}
	if validateVersion("  ") == nil || validateVersion("1.37.0") != nil {
		t.Error("validateVersion")
	}
}
