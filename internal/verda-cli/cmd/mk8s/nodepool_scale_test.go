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
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// scaleAPI has pool "workers" in cluster "prod": 3 of 3 workers, active.
func scaleAPI() *fakeAPI {
	api := seededClusters()
	api.addPool("prod", "workers", "active", 3, 3)
	return api
}

func scaleArgs(extra ...string) []string {
	return append([]string{"node-pool", "scale", "workers", "--cluster", "prod"}, extra...)
}

func patchedReplicas(t *testing.T, api *fakeAPI) any {
	t.Helper()
	patches := api.requestsMatching("PATCH")
	if len(patches) != 1 || patches[0].Path != "/mk8sclusters/FIN-03/prod/pools/workers" {
		t.Fatalf("PATCH requests = %v", patches)
	}
	return decodeJSON[map[string]any](t, patches[0].Body)["replicas"]
}

func TestNodePoolScale_UpNeedsNoYesOffTerminal(t *testing.T) {
	t.Parallel()
	api := scaleAPI()
	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), scaleArgs("--replicas", "5")...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Scaling of node pool workers from 3 to 5 workers accepted (cluster prod, FIN-03).") {
		t.Errorf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "verda mk8s node-pool describe workers --cluster prod -l FIN-03") {
		t.Errorf("stderr = %q", errOut)
	}
	if got := patchedReplicas(t, api); got != float64(5) {
		t.Errorf("replicas = %v", got)
	}
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod/pools/workers"}) {
		t.Errorf("reads = %v", got)
	}
}

func TestNodePoolScale_DownNeedsYesOffTerminal(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*fakeAPI, *testing.T) *cmdutil.TestFactory{
		"agent":          func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return agentFactory(newFakeClient(t, api)) },
		"json on a pipe": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
		"table on a pipe": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory {
			return tableFactory(newFakeClient(t, api), nil)
		},
	} {
		for _, replicas := range []string{"1", "0"} {
			api := scaleAPI()
			_, _, err := runMK8s(t, mk(api, t), scaleArgs("--replicas", replicas)...)
			if ae := cmdutil.ClassifyError(err); ae == nil || ae.Code != "CONFIRMATION_REQUIRED" || ae.Message != confirmationRequired("scale").Message {
				t.Errorf("%s to %s: err = %v", name, replicas, err)
			}
			if n := len(api.requestsMatching("PATCH")); n != 0 {
				t.Errorf("%s to %s: %d PATCH(es) without --yes", name, replicas, n)
			}
		}
	}

	api := scaleAPI()
	if _, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), scaleArgs("--replicas", "0", "--yes")...); err != nil {
		t.Fatal(err)
	}
	if got := patchedReplicas(t, api); got != float64(0) {
		t.Errorf("replicas = %v, want an explicit 0", got)
	}
}

func TestNodePoolScale_SameCountSendsNothing(t *testing.T) {
	t.Parallel()
	api := scaleAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), scaleArgs("--replicas", "3")...)
	if err != nil || out != "Node pool workers already has 3 replicas; nothing to do.\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	api.pools["prod"][0].Replicas, api.pools["prod"][0].ReadyReplicas = 1, 1
	out, _, err = runMK8s(t, tableFactory(newFakeClient(t, api), nil), scaleArgs("--replicas", "1")...)
	if err != nil || out != "Node pool workers already has 1 replica; nothing to do.\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	api.pools["prod"][0].Replicas, api.pools["prod"][0].ReadyReplicas = 3, 3
	out, _, err = runMK8s(t, jsonFactory(newFakeClient(t, api)), scaleArgs("--replicas", "3", "--wait")...)
	if r := decodeJSON[PoolScaleResult](t, out); err != nil || r.Status != "unchanged" || r.PreviousReplicas != 3 || r.Pool.Replicas != 3 {
		t.Errorf("result = %+v err=%v", r, err)
	}
	if n := len(api.requestsMatching("PATCH")); n != 0 {
		t.Errorf("PATCH requests = %d, want none", n)
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestNodePoolScale_SameCountWaitsUntilSettled(t *testing.T) {
	fastPoll(t)
	cases := map[string]struct {
		state      string
		ready      int
		wait       bool
		wantStatus string
	}{
		"not ready, no --wait":          {state: "active", ready: 1, wantStatus: "unchanged"},
		"fully ready, --wait":           {state: "active", ready: 2, wait: true, wantStatus: "unchanged"},
		"not ready, --wait":             {state: "active", ready: 1, wait: true, wantStatus: "completed"},
		"degraded at the count, --wait": {state: "degraded", ready: 2, wait: true, wantStatus: "completed"},
	}
	for name, tc := range cases {
		api := seededClusters()
		api.addPool("prod", "workers", tc.state, 2, tc.ready)
		var reads atomic.Int32
		api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
			if reads.Add(1) > 2 {
				p.State, p.Replicas, p.ReadyReplicas = verda.ManagedKubernetesNodePoolStateActive, 2, 2
			}
		}
		args := scaleArgs("--replicas", "2")
		if tc.wait {
			args = append(args, "--wait", "--wait-timeout", "30s")
		}
		out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r := decodeJSON[PoolScaleResult](t, out); r.Status != tc.wantStatus || r.PreviousReplicas != 2 || r.Pool.Replicas != 2 {
			t.Errorf("%s: result = %+v", name, r)
		}
		if n := len(api.requestsMatching("PATCH")); n != 0 {
			t.Errorf("%s: %d PATCH(es), want none", name, n)
		}
		if waited := reads.Load() > 1; waited != (tc.wantStatus == "completed") {
			t.Errorf("%s: waited = %v, pool reads = %d", name, waited, reads.Load())
		}
	}
}

