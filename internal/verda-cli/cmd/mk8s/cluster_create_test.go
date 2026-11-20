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
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

var createArgs = []string{
	"cluster", "create", "--name", "prod", "-l", "FIN-03", "--kubernetes-version", "1.37.0",
	"--network", "pn-123", "--subnet", "sn-456",
}

func withArgs(extra ...string) []string { return append(slices.Clone(createArgs), extra...) }

// fastPoll shortens the wait loop. Never call t.Parallel in or under a test that
// uses it, withTerminal or withWizardAnswers: they change package variables.
func fastPoll(t *testing.T) {
	t.Helper()
	orig := pollInterval
	pollInterval = 5 * time.Millisecond
	t.Cleanup(func() { pollInterval = orig })
}

func TestClusterCreate_FlagsSendExactlyTheGivenIDs(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), createArgs...)
	if err != nil {
		t.Fatal(err)
	}

	got := decodeJSON[map[string]any](t, out)
	if got["status"] != "accepted" {
		t.Errorf("status = %v", got["status"])
	}
	if c, _ := got["cluster"].(map[string]any); c["name"] != "prod" || c["phase"] != "provisioning" {
		t.Errorf("cluster = %v", got["cluster"])
	}

	reqs := api.requestsMatching("POST")
	if len(reqs) != 1 || reqs[0].Path != "/mk8sclusters" {
		t.Fatalf("POST requests = %v", reqs)
	}
	body := decodeJSON[map[string]string](t, reqs[0].Body)
	want := map[string]string{
		"name": "prod", "location": "FIN-03", "kubernetes_version": "1.37.0",
		"network": "pn-123", "subnet": "sn-456", "pod_cidr": "10.100.0.0/16", "service_cidr": "10.200.0.0/16",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%s] = %q, want %q", k, body[k], v)
		}
	}
	// IDs go through unchecked: no network, subnet or cluster read happens.
	if all := api.paths("GET"); len(all) != 0 {
		t.Errorf("unexpected reads: %v", all)
	}
}

func TestClusterCreate_CustomCIDRsAndTrimming(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	args := []string{
		"cluster", "create", "--name", " prod ", "-l", "FIN-03", "--kubernetes-version", "1.37.0",
		"--network", " pn-123 ", "--subnet", "sn-456", "--pod-cidr", "10.50.0.0/16", "--service-cidr", "10.60.0.0/16",
	}
	if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), args...); err != nil {
		t.Fatal(err)
	}
	body := decodeJSON[map[string]string](t, api.requestsMatching("POST")[0].Body)
	if body["name"] != "prod" || body["network"] != "pn-123" || body["pod_cidr"] != "10.50.0.0/16" || body["service_cidr"] != "10.60.0.0/16" {
		t.Errorf("body = %v", body)
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
	for name, f := range map[string]*cmdutil.TestFactory{
		"table": tableFactory(newFakeClient(t, newFakeAPI()), nil),
		"agent": agentFactory(newFakeClient(t, newFakeAPI())),
	} {
		_, _, err := runMK8s(t, f, "cluster", "create")
		ae := cmdutil.ClassifyError(err)
		if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
			t.Fatalf("%s: err = %v", name, err)
		}
		missing, _ := ae.Details["missing"].([]string)
		want := []string{"--name", "--location", "--kubernetes-version", "--network", "--subnet"}
		if !slices.Equal(missing, want) {
			t.Errorf("%s: missing = %v, want %v", name, missing, want)
		}
	}

	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, newFakeAPI())), "cluster", "create", "--name", "prod", "-l", "FIN-03")
	missing, _ := cmdutil.ClassifyError(err).Details["missing"].([]string)
	if !slices.Equal(missing, []string{"--kubernetes-version", "--network", "--subnet"}) {
		t.Errorf("only the missing flags are listed: %v", missing)
	}
}

