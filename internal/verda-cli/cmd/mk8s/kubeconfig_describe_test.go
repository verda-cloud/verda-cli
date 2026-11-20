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
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// The view has no YAML and no file; a pending kubeconfig has groups [] and not_after null.
func TestKubeconfigDescribe_JSONHasEveryField(t *testing.T) {
	t.Parallel()
	for i, id := range []string{"prod-kc-aaa", "prod-kc-bbb"} {
		out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, kcFixtureAPI())), "kubeconfig", "describe", id, "--cluster", "prod")
		if err != nil {
			t.Fatal(err)
		}
		if got := decodeJSON[KubeconfigView](t, out); !reflect.DeepEqual(got, kcWantViews()[i]) || len(decodeJSON[map[string]any](t, out)) != 6 {
			t.Errorf("got  %s\nwant %+v", out, kcWantViews()[i])
		}
	}
}

var kcDescribeLine = regexp.MustCompile(`^([A-Za-z][A-Za-z ]*):\s+(\S.*)$`)

func TestKubeconfigDescribe_Table(t *testing.T) {
	t.Parallel()
	api := kcFixtureAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "describe", "prod-kc-aaa", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if m := kcDescribeLine.FindStringSubmatch(line); m != nil {
			got[m[1]] = strings.TrimSpace(m[2])
		}
	}
	want := map[string]string{
		"ID": "prod-kc-aaa", "Display name": "alice-laptop", "Common name": "alice", "Groups": "system:masters,dev",
		"Status": "ready", "Expires": "2027-02-17 09:30 UTC", "Cluster": "prod", "Location": "FIN-03",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod/kubeconfigs/prod-kc-aaa"}) {
		t.Errorf("requests = %v", got)
	}
}

func TestKubeconfigDescribe_MissingTargetsAndErrors(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, kcFixtureAPI())
	_, _, err := runMK8s(t, agentFactory(client), "kubeconfig", "describe")
	if m, _ := cmdutil.ClassifyError(err).Details["missing"].([]string); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" || !slices.Equal(m, []string{"--cluster", "<id>"}) {
		t.Errorf("err = %v, missing = %v", err, m)
	}
	if _, _, err := runMK8s(t, tableFactory(client, nil), "kubeconfig", "describe", "ghost", "--cluster", "prod"); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown id: %v", err)
	}
}

// Without --cluster and <id>, a terminal picks both; the list already has every
// field of the view, so the kubeconfig is not read again.
func TestKubeconfigDescribe_Picker(t *testing.T) {
	withTerminal(t)
	api := kcFixtureAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), pickerAnswers(0, 1)), "kubeconfig", "describe")
	if err != nil || !strings.Contains(out, "prod-kc-bbb") || !strings.Contains(out, "ci-bot") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod/kubeconfigs"}) {
		t.Errorf("requests = %v", got)
	}
}

// S1: the YAML (with the client key) never reaches stdout, stderr, --debug or
// an error, for a person or an agent.
func TestKubeconfigOutputNeverHasThePrivateKey(t *testing.T) {
	t.Parallel()
	failingRead := func() *fakeAPI {
		api := kcFixtureAPI()
		api.failures["GET /mk8sclusters/{loc}/{name}/kubeconfigs/{id}"] = apiFailure{Status: 500, Body: `{"code":"internal","message":"read failed"}`}
		return api
	}
	for cname, tc := range map[string]struct {
		api  func() *fakeAPI
		args func(file string) []string
	}{
		"create":       {kcAPI, func(file string) []string { return kcArgs(file) }},
		"save":         {func() *fakeAPI { return kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady) }, func(file string) []string { return kcSaveArgs(file) }},
		"describe":     {kcFixtureAPI, func(string) []string { return []string{"kubeconfig", "describe", "prod-kc-aaa", "--cluster", "prod"} }},
		"describe 500": {failingRead, func(string) []string { return []string{"kubeconfig", "describe", "prod-kc-aaa", "--cluster", "prod"} }},
	} {
		for fname, mk := range map[string]func(*verda.Client) *cmdutil.TestFactory{
			"table": func(c *verda.Client) *cmdutil.TestFactory { return tableFactory(c, nil) },
			"agent": agentFactory,
		} {
			f := mk(newFakeClient(t, tc.api()))
			f.DebugOverride = true
			out, errOut, err := runMK8s(t, f, tc.args(filepath.Join(t.TempDir(), "kc.yaml"))...)
			if (err != nil) != (cname == "describe 500") {
				t.Errorf("%s/%s: err = %v", cname, fname, err)
			}
			if err == nil && !strings.Contains(errOut, "DEBUG:") {
				t.Errorf("%s/%s: no debug output", cname, fname)
			}
			assertNoPrivateKey(t, cname+"/"+fname, out, errOut)
			kcAssertErrNoKey(t, err)
		}
	}
}