func TestNodePoolScale_SameCountWaitTimeout(t *testing.T) {
	fastPoll(t)
	api := seededClusters()
	api.addPool("prod", "workers", "active", 2, 1)
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) { p.ReadyReplicas = 1 }
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), scaleArgs("--replicas", "2", "--wait", "--wait-timeout", "300ms")...)
	if err == nil {
		t.Fatal("a timeout must fail")
	}
	for _, want := range []string{"timed out after 300ms", "scaling of node pool workers in cluster prod (FIN-03)", "1/2 workers ready", "The backend keeps trying to create workers."} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error misses %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "scale back") {
		t.Errorf("there is no previous count to scale back to: %v", err)
	}
	if r := decodeJSON[PoolScaleResult](t, out); r.Status != "accepted" || r.WaitError == "" || len(api.requestsMatching("PATCH")) != 0 {
		t.Errorf("result = %+v", r)
	}
}

func TestNodePoolScale_MissingArguments(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, scaleAPI())), "node-pool", "scale")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster", "<pool>", "--replicas"}) {
		t.Errorf("missing = %v", missing)
	}
}

func TestNodePoolScale_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, agentFactory(newFakeClient(t, scaleAPI())), scaleArgs("--replicas", "5", "-l", "FIN-03")...)
	if err != nil {
		t.Fatal(err)
	}
	r := decodeJSON[PoolScaleResult](t, out)
	if r.Status != "accepted" || r.PreviousReplicas != 3 || r.Pool.Name != "workers" || r.Pool.Replicas != 5 {
		t.Errorf("result = %+v", r)
	}
}

func TestNodePoolScale_Interactive(t *testing.T) {
	withTerminal(t)

	t.Run("scale-up shows a summary and asks", func(t *testing.T) {
		api := scaleAPI()
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddConfirm(true)), scaleArgs("--replicas", "5")...)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut, "Workers:  3 -> 5") || strings.Contains(errOut, "cannot be undone") {
			t.Errorf("summary = %q", errOut)
		}
		if got := patchedReplicas(t, api); got != float64(5) {
			t.Errorf("replicas = %v", got)
		}
	})

	t.Run("scale-down, to 0 too, shows the red warning", func(t *testing.T) {
		for _, replicas := range []string{"1", "0"} {
			api := scaleAPI()
			_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddConfirm(true)), scaleArgs("--replicas", replicas)...)
			if err != nil {
				t.Fatal(err)
			}
			if want := "Scales node pool workers down from 3 to " + pluralize(mustAtoi(t, replicas), "worker") + "."; !strings.Contains(errOut, want) || !strings.Contains(errOut, "VMs are deleted") {
				t.Errorf("to %s: warning = %q", replicas, errOut)
			}
		}
	})

	t.Run("declined, Esc and Ctrl+C change nothing", func(t *testing.T) {
		for name, p := range map[string]*cancelPrompter{
			"declined": {Prompter: tuitest.New().AddConfirm(false)},
			"esc":      {Prompter: tuitest.New(), confirmErr: contextCanceled()},
			"ctrl+c":   {Prompter: tuitest.New(), confirmErr: errInterrupted()},
		} {
			api := scaleAPI()
			f := tableFactory(newFakeClient(t, api), nil)
			f.PrompterOverride = p
			_, errOut, err := runMK8s(t, f, scaleArgs("--replicas", "1")...)
			if err != nil || !strings.Contains(errOut, canceledMsg) || len(api.requestsMatching("PATCH")) != 0 {
				t.Errorf("%s: err=%v stderr=%q", name, err, errOut)
			}
		}
	})

	t.Run("--yes skips the confirmation", func(t *testing.T) {
		api := scaleAPI()
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New()), scaleArgs("--replicas", "1", "--yes")...); err != nil {
			t.Fatal(err)
		}
		if got := patchedReplicas(t, api); got != float64(1) {
			t.Errorf("replicas = %v", got)
		}
	})

	t.Run("pickers and the replicas prompt", func(t *testing.T) {
		api := scaleAPI()
		p := pickerAnswers(0, 0).AddTextInput("4").AddConfirm(true) // cluster, pool, count, confirm
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), p), "node-pool", "scale"); err != nil {
			t.Fatal(err)
		}
		if got := patchedReplicas(t, api); got != float64(4) {
			t.Errorf("replicas = %v", got)
		}
	})

	t.Run("canceling the replicas prompt changes nothing", func(t *testing.T) {
		api := scaleAPI()
		f := tableFactory(newFakeClient(t, api), nil)
		f.PrompterOverride = &textCancelPrompter{Prompter: tuitest.New()}
		_, errOut, err := runMK8s(t, f, scaleArgs()...)
		if err != nil || !strings.Contains(errOut, canceledMsg) || len(api.requestsMatching("PATCH")) != 0 {
			t.Errorf("err=%v stderr=%q", err, errOut)
		}
	})
}

