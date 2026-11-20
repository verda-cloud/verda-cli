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
	"sync/atomic"
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
	api := poolWizardAPI()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), poolCreateArgs...)
	if err != nil {
		t.Fatal(err)
	}
	result := decodeJSON[PoolResult](t, out)
	if result.Status != "accepted" || result.Pool.Name != "workers" || result.Pool.State != "creating" {
		t.Errorf("result = %+v", result)
	}

	posts := api.requestsMatching("POST")
	if len(posts) != 1 || posts[0].Path != "/mk8sclusters/FIN-03/prod/pools" {
		t.Fatalf("POST requests = %v", posts)
	}
	body := postBodyAny(t, api)
	if body["name"] != "workers" || body["instance_type"] != "1V100.6V" || body["replicas"] != float64(1) {
		t.Errorf("body = %v", body)
	}
	if keys, _ := body["ssh_key_ids"].([]any); len(keys) != 2 || keys[0] != "key-1" || keys[1] != "key-2" {
		t.Errorf("ssh_key_ids = %v", body["ssh_key_ids"])
	}
	// The instance type is not checked: no availability or SSH key lookup.
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters"}) {
		t.Errorf("reads = %v, want only the cluster lookup", got)
	}
}

func TestNodePoolCreate_ReplicasAndLocation(t *testing.T) {
	t.Parallel()
	api := poolWizardAPI()
	if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), withPoolArgs("--replicas", "0", "-l", "FIN-03")...); err != nil {
		t.Fatal(err)
	}
	if body := postBodyAny(t, api); body["replicas"] != float64(0) {
		t.Errorf("an explicit 0 must be sent, body = %v", body)
	}
	if got := api.paths("GET"); len(got) != 0 {
		t.Errorf("-l skips the cluster lookup, reads = %v", got)
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
	for name, f := range map[string]*cmdutil.TestFactory{
		"table": tableFactory(newFakeClient(t, poolWizardAPI()), nil),
		"agent": agentFactory(newFakeClient(t, poolWizardAPI())),
	} {
		_, _, err := runMK8s(t, f, "node-pool", "create")
		ae := cmdutil.ClassifyError(err)
		if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
			t.Fatalf("%s: err = %v", name, err)
		}
		if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster", "--name", "--instance-type", "--ssh-key"}) {
			t.Errorf("%s: missing = %v", name, missing)
		}
	}
}

func TestNodePoolCreate_UnknownClusterAndInvalidInput(t *testing.T) {
	t.Parallel()
	api := poolWizardAPI()
	args := slices.Clone(poolCreateArgs)
	args[slices.Index(args, "--cluster")+1] = "ghost"
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), args...); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown cluster: %v", err)
	}
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withPoolArgs("--replicas", "-1")...)
	if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) {
		t.Errorf("a negative count is a usage error, got %v", err)
	}
	if n := len(api.requestsMatching("POST")); n != 0 {
		t.Errorf("%d POST(s) for invalid input", n)
	}
}

func TestNodePoolCreate_FailureWarnings(t *testing.T) {
	t.Parallel()
	const route = "POST /mk8sclusters/{loc}/{name}/pools"

	t.Run("502 warns once", func(t *testing.T) {
		t.Parallel()
		api := poolWizardAPI()
		api.failures[route] = apiFailure{Status: 502, Body: `{"code":"bad_gateway","message":"upstream failed"}`}
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), poolCreateArgs...)
		if err == nil || !strings.Contains(err.Error(), "upstream failed") {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(errOut, "The node pool may have been created anyway.") || !strings.Contains(errOut, "node-pool list --cluster prod -l FIN-03") {
			t.Errorf("stderr = %q", errOut)
		}
		if n := len(api.requestsMatching("POST")); n != 1 {
			t.Errorf("POSTs = %d, a create is sent once", n)
		}
	})

	t.Run("502 in agent details", func(t *testing.T) {
		t.Parallel()
		api := poolWizardAPI()
		api.failures[route] = apiFailure{Status: 502, Body: `{"code":"bad_gateway","message":"upstream failed"}`}
		_, errOut, err := runMK8s(t, agentFactory(newFakeClient(t, api)), poolCreateArgs...)
		if w, _ := cmdutil.ClassifyError(err).Details["warning"].(string); !strings.Contains(w, "node pool may have been created anyway") || errOut != "" {
			t.Errorf("details=%v stderr=%q", cmdutil.ClassifyError(err).Details, errOut)
		}
	})

	t.Run("400 and 409 do not warn", func(t *testing.T) {
		t.Parallel()
		api := poolWizardAPI()
		api.addPool("staging", "workers", "active", 1, 1) // the name is taken in another cluster
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), poolCreateArgs...)
		if err == nil || !strings.Contains(err.Error(), "pool_already_exists") {
			t.Fatalf("the API reason must be shown: %v", err)
		}
		if errOut != "" {
			t.Errorf("unexpected warning %q", errOut)
		}
	})
}

