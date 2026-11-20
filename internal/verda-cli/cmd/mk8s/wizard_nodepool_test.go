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
	"slices"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestNodePoolCreateWizard_EndsInTheSameRequestAsFlags(t *testing.T) {
	withTerminal(t)

	flagsAPI := poolWizardAPI()
	if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, flagsAPI)), withPoolArgs("--replicas", "2")...); err != nil {
		t.Fatal(err)
	}

	wizAPI := poolWizardAPI()
	withWizardAnswers(t,
		wizard.SelectResult(0),                // cluster: prod
		wizard.TextResult("workers"),          // name
		wizard.SelectResult(1),                // instance type: 1V100.6V, after the CPU type
		wizard.MultiSelectResult([]int{0, 1}), // key-1, key-2
		wizard.TextResult("2"),                // replicas
	)
	f := tableFactory(newFakeClient(t, wizAPI), nil)
	f.PrompterOverride = confirmYes()
	if _, _, err := runMK8s(t, f, "node-pool", "create"); err != nil {
		t.Fatal(err)
	}

	want, got := postBodyAny(t, flagsAPI), postBodyAny(t, wizAPI)
	if !equalJSON(want, got) {
		t.Errorf("wizard body = %v, flags body = %v", got, want)
	}
	if posts := wizAPI.requestsMatching("POST"); posts[0].Path != "/mk8sclusters/FIN-03/prod/pools" {
		t.Errorf("POST path = %s", posts[0].Path)
	}
}

func TestNodePoolCreateWizard_FlagsSkipTheirSteps(t *testing.T) {
	withTerminal(t)
	api := poolWizardAPI()
	// Only --cluster and --ssh-key are given: name, type and replicas are asked.
	withWizardAnswers(t, wizard.TextResult("workers"), wizard.SelectResult(2), wizard.TextResult(""))
	f := tableFactory(newFakeClient(t, api), nil)
	f.PrompterOverride = confirmYes()
	if _, _, err := runMK8s(t, f, "node-pool", "create", "--cluster", "prod", "--ssh-key", "key-2"); err != nil {
		t.Fatal(err)
	}
	body := postBodyAny(t, api)
	if body["instance_type"] != "2A100.80S" || body["replicas"] != float64(1) {
		t.Errorf("body = %v", body)
	}
	if keys, _ := body["ssh_key_ids"].([]any); len(keys) != 1 || keys[0] != "key-2" {
		t.Errorf("ssh_key_ids = %v", body["ssh_key_ids"])
	}
	// The cluster was found once, before the wizard; no SSH key lookup for a key given by flag.
	for _, p := range api.paths("GET") {
		if p == "/ssh-keys" {
			t.Error("SSH keys were listed although --ssh-key was given")
		}
	}
}

func TestNodePoolCreateWizard_UnknownClusterFailsBeforeAnyPrompt(t *testing.T) {
	withTerminal(t)
	api := poolWizardAPI()
	// No wizard answers queued: a prompt would hang or fail.
	withWizardAnswers(t)
	f := tableFactory(newFakeClient(t, api), nil)
	f.PrompterOverride = confirmYes()
	_, _, err := runMK8s(t, f, "node-pool", "create", "--cluster", "ghost")
	if agentErrorCode(err) != "NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
}

func TestNodePoolCreateWizard_LocationLimitsTheClusters(t *testing.T) {
	withTerminal(t)
	api := poolWizardAPI()
	// -l FIN-01 leaves one cluster, staging; its location offers 1A100.22V.
	withWizardAnswers(t, wizard.SelectResult(0), wizard.TextResult("w"), wizard.SelectResult(0), wizard.MultiSelectResult([]int{0}), wizard.TextResult(""))
	f := tableFactory(newFakeClient(t, api), nil)
	f.PrompterOverride = confirmYes()
	if _, _, err := runMK8s(t, f, "node-pool", "create", "-l", "FIN-01"); err != nil {
		t.Fatal(err)
	}
	if got := api.requestsMatching("POST")[0].Path; got != "/mk8sclusters/FIN-01/staging/pools" {
		t.Errorf("POST path = %s", got)
	}
	if body := postBodyAny(t, api); body["instance_type"] != "1A100.22V" {
		t.Errorf("body = %v", body)
	}
}