// patchSeen reports whether the fake has taken an update; it runs inside a handler, which holds the lock.
func patchSeen(api *fakeAPI) bool {
	for _, r := range api.requests {
		if r.Method == http.MethodPatch {
			return true
		}
	}
	return false
}

func TestNodePoolScale_WaitUp(t *testing.T) {
	fastPoll(t)
	api := scaleAPI()
	var waited atomic.Int32
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
		if !patchSeen(api) {
			return // the read that finds the pool, before the update
		}
		switch waited.Add(1) {
		case 1: // stale: the old count, all ready, active
			p.Replicas, p.ReadyReplicas = 3, 3
		case 2: // new count, workers still joining
			p.Replicas, p.ReadyReplicas = 5, 3
		default:
			p.Replicas, p.ReadyReplicas = 5, 5
		}
		p.State = verda.ManagedKubernetesNodePoolStateActive
	}
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), scaleArgs("--replicas", "5", "--wait", "--wait-timeout", "30s")...)
	if err != nil || !strings.Contains(out, "Node pool workers scaled from 3 to 5 workers (cluster prod, FIN-03).") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if strings.Contains(out, "VMs are still being deleted") {
		t.Errorf("a scale-up has no removed VMs: %q", out)
	}
	if got := waited.Load(); got != 3 {
		t.Errorf("reads while waiting = %d, want 3: the stale first read must not end the wait", got)
	}
}

func TestNodePoolScale_WaitDownSaysVMsAreStillDeleted(t *testing.T) {
	fastPoll(t)
	api := scaleAPI()
	var reads atomic.Int32
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
		if reads.Add(1) == 2 { // the first read after the update: workers not removed yet
			p.Replicas, p.ReadyReplicas = 1, 3
			return
		}
		if reads.Load() > 2 {
			p.Replicas, p.ReadyReplicas = 1, 1
		}
	}
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), scaleArgs("--replicas", "1", "--yes", "--wait")...)
	if err != nil || !strings.Contains(out, "scaled from 3 to 1 worker (") || !strings.Contains(out, "The removed workers' VMs are still being deleted in the background.") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestNodePoolScale_WaitTimeout(t *testing.T) {
	fastPoll(t)
	api := scaleAPI()
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) { p.ReadyReplicas = 3 }
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), scaleArgs("--replicas", "5", "--wait", "--wait-timeout", "300ms")...)
	if err == nil {
		t.Fatal("a timeout must fail")
	}
	for _, want := range []string{"timed out after 300ms", "node pool workers in cluster prod (FIN-03)", "3/5 workers ready", "The backend keeps trying to create workers.", "To stop, scale back: `verda mk8s node-pool scale workers --cluster prod -l FIN-03 --replicas 3 --yes`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error misses %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "node-pool delete") {
		t.Errorf("a scale-up timeout must not advise deleting the pool: %v", err)
	}
	if r := decodeJSON[PoolScaleResult](t, out); r.Status != "accepted" || r.WaitError == "" {
		t.Errorf("result = %+v", r)
	}

	// For a person the advice is in the error only, not also on stderr before it.
	api = scaleAPI()
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) { p.ReadyReplicas = 3 }
	_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), scaleArgs("--replicas", "5", "--wait", "--wait-timeout", "300ms")...)
	if err == nil || strings.Contains(errOut, "Check progress with") {
		t.Errorf("err=%v stderr=%q", err, errOut)
	}
}