func TestClusterCreate_SDKValidationHappensBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	args := slices.Clone(createArgs)
	args[slices.Index(args, "--name")+1] = "Not_A_Label"
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), args...)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "name") {
		t.Fatalf("err = %v", err)
	}
	if n := len(api.requestsMatching("POST")); n != 0 {
		t.Errorf("%d POST(s) sent for an invalid name", n)
	}
	if got := createMayHaveSucceeded(err); got {
		t.Error("a validation error must not claim the cluster may exist")
	}
}

func TestClusterCreate_FailureWarnings(t *testing.T) {
	t.Parallel()
	const route = "POST /mk8sclusters"

	t.Run("502 warns, once, in human mode", func(t *testing.T) {
		t.Parallel()
		api := newFakeAPI()
		api.failures[route] = apiFailure{Status: 502, Body: `{"code":"bad_gateway","message":"upstream failed"}`}
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), createArgs...)
		if err == nil || !strings.Contains(err.Error(), "upstream failed") {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(errOut, "The cluster may have been created anyway.") || !strings.Contains(errOut, "verda mk8s cluster list") {
			t.Errorf("stderr = %q", errOut)
		}
		if n := len(api.requestsMatching("POST")); n != 1 {
			t.Errorf("a create must be sent once, got %d POSTs", n)
		}
	})

	t.Run("502 warns in agent details", func(t *testing.T) {
		t.Parallel()
		api := newFakeAPI()
		api.failures[route] = apiFailure{Status: 502, Body: `{"code":"bad_gateway","message":"upstream failed"}`}
		_, errOut, err := runMK8s(t, agentFactory(newFakeClient(t, api)), createArgs...)
		ae := cmdutil.ClassifyError(err)
		if w, _ := ae.Details["warning"].(string); !strings.Contains(w, "may have been created anyway") {
			t.Errorf("details = %v", ae.Details)
		}
		if errOut != "" {
			t.Errorf("agent mode reports through the error only: %q", errOut)
		}
	})

	t.Run("400 does not warn", func(t *testing.T) {
		t.Parallel()
		api := newFakeAPI()
		api.failures[route] = apiFailure{Status: 400, Body: `{"code":"bad_request","message":"quota","details":"cluster_quota_exceeded"}`}
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), createArgs...)
		if err == nil || !strings.Contains(err.Error(), "cluster_quota_exceeded") {
			t.Fatalf("the API reason must be shown: %v", err)
		}
		if errOut != "" {
			t.Errorf("unexpected warning: %q", errOut)
		}
	})

	t.Run("feature not enabled", func(t *testing.T) {
		t.Parallel()
		api := newFakeAPI()
		api.disabled = true
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), createArgs...)
		if err == nil || agentErrorCode(err) != "AUTH_ERROR" || !strings.Contains(err.Error(), "not enabled for this project") {
			t.Fatalf("err = %v", err)
		}
		if errOut != "" {
			t.Errorf("a 403 never warns: %q", errOut)
		}
	})
}

func TestClusterCreate_WaitCompletesOnRunningWithEndpoint(t *testing.T) {
	fastPoll(t)
	api := newFakeAPI()
	var gets atomic.Int32
	api.onClusterGet = func(c *verda.ManagedKubernetesCluster) {
		switch gets.Add(1) {
		case 1: // still provisioning
		case 2: // running, but the endpoint is not published yet: not done
			c.Phase = verda.ManagedKubernetesClusterPhaseRunning
		default:
			c.Phase = verda.ManagedKubernetesClusterPhaseRunning
			c.ExternalEndpoint = "https://prod.example.test:6443"
		}
	}

	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withArgs("--wait", "--wait-timeout", "30s")...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Cluster prod completed (FIN-03)") || !strings.Contains(out, "https://prod.example.test:6443") {
		t.Errorf("stdout = %q", out)
	}
	if got := gets.Load(); got != 3 {
		t.Errorf("reads = %d, want 3: running without an endpoint must not end the wait", got)
	}
	if n := len(api.requestsMatching("POST")); n != 1 {
		t.Errorf("POSTs = %d", n)
	}

	api2 := newFakeAPI()
	api2.onClusterGet = func(c *verda.ManagedKubernetesCluster) {
		c.Phase = verda.ManagedKubernetesClusterPhaseRunning
		c.ExternalEndpoint = "https://prod.example.test:6443"
	}
	out, _, err = runMK8s(t, jsonFactory(newFakeClient(t, api2)), withArgs("--wait")...)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[map[string]any](t, out)
	if got["status"] != "completed" || got["cluster"].(map[string]any)["external_endpoint"] != "https://prod.example.test:6443" {
		t.Errorf("result = %v", got)
	}
	if _, has := got["wait_error"]; has {
		t.Error("no wait_error on success")
	}
}

