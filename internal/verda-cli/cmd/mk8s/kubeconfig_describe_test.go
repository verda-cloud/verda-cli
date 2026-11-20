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
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"go.yaml.in/yaml/v3"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestKubeconfigDescribe_JSONHasEveryFieldInItsPlace(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, kcFixtureAPI())), "kubeconfig", "describe", "prod-kc-aaa", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeJSON[KubeconfigView](t, out); !reflect.DeepEqual(got, kcWantViews()[0]) {
		t.Errorf("got  %+v\nwant %+v", got, kcWantViews()[0])
	}
	if got := sortedKeys(decodeJSON[map[string]any](t, out)); !slices.Equal(got, []string{"common_name", "display_name", "groups", "id", "not_after", "status"}) {
		t.Errorf("keys = %v: the view has no YAML and no file", got)
	}

	out, _, err = runMK8s(t, jsonFactory(newFakeClient(t, kcFixtureAPI())), "kubeconfig", "describe", "prod-kc-bbb", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeJSON[KubeconfigView](t, out); !reflect.DeepEqual(got, kcWantViews()[1]) {
		t.Errorf("pending: got %+v, want %+v", got, kcWantViews()[1])
	}
	raw := decodeJSON[map[string]any](t, out)
	if groups, ok := raw["groups"].([]any); !ok || len(groups) != 0 {
		t.Errorf("groups = %#v, want []", raw["groups"])
	}
	if v, ok := raw["not_after"]; !ok || v != nil {
		t.Errorf("not_after = %#v, want null", v)
	}
}

func TestKubeconfigDescribe_YAMLShape(t *testing.T) {
	t.Parallel()
	out, _, err := runMK8s(t, yamlFactory(newFakeClient(t, kcFixtureAPI())), "kubeconfig", "show", "prod-kc-aaa", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"id": "prod-kc-aaa", "display_name": "alice-laptop", "common_name": "alice",
		"groups": []any{"system:masters", "dev"}, "status": "ready", "not_after": kcExpiry,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

var kcDescribeLine = regexp.MustCompile(`^([A-Za-z][A-Za-z ]*):\s+(\S.*)$`)

func TestKubeconfigDescribe_Table(t *testing.T) {
	t.Parallel()
	api := kcFixtureAPI()
	parse := func(id string) map[string]string {
		out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "describe", id, "--cluster", "prod")
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for line := range strings.SplitSeq(out, "\n") {
			if m := kcDescribeLine.FindStringSubmatch(line); m != nil {
				got[m[1]] = strings.TrimSpace(m[2])
			}
		}
		return got
	}
	want := map[string]string{
		"ID": "prod-kc-aaa", "Display name": "alice-laptop", "Common name": "alice", "Groups": "system:masters,dev",
		"Status": "ready", "Expires": "2027-02-17 09:30 UTC", "Cluster": "prod", "Location": "FIN-03",
	}
	if got := parse("prod-kc-aaa"); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	want = map[string]string{
		"ID": "prod-kc-bbb", "Display name": "-", "Common name": "ci-bot", "Groups": "-",
		"Status": "pending", "Expires": "-", "Cluster": "prod", "Location": "FIN-03",
	}
	if got := parse("prod-kc-bbb"); !reflect.DeepEqual(got, want) {
		t.Errorf("pending: got %v\nwant %v", got, want)
	}
	wantReqs := []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod/kubeconfigs/prod-kc-aaa", "/mk8sclusters", "/mk8sclusters/FIN-03/prod/kubeconfigs/prod-kc-bbb"}
	if got := api.paths("GET"); !slices.Equal(got, wantReqs) {
		t.Errorf("requests = %v, want %v", got, wantReqs)
	}
}

func TestKubeconfigDescribe_AgentAndLocation(t *testing.T) {
	t.Parallel()
	api := kcFixtureAPI()
	out, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), "kubeconfig", "get", "prod-kc-aaa", "--cluster", "prod", "-l", "FIN-03")
	if err != nil {
		t.Fatal(err)
	}
	if v := decodeJSON[KubeconfigView](t, out); !reflect.DeepEqual(v, kcWantViews()[0]) {
		t.Errorf("view = %+v", v)
	}
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters/FIN-03/prod/kubeconfigs/prod-kc-aaa"}) {
		t.Errorf("-l must skip the cluster lookup, got %v", got)
	}
}

