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

	"go.yaml.in/yaml/v3"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

const kcDeletePath = "/mk8sclusters/FIN-03/prod/kubeconfigs/prod-kc-aaa"

func TestKubeconfigDelete_YesMakesOneDeleteCall(t *testing.T) {
	t.Parallel()
	api := kcFixtureAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Deletion accepted.") || !strings.Contains(out, "not revoked") || !strings.Contains(out, "still work until they expire.") || !strings.Contains(out, "Deleting the cluster invalidates them") || !strings.Contains(out, "does not use system:masters") {
		t.Errorf("stdout = %q", out)
	}
	if dels := api.requestsMatching("DELETE"); len(dels) != 1 || dels[0].Path != kcDeletePath {
		t.Fatalf("DELETE requests = %v", dels)
	}
	if len(api.requestsMatching("POST"))+len(api.requestsMatching("PATCH")) != 0 {
		t.Error("delete must not write anything else")
	}
	// A named id is not read first (no GET of the kubeconfig, so no YAML either), and there is no --wait.
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters"}) {
		t.Errorf("reads = %v, want only the cluster lookup", got)
	}
	if len(api.kubeconfigs["prod"]) != 1 || api.kubeconfigs["prod"][0].ID != "prod-kc-bbb" {
		t.Errorf("kubeconfigs left = %+v", api.kubeconfigs["prod"])
	}
}

func TestKubeconfigDelete_AKubeconfigThatCannotBeReadCanStillBeDeleted(t *testing.T) {
	t.Parallel()
	api := kcFixtureAPI()
	api.failures["GET /mk8sclusters/{loc}/{name}/kubeconfigs/{id}"] = apiFailure{Status: 500, Body: `{"code":"internal","message":"read failed"}`}
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "rm", "prod-kc-aaa", "--cluster", "prod", "-l", "FIN-03", "--yes"); err != nil {
		t.Fatal(err)
	}
	if got := api.paths("GET"); len(got) != 0 {
		t.Errorf("reads = %v, want none with -l", got)
	}
	if n := len(api.requestsMatching("DELETE")); n != 1 {
		t.Errorf("DELETEs = %d", n)
	}
}

func TestKubeconfigDelete_Structured(t *testing.T) {
	t.Parallel()
	want := ActionResult{Resource: "kubeconfig", ID: "prod-kc-aaa", Name: "prod-kc-aaa", Location: "FIN-03", Cluster: "prod", Action: "delete", Status: "accepted"}
	wantKeys := []string{"action", "cluster", "id", "location", "name", "resource", "status"}
	for name, f := range map[string]*cmdutil.TestFactory{
		"json":  jsonFactory(newFakeClient(t, kcFixtureAPI())),
		"agent": agentFactory(newFakeClient(t, kcFixtureAPI())),
	} {
		out, _, err := runMK8s(t, f, "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod", "-l", "FIN-03", "--yes")
		if err != nil {
			t.Fatal(err)
		}
		if got := decodeJSON[ActionResult](t, out); got != want {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
		if got := sortedKeys(decodeJSON[map[string]any](t, out)); !slices.Equal(got, wantKeys) {
			t.Errorf("%s: keys = %v", name, got)
		}
	}

	out, _, err := runMK8s(t, yamlFactory(newFakeClient(t, kcFixtureAPI())), "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod", "-l", "FIN-03", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	var got ActionResult
	if err := yaml.Unmarshal([]byte(out), &got); err != nil || got != want {
		t.Errorf("yaml: got %+v (%v), want %+v", got, err, want)
	}
}

func TestKubeconfigDelete_NeedsYesOffTerminalBeforeAnyCall(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*fakeAPI, *testing.T) *cmdutil.TestFactory{
		"agent":          func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return agentFactory(newFakeClient(t, api)) },
		"json on a pipe": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
		"table on a pipe": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory {
			return tableFactory(newFakeClient(t, api), nil)
		},
	} {
		api := kcFixtureAPI()
		_, _, err := runMK8s(t, mk(api, t), "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod")
		if ae := cmdutil.ClassifyError(err); ae == nil || ae.Code != "CONFIRMATION_REQUIRED" {
			t.Errorf("%s: err = %v", name, err)
		}
		if n := len(api.requests); n != 0 {
			t.Errorf("%s: %d request(s) before the check", name, n)
		}
	}
}