func TestClusterCreate_WaitTimeout(t *testing.T) {
	fastPoll(t)
	api := newFakeAPI() // stays provisioning forever
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), withArgs("--wait", "--wait-timeout", "300ms")...)
	if err == nil {
		t.Fatal("a timeout must fail")
	}
	for _, want := range []string{"timed out after 300ms", "cluster prod in FIN-03", "last phase: provisioning", "verda mk8s cluster describe prod -l FIN-03"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error misses %q: %v", want, err)
		}
	}
	got := decodeJSON[map[string]any](t, out)
	if got["status"] != "accepted" || got["wait_error"] == nil {
		t.Errorf("the structured result is still written, with wait_error: %v", got)
	}
}

const clusterGetRoute = "GET /mk8sclusters/{loc}/{name}"

func TestClusterCreate_WaitReadFailures(t *testing.T) {
	fastPoll(t)

	t.Run("reads that keep failing end the wait and say the cluster was accepted", func(t *testing.T) {
		api := newFakeAPI()
		api.failures[clusterGetRoute] = apiFailure{Status: 500, Body: `{"code":"internal","message":"read failed"}`}
		_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withArgs("--wait")...)
		if err == nil || !strings.Contains(err.Error(), "was accepted, but --wait could not read it") || !strings.Contains(err.Error(), "read failed") {
			t.Fatalf("err = %v", err)
		}
		if n := len(api.paths("GET")); n != maxReadFailures {
			t.Errorf("reads = %d, want %d in a row before giving up", n, maxReadFailures)
		}
		if n := len(api.requestsMatching("POST")); n != 1 {
			t.Errorf("POSTs = %d", n)
		}
	})

	t.Run("a few failed reads do not fail the wait", func(t *testing.T) {
		api := newFakeAPI()
		api.failures[clusterGetRoute] = apiFailure{Status: 502, Body: `{"code":"bad_gateway","message":"lb"}`, Times: maxReadFailures - 1}
		api.onClusterGet = func(c *verda.ManagedKubernetesCluster) {
			c.Phase = verda.ManagedKubernetesClusterPhaseRunning
			c.ExternalEndpoint = "https://prod.example.test:6443"
		}
		out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withArgs("--wait")...)
		if err != nil || !strings.Contains(out, "completed") {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})

	t.Run("a read the API refuses ends the wait at once", func(t *testing.T) {
		api := newFakeAPI()
		api.failures[clusterGetRoute] = apiFailure{Status: 403, Body: `{"code":"forbidden","message":"no access"}`}
		_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withArgs("--wait")...)
		if err == nil || !strings.Contains(err.Error(), "no access") {
			t.Fatalf("err = %v", err)
		}
		if n := len(api.paths("GET")); n != 1 {
			t.Errorf("reads = %d, want 1", n)
		}
	})

	t.Run("a stale 404 right after the create is tolerated", func(t *testing.T) {
		api := newFakeAPI()
		api.failures[clusterGetRoute] = apiFailure{Status: 404, Body: `{"code":"not_found","message":"cluster not found"}`, Times: maxReadFailures + 2}
		api.onClusterGet = func(c *verda.ManagedKubernetesCluster) {
			c.Phase = verda.ManagedKubernetesClusterPhaseRunning
			c.ExternalEndpoint = "https://prod.example.test:6443"
		}
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withArgs("--wait")...); err != nil {
			t.Fatal(err)
		}
	})
}

