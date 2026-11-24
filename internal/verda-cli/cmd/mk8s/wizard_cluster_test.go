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
	"reflect"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
)

func wizardAPI() *fakeAPI {
	api := newFakeAPI()
	api.addNetwork("pn-a", "alpha")
	api.addNetwork("pn-123", "beta")
	api.addSubnet("pn-123", "sn-1", "other-loc", "FIN-01", "10.0.1.0/24")
	api.addSubnet("pn-123", "sn-456", "fin-03", "FIN-03", "10.0.3.0/24")
	return api
}

// The wizard ends in the same request as the flags; a flag skips its step and
// needs no lookup.
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
	want, got := postBodyAny(t, flagsAPI), postBodyAny(t, wizAPI)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("wizard body = %v, flags body = %v", got, want)
	}

	// The name and --service-cidr are not given: two answers.
	partialAPI := wizardAPI()
	withWizardAnswers(t, wizard.TextResult("prod"), wizard.TextResult(""))
	f = tableFactory(newFakeClient(t, partialAPI), nil)
	f.PrompterOverride = confirmYes()
	args := []string{"cluster", "create", "-l", "FIN-03", "--kubernetes-version", "1.37.0", "--network", "pn-123", "--subnet", "sn-456", "--pod-cidr", "10.50.0.0/16"}
	if _, _, err := runMK8s(t, f, args...); err != nil {
		t.Fatal(err)
	}
	body := postBodyAny(t, partialAPI)
	if body["name"] != "prod" || body["pod_cidr"] != "10.50.0.0/16" || body["service_cidr"] != defaultServiceCIDR {
		t.Errorf("body = %v", body)
	}
	if got := partialAPI.paths("GET"); len(got) != 0 {
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

func TestValidateClusterWizardInputs(t *testing.T) {
	t.Parallel()
	// The name step applies the SDK's rule; the API owns the CIDR rules beyond the form.
	for name, ok := range map[string]bool{
		"prod": true, " prod-1 ": true, "": false, "Prod": false, "a_b": false, "-a": false,
		strings.Repeat("a", 30): true, strings.Repeat("a", 31): false,
	} {
		if err := validateName(name); (err == nil) != ok {
			t.Errorf("validateName(%q) = %v, ok=%v", name, err, ok)
		}
	}
	for cidr, ok := range map[string]bool{"10.100.0.0/16": true, "": false, "10.0.0.1": false, "x/16": false} {
		if err := validateCIDR(cidr, defaultPodCIDR); (err == nil) != ok {
			t.Errorf("validateCIDR(%q) = %v, ok=%v", cidr, err, ok)
		}
	}
	if validateVersion("  ") == nil || validateVersion("1.37.0") != nil {
		t.Error("validateVersion")
	}
}

// Values the SDK rejects stop after the wizard, before the confirmation and any POST.
func TestClusterCreateWizard_ChecksTheRequestBeforeTheConfirmation(t *testing.T) {
	withTerminal(t)
	api := wizardAPI()
	withWizardAnswers(t, wizard.TextResult("prod"), wizard.TextResult(""))
	f := tableFactory(newFakeClient(t, api), nil) // no confirm answer: asking would fail differently
	args := []string{"cluster", "create", "-l", "FIN-03", "--kubernetes-version", "1.37.0", "--network", "pn-123", "--subnet", "sn-456", "--pod-cidr", "10.50.0.0/24"}
	_, _, err := runMK8s(t, f, args...)
	if !isUsageError(err) || !strings.Contains(err.Error(), "invalid cluster: pod_cidr: must be /21 or larger") || len(api.requestsMatching("POST")) != 0 {
		t.Errorf("err = %v, POSTs = %d", err, len(api.requestsMatching("POST")))
	}
}