func TestKubeconfigDelete_MissingTargets(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, kcFixtureAPI())
	_, _, err := runMK8s(t, agentFactory(client), "kubeconfig", "delete", "--yes")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster", "<id>"}) {
		t.Errorf("missing = %v", missing)
	}
	// Missing targets are reported before the missing --yes, as in node-pool delete.
	if _, _, err := runMK8s(t, agentFactory(client), "kubeconfig", "delete"); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("no args, no --yes: %v", err)
	}
	if _, _, err := runMK8s(t, agentFactory(client), "kubeconfig", "delete", " ", "--cluster", "prod", "--yes"); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("blank id: %v", err)
	}
}

func TestKubeconfigDelete_Interactive(t *testing.T) {
	withTerminal(t)

	t.Run("the warning says copies are not revoked", func(t *testing.T) {
		api := kcFixtureAPI()
		out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddConfirm(true)), "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"Deletes kubeconfig prod-kc-aaa.", "Copies already downloaded are NOT revoked", "keep working until they expire.", "Deleting the cluster invalidates them", "does not use system:masters", "cannot be undone"} {
			if !strings.Contains(errOut, s) {
				t.Errorf("warning misses %q:\n%s", s, errOut)
			}
		}
		if !strings.Contains(out, "still work until they expire.") {
			t.Errorf("stdout = %q", out)
		}
		if n := len(api.requestsMatching("DELETE")); n != 1 {
			t.Errorf("DELETEs = %d", n)
		}
	})
}

func TestKubeconfigDelete_InteractivePickers(t *testing.T) {
	withTerminal(t)

	t.Run("a picked kubeconfig names its expiry", func(t *testing.T) {
		api := kcFixtureAPI()
		p := &kcSelectRecorder{Prompter: pickerAnswers(0, 0).AddConfirm(true)}
		f := tableFactory(newFakeClient(t, api), nil)
		f.PrompterOverride = p
		out, errOut, err := runMK8s(t, f, "kubeconfig", "delete")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut, "keep working until 2027-02-17 09:30 UTC.") || !strings.Contains(out, "still work until 2027-02-17 09:30 UTC.") {
			t.Errorf("stdout = %q\nstderr = %q", out, errOut)
		}
		if dels := api.requestsMatching("DELETE"); len(dels) != 1 || dels[0].Path != kcDeletePath {
			t.Errorf("DELETE requests = %v", dels)
		}
		if len(p.calls) != 2 || p.calls[1].Message != "Select kubeconfig" || !p.calls[1].Hints || p.calls[1].Labels[len(p.calls[1].Labels)-1] != cancelLabel {
			t.Errorf("picker calls = %+v", p.calls)
		}
	})

	t.Run("a picked pending kubeconfig has no expiry yet", func(t *testing.T) {
		api := kcFixtureAPI()
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), pickerAnswers(0, 1).AddConfirm(true)), "kubeconfig", "delete")
		if err != nil || !strings.Contains(errOut, "keep working until they expire.") {
			t.Fatalf("err=%v stderr=%q", err, errOut)
		}
		if dels := api.requestsMatching("DELETE"); len(dels) != 1 || !strings.HasSuffix(dels[0].Path, "/prod-kc-bbb") {
			t.Errorf("DELETE requests = %v", dels)
		}
	})

	t.Run("--yes skips the confirmation but not the pickers", func(t *testing.T) {
		api := kcFixtureAPI()
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), pickerAnswers(0, 0)), "kubeconfig", "delete", "--yes"); err != nil {
			t.Fatal(err)
		}
		if n := len(api.requestsMatching("DELETE")); n != 1 {
			t.Errorf("DELETEs = %d", n)
		}
	})
}