func TestClusterCreate_WaitTimeoutReportsTheLastPhaseSeen(t *testing.T) {
	fastPoll(t)
	api := newFakeAPI()
	api.onClusterGet = func(c *verda.ManagedKubernetesCluster) { c.Phase = verda.ManagedKubernetesClusterPhaseDegraded }
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), withArgs("--wait", "--wait-timeout", "300ms")...)
	if err == nil || !strings.Contains(err.Error(), "last phase: degraded") {
		t.Fatalf("err = %v, want the last phase seen, not the one from the create answer", err)
	}
}

func TestClusterCreate_WaitKeepsStderrQuietOffTerminal(t *testing.T) {
	fastPoll(t)
	for name, mk := range map[string]func(*fakeAPI, *testing.T) *cmdutil.TestFactory{
		"json":  func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
		"agent": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return agentFactory(newFakeClient(t, api)) },
	} {
		api := newFakeAPI()
		api.onClusterGet = func(c *verda.ManagedKubernetesCluster) {
			c.Phase = verda.ManagedKubernetesClusterPhaseRunning
			c.ExternalEndpoint = "https://prod.example.test:6443"
		}
		_, errOut, err := runMK8s(t, mk(api, t), withArgs("--wait")...)
		if err != nil || errOut != "" {
			t.Errorf("%s: err=%v stderr=%q", name, err, errOut)
		}
	}
}

func TestClusterCreate_FlagValuesAreTrimmed(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	args := []string{
		"cluster", "create", "--name", " prod ", "-l", " FIN-03 ", "--kubernetes-version", " 1.37.0 ",
		"--network", " pn-123 ", "--subnet", " sn-456 ", "--pod-cidr", " 10.50.0.0/16 ", "--service-cidr", " 10.60.0.0/16 ",
	}
	if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), args...); err != nil {
		t.Fatal(err)
	}
	for k, v := range decodeJSON[map[string]string](t, api.requestsMatching("POST")[0].Body) {
		if strings.TrimSpace(v) != v {
			t.Errorf("body[%s] = %q was not trimmed", k, v)
		}
	}
}

func TestClusterCreate_WaitTimeoutMustBePositive(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, newFakeAPI()), nil), withArgs("--wait", "--wait-timeout", "0s")...)
	if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) {
		t.Fatalf("err = %v: want a usage error (an AgentError would print as JSON for a person)", err)
	}
}

func TestClusterCreate_InteractiveSummaryAndConfirm(t *testing.T) {
	withTerminal(t)

	t.Run("confirmed", func(t *testing.T) {
		api := newFakeAPI()
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddConfirm(true)), createArgs...)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Create cluster prod", "pn-123", "sn-456", "10.100.0.0/16"} {
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
			api := newFakeAPI()
			f := tableFactory(newFakeClient(t, api), nil)
			f.PrompterOverride = p
			_, errOut, err := runMK8s(t, f, createArgs...)
			if err != nil || !strings.Contains(errOut, canceledMsg) {
				t.Errorf("%s: err=%v stderr=%q", name, err, errOut)
			}
			if n := len(api.requestsMatching("POST")); n != 0 {
				t.Errorf("%s: %d POST(s)", name, n)
			}
		}
	})

	t.Run("agent mode never prompts", func(t *testing.T) {
		api := newFakeAPI()
		// No prompter: a prompt would fail the test.
		if _, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), createArgs...); err != nil {
			t.Fatal(err)
		}
		if n := len(api.requestsMatching("POST")); n != 1 {
			t.Errorf("POSTs = %d", n)
		}
	})

	t.Run("json output never prompts", func(t *testing.T) {
		api := newFakeAPI()
		if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), createArgs...); err != nil {
			t.Fatal(err)
		}
		if n := len(api.requestsMatching("POST")); n != 1 {
			t.Errorf("POSTs = %d", n)
		}
	})
}