func TestNodePoolCreateWizard_InstanceTypesAreSmallestFirstAndHaveNoPrice(t *testing.T) {
	t.Parallel()
	api := poolWizardAPI()
	opts := &nodePoolCreateOptions{}
	opts.Location = "FIN-03"
	choices, err := instanceTypeLoader(opts, newWizardCache(tableFactory(newFakeClient(t, api), nil), newFakeClient(t, api)))(t.Context(), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	labels := make([]string, 0, len(choices))
	for _, c := range choices {
		labels = append(labels, c.Label)
		if c.Description != "instance types in FIN-03" {
			t.Errorf("description = %q", c.Description)
		}
		if s := strings.ToLower(c.Label + c.Description + c.Value); strings.Contains(s, "$") || strings.Contains(s, "price") || strings.Contains(s, "/h") {
			t.Errorf("a price must not be shown: %+v", c)
		}
	}
	if want := []string{"CPU.4V.16G", "1V100.6V", "2A100.80S"}; !slices.Equal(labels, want) {
		t.Errorf("labels = %v, want %v (only FIN-03, smallest first)", labels, want)
	}
}

func TestNodePoolCreateWizard_StopsOnEmptyPickers(t *testing.T) {
	withTerminal(t)
	for name, tc := range map[string]struct {
		api     func() *fakeAPI
		answers []wizard.TestResult
		want    string
	}{
		"no cluster": {
			api:  func() *fakeAPI { return newFakeAPI() },
			want: "No cluster found. Create one with `verda mk8s cluster create`.",
		},
		"no instance types": {
			api: func() *fakeAPI {
				api := poolWizardAPI()
				api.availability = map[string][]string{}
				return api
			},
			answers: []wizard.TestResult{wizard.SelectResult(0), wizard.TextResult("w")},
			want:    "No instance types are listed for FIN-03.",
		},
		"no ssh key": {
			api: func() *fakeAPI {
				api := poolWizardAPI()
				api.sshKeys = nil
				return api
			},
			answers: []wizard.TestResult{wizard.SelectResult(0), wizard.TextResult("w"), wizard.SelectResult(0)},
			want:    "No SSH key found. Add one with `verda ssh-key add`.",
		},
	} {
		api := tc.api()
		withWizardAnswers(t, tc.answers...)
		f := tableFactory(newFakeClient(t, api), nil)
		f.PrompterOverride = confirmYes()
		_, _, err := runMK8s(t, f, "node-pool", "create")
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want exactly %q", name, err, tc.want)
		}
		if n := len(api.requestsMatching("POST")); n != 0 {
			t.Errorf("%s: %d POST(s) before the stop", name, n)
		}
	}
}

func TestNodePoolCreateWizard_NotUsedOffTerminal(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, poolWizardAPI()), nil), "node-pool", "create", "--cluster", "prod")
	if ae := cmdutil.ClassifyError(err); ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateReplicas(t *testing.T) {
	t.Parallel()
	for in, ok := range map[string]bool{"0": true, "1": true, " 5 ": true, "": false, "-1": false, "two": false, "1.5": false} {
		if err := validateReplicas(in); (err == nil) != ok {
			t.Errorf("validateReplicas(%q) = %v, ok=%v", in, err, ok)
		}
	}
}

func equalJSON(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !deepEqualAny(v, b[k]) {
			return false
		}
	}
	return true
}

func deepEqualAny(a, b any) bool {
	as, aok := a.([]any)
	bs, bok := b.([]any)
	if aok && bok {
		return slices.EqualFunc(as, bs, deepEqualAny)
	}
	return a == b
}

func TestNodePoolCreateWizard_BackRestoresTheLocationFilter(t *testing.T) {
	t.Parallel()
	api := poolWizardAPI()
	client := newFakeClient(t, api)
	cache := newWizardCache(tableFactory(client, nil), client)

	t.Run("no -l: going Back offers every cluster again", func(t *testing.T) {
		opts := &nodePoolCreateOptions{}
		flow := buildNodePoolCreateFlow(opts, cache)
		step := flow.Steps[0]
		if _, err := step.Loader(t.Context(), nil, nil, nil); err != nil { // the engine loads the choices first
			t.Fatal(err)
		}
		step.Setter("staging") // FIN-01
		if opts.Location != "FIN-01" {
			t.Fatalf("location = %q after picking staging", opts.Location)
		}
		step.Resetter()
		if opts.Cluster != "" || opts.Location != "" {
			t.Errorf("after Back: cluster=%q location=%q, want both cleared", opts.Cluster, opts.Location)
		}
		choices, err := step.Loader(t.Context(), nil, nil, nil)
		if err != nil || len(choices) != 2 {
			t.Errorf("choices = %v err=%v, want both clusters", choices, err)
		}
	})

	t.Run("with -l: Back keeps the flag", func(t *testing.T) {
		opts := &nodePoolCreateOptions{}
		opts.Location = "FIN-03"
		flow := buildNodePoolCreateFlow(opts, cache)
		step := flow.Steps[0]
		step.Setter("prod")
		step.Resetter()
		if opts.Location != "FIN-03" {
			t.Errorf("location = %q, want the -l value kept", opts.Location)
		}
		if choices, _ := step.Loader(t.Context(), nil, nil, nil); len(choices) != 1 || choices[0].Value != "prod" {
			t.Errorf("choices = %v, want prod only", choices)
		}
	})
}

func TestInstanceTypeRank(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]int{
		"CPU-TURIN.4V.16G": 0, "CPU.4V.16G": 0, "1V100.6V": 1, "8B200.240V.CC.AMD": 8, "16H100.1": 16, "x": 0,
	} {
		if got := instanceTypeRank(in); got != want {
			t.Errorf("instanceTypeRank(%q) = %d, want %d", in, got, want)
		}
	}
}
