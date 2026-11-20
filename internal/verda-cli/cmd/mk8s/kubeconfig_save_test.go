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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func kcSaveAPI(status string) *fakeAPI {
	api := seededClusters()
	api.addKubeconfig("prod", verda.ManagedKubernetesKubeconfig{
		ID: "prod-kc-abc", CommonName: "alice", Groups: []string{groupAdmin}, Status: status,
	})
	kcReadyOnGet(api)
	if status != verda.ManagedKubernetesKubeconfigStatusReady {
		api.onKubeconfigGet = nil
	}
	return api
}

func kcSaveArgs(file string, extra ...string) []string {
	args := []string{"kubeconfig", "save", "prod-kc-abc", "--cluster", "prod", "--file", file}
	return append(args, extra...)
}

// save issues no certificate: it reads the kubeconfig and writes the file.
func TestKubeconfigSave_WritesFile(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*verda.Client) *cmdutil.TestFactory{
		"table": func(c *verda.Client) *cmdutil.TestFactory { return tableFactory(c, nil) },
		"json":  jsonFactory,
	} {
		api := kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)
		file := filepath.Join(t.TempDir(), "kc.yaml")
		out, errOut, err := runMK8s(t, mk(newFakeClient(t, api)), kcSaveArgs(file)...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := readFile(t, file); got != kcYAML || kcPosts(api) != 0 {
			t.Errorf("%s: file = %q, POSTs = %d", name, got, kcPosts(api))
		}
		assertMode(t, file, 0o600)
		if name == "table" {
			if !strings.Contains(errOut, "export KUBECONFIG="+file) {
				t.Errorf("stderr = %q", errOut)
			}
			continue
		}
		if v := decodeJSON[KubeconfigView](t, out); v.File != file || v.ID != "prod-kc-abc" || v.Status != "ready" {
			t.Errorf("view = %+v", v)
		}
	}
}

func TestKubeconfigSave_NotReady(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status string
		want   string
	}{
		"pending":            {verda.ManagedKubernetesKubeconfigStatusPending, "status pending"},
		"ready without YAML": {verda.ManagedKubernetesKubeconfigStatusReady, "status ready, content not available yet"},
	} {
		api := kcSaveAPI(tc.status)
		api.onKubeconfigGet = nil // the fake serves no YAML
		file := filepath.Join(t.TempDir(), "kc.yaml")
		out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), kcSaveArgs(file)...)
		if want := "kubeconfig prod-kc-abc is not ready yet (" + tc.want + "); run save again in a moment"; err == nil || err.Error() != want || out != "" {
			t.Errorf("%s: err = %v, stdout = %q", name, err, out)
		}
		if _, statErr := os.Stat(file); statErr == nil {
			t.Errorf("%s: no file may be written", name)
		}
	}
}

func TestKubeconfigSave_DefaultPathUsesTheCommonName(t *testing.T) {
	home := setHome(t)
	api := kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)
	_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), "kubeconfig", "save", "prod-kc-abc", "--cluster", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(home, ".kube", "verda-prod.alice.yaml")); got != kcYAML {
		t.Errorf("file = %q", got)
	}
}

func TestKubeconfigSave_MissingAndUnknown(t *testing.T) {
	t.Parallel()
	api := kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), "kubeconfig", "save")
	if m, _ := cmdutil.ClassifyError(err).Details["missing"].([]string); agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" || !slices.Equal(m, []string{"--cluster", "<id>"}) {
		t.Errorf("err = %v, missing = %v", err, m)
	}
	_, _, err = runMK8s(t, jsonFactory(newFakeClient(t, api)), "kubeconfig", "save", "ghost", "--cluster", "prod", "--file", filepath.Join(t.TempDir(), "x"))
	if agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown id: %v", err)
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"/home/a/.kube/x.yaml": "/home/a/.kube/x.yaml",
		"-":                    "-",
		"a b":                  "'a b'",
		"it's":                 `'it'\''s'`,
		"$HOME/x":              "'$HOME/x'",
		"/a:/b":                "'/a:/b'", // ':' separates the paths of KUBECONFIG
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKubeconfigSaveCmd(t *testing.T) {
	t.Parallel()
	tgt := &clusterTarget{Name: "my cluster", Location: "FIN-03"}
	cases := map[string]struct {
		dest  *kubeconfigDest
		force bool
		want  string
	}{
		"custom file":  {&kubeconfigDest{target: kubeconfigTarget{path: "/tmp/a b.yaml"}}, true, "verda mk8s kubeconfig save kc-1 --cluster 'my cluster' -l FIN-03 --file '/tmp/a b.yaml' --force"},
		"default path": {&kubeconfigDest{target: kubeconfigTarget{path: "/h/.kube/x.yaml", defaultDir: true}}, false, "verda mk8s kubeconfig save kc-1 --cluster 'my cluster' -l FIN-03"},
		"stdout":       {&kubeconfigDest{stdout: true}, true, "verda mk8s kubeconfig save kc-1 --cluster 'my cluster' -l FIN-03"},
	}
	for name, tc := range cases {
		if got := kubeconfigSaveCmd(tgt, "kc-1", tc.dest, tc.force); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}
