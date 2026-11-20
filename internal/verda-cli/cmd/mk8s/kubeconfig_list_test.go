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
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"go.yaml.in/yaml/v3"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// kcPrivateKeyMarker stands for the client key in the fake kubeconfig YAML: it must never be printed.
const kcPrivateKeyMarker = "MARKER-PRIVATE-KEY"

var kcExpiry = time.Date(2027, 2, 17, 9, 30, 0, 0, time.UTC)

// kcFixtureAPI has the clusters of seededClusters and two kubeconfigs in "prod": a
// ready one with groups and an expiry, and a pending one with neither.
func kcFixtureAPI() *fakeAPI {
	api := seededClusters()
	api.addKubeconfig("prod", verda.ManagedKubernetesKubeconfig{
		ID: "prod-kc-aaa", DisplayName: "alice-laptop", CommonName: "alice", Groups: []string{"system:masters", "dev"},
		Status: verda.ManagedKubernetesKubeconfigStatusReady, NotAfter: &kcExpiry,
		Kubeconfig: "apiVersion: v1\nusers:\n- user:\n    client-key-data: " + kcPrivateKeyMarker + "\n",
	})
	api.addKubeconfig("prod", verda.ManagedKubernetesKubeconfig{
		ID: "prod-kc-bbb", CommonName: "ci-bot", Status: verda.ManagedKubernetesKubeconfigStatusPending,
		Kubeconfig: "client-key-data: " + kcPrivateKeyMarker,
	})
	return api
}

func kcWantViews() []KubeconfigView {
	return []KubeconfigView{
		{ID: "prod-kc-aaa", DisplayName: "alice-laptop", CommonName: "alice", Groups: []string{"system:masters", "dev"}, Status: "ready", NotAfter: &kcExpiry},
		{ID: "prod-kc-bbb", CommonName: "ci-bot", Groups: []string{}, Status: "pending"},
	}
}

func TestKubeconfigList_Table(t *testing.T) {
	t.Parallel()
	api := kcFixtureAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "list", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a header and 2 rows:\n%s", out)
	}
	if got := strings.Join(strings.Fields(lines[0]), " "); got != "ID DISPLAY NAME COMMON NAME GROUPS STATUS EXPIRES" {
		t.Errorf("header = %q", got)
	}
	want := [][]string{
		{"prod-kc-aaa", "alice-laptop", "alice", "system:masters,dev", "ready", "2027-02-17", "09:30", "UTC"},
		{"prod-kc-bbb", "-", "ci-bot", "-", "pending", "-"},
	}
	for i, w := range want {
		if got := strings.Fields(lines[i+1]); !slices.Equal(got, w) {
			t.Errorf("row %d = %v, want %v", i, got, w)
		}
	}
	wantReqs := []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod/kubeconfigs"}
	if got := api.paths("GET"); !slices.Equal(got, wantReqs) {
		t.Errorf("requests = %v, want %v", got, wantReqs)
	}
}

func TestKubeconfigList_LocationSkipsTheClusterLookup(t *testing.T) {
	t.Parallel()
	api := kcFixtureAPI()
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "ls", "--cluster", "prod", "-l", "FIN-03"); err != nil {
		t.Fatal(err)
	}
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters/FIN-03/prod/kubeconfigs"}) {
		t.Errorf("requests = %v", got)
	}
}

func TestKubeconfigList_Structured(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*fakeAPI, *testing.T) *cmdutil.TestFactory{
		"json":  func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return jsonFactory(newFakeClient(t, api)) },
		"agent": func(api *fakeAPI, t *testing.T) *cmdutil.TestFactory { return agentFactory(newFakeClient(t, api)) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, _, err := runMK8s(t, mk(kcFixtureAPI(), t), "kubeconfig", "list", "--cluster", "prod")
			if err != nil {
				t.Fatal(err)
			}
			if got := decodeJSON[[]KubeconfigView](t, out); !reflect.DeepEqual(got, kcWantViews()) {
				t.Errorf("got  %+v\nwant %+v", got, kcWantViews())
			}
			for i, m := range decodeJSON[[]map[string]any](t, out) {
				if got := sortedKeys(m); !slices.Equal(got, []string{"common_name", "display_name", "groups", "id", "not_after", "status"}) {
					t.Errorf("item %d keys = %v", i, got)
				}
			}
		})
	}

	t.Run("yaml", func(t *testing.T) {
		t.Parallel()
		out, _, err := runMK8s(t, yamlFactory(newFakeClient(t, kcFixtureAPI())), "kubeconfig", "list", "--cluster", "prod")
		if err != nil {
			t.Fatal(err)
		}
		var got []map[string]any
		if err := yaml.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0]["id"] != "prod-kc-aaa" || got[1]["status"] != "pending" {
			t.Fatalf("got %v", got)
		}
		if groups, _ := got[0]["groups"].([]any); !reflect.DeepEqual(groups, []any{"system:masters", "dev"}) {
			t.Errorf("groups = %v", got[0]["groups"])
		}
		if groups, ok := got[1]["groups"].([]any); !ok || len(groups) != 0 {
			t.Errorf("pending groups = %#v, want an empty list", got[1]["groups"])
		}
		if v, ok := got[1]["not_after"]; !ok || v != nil {
			t.Errorf("pending not_after = %#v, want null", v)
		}
		if _, ok := got[0]["kubeconfig"]; ok {
			t.Error("the YAML key must not be in the output")
		}
	})
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestKubeconfigList_Empty(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededClusters())
	out, errOut, err := runMK8s(t, tableFactory(client, nil), "kubeconfig", "list", "--cluster", "prod")
	if err != nil || !strings.Contains(out, "No kubeconfigs in cluster prod.") || !strings.Contains(errOut, "verda mk8s kubeconfig create --cluster prod") {
		t.Errorf("out=%q err=%q error=%v", out, errOut, err)
	}
	for name, f := range map[string]*cmdutil.TestFactory{"json": jsonFactory(client), "agent": agentFactory(client)} {
		out, _, err = runMK8s(t, f, "kubeconfig", "list", "--cluster", "prod")
		if err != nil || strings.TrimSpace(out) != "[]" {
			t.Errorf("%s: empty output = %q, error=%v", name, out, err)
		}
	}
	out, _, err = runMK8s(t, yamlFactory(client), "kubeconfig", "list", "--cluster", "prod")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Errorf("yaml: empty output = %q, error=%v", out, err)
	}
}

