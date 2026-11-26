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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

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
	api := poolAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), scaleArgs("--replicas", "5")...)
	if err != nil || !strings.Contains(out, "Scaling of node pool workers from 3 to 5 workers accepted (cluster prod, FIN-03).") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got := patchedReplicas(t, api); got != float64(5) {
		t.Errorf("replicas = %v", got)
	}
}

func TestNodePoolScale_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, agentFactory(newFakeClient(t, poolAPI())), scaleArgs("--replicas", "5", "-l", "FIN-03")...)
	if err != nil {
		t.Fatal(err)
	}
	if r := decodeJSON[PoolScaleResult](t, out); r.Status != "accepted" || r.PreviousReplicas != 3 || r.Pool.Name != "workers" || r.Pool.Replicas != 5 {
		t.Errorf("result = %+v", r)
	}
}

func TestNodePoolScale_MissingArguments(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, poolAPI())), "node-pool", "scale")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster", "<pool>", "--replicas"}) {
		t.Errorf("missing = %v", missing)
	}
}

func TestNodePoolScale_InvalidFlagsSendNothing(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{scaleArgs("--replicas", "-1"), scaleArgs("--replicas", "5", "--wait", "--wait-timeout", "0s")} {
		api := poolAPI()
		if _, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), args...); !isUsageError(err) || len(api.requests) != 0 {
			t.Errorf("%v: err = %v, %d request(s)", args, err, len(api.requests))
		}
	}
}

func TestNodePoolScale_SameCountSendsNothing(t *testing.T) {
	fastPoll(t)
	for name, tc := range map[string]struct {
		ready      int
		wait       bool
		wantStatus string
	}{
		"no --wait":               {ready: 1, wantStatus: "unchanged"},
		"--wait, settled":         {ready: 2, wait: true, wantStatus: "unchanged"},
		"--wait, not settled yet": {ready: 1, wait: true, wantStatus: "completed"},
	} {
		api := seededClusters()
		api.addPool("prod", "workers", "active", 2, tc.ready)
		var reads atomic.Int32
		api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
			if reads.Add(1) > 1 {
				p.ReadyReplicas = 2
			}
		}
		args := scaleArgs("--replicas", "2")
		if tc.wait {
			args = append(args, "--wait")
		}
		out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), args...)
		if r := decodeJSON[PoolScaleResult](t, out); err != nil || r.Status != tc.wantStatus || len(api.requestsMatching("PATCH")) != 0 {
			t.Errorf("%s: result = %+v, err = %v", name, r, err)
		}
	}
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, poolAPI()), nil), scaleArgs("--replicas", "3")...)
	if err != nil || out != "Node pool workers already has 3 replicas; nothing to do.\n" {
		t.Errorf("out=%q err=%v", out, err)
	}
}

func TestNodePoolScale_Interactive(t *testing.T) {
	withTerminal(t)

	t.Run("up shows a summary, down the red warning", func(t *testing.T) {
		for _, tc := range []struct {
			replicas, want string
			sent           float64
		}{{"5", "Workers:  3 -> 5", 5}, {"1", "VMs are deleted", 1}} {
			api := poolAPI()
			_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), confirmYes()), scaleArgs("--replicas", tc.replicas)...)
			if err != nil || !strings.Contains(errOut, tc.want) {
				t.Errorf("to %s: err=%v stderr=%q", tc.replicas, err, errOut)
			}
			if got := patchedReplicas(t, api); got != tc.sent {
				t.Errorf("to %s: replicas = %v", tc.replicas, got)
			}
		}
	})

	t.Run("--yes skips the confirmation", func(t *testing.T) {
		api := poolAPI()
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New()), scaleArgs("--replicas", "1", "--yes")...); err != nil {
			t.Fatal(err)
		}
		if got := patchedReplicas(t, api); got != float64(1) {
			t.Errorf("replicas = %v", got)
		}
	})

	t.Run("pickers and the replicas prompt", func(t *testing.T) {
		api := poolAPI()
		p := pickerAnswers(0, 0).AddTextInput("4").AddConfirm(true) // cluster, pool, count, confirm
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), p), "node-pool", "scale"); err != nil {
			t.Fatal(err)
		}
		if got := patchedReplicas(t, api); got != float64(4) {
			t.Errorf("replicas = %v", got)
		}
	})
}

