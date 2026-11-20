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

func TestClusterUpgrade_YesSendsTheNewVersion(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil),
		"cluster", "upgrade", "prod", "--kubernetes-version", "1.38.0", "--yes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Upgrade of cluster prod from 1.37.0 to 1.38.0 accepted (FIN-03).") {
		t.Errorf("stdout = %q", out)
	}
	for _, want := range []string{"verda mk8s cluster describe prod -l FIN-03", "`describe` may show `degraded` while the rollout runs."} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr misses %q: %q", want, errOut)
		}
	}

	patches := api.requestsMatching("PATCH")
	if len(patches) != 1 || patches[0].Path != "/mk8sclusters/FIN-03/prod" {
		t.Fatalf("PATCH requests = %v", patches)
	}
	if body := decodeJSON[map[string]string](t, patches[0].Body); len(body) != 1 || body["kubernetes_version"] != "1.38.0" {
		t.Errorf("body = %v", body)
	}
	// Name lookup, then the read for the "from" version, then the update. No wait.
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod"}) {
		t.Errorf("reads = %v", got)
	}
}

func TestClusterUpgrade_VersionIsTrimmed(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "upgrade", "prod", "--kubernetes-version", " 1.38.0 ", "--yes"); err != nil {
		t.Fatal(err)
	}
	if body := decodeJSON[map[string]string](t, api.requestsMatching("PATCH")[0].Body); body["kubernetes_version"] != "1.38.0" {
		t.Errorf("body = %v", body)
	}
}

func TestClusterUpgrade_Structured(t *testing.T) {
	t.Parallel()
	for name, f := range map[string]func(*fakeAPI, *testing.T) *cmdutil.TestFactory{
		"json":  func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
		"agent": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return agentFactory(newFakeClient(t, api)) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, _, err := runMK8s(t, f(seededClusters(), t), "cluster", "upgrade", "prod", "-l", "FIN-03", "--kubernetes-version", "1.38.0", "--yes")
			if err != nil {
				t.Fatal(err)
			}
			got := decodeJSON[map[string]any](t, out)
			if got["status"] != "accepted" || got["previous_version"] != "1.37.0" {
				t.Errorf("result = %v", got)
			}
			if c, _ := got["cluster"].(map[string]any); c["kubernetes_version"] != "1.38.0" {
				t.Errorf("cluster = %v", got["cluster"])
			}
		})
	}
}

func TestClusterUpgrade_NeedsYesOffTerminalBeforeAnyCall(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*fakeAPI, *testing.T) *cmdutil.TestFactory{
		"agent":          func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return agentFactory(newFakeClient(t, api)) },
		"json on a pipe": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
		"table on a pipe": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory {
			return tableFactory(newFakeClient(t, api), nil)
		},
	} {
		api := seededClusters()
		_, _, err := runMK8s(t, mk(api, t), "cluster", "upgrade", "prod", "--kubernetes-version", "1.38.0")
		if ae := cmdutil.ClassifyError(err); ae == nil || ae.Code != "CONFIRMATION_REQUIRED" {
			t.Errorf("%s: err = %v", name, err)
		}
		if n := len(api.requests); n != 0 {
			t.Errorf("%s: %d request(s) before the check", name, n)
		}
	}
}

func TestClusterUpgrade_MissingArguments(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, seededClusters())), "cluster", "upgrade", "--yes")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("err = %v", err)
	}
	if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"<cluster>", "--kubernetes-version"}) {
		t.Errorf("missing = %v", missing)
	}
}

func TestClusterUpgrade_Interactive(t *testing.T) {
	withTerminal(t)

	t.Run("confirm shows the rolling upgrade warning", func(t *testing.T) {
		api := seededClusters()
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddConfirm(true)),
			"cluster", "upgrade", "prod", "--kubernetes-version", "1.38.0")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut, "Rolling upgrade from 1.37.0 to 1.38.0") {
			t.Errorf("stderr = %q", errOut)
		}
		if n := len(api.requestsMatching("PATCH")); n != 1 {
			t.Errorf("PATCHes = %d", n)
		}
	})

	t.Run("version is asked when the flag is missing", func(t *testing.T) {
		api := seededClusters()
		p := tuitest.New().AddTextInput("1.39.0").AddConfirm(true)
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), p), "cluster", "upgrade", "prod"); err != nil {
			t.Fatal(err)
		}
		if body := decodeJSON[map[string]string](t, api.requestsMatching("PATCH")[0].Body); body["kubernetes_version"] != "1.39.0" {
			t.Errorf("body = %v", body)
		}
	})

	t.Run("picker then confirm", func(t *testing.T) {
		api := seededClusters()
		p := tuitest.New().AddSelect(1).AddConfirm(true)
		if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), p), "cluster", "upgrade", "--kubernetes-version", "1.38.0"); err != nil {
			t.Fatal(err)
		}
		if got := api.requestsMatching("PATCH")[0].Path; got != "/mk8sclusters/FIN-01/staging" {
			t.Errorf("PATCH path = %s", got)
		}
	})

	t.Run("declined, Esc and Ctrl+C change nothing", func(t *testing.T) {
		for name, p := range map[string]*cancelPrompter{
			"declined": {Prompter: tuitest.New().AddConfirm(false)},
			"esc":      {Prompter: tuitest.New(), confirmErr: contextCanceled()},
			"ctrl+c":   {Prompter: tuitest.New(), confirmErr: errInterrupted()},
		} {
			api := seededClusters()
			f := tableFactory(newFakeClient(t, api), nil)
			f.PrompterOverride = p
			_, errOut, err := runMK8s(t, f, "cluster", "upgrade", "prod", "--kubernetes-version", "1.38.0")
			if err != nil || !strings.Contains(errOut, canceledMsg) {
				t.Errorf("%s: err=%v stderr=%q", name, err, errOut)
			}
			if n := len(api.requestsMatching("PATCH")); n != 0 {
				t.Errorf("%s: %d PATCH(es)", name, n)
			}
		}
	})

	t.Run("canceling the version prompt changes nothing", func(t *testing.T) {
		api := seededClusters()
		f := tableFactory(newFakeClient(t, api), nil)
		f.PrompterOverride = &textCancelPrompter{Prompter: tuitest.New()}
		_, errOut, err := runMK8s(t, f, "cluster", "upgrade", "prod")
		if err != nil || !strings.Contains(errOut, canceledMsg) || len(api.requestsMatching("PATCH")) != 0 {
			t.Errorf("err=%v stderr=%q", err, errOut)
		}
	})
}

func TestClusterUpgrade_APIErrorsShownAsSent(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	api.failures["PATCH /mk8sclusters/{loc}/{name}"] = apiFailure{Status: 409, Body: `{"code":"conflict","message":"busy","details":"concurrent_modification"}`}
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "upgrade", "prod", "--kubernetes-version", "1.38.0", "--yes")
	if err == nil || !strings.Contains(err.Error(), "concurrent_modification") {
		t.Fatalf("err = %v", err)
	}

	_, _, err = runMK8s(t, tableFactory(newFakeClient(t, seededClusters()), nil), "cluster", "upgrade", "ghost", "--kubernetes-version", "1.38.0", "--yes")
	if agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown cluster: %v", err)
	}
}