func TestNodePoolScale_WaitDownTimeoutDoesNotTalkAboutCreatingWorkers(t *testing.T) {
	fastPoll(t)
	api := scaleAPI()
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) { p.ReadyReplicas = 3 }
	_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), scaleArgs("--replicas", "1", "--yes", "--wait", "--wait-timeout", "300ms")...)
	if err == nil {
		t.Fatal("a timeout must fail")
	}
	if !strings.Contains(err.Error(), "scale-down of node pool workers") || !strings.Contains(err.Error(), "still removing workers") {
		t.Errorf("err = %v", err)
	}
	for _, bad := range []string{"keeps trying to create", "keep billing", "node-pool delete"} {
		if strings.Contains(err.Error(), bad) {
			t.Errorf("a scale-down timeout must not say %q: %v", bad, err)
		}
	}
}

func TestNodePoolScale_WaitUntilActiveAndTheNewCount(t *testing.T) {
	fastPoll(t)
	cases := map[string]struct {
		want  int
		reads []verda.ManagedKubernetesNodePool // the waiting reads, in order
	}{
		"to 0, a stale read still shows the workers": {want: 0, reads: []verda.ManagedKubernetesNodePool{
			{State: "active", Replicas: 0, ReadyReplicas: 3},
			{State: "active", Replicas: 0, ReadyReplicas: 0},
		}},
		"degraded at the new count is not done": {want: 5, reads: []verda.ManagedKubernetesNodePool{
			{State: "degraded", Replicas: 5, ReadyReplicas: 5},
			{State: "active", Replicas: 5, ReadyReplicas: 5},
		}},
		"ready equals the target but replicas is still the old count": {want: 5, reads: []verda.ManagedKubernetesNodePool{
			{State: "active", Replicas: 3, ReadyReplicas: 5},
			{State: "active", Replicas: 5, ReadyReplicas: 5},
		}},
	}
	for name, tc := range cases {
		api := scaleAPI()
		var waited atomic.Int32
		api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
			if !patchSeen(api) {
				return
			}
			i := min(int(waited.Add(1))-1, len(tc.reads)-1)
			p.State, p.Replicas, p.ReadyReplicas = tc.reads[i].State, tc.reads[i].Replicas, tc.reads[i].ReadyReplicas
		}
		out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), scaleArgs("--replicas", strconv.Itoa(tc.want), "--yes", "--wait")...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r := decodeJSON[PoolScaleResult](t, out); r.Status != "completed" || int(waited.Load()) != len(tc.reads) {
			t.Errorf("%s: status=%s waiting reads=%d, want completed after %d", name, r.Status, waited.Load(), len(tc.reads))
		}
	}
}

func TestNodePoolScale_NegativeReplicasIsAUsageErrorBeforeAnyConfirmation(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"with --yes":    scaleArgs("--replicas", "-1", "--yes"),
		"without --yes": scaleArgs("--replicas", "-1"),
	} {
		api := scaleAPI()
		_, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), args...)
		if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) {
			t.Errorf("%s: err = %v, want a usage error, not CONFIRMATION_REQUIRED", name, err)
		}
		if len(api.requests) != 0 {
			t.Errorf("%s: %d request(s) before the check", name, len(api.requests))
		}
	}
}

func TestNodePoolScale_WaitTimeoutMustBePositive(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, scaleAPI()), nil), scaleArgs("--replicas", "5", "--wait", "--wait-timeout", "0s")...)
	if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) {
		t.Errorf("err = %v", err)
	}
}

func TestNodePoolScale_APIErrorAndValidation(t *testing.T) {
	t.Parallel()
	api := scaleAPI()
	api.failures["PATCH /mk8sclusters/{loc}/{name}/pools/{pool}"] = apiFailure{Status: 409, Body: `{"code":"conflict","message":"busy","details":"concurrent_modification"}`}
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), scaleArgs("--replicas", "5")...)
	if err == nil || !strings.Contains(err.Error(), "concurrent_modification") {
		t.Errorf("err = %v", err)
	}

	if _, _, err := runMK8s(t, agentFactory(newFakeClient(t, scaleAPI())), "node-pool", "scale", "ghost", "--cluster", "prod", "--replicas", "2"); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown pool: %v", err)
	}
}