func TestKubeconfigDescribe_MissingTargetsAndErrors(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, kcFixtureAPI())
	for _, f := range []*cmdutil.TestFactory{agentFactory(client), tableFactory(client, nil)} {
		_, _, err := runMK8s(t, f, "kubeconfig", "describe")
		ae := cmdutil.ClassifyError(err)
		if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
			t.Fatalf("err = %v", err)
		}
		if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster", "<id>"}) {
			t.Errorf("missing = %v", missing)
		}
	}
	_, _, err := runMK8s(t, agentFactory(client), "kubeconfig", "describe", "prod-kc-aaa")
	if ae := cmdutil.ClassifyError(err); ae == nil || !slices.Equal(ae.Details["missing"].([]string), []string{"--cluster"}) {
		t.Errorf("only --cluster missing: %v", err)
	}
	_, _, err = runMK8s(t, agentFactory(client), "kubeconfig", "describe", "--cluster", "prod")
	if ae := cmdutil.ClassifyError(err); ae == nil || !slices.Equal(ae.Details["missing"].([]string), []string{"<id>"}) {
		t.Errorf("only <id> missing: %v", err)
	}
	if _, _, err := runMK8s(t, agentFactory(client), "kubeconfig", "describe", "  ", "--cluster", "prod"); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("blank id: %v", err)
	}
	if _, _, err := runMK8s(t, tableFactory(client, nil), "kubeconfig", "describe", "ghost", "--cluster", "prod"); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown id: %v", err)
	}
	if _, _, err := runMK8s(t, tableFactory(client, nil), "kubeconfig", "describe", "prod-kc-aaa", "--cluster", "ghost"); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown cluster: %v", err)
	}

	api := newFakeAPI()
	api.disabled = true
	_, _, err = runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "describe", "x", "--cluster", "prod", "-l", "FIN-03")
	if err == nil || agentErrorCode(err) != "AUTH_ERROR" || !strings.Contains(err.Error(), "not enabled for this project") {
		t.Errorf("not enabled: %v", err)
	}
}

func TestKubeconfigDescribe_Debug(t *testing.T) {
	t.Parallel()
	f := tableFactory(newFakeClient(t, kcFixtureAPI()), nil)
	f.DebugOverride = true
	_, errOut, err := runMK8s(t, f, "kubeconfig", "describe", "prod-kc-aaa", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"API response: kubeconfig:", `"id": "prod-kc-aaa"`, `"common_name": "alice"`, `"status": "ready"`} {
		if !strings.Contains(errOut, s) {
			t.Errorf("debug output misses %q:\n%s", s, errOut)
		}
	}
	if strings.Contains(errOut, `"kubeconfig"`) || strings.Contains(errOut, "client-key-data") {
		t.Errorf("debug output has the kubeconfig YAML:\n%s", errOut)
	}
}

func TestKubeconfigDescribe_Pickers(t *testing.T) {
	withTerminal(t)

	t.Run("cluster picker then kubeconfig picker", func(t *testing.T) {
		api := kcFixtureAPI()
		out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), pickerAnswers(0, 1)), "kubeconfig", "describe")
		if err != nil || !strings.Contains(out, "prod-kc-bbb") || !strings.Contains(out, "ci-bot") {
			t.Fatalf("out=%q err=%v", out, err)
		}
		// The list has every field of the view and never the YAML: no second read.
		if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod/kubeconfigs"}) {
			t.Errorf("requests = %v", got)
		}
	})

	t.Run("kubeconfig picker only", func(t *testing.T) {
		out, _, err := runMK8s(t, tableFactory(newFakeClient(t, kcFixtureAPI()), pickerAnswers(0)), "kubeconfig", "describe", "--cluster", "prod")
		if err != nil || !strings.Contains(out, "prod-kc-aaa") {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})

	t.Run("the picker offers every kubeconfig and Cancel", func(t *testing.T) {
		p := &kcSelectRecorder{Prompter: pickerAnswers(2)}
		f := tableFactory(newFakeClient(t, kcFixtureAPI()), nil)
		f.PrompterOverride = p
		if _, _, err := runMK8s(t, f, "kubeconfig", "describe", "--cluster", "prod"); err != nil {
			t.Fatal(err)
		}
		calls := p.calls
		if len(calls) != 1 || calls[0].Message != "Select kubeconfig" || !calls[0].Hints || len(calls[0].Labels) != 3 || calls[0].Labels[2] != cancelLabel {
			t.Fatalf("calls = %+v", calls)
		}
		if !strings.Contains(calls[0].Labels[0], "prod-kc-aaa") || !strings.Contains(calls[0].Labels[1], "prod-kc-bbb") {
			t.Errorf("labels = %v", calls[0].Labels)
		}
	})
}

