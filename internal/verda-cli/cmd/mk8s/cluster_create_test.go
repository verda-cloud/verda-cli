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

var createArgs = []string{
	"cluster", "create", "--name", "prod", "-l", "FIN-03", "--kubernetes-version", "1.37.0",
	"--network", "pn-123", "--subnet", "sn-456",
}

func withArgs(extra ...string) []string { return append(slices.Clone(createArgs), extra...) }

// The IDs go through unchecked: no network, subnet or cluster read happens.
func TestClusterCreate_FlagsSendExactlyTheGivenIDs(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		extra    []string
		pod, svc string
	}{
		"default ranges": {nil, "10.100.0.0/16", "10.200.0.0/16"},
		"custom ranges":  {[]string{"--pod-cidr", "10.50.0.0/16", "--service-cidr", "10.60.0.0/16"}, "10.50.0.0/16", "10.60.0.0/16"},
	} {
		api := newFakeAPI()
		out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), withArgs(tc.extra...)...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := decodeJSON[map[string]any](t, out)
		if c, _ := got["cluster"].(map[string]any); got["status"] != "accepted" || c["name"] != "prod" || c["phase"] != "provisioning" {
			t.Errorf("%s: result = %v", name, got)
		}
		reqs := api.requestsMatching("POST")
		if len(reqs) != 1 || reqs[0].Path != "/mk8sclusters" {
			t.Fatalf("%s: POST requests = %v", name, reqs)
		}
		body := decodeJSON[map[string]string](t, reqs[0].Body)
		want := map[string]string{
			"name": "prod", "location": "FIN-03", "kubernetes_version": "1.37.0",
			"network": "pn-123", "subnet": "sn-456", "pod_cidr": tc.pod, "service_cidr": tc.svc,
		}
		for k, v := range want {
			if body[k] != v {
				t.Errorf("%s: body[%s] = %q, want %q", name, k, body[k], v)
			}
		}
		if all := api.paths("GET"); len(all) != 0 {
			t.Errorf("%s: unexpected reads: %v", name, all)
		}
	}
}

func TestClusterCreate_TableSaysAccepted(t *testing.T) {
	t.Parallel()
	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, newFakeAPI()), nil), createArgs...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Cluster prod accepted (FIN-03)") || strings.Contains(out, "completed") {
		t.Errorf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "verda mk8s cluster describe prod -l FIN-03") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestClusterCreate_MissingFlagsOffTerminal(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, newFakeAPI())), "cluster", "create", "--name", "prod", "-l", "FIN-03")
	ae := cmdutil.ClassifyError(err)
	missing, _ := ae.Details["missing"].([]string)
	if ae.Code != "MISSING_REQUIRED_FLAGS" || !slices.Equal(missing, []string{"--kubernetes-version", "--network", "--subnet"}) {
		t.Errorf("err = %v, missing = %v", err, missing)
	}
}

func TestClusterCreate_WaitCompletesOnRunningWithEndpoint(t *testing.T) {
	fastPoll(t)
	for name, mk := range map[string]func(*fakeAPI) *cmdutil.TestFactory{
		"table": func(api *fakeAPI) *cmdutil.TestFactory { return tableFactory(newFakeClient(t, api), nil) },
		"json":  func(api *fakeAPI) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
	} {
		api := newFakeAPI()
		api.onClusterGet = func(c *verda.ManagedKubernetesCluster) {
			c.Phase = verda.ManagedKubernetesClusterPhaseRunning
			c.ExternalEndpoint = "https://prod.example.test:6443"
		}
		out, errOut, err := runMK8s(t, mk(api), withArgs("--wait")...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if n := len(api.requestsMatching("POST")); n != 1 {
			t.Errorf("%s: POSTs = %d", name, n)
		}
		if name == "table" {
			if !strings.Contains(out, "Cluster prod completed (FIN-03)") || !strings.Contains(out, "https://prod.example.test:6443") {
				t.Errorf("stdout = %q", out)
			}
			continue
		}
		got := decodeJSON[map[string]any](t, out)
		if got["status"] != "completed" || got["cluster"].(map[string]any)["external_endpoint"] != "https://prod.example.test:6443" {
			t.Errorf("result = %v", got)
		}
		if _, has := got["wait_error"]; has || errOut != "" {
			t.Errorf("no wait_error and a quiet stderr off a terminal: %v %q", got, errOut)
		}
	}
}

func TestClusterCreate_WaitTimeout(t *testing.T) {
	fastPoll(t)
	api := newFakeAPI()
	api.onClusterGet = func(c *verda.ManagedKubernetesCluster) { c.Phase = verda.ManagedKubernetesClusterPhaseDegraded }
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), withArgs("--wait", "--wait-timeout", "300ms")...)
	if err == nil {
		t.Fatal("a timeout must fail")
	}
	// The last phase is the one read, not the one from the create answer.
	for _, want := range []string{"timed out after 300ms", "cluster prod in FIN-03", "last phase: degraded", "verda mk8s cluster describe prod -l FIN-03"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error misses %q: %v", want, err)
		}
	}
	got := decodeJSON[map[string]any](t, out)
	if got["status"] != "accepted" || got["wait_error"] == nil {
		t.Errorf("the structured result is still written, with wait_error: %v", got)
	}
}

func TestClusterCreate_InteractiveSummaryAndConfirm(t *testing.T) {
	withTerminal(t)
	for _, confirm := range []bool{true, false} {
		api := newFakeAPI()
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddConfirm(confirm)), createArgs...)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Create cluster prod", "pn-123", "sn-456", "10.100.0.0/16"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("summary misses %q: %s", want, errOut)
			}
		}
		if want, n := map[bool]int{true: 1, false: 0}[confirm], len(api.requestsMatching("POST")); n != want {
			t.Errorf("confirm=%v: POSTs = %d, want %d", confirm, n, want)
		}
	}
}