func TestNodePoolCreate_WaitCompletes(t *testing.T) {
	fastPoll(t)
	api := poolWizardAPI()
	var reads atomic.Int32
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
		switch reads.Add(1) {
		case 1: // stale: active with no worker yet, not what was asked for
			p.State, p.ReadyReplicas = verda.ManagedKubernetesNodePoolStateActive, 0
		case 2:
			p.State, p.ReadyReplicas = verda.ManagedKubernetesNodePoolStateActive, 2
		default:
			p.State, p.ReadyReplicas = verda.ManagedKubernetesNodePoolStateActive, 3
		}
	}
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withPoolArgs("--replicas", "3", "--wait", "--wait-timeout", "30s")...)
	if err != nil || !strings.Contains(out, "Node pool workers completed (cluster prod, FIN-03): 3/3 workers ready") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got := reads.Load(); got != 3 {
		t.Errorf("reads = %d, want 3: a stale or partial read must not end the wait", got)
	}
}

func TestNodePoolCreate_WaitKeepsGoingWhileDegraded(t *testing.T) {
	fastPoll(t)
	api := poolWizardAPI()
	var reads atomic.Int32
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
		if reads.Add(1) < 3 {
			p.State, p.ReadyReplicas = verda.ManagedKubernetesNodePoolStateDegraded, 0
			p.Errors = []verda.ManagedKubernetesNodePoolError{{Reason: "insufficient_capacity", Since: testCreated}}
			return
		}
		p.State, p.ReadyReplicas, p.Errors = verda.ManagedKubernetesNodePoolStateActive, 1, nil
	}
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), withPoolArgs("--wait")...)
	if err != nil {
		t.Fatal(err)
	}
	if r := decodeJSON[PoolResult](t, out); r.Status != "completed" || reads.Load() != 3 {
		t.Errorf("result=%+v reads=%d", r, reads.Load())
	}
}

func TestNodePoolCreate_WaitTimeout(t *testing.T) {
	fastPoll(t)
	api := poolWizardAPI()
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
		p.State, p.ReadyReplicas = verda.ManagedKubernetesNodePoolStateDegraded, 1
		p.Errors = []verda.ManagedKubernetesNodePoolError{{Reason: "insufficient_capacity", Since: testCreated}}
	}
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), withPoolArgs("--replicas", "3", "--wait", "--wait-timeout", "300ms")...)
	if err == nil {
		t.Fatal("a timeout must fail")
	}
	for _, want := range []string{
		"timed out after 300ms", "node pool workers in cluster prod (FIN-03)", "last state: degraded, 1/3 workers ready",
		"errors: insufficient_capacity", "The backend keeps trying to create workers.", "keep billing",
		"verda mk8s node-pool delete workers --cluster prod -l FIN-03",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error misses %q: %v", want, err)
		}
	}
	if r := decodeJSON[PoolResult](t, out); r.Status != "accepted" || r.WaitError == "" || r.Pool.ReadyReplicas != 1 {
		t.Errorf("result = %+v", r)
	}
}

func TestNodePoolCreate_WaitReadFailure(t *testing.T) {
	fastPoll(t)
	api := poolWizardAPI()
	api.failures["GET /mk8sclusters/{loc}/{name}/pools/{pool}"] = apiFailure{Status: 500, Body: `{"code":"internal","message":"read failed"}`}
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withPoolArgs("--wait")...)
	if err == nil || !strings.Contains(err.Error(), "node pool workers in cluster prod (FIN-03) was accepted, but --wait could not read it") {
		t.Fatalf("err = %v", err)
	}
	// The pool exists and may keep creating workers, so this ending carries the billing note too.
	if !strings.Contains(err.Error(), "keep billing") {
		t.Errorf("err = %v, want the billing note", err)
	}
	if n := len(api.requestsMatching("POST")); n != 1 {
		t.Errorf("POSTs = %d", n)
	}
}