func TestNodePoolScale_Wait(t *testing.T) {
	fastPoll(t)
	for _, tc := range []struct{ replicas, want string }{
		{"5", "Node pool workers scaled from 3 to 5 workers (cluster prod, FIN-03).\n"},
		{"1", "Node pool workers scaled from 3 to 1 worker (cluster prod, FIN-03). The removed workers' VMs are still being deleted in the background.\n"},
	} {
		api := poolAPI()
		api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) { p.ReadyReplicas = p.Replicas }
		out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), scaleArgs("--replicas", tc.replicas, "--yes", "--wait")...)
		if err != nil || out != tc.want {
			t.Errorf("to %s: out=%q err=%v", tc.replicas, out, err)
		}
	}

	// Ready already at the target but replicas still the old count is not done:
	// scale waits for poolScaled, not poolCreated.
	api := poolAPI()
	reads := 0
	api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) {
		reads++ // read 1 is the pool lookup before the PATCH
		switch reads {
		case 1:
		case 2:
			p.Replicas, p.ReadyReplicas = 3, 5
		default:
			p.Replicas, p.ReadyReplicas = 5, 5
		}
	}
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), scaleArgs("--replicas", "5", "--wait")...)
	if r := decodeJSON[PoolScaleResult](t, out); err != nil || r.Status != "completed" || reads != 3 {
		t.Errorf("status=%s reads=%d err=%v, want completed after the third read", r.Status, reads, err)
	}
}

// After a scale-up the advice is to scale back; a same-count wait sends no
// PATCH and has no previous count to scale back to.
func TestNodePoolScale_WaitTimeout(t *testing.T) {
	fastPoll(t)
	for _, tc := range []struct {
		api            *fakeAPI
		replicas, want string
		ready, patches int
	}{
		{poolAPI(), "5", "To stop, scale back: `verda mk8s node-pool scale workers --cluster prod -l FIN-03 --replicas 3 --yes`", 3, 1},
		{poolAPI(), "1", "scale-down of node pool workers in cluster prod (FIN-03) (last state: active, 3/1 workers ready). The API is still removing workers.", 3, 1},
		{sameCountAPI(), "2", "scaling of node pool workers in cluster prod (FIN-03) (last state: active, 1/2 workers ready). The backend keeps trying to create workers.", 1, 0},
	} {
		api := tc.api
		api.onPoolGet = func(p *verda.ManagedKubernetesNodePool) { p.ReadyReplicas = tc.ready }
		out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), scaleArgs("--replicas", tc.replicas, "--yes", "--wait", "--wait-timeout", "300ms")...)
		if err == nil || !strings.Contains(err.Error(), tc.want) || (tc.patches == 0 && strings.Contains(err.Error(), "scale back")) {
			t.Errorf("to %s: err = %v", tc.replicas, err)
		}
		if r := decodeJSON[PoolScaleResult](t, out); r.Status != "accepted" || r.WaitError == "" || len(api.requestsMatching("PATCH")) != tc.patches {
			t.Errorf("to %s: result = %+v", tc.replicas, r)
		}
	}
}

// A pool deleted during the wait ends it as a failed read, not a timeout:
// scale read the pool first, so a 404 is not a read lagging a create.
func TestNodePoolScale_WaitEndsWhenThePoolIsGone(t *testing.T) {
	fastPoll(t)
	api := poolAPI()
	api.onPoolGet = func(*verda.ManagedKubernetesNodePool) {
		api.failures["GET /mk8sclusters/{loc}/{name}/pools/{pool}"] = apiFailure{Status: http.StatusNotFound, Body: `{"code":"not_found","message":"node pool not found"}`}
	}
	_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), scaleArgs("--replicas", "5", "--yes", "--wait", "--wait-timeout", "10s")...)
	if err == nil || !strings.Contains(err.Error(), "could not read it") || strings.Contains(err.Error(), "scale back") {
		t.Errorf("err = %v, want a failed read without scale-back advice", err)
	}
}

func sameCountAPI() *fakeAPI {
	api := seededClusters()
	api.addPool("prod", "workers", "active", 2, 1)
	return api
}
