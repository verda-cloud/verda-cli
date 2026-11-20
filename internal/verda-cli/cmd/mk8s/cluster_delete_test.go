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
	"strings"
	"testing"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestClusterDelete_YesMakesOneDeleteCall(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	api.addPool("prod", "workers", "active", 3, 3)
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "delete", "prod", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Deletion accepted. Node pools and worker VMs are deleted in the background.") {
		t.Errorf("stdout = %q", out)
	}
	dels := api.requestsMatching("DELETE")
	if len(dels) != 1 || dels[0].Path != "/mk8sclusters/FIN-03/prod" {
		t.Fatalf("DELETE requests = %v", dels)
	}
	// No node pool check, no wait.
	for _, p := range api.paths("GET") {
		if strings.Contains(p, "/pools") {
			t.Errorf("delete must not read node pools: %s", p)
		}
	}
	if len(api.requestsMatching("POST"))+len(api.requestsMatching("PATCH")) != 0 {
		t.Error("delete must not write anything else")
	}
}

func TestClusterDelete_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, agentFactory(newFakeClient(t, seededClusters())), "cluster", "rm", "prod", "-l", "FIN-03", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[map[string]any](t, out)
	want := map[string]any{"resource": "cluster", "name": "prod", "location": "FIN-03", "action": "delete", "status": "accepted"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("result[%s] = %v, want %v", k, got[k], v)
		}
	}
	for _, k := range []string{"cluster", "id"} {
		if _, has := got[k]; has {
			t.Errorf("%q must be absent for a cluster: %v", k, got)
		}
	}
}

func TestClusterDelete_NeedsYesOffTerminalBeforeAnyCall(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*fakeAPI, *testing.T) *cmdutil.TestFactory{
		"agent":          func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return agentFactory(newFakeClient(t, api)) },
		"json on a pipe": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
		"table on a pipe": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory {
			return tableFactory(newFakeClient(t, api), nil)
		},
	} {
		api := seededClusters()
		_, _, err := runMK8s(t, mk(api, t), "cluster", "delete", "prod")
		if ae := cmdutil.ClassifyError(err); ae == nil || ae.Code != "CONFIRMATION_REQUIRED" {
			t.Errorf("%s: err = %v", name, err)
		} else if !strings.Contains(ae.Message, "without a terminal, in agent mode or with -o json|yaml") {
			t.Errorf("%s: the message must name every cause: %q", name, ae.Message)
		}
		if n := len(api.requests); n != 0 {
			t.Errorf("%s: %d request(s) before the check", name, n)
		}
	}
}

func TestClusterDelete_MissingCluster(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, seededClusters())), "cluster", "delete", "--yes")
	if ae := cmdutil.ClassifyError(err); ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
}

func TestClusterDelete_Interactive(t *testing.T) {
	withTerminal(t)

	t.Run("the warning names pools, worker VMs and kubeconfigs", func(t *testing.T) {
		api := seededClusters()
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddConfirm(true)), "cluster", "delete", "prod")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"node pools", "worker VMs", "kubeconfigs", "Downloaded kubeconfigs stop working", "cannot be undone"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("warning misses %q: %s", want, errOut)
			}
		}
		if n := len(api.requestsMatching("DELETE")); n != 1 {
			t.Errorf("DELETEs = %d", n)
		}
	})

	t.Run("picker then confirm deletes the chosen cluster", func(t *testing.T) {
		api := seededClusters()
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddSelect(1).AddConfirm(true)), "cluster", "delete"); err != nil {
			t.Fatal(err)
		}
		dels := api.requestsMatching("DELETE")
		if len(dels) != 1 || dels[0].Path != "/mk8sclusters/FIN-01/staging" {
			t.Errorf("DELETE requests = %v", dels)
		}
	})

	t.Run("declined, Esc and Ctrl+C delete nothing", func(t *testing.T) {
		for name, p := range map[string]*cancelPrompter{
			"declined": {Prompter: tuitest.New().AddConfirm(false)},
			"esc":      {Prompter: tuitest.New(), confirmErr: contextCanceled()},
			"ctrl+c":   {Prompter: tuitest.New(), confirmErr: errInterrupted()},
		} {
			api := seededClusters()
			f := tableFactory(newFakeClient(t, api), nil)
			f.PrompterOverride = p
			_, errOut, err := runMK8s(t, f, "cluster", "delete", "prod")
			if err != nil || !strings.Contains(errOut, canceledMsg) {
				t.Errorf("%s: err=%v stderr=%q", name, err, errOut)
			}
			if n := len(api.requestsMatching("DELETE")); n != 0 {
				t.Errorf("%s: %d DELETE(s)", name, n)
			}
		}
	})

	t.Run("--yes skips the confirmation", func(t *testing.T) {
		api := seededClusters()
		// No prompter answers: a confirm would fail the test.
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New()), "cluster", "delete", "prod", "--yes"); err != nil {
			t.Fatal(err)
		}
		if n := len(api.requestsMatching("DELETE")); n != 1 {
			t.Errorf("DELETEs = %d", n)
		}
	})
}

func TestClusterDelete_Errors(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, seededClusters()), nil), "cluster", "delete", "ghost", "--yes")
	if agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown cluster: %v", err)
	}

	api := newFakeAPI()
	api.disabled = true
	_, _, err = runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "delete", "prod", "-l", "FIN-03", "--yes")
	if err == nil || agentErrorCode(err) != "AUTH_ERROR" || !strings.Contains(err.Error(), "not enabled for this project") {
		t.Errorf("not enabled: %v", err)
	}
}