func TestKubeconfigList_ClusterErrors(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, kcFixtureAPI())
	for _, f := range []*cmdutil.TestFactory{tableFactory(client, nil), agentFactory(client), jsonFactory(client)} {
		_, _, err := runMK8s(t, f, "kubeconfig", "list")
		ae := cmdutil.ClassifyError(err)
		if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
			t.Fatalf("err = %v", err)
		}
		if missing, _ := ae.Details["missing"].([]string); !slices.Equal(missing, []string{"--cluster"}) {
			t.Errorf("missing = %v", missing)
		}
	}
	if _, _, err := runMK8s(t, tableFactory(client, nil), "kubeconfig", "list", "--cluster", "ghost"); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown cluster: %v", err)
	}
	if _, _, err := runMK8s(t, tableFactory(client, nil), "kubeconfig", "list", "--cluster", " "); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("blank cluster: %v", err)
	}
	if _, _, err := runMK8s(t, tableFactory(client, nil), "kubeconfig", "list", "extra", "--cluster", "prod"); err == nil {
		t.Error("a positional argument must be rejected")
	}
}

func TestKubeconfigList_Debug(t *testing.T) {
	t.Parallel()
	f := tableFactory(newFakeClient(t, kcFixtureAPI()), nil)
	f.DebugOverride = true
	_, errOut, err := runMK8s(t, f, "kubeconfig", "list", "--cluster", "prod")
	if err != nil || !strings.Contains(errOut, "API response: 2 kubeconfig(s):") || !strings.Contains(errOut, `"common_name": "alice"`) {
		t.Errorf("err=%v debug=%q", err, errOut)
	}
	if strings.Contains(errOut, `"kubeconfig"`) {
		t.Errorf("debug output has a kubeconfig key: %q", errOut)
	}
}

func TestKubeconfigList_ClusterPicker(t *testing.T) {
	withTerminal(t)
	api := kcFixtureAPI()
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), pickerAnswers(0)), "kubeconfig", "list")
	if err != nil || !strings.Contains(out, "prod-kc-aaa") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if got := api.paths("GET"); !slices.Equal(got, []string{"/mk8sclusters", "/mk8sclusters/FIN-03/prod/kubeconfigs"}) {
		t.Errorf("requests = %v", got)
	}

	f := tableFactory(newFakeClient(t, kcFixtureAPI()), nil)
	f.PrompterOverride = &cancelPrompter{Prompter: pickerAnswers(), selectErr: contextCanceled()}
	out, errOut, err := runMK8s(t, f, "kubeconfig", "list")
	if err != nil || out != "" || !strings.Contains(errOut, canceledMsg) {
		t.Errorf("esc: out=%q err=%q error=%v", out, errOut, err)
	}
}

func TestKubeconfigList_NotEnabled(t *testing.T) {
	t.Parallel()
	api := newFakeAPI()
	api.disabled = true
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "kubeconfig", "list", "--cluster", "prod", "-l", "FIN-03")
	if err == nil || agentErrorCode(err) != "AUTH_ERROR" || !strings.Contains(err.Error(), "not enabled for this project") {
		t.Fatalf("err = %v", err)
	}
}

type kcSelectCall struct {
	Message string
	Labels  []string
	Hints   bool
}

// kcSelectRecorder answers like the wrapped prompter and keeps what each Select showed.
type kcSelectRecorder struct {
	*tuitest.Prompter
	calls []kcSelectCall
}

func (p *kcSelectRecorder) Select(ctx context.Context, msg string, labels []string, opts ...tui.SelectOption) (int, error) {
	p.calls = append(p.calls, kcSelectCall{Message: msg, Labels: labels, Hints: tui.ResolveSelectConfig(opts).ShowHints})
	return p.Prompter.Select(ctx, msg, labels, opts...)
}
