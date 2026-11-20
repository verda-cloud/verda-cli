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

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestNodePoolDelete_YesMakesOneDeleteCall(t *testing.T) {
	t.Parallel()
	api := scaleAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "node-pool", "delete", "workers", "--cluster", "prod", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Deletion accepted. Worker VMs are deleted in the background.") {
		t.Errorf("stdout = %q", out)
	}
	dels := api.requestsMatching("DELETE")
	if len(dels) != 1 || dels[0].Path != "/mk8sclusters/FIN-03/prod/pools/workers" {
		t.Fatalf("DELETE requests = %v", dels)
	}
	if len(api.requestsMatching("POST"))+len(api.requestsMatching("PATCH")) != 0 {
		t.Error("delete must not write anything else")
	}
	// A named pool is not read first (a pool whose read fails must stay deletable), and there is no --wait.
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters"}) {
		t.Errorf("reads = %v, want only the cluster lookup", got)
	}
}

func TestNodePoolDelete_APoolThatCannotBeReadCanStillBeDeleted(t *testing.T) {
	t.Parallel()
	api := scaleAPI()
	api.failures["GET /mk8sclusters/{loc}/{name}/pools/{pool}"] = apiFailure{Status: 500, Body: `{"code":"internal","message":"read failed"}`}
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "node-pool", "delete", "workers", "--cluster", "prod", "-l", "FIN-03", "--yes"); err != nil {
		t.Fatal(err)
	}
	if got := api.paths("GET"); len(got) != 0 {
		t.Errorf("reads = %v, want none with -l", got)
	}
	if n := len(api.requestsMatching("DELETE")); n != 1 {
		t.Errorf("DELETEs = %d", n)
	}
}

func TestNodePoolDelete_Structured(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, agentFactory(newFakeClient(t, scaleAPI())), "np", "rm", "workers", "--cluster", "prod", "-l", "FIN-03", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeJSON[map[string]any](t, out)
	want := map[string]any{"resource": "node pool", "name": "workers", "cluster": "prod", "location": "FIN-03", "action": "delete", "status": "accepted"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("result[%s] = %v, want %v", k, got[k], v)
		}
	}
}

func TestNodePoolDelete_NeedsYesOffTerminalBeforeAnyCall(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*fakeAPI, *testing.T) *cmdutil.TestFactory{
		"agent":          func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return agentFactory(newFakeClient(t, api)) },
		"json on a pipe": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
		"table on a pipe": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory {
			return tableFactory(newFakeClient(t, api), nil)
		},
	} {
		api := scaleAPI()
		_, _, err := runMK8s(t, mk(api, t), "node-pool", "delete", "workers", "--cluster", "prod")
		if ae := cmdutil.ClassifyError(err); ae == nil || ae.Code != "CONFIRMATION_REQUIRED" {
			t.Errorf("%s: err = %v", name, err)
		}
		if n := len(api.requests); n != 0 {
			t.Errorf("%s: %d request(s) before the check", name, n)
		}
	}
}

func TestNodePoolDelete_MissingTargets(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, scaleAPI())), "node-pool", "delete", "--yes")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster", "<pool>"}) {
		t.Errorf("missing = %v", missing)
	}
}

func TestNodePoolDelete_Interactive(t *testing.T) {
	withTerminal(t)

	t.Run("the warning names the worker VMs", func(t *testing.T) {
		api := scaleAPI()
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddConfirm(true)), "node-pool", "delete", "workers", "--cluster", "prod")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut, "Deletes node pool workers and its worker VMs.") || !strings.Contains(errOut, "cannot be undone") {
			t.Errorf("warning = %q", errOut)
		}
		if n := len(api.requestsMatching("DELETE")); n != 1 {
			t.Errorf("DELETEs = %d", n)
		}
	})

	t.Run("pickers then confirm", func(t *testing.T) {
		api := scaleAPI()
		p := pickerAnswers(0, 0).AddConfirm(true)
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), p), "node-pool", "delete"); err != nil {
			t.Fatal(err)
		}
		if dels := api.requestsMatching("DELETE"); len(dels) != 1 || dels[0].Path != "/mk8sclusters/FIN-03/prod/pools/workers" {
			t.Errorf("DELETE requests = %v", dels)
		}
	})

	t.Run("declined, Esc and Ctrl+C delete nothing", func(t *testing.T) {
		for name, p := range map[string]*cancelPrompter{
			"declined": {Prompter: tuitest.New().AddConfirm(false)},
			"esc":      {Prompter: tuitest.New(), confirmErr: contextCanceled()},
			"ctrl+c":   {Prompter: tuitest.New(), confirmErr: errInterrupted()},
		} {
			api := scaleAPI()
			f := tableFactory(newFakeClient(t, api), nil)
			f.PrompterOverride = p
			_, errOut, err := runMK8s(t, f, "node-pool", "delete", "workers", "--cluster", "prod")
			if err != nil || !strings.Contains(errOut, canceledMsg) || len(api.requestsMatching("DELETE")) != 0 {
				t.Errorf("%s: err=%v stderr=%q", name, err, errOut)
			}
		}
	})
}

func TestNodePoolDelete_Errors(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, scaleAPI()), nil), "node-pool", "delete", "ghost", "--cluster", "prod", "--yes")
	if agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown pool: %v", err)
	}
	api := newFakeAPI()
	api.disabled = true
	_, _, err = runMK8s(t, tableFactory(newFakeClient(t, api), nil), "node-pool", "delete", "workers", "--cluster", "prod", "-l", "FIN-03", "--yes")
	if err == nil || agentErrorCode(err) != "AUTH_ERROR" || !strings.Contains(err.Error(), "not enabled for this project") {
		t.Errorf("not enabled: %v", err)
	}
}