func TestKubeconfigDescribe_PickerCancelAndEmpty(t *testing.T) {
	withTerminal(t)

	t.Run("cancel row, Esc and Ctrl+C end cleanly", func(t *testing.T) {
		for name, p := range map[string]*cancelPrompter{
			"cancel row": {Prompter: pickerAnswers(2)},
			"esc":        {Prompter: tuitest.New(), selectErr: contextCanceled()},
			"ctrl+c":     {Prompter: tuitest.New(), selectErr: errInterrupted()},
		} {
			f := tableFactory(newFakeClient(t, kcFixtureAPI()), nil)
			f.PrompterOverride = p
			out, errOut, err := runMK8s(t, f, "kubeconfig", "describe", "--cluster", "prod")
			if err != nil || out != "" || !strings.Contains(errOut, canceledMsg) {
				t.Errorf("%s: out=%q err=%q error=%v", name, out, errOut, err)
			}
		}
	})

	t.Run("no kubeconfigs", func(t *testing.T) {
		out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, seededClusters()), tuitest.New()), "kubeconfig", "describe", "--cluster", "prod")
		if err != nil || !strings.Contains(out, "No kubeconfigs in cluster prod.") || !strings.Contains(errOut, "kubeconfig create --cluster prod") {
			t.Errorf("out=%q err=%q error=%v", out, errOut, err)
		}
	})
}

// kcOutputFormats runs one command in each output mode, with --debug on.
func kcOutputFormats(client *verda.Client) map[string]*cmdutil.TestFactory {
	return map[string]*cmdutil.TestFactory{
		"table": tableFactory(client, nil),
		"json":  jsonFactory(client),
		"yaml":  yamlFactory(client),
		"agent": agentFactory(client),
	}
}

func assertNoPrivateKey(t *testing.T, label string, outs ...string) {
	t.Helper()
	for _, s := range outs {
		if strings.Contains(s, kcPrivateKeyMarker) || strings.Contains(s, "client-key-data") || strings.Contains(s, "apiVersion: v1") {
			t.Errorf("%s: the private key reached the output:\n%s", label, s)
		}
	}
}

// Security: the YAML (with the client key) never reaches stdout, stderr, --debug or an error, in any format.
func TestKubeconfigOutputNeverHasThePrivateKey(t *testing.T) {
	t.Parallel()
	commands := map[string][]string{
		"describe":       {"kubeconfig", "describe", "prod-kc-aaa", "--cluster", "prod"},
		"describe other": {"kubeconfig", "describe", "prod-kc-bbb", "--cluster", "prod", "-l", "FIN-03"},
		"list":           {"kubeconfig", "list", "--cluster", "prod"},
		"delete":         {"kubeconfig", "delete", "prod-kc-aaa", "--cluster", "prod", "--yes"},
		"describe 404":   {"kubeconfig", "describe", "ghost", "--cluster", "prod"},
	}
	for cname, args := range commands {
		for fname := range kcOutputFormats(nil) {
			t.Run(cname+"/"+fname, func(t *testing.T) {
				t.Parallel()
				f := kcOutputFormats(newFakeClient(t, kcFixtureAPI()))[fname]
				f.DebugOverride = true
				out, errOut, err := runMK8s(t, f, args...)
				errText := ""
				if err != nil {
					errText = err.Error()
				}
				assertNoPrivateKey(t, cname+"/"+fname, out, errOut, errText)
			})
		}
	}

	t.Run("API errors", func(t *testing.T) {
		t.Parallel()
		api := kcFixtureAPI()
		api.failures["GET /mk8sclusters/{loc}/{name}/kubeconfigs/{id}"] = apiFailure{Status: 500, Body: `{"code":"internal","message":"read failed"}`}
		f := agentFactory(newFakeClient(t, api))
		f.DebugOverride = true
		out, errOut, err := runMK8s(t, f, "kubeconfig", "describe", "prod-kc-aaa", "--cluster", "prod")
		if err == nil {
			t.Fatal("want an error")
		}
		assertNoPrivateKey(t, "500", out, errOut, err.Error())
	})
}

func TestKubeconfigPickerAndConfirmPathsNeverPrintTheKey(t *testing.T) {
	withTerminal(t)
	f := tableFactory(newFakeClient(t, kcFixtureAPI()), pickerAnswers(0, 0))
	f.DebugOverride = true
	out, errOut, err := runMK8s(t, f, "kubeconfig", "describe")
	if err != nil {
		t.Fatal(err)
	}
	assertNoPrivateKey(t, "describe pickers", out, errOut)

	f = tableFactory(newFakeClient(t, kcFixtureAPI()), pickerAnswers(0, 0).AddConfirm(true))
	f.DebugOverride = true
	out, errOut, err = runMK8s(t, f, "kubeconfig", "delete")
	if err != nil {
		t.Fatal(err)
	}
	assertNoPrivateKey(t, "delete pickers", out, errOut)
}