func TestNodePoolCreate_WaitTimeoutMustBePositive(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, poolWizardAPI()), nil), withPoolArgs("--wait", "--wait-timeout", "0s")...)
	if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) {
		t.Fatalf("err = %v: want a usage error", err)
	}
}

func TestNodePoolCreate_ValuesAreTrimmed(t *testing.T) {
	t.Parallel()
	api := poolWizardAPI()
	args := []string{
		"node-pool", "create", "--cluster", " prod ", "--name", " workers ", "--instance-type", " 1V100.6V ",
		"--ssh-key", " key-1 ", "--ssh-key", "  ", "--ssh-key", "key-1",
	}
	if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), args...); err != nil {
		t.Fatal(err)
	}
	body := postBodyAny(t, api)
	if body["name"] != "workers" || body["instance_type"] != "1V100.6V" {
		t.Errorf("body = %v", body)
	}
	if keys, _ := body["ssh_key_ids"].([]any); len(keys) != 1 || keys[0] != "key-1" {
		t.Errorf("a blank key is dropped and the others trimmed: %v", body["ssh_key_ids"])
	}
}

func TestNodePoolCreate_NegativeReplicasNeverReachesTheSummary(t *testing.T) {
	withTerminal(t)
	api := poolWizardAPI()
	// No confirm queued: reaching the summary would fail the test.
	_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New()), withPoolArgs("--replicas", "-1")...)
	if err == nil || strings.Contains(errOut, "Create node pool") {
		t.Errorf("err=%v stderr=%q", err, errOut)
	}
}

func TestNodePoolCreate_WaitUntilActiveAndNotJustReady(t *testing.T) {
	fastPoll(t)
	// A pool that is degraded or still creating is not done, even when ready == requested.
	for name, replicas := range map[string]string{"zero workers": "0", "three workers": "3"} {
		api := poolWizardAPI()
		want := 0
		if replicas == "3" {
			want = 3
		}
		var reads atomic.Int32
		api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
			state := verda.ManagedKubernetesNodePoolStateCreating
			switch reads.Add(1) {
			case 2:
				state = verda.ManagedKubernetesNodePoolStateDegraded
			case 3:
				state = verda.ManagedKubernetesNodePoolStateActive
			}
			p.State, p.ReadyReplicas = state, want
		}
		out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), withPoolArgs("--replicas", replicas, "--wait")...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r := decodeJSON[PoolResult](t, out); r.Status != "completed" || reads.Load() != 3 {
			t.Errorf("%s: status=%s reads=%d, want completed after the third read", name, r.Status, reads.Load())
		}
	}
}

func TestNodePoolCreate_InteractiveSummaryAndConfirm(t *testing.T) {
	withTerminal(t)

	t.Run("confirmed", func(t *testing.T) {
		api := poolWizardAPI()
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddConfirm(true)), poolCreateArgs...)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Create node pool workers", "prod (FIN-03)", "1V100.6V", "key-1, key-2"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("summary misses %q: %s", want, errOut)
			}
		}
		if n := len(api.requestsMatching("POST")); n != 1 {
			t.Errorf("POSTs = %d", n)
		}
	})

	t.Run("declined, Esc and Ctrl+C create nothing", func(t *testing.T) {
		for name, p := range map[string]*cancelPrompter{
			"declined": {Prompter: tuitest.New().AddConfirm(false)},
			"esc":      {Prompter: tuitest.New(), confirmErr: contextCanceled()},
			"ctrl+c":   {Prompter: tuitest.New(), confirmErr: errInterrupted()},
		} {
			api := poolWizardAPI()
			f := tableFactory(newFakeClient(t, api), nil)
			f.PrompterOverride = p
			_, errOut, err := runMK8s(t, f, poolCreateArgs...)
			if err != nil || !strings.Contains(errOut, canceledMsg) || len(api.requestsMatching("POST")) != 0 {
				t.Errorf("%s: err=%v stderr=%q", name, err, errOut)
			}
		}
	})

	t.Run("agent and json never prompt", func(t *testing.T) {
		for name, f := range map[string]*cmdutil.TestFactory{
			"agent": agentFactory(newFakeClient(t, poolWizardAPI())),
			"json":  jsonFactory(newFakeClient(t, poolWizardAPI())),
		} {
			if _, _, err := runMK8s(t, f, poolCreateArgs...); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	})
}
