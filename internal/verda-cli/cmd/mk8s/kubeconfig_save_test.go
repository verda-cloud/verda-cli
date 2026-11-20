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
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"go.yaml.in/yaml/v3"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

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

func TestKubeconfigSave_WritesFileInEveryOutputMode(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*verda.Client) *cmdutil.TestFactory{
		"table": func(c *verda.Client) *cmdutil.TestFactory { return tableFactory(c, nil) },
		"json":  jsonFactory,
		"yaml":  yamlFactory,
		"agent": agentFactory,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)
			file := filepath.Join(t.TempDir(), "kc.yaml")
			out, errOut, err := runMK8s(t, mk(newFakeClient(t, api)), kcSaveArgs(file)...)
			if err != nil {
				t.Fatal(err)
			}
			assertNoPrivateKey(t, "stdout", out)
			assertNoPrivateKey(t, "stderr", errOut)
			if got := readFile(t, file); got != kcYAML {
				t.Errorf("file = %q", got)
			}
			assertMode(t, file, 0o600)
			if n := kcPosts(api); n != 0 {
				t.Errorf("save made %d POST(s)", n)
			}
			switch name {
			case "table":
				if !strings.Contains(errOut, "export KUBECONFIG="+file) || !strings.Contains(errOut, "kubectl get nodes") {
					t.Errorf("stderr = %q", errOut)
				}
			case "yaml":
				var v KubeconfigView
				if err := yaml.Unmarshal([]byte(out), &v); err != nil || v.File != file || v.ID != "prod-kc-abc" {
					t.Errorf("view = %+v err=%v", v, err)
				}
			default:
				if v := decodeJSON[KubeconfigView](t, out); v.File != file || v.ID != "prod-kc-abc" || v.Status != "ready" {
					t.Errorf("view = %+v", v)
				}
			}
		})
	}
}

func TestKubeconfigSave_NotReady(t *testing.T) {
	t.Parallel()
	api := kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusPending)
	file := filepath.Join(t.TempDir(), "kc.yaml")
	out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), kcSaveArgs(file)...)
	if err == nil || err.Error() != "kubeconfig prod-kc-abc is not ready yet (status pending); run save again in a moment" {
		t.Fatalf("err = %v", err)
	}
	if out != "" {
		t.Errorf("stdout = %q", out)
	}
	if _, statErr := os.Stat(file); statErr == nil {
		t.Error("no file may be written for a kubeconfig that is not ready")
	}
}

func TestKubeconfigSave_ReadyWithoutYAMLIsNotReady(t *testing.T) {
	t.Parallel()
	api := kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)
	api.onKubeconfigGet = nil // ready, but the fake serves no YAML
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), kcSaveArgs(filepath.Join(t.TempDir(), "kc.yaml"))...)
	if err == nil || err.Error() != "kubeconfig prod-kc-abc is not ready yet (status ready, content not available yet); run save again in a moment" {
		t.Errorf("err = %v", err)
	}
}

func TestKubeconfigSave_FileDash(t *testing.T) {
	t.Parallel()
	api := kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)
	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), kcSaveArgs("-")...)
	if err != nil {
		t.Fatal(err)
	}
	if out != kcYAML {
		t.Errorf("stdout = %q, want exactly the YAML", out)
	}
	assertNoPrivateKey(t, "stderr", errOut)

	for name, f := range map[string]*cmdutil.TestFactory{
		"json":  jsonFactory(newFakeClient(t, api)),
		"yaml":  yamlFactory(newFakeClient(t, api)),
		"agent": agentFactory(newFakeClient(t, api)),
	} {
		before := len(api.requests)
		out, _, err := runMK8s(t, f, kcSaveArgs("-")...)
		if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) || out != "" {
			t.Errorf("%s: out=%q err=%v", name, out, err)
		}
		if len(api.requests) != before {
			t.Errorf("%s: --file - with structured output reached the API", name)
		}
	}
}

