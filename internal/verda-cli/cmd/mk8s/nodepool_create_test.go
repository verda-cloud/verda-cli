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

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

var poolCreateArgs = []string{
	"node-pool", "create", "--cluster", "prod", "--name", "workers", "--instance-type", "1V100.6V",
	"--ssh-key", "key-1", "--ssh-key", "key-2",
}

func withPoolArgs(extra ...string) []string { return append(slices.Clone(poolCreateArgs), extra...) }

func TestNodePoolCreate_FlagsSendOneRequest(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		extra    []string
		replicas float64
		reads    []string
	}{
		"defaults":                     {replicas: 1, reads: []string{"/mk8sclusters"}},
		"an explicit 0, -l skips GETs": {extra: []string{"--replicas", "0", "-l", "FIN-03"}, replicas: 0},
	} {
		api := poolWizardAPI()
		out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), withPoolArgs(tc.extra...)...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r := decodeJSON[PoolResult](t, out); r.Status != "accepted" || r.Pool.Name != "workers" || r.Pool.State != "creating" {
			t.Errorf("%s: result = %+v", name, r)
		}
		if posts := api.requestsMatching("POST"); len(posts) != 1 || posts[0].Path != "/mk8sclusters/FIN-03/prod/pools" {
			t.Fatalf("%s: POST requests = %v", name, posts)
		}
		body := postBodyAny(t, api)
		if keys, _ := body["ssh_key_ids"].([]any); body["name"] != "workers" || body["instance_type"] != "1V100.6V" || body["replicas"] != tc.replicas || !slices.Equal(keys, []any{"key-1", "key-2"}) {
			t.Errorf("%s: body = %v", name, body)
		}
		if got := api.paths("GET"); !slices.Equal(got, tc.reads) {
			t.Errorf("%s: reads = %v, want %v", name, got, tc.reads)
		}
	}
}

func TestNodePoolCreate_TableSaysAccepted(t *testing.T) {
	t.Parallel()
	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, poolWizardAPI()), nil), poolCreateArgs...)
	if err != nil || !strings.Contains(out, "Node pool workers accepted (cluster prod, FIN-03)") || strings.Contains(out, "completed") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if !strings.Contains(errOut, "verda mk8s node-pool describe workers --cluster prod -l FIN-03") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestNodePoolCreate_MissingFlagsOffTerminal(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, poolWizardAPI())), "node-pool", "create")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster", "--name", "--instance-type", "--ssh-key"}) {
		t.Errorf("missing = %v", missing)
	}
}

func TestNodePoolCreate_InvalidFlagsSendNothing(t *testing.T) {
	t.Parallel()
	for _, extra := range [][]string{{"--replicas", "-1"}, {"--wait", "--wait-timeout", "0s"}} {
		api := poolWizardAPI()
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withPoolArgs(extra...)...); !isUsageError(err) || len(api.requests) != 0 {
			t.Errorf("%v: err = %v, %d request(s)", extra, err, len(api.requests))
		}
	}
}

// A read at the count but not yet active must not end the wait.
func TestNodePoolCreate_WaitCompletes(t *testing.T) {
	fastPoll(t)
	api := poolWizardAPI()
	reads := 0
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
		reads++
		p.State, p.ReadyReplicas = verda.ManagedKubernetesNodePoolStateCreating, p.Replicas
		if reads > 1 {
			p.State = verda.ManagedKubernetesNodePoolStateActive
		}
	}
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withPoolArgs("--replicas", "3", "--wait")...)
	if err != nil || !strings.Contains(out, "Node pool workers completed (cluster prod, FIN-03): 3/3 workers ready") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if reads != 2 {
		t.Errorf("reads = %d, want 2", reads)
	}
}

func TestNodePoolCreate_WaitTimeout(t *testing.T) {
	fastPoll(t)
	api := poolWizardAPI()
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
		p.State, p.ReadyReplicas = verda.ManagedKubernetesNodePoolStateDegraded, 1
	}
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), withPoolArgs("--replicas", "3", "--wait", "--wait-timeout", "300ms")...)
	if err == nil {
		t.Fatal("a timeout must fail")
	}
	for _, want := range []string{"keep billing", "verda mk8s node-pool delete workers --cluster prod -l FIN-03"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error misses %q: %v", want, err)
		}
	}
	if r := decodeJSON[PoolResult](t, out); r.Status != "accepted" || r.WaitError == "" || r.Pool.ReadyReplicas != 1 {
		t.Errorf("result = %+v", r)
	}
}

func TestNodePoolCreate_InteractiveSummaryAndConfirm(t *testing.T) {
	withTerminal(t)
	for _, tc := range []struct {
		p     *tuitest.Prompter
		posts int
	}{{confirmYes(), 1}, {confirmNo(), 0}} {
		api := poolWizardAPI()
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tc.p), poolCreateArgs...)
		if err != nil || len(api.requestsMatching("POST")) != tc.posts {
			t.Errorf("want %d POST(s): err=%v requests=%v", tc.posts, err, api.requestsMatching("POST"))
		}
		for _, want := range []string{"Create node pool workers", "prod (FIN-03)", "1V100.6V", "key-1, key-2"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("summary misses %q: %s", want, errOut)
			}
		}
	}
}