func TestKubeconfigDelete_InteractiveDeclined(t *testing.T) {
	withTerminal(t)

	t.Run("declined, Esc and Ctrl+C delete nothing", func(t *testing.T) {
		for name, p := range map[string]*cancelPrompter{
			"declined": {Prompter: confirmNo()},
			"esc":      {Prompter: tuitest.New(), confirmErr: contextCanceled()},
			"ctrl+c":   {Prompter: tuitest.New(), confirmErr: errInterrupted()},
		} {
			api := kcFixtureAPI()
			f := tableFactory(newFakeClient(t, api), nil)
			f.PrompterOverride = p
			out, errOut, err := runMK8s(t, f, "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod")
			if err != nil || out != "" || !strings.Contains(errOut, canceledMsg) || len(api.requestsMatching("DELETE")) != 0 {
				t.Errorf("%s: out=%q err=%q error=%v", name, out, errOut, err)
			}
		}
	})
}

func TestKubeconfigDelete_InteractivePickerCancelAndEmpty(t *testing.T) {
	withTerminal(t)

	t.Run("picker cancel paths delete nothing", func(t *testing.T) {
		for name, p := range map[string]*cancelPrompter{
			"cancel row": {Prompter: pickerAnswers(2)},
			"esc":        {Prompter: tuitest.New(), selectErr: contextCanceled()},
			"ctrl+c":     {Prompter: tuitest.New(), selectErr: errInterrupted()},
		} {
			api := kcFixtureAPI()
			f := tableFactory(newFakeClient(t, api), nil)
			f.PrompterOverride = p
			out, errOut, err := runMK8s(t, f, "kubeconfig", "delete", "--cluster", "prod")
			if err != nil || out != "" || !strings.Contains(errOut, canceledMsg) || len(api.requestsMatching("DELETE")) != 0 {
				t.Errorf("%s: out=%q err=%q error=%v", name, out, errOut, err)
			}
		}
	})

	t.Run("no kubeconfigs", func(t *testing.T) {
		api := seededClusters()
		out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New()), "kubeconfig", "delete", "--cluster", "prod")
		if err != nil || !strings.Contains(out, "No kubeconfigs in cluster prod.") || !strings.Contains(errOut, "kubeconfig create --cluster prod") || len(api.requestsMatching("DELETE")) != 0 {
			t.Errorf("out=%q err=%q error=%v", out, errOut, err)
		}
	})
}

func TestKubeconfigDelete_Errors(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, kcFixtureAPI()), nil), "kubeconfig", "delete", "ghost", "--cluster", "prod", "--yes")
	if agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown id: %v", err)
	}
	_, _, err = runMK8s(t, tableFactory(newFakeClient(t, kcFixtureAPI()), nil), "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "ghost", "--yes")
	if agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown cluster: %v", err)
	}
	api := newFakeAPI()
	api.disabled = true
	_, _, err = runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "delete", "x", "--cluster", "prod", "-l", "FIN-03", "--yes")
	if err == nil || agentErrorCode(err) != "AUTH_ERROR" || !strings.Contains(err.Error(), "not enabled for this project") {
		t.Errorf("not enabled: %v", err)
	}
}

func TestKubeconfigDelete_Debug(t *testing.T) {
	t.Parallel()
	f := tableFactory(newFakeClient(t, kcFixtureAPI()), nil)
	f.DebugOverride = true
	_, errOut, err := runMK8s(t, f, "kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod", "-l", "FIN-03", "--yes")
	if err != nil || !strings.Contains(errOut, "Delete kubeconfig:") || !strings.Contains(errOut, `"id": "prod-kc-aaa"`) {
		t.Errorf("err=%v debug=%q", err, errOut)
	}
	assertNoPrivateKey(t, "delete debug", errOut)
}