func TestKubeconfigSave_ExistingFile(t *testing.T) {
	t.Parallel()
	api := kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)
	file := filepath.Join(t.TempDir(), "kc.yaml")
	if err := os.WriteFile(file, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcSaveArgs(file)...)
	if err == nil || !strings.Contains(err.Error(), "--force") || !isUsageError(err) {
		t.Fatalf("err = %v", err)
	}
	kcAssertErrNoKey(t, err)
	if got := readFile(t, file); got != "old" {
		t.Errorf("file = %q", got)
	}
	_, errOut, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcSaveArgs(file, "--force")...)
	if err != nil || strings.Contains(errOut, "still valid") || strings.Contains(errOut, "kubeconfig list") {
		t.Fatalf("save issues no new certificate, so it must not advise deleting one: err=%v stderr=%q", err, errOut)
	}
	if got := readFile(t, file); got != kcYAML {
		t.Errorf("file = %q", got)
	}
	assertMode(t, file, 0o600)
}

func TestKubeconfigSave_DefaultPathUsesTheCommonName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
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
	for name, f := range map[string]*cmdutil.TestFactory{
		"table": tableFactory(newFakeClient(t, api), nil),
		"agent": agentFactory(newFakeClient(t, api)),
	} {
		_, _, err := runMK8s(t, f, "kubeconfig", "save")
		ae := cmdutil.ClassifyError(err)
		if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
			t.Fatalf("%s: err = %v", name, err)
		}
		if m, _ := ae.Details["missing"].([]string); len(m) != 2 || m[0] != "--cluster" || m[1] != "<id>" {
			t.Errorf("%s: missing = %v", name, ae.Details)
		}
	}
	_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), "kubeconfig", "save", "ghost", "--cluster", "prod", "--file", filepath.Join(t.TempDir(), "x"))
	if agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown id: %v", err)
	}
}

func TestKubeconfigSave_DebugNeverLogsTheKey(t *testing.T) {
	t.Parallel()
	f := tableFactory(newFakeClient(t, kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)), nil)
	f.DebugOverride = true
	out, errOut, err := runMK8s(t, f, kcSaveArgs(filepath.Join(t.TempDir(), "kc.yaml"))...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "DEBUG: Kubeconfig:") {
		t.Errorf("no debug output: %q", errOut)
	}
	assertNoPrivateKey(t, "stdout", out)
	assertNoPrivateKey(t, "debug output", errOut)
}

func TestKubeconfigSave_PickerWhenIDOmitted(t *testing.T) {
	withTerminal(t)
	t.Run("picks and writes", func(t *testing.T) {
		api := kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)
		file := filepath.Join(t.TempDir(), "kc.yaml")
		rec := &kcRecorder{Prompter: pickerAnswers(0)}
		_, _, err := runMK8s(t, kcTable(newFakeClient(t, api), rec), "kubeconfig", "save", "--cluster", "prod", "--file", file)
		if err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, file); got != kcYAML {
			t.Errorf("file = %q", got)
		}
		if len(rec.selects) != 1 || !rec.selects[0].hints {
			t.Errorf("selects = %+v, want one with hints", rec.selects)
		}
	})
	t.Run("cancel row and Ctrl+C", func(t *testing.T) {
		api := kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)
		file := filepath.Join(t.TempDir(), "kc.yaml")
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), pickerAnswers(1)), "kubeconfig", "save", "--cluster", "prod", "--file", file)
		if err != nil || !strings.Contains(errOut, "Canceled.") {
			t.Errorf("Cancel row: err=%v stderr=%q", err, errOut)
		}
		if _, statErr := os.Stat(file); statErr == nil {
			t.Error("the Cancel row must not write a file")
		}
		api = kcSaveAPI(verda.ManagedKubernetesKubeconfigStatusReady)
		p := &cancelPrompter{Prompter: tuitest.New(), selectErr: errInterrupted()}
		_, errOut, err = runMK8s(t, kcTable(newFakeClient(t, api), p), "kubeconfig", "save", "--cluster", "prod")
		if err != nil || !strings.Contains(errOut, "Canceled.") {
			t.Errorf("Ctrl+C: err=%v stderr=%q", err, errOut)
		}
	})
	t.Run("no kubeconfigs", func(t *testing.T) {
		api := seededClusters()
		out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New()), "kubeconfig", "save", "--cluster", "prod")
		if err != nil || !strings.Contains(out, "No kubeconfigs in cluster prod.") || !strings.Contains(errOut, "kubeconfig create --cluster prod") {
			t.Errorf("out=%q err=%q %v", out, errOut, err)
		}
	})
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
