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
	"slices"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
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
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, wizAPI), confirmYes()), "node-pool", "create"); err != nil {
		t.Fatal(err)
	}

	if want, got := postBodyAny(t, flagsAPI), postBodyAny(t, wizAPI); !reflect.DeepEqual(want, got) {
		t.Errorf("wizard body = %v, flags body = %v", got, want)
	}
	if posts := wizAPI.requestsMatching("POST"); posts[0].Path != "/mk8sclusters/FIN-03/prod/pools" {
		t.Errorf("POST path = %s", posts[0].Path)
	}
}

// Only --cluster and --ssh-key are given: name, type and replicas are asked,
// and the SSH keys are not listed.
func TestNodePoolCreateWizard_FlagsSkipTheirSteps(t *testing.T) {
	withTerminal(t)
	api := poolWizardAPI()
	withWizardAnswers(t, wizard.TextResult("workers"), wizard.SelectResult(2), wizard.TextResult(""))
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), confirmYes()), "node-pool", "create", "--cluster", "prod", "--ssh-key", "key-2"); err != nil {
		t.Fatal(err)
	}
	body := postBodyAny(t, api)
	if keys, _ := body["ssh_key_ids"].([]any); body["instance_type"] != "2A100.80S" || body["replicas"] != float64(1) || len(keys) != 1 || keys[0] != "key-2" {
		t.Errorf("body = %v", body)
	}
	if slices.Contains(api.paths("GET"), "/ssh-keys") {
		t.Error("SSH keys were listed although --ssh-key was given")
	}
}

// The cluster step sets the location from the picked cluster; Back clears it,
// but keeps a location given with -l.
func TestNodePoolCreateWizard_BackRestoresTheLocationFilter(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, poolWizardAPI())
	cache := newWizardCache(tableFactory(client, nil), client)
	for flag, choices := range map[string]int{"": 2, "FIN-03": 1} {
		opts := &nodePoolCreateOptions{}
		opts.Location = flag
		step := buildNodePoolCreateFlow(opts, cache).Steps[0]
		if _, err := step.Loader(t.Context(), nil, nil, nil); err != nil { // the engine loads the choices first
			t.Fatal(err)
		}
		step.Setter("prod")
		if opts.Location != "FIN-03" {
			t.Errorf("-l %q: location = %q after picking prod", flag, opts.Location)
		}
		step.Resetter()
		if got, err := step.Loader(t.Context(), nil, nil, nil); opts.Cluster != "" || opts.Location != flag || err != nil || len(got) != choices {
			t.Errorf("-l %q: after Back cluster=%q location=%q choices=%v err=%v", flag, opts.Cluster, opts.Location, got, err)
		}
	}
}

func TestNodePoolCreateWizard_StopsOnEmptyPickers(t *testing.T) {
	withTerminal(t)
	api := newFakeAPI()
	withWizardAnswers(t)
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), confirmYes()), "node-pool", "create")
	if err == nil || !strings.Contains(err.Error(), "No cluster found") || len(api.requestsMatching("POST")) != 0 {
		t.Errorf("err = %v, POSTs = %v", err, api.requestsMatching("POST"))
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
