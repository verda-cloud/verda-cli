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
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"go.yaml.in/yaml/v3"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// kcYAML is the YAML the fake serves; kcPrivateKeyMarker stands for the private key.
const kcYAML = "apiVersion: v1\nkind: Config\nusers:\n- name: alice\n  user:\n    client-key-data: " + kcPrivateKeyMarker + "\n"

const kcCreateRoute = "POST /mk8sclusters/{loc}/{name}/kubeconfigs"

// kcReadyOnGet makes every kubeconfig GET answer ready with the YAML.
func kcReadyOnGet(api *fakeAPI) {
	api.onKubeconfigGet = func(k *verda.ManagedKubernetesKubeconfig) {
		na := testCreated.Add(90 * 24 * time.Hour)
		k.Status, k.Kubeconfig, k.NotAfter = verda.ManagedKubernetesKubeconfigStatusReady, kcYAML, &na
	}
}

func kcAPI() *fakeAPI {
	api := seededClusters()
	kcReadyOnGet(api)
	return api
}

// kcArgs are the flags of a valid non-interactive create writing to file.
func kcArgs(file string, extra ...string) []string {
	args := []string{"kubeconfig", "create", "--cluster", "prod", "--common-name", "alice", "--admin", "--file", file}
	return append(args, extra...)
}

func kcPosts(api *fakeAPI) int { return len(api.requestsMatching("POST")) }

// kcAssertErrNoKey checks the error text and, for an AgentError, its serialized details.
func kcAssertErrNoKey(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	assertNoPrivateKey(t, "error", err.Error())
	if ae := cmdutil.ClassifyError(err); ae != nil {
		b, _ := json.Marshal(ae)
		assertNoPrivateKey(t, "agent error", string(b))
	}
}

func kcBody(t *testing.T, api *fakeAPI) map[string]any {
	t.Helper()
	return postBodyAny(t, api)
}

func TestKubeconfigCreate_WritesFileInEveryOutputMode(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*verda.Client) *cmdutil.TestFactory{
		"table": func(c *verda.Client) *cmdutil.TestFactory { return tableFactory(c, nil) },
		"json":  jsonFactory,
		"yaml":  yamlFactory,
		"agent": agentFactory,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := kcAPI()
			file := filepath.Join(t.TempDir(), "kc.yaml")
			out, errOut, err := runMK8s(t, mk(newFakeClient(t, api)), kcArgs(file)...)
			if err != nil {
				t.Fatal(err)
			}
			assertNoPrivateKey(t, "stdout", out)
			assertNoPrivateKey(t, "stderr", errOut)
			if got := readFile(t, file); got != kcYAML {
				t.Errorf("file = %q", got)
			}
			assertMode(t, file, 0o600)
			if n := kcPosts(api); n != 1 {
				t.Errorf("POSTs = %d, want 1", n)
			}
			switch name {
			case "table":
				if !strings.Contains(out, "Kubeconfig prod-kc-1 saved to "+file) {
					t.Errorf("stdout = %q", out)
				}
				if !strings.Contains(errOut, "export KUBECONFIG="+file) || !strings.Contains(errOut, "kubectl get nodes") {
					t.Errorf("stderr = %q", errOut)
				}
			case "yaml":
				var v KubeconfigView
				if err := yaml.Unmarshal([]byte(out), &v); err != nil || v.File != file || v.ID != "prod-kc-1" || v.Status != "ready" {
					t.Errorf("view = %+v err=%v", v, err)
				}
			default:
				v := decodeJSON[KubeconfigView](t, out)
				if v.File != file || v.CommonName != "alice" || !slices.Equal(v.Groups, []string{groupAdmin}) || v.NotAfter == nil {
					t.Errorf("view = %+v", v)
				}
				if errOut != "" {
					t.Errorf("stderr = %q, want empty", errOut)
				}
			}
		})
	}
}

func TestKubeconfigCreate_RequestBody(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		flags  []string
		groups []string
		extra  map[string]any
	}{
		"admin":        {flags: []string{"--admin"}, groups: []string{groupAdmin}},
		"groups":       {flags: []string{"--group", "dev", "--group", "ops"}, groups: []string{"dev", "ops"}},
		"both":         {flags: []string{"--admin", "--group", "dev"}, groups: []string{groupAdmin, "dev"}},
		"admin twice":  {flags: []string{"--admin", "--group", groupAdmin}, groups: []string{groupAdmin}},
		"duration":     {flags: []string{"--admin", "--duration", "720h"}, groups: []string{groupAdmin}, extra: map[string]any{"duration": "720h"}},
		"display name": {flags: []string{"--group", "dev", "--display-name", "Alice laptop"}, groups: []string{"dev"}, extra: map[string]any{"display_name": "Alice laptop"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := kcAPI()
			file := filepath.Join(t.TempDir(), "kc.yaml")
			args := []string{"kubeconfig", "create", "--cluster", "prod", "--common-name", "alice", "--file", file}
			if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), append(args, tc.flags...)...); err != nil {
				t.Fatal(err)
			}
			body := kcBody(t, api)
			got, _ := body["groups"].([]any)
			var groups []string
			for _, g := range got {
				groups = append(groups, g.(string))
			}
			if body["common_name"] != "alice" || !slices.Equal(groups, tc.groups) {
				t.Errorf("body = %v", body)
			}
			for _, key := range []string{"duration", "display_name"} {
				want, has := tc.extra[key]
				if got, ok := body[key]; ok != has || (has && got != want) {
					t.Errorf("%s = %v (present %v), want %v (present %v)", key, got, ok, want, has)
				}
			}
			if reqs := api.requestsMatching("POST"); reqs[0].Path != "/mk8sclusters/FIN-03/prod/kubeconfigs" {
				t.Errorf("path = %s", reqs[0].Path)
			}
		})
	}
}

func TestKubeconfigCreate_LocationFlagSkipsLookup(t *testing.T) {
	t.Parallel()
	api := kcAPI()
	file := filepath.Join(t.TempDir(), "kc.yaml")
	if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(file, "-l", "FIN-03")...); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(api.paths("GET"), "/mk8sclusters") {
		t.Errorf("-l must skip the cluster lookup, GETs = %v", api.paths("GET"))
	}
}

func TestKubeconfigCreate_DefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	api := kcAPI()
	args := []string{"kubeconfig", "create", "--cluster", "prod", "--common-name", "alice", "--admin"}
	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), args...)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".kube", "verda-prod.alice.yaml")
	if got := readFile(t, want); got != kcYAML {
		t.Errorf("file = %q", got)
	}
	assertMode(t, want, 0o600)
	if !strings.Contains(out, want) || !strings.Contains(errOut, "export KUBECONFIG="+want) {
		t.Errorf("out=%q err=%q", out, errOut)
	}
}

func TestKubeconfigCreate_ExportLineQuotesThePath(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "my kube's.yaml")
	_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, kcAPI()), nil), kcArgs(file)...)
	if err != nil {
		t.Fatal(err)
	}
	if want := "export KUBECONFIG=" + shellQuote(file); !strings.Contains(errOut, want) || !strings.Contains(errOut, "'\\''") {
		t.Errorf("stderr = %q, want %q", errOut, want)
	}
}

func TestKubeconfigCreate_ExistingTargetStopsBeforeAnyPOST(t *testing.T) {
	t.Parallel()
	t.Run("file without force", func(t *testing.T) {
		t.Parallel()
		api := kcAPI()
		file := filepath.Join(t.TempDir(), "kc.yaml")
		if err := os.WriteFile(file, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(file)...)
		if err == nil || !strings.Contains(err.Error(), "--force") || !isUsageError(err) {
			t.Fatalf("err = %v", err)
		}
		if n := kcPosts(api); n != 0 {
			t.Errorf("%d POST(s): a certificate was issued for an unwritable path", n)
		}
		if got := readFile(t, file); got != "old" {
			t.Errorf("file = %q", got)
		}
	})
	t.Run("directory", func(t *testing.T) {
		t.Parallel()
		api := kcAPI()
		_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(t.TempDir(), "--force")...)
		if err == nil || !strings.Contains(err.Error(), "is a directory") || !isUsageError(err) || kcPosts(api) != 0 {
			t.Errorf("err = %v, POSTs = %d", err, kcPosts(api))
		}
	})
	t.Run("symlink", func(t *testing.T) {
		t.Parallel()
		skipModeTestOnWindows(t)
		api := kcAPI()
		dir := t.TempDir()
		link := filepath.Join(dir, "link.yaml")
		if err := os.Symlink(filepath.Join(dir, "elsewhere"), link); err != nil {
			t.Fatal(err)
		}
		_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(link, "--force")...)
		if err == nil || !strings.Contains(err.Error(), "symlink") || !isUsageError(err) || kcPosts(api) != 0 {
			t.Errorf("err = %v, POSTs = %d", err, kcPosts(api))
		}
	})
	t.Run("missing parent directory", func(t *testing.T) {
		t.Parallel()
		api := kcAPI()
		_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(filepath.Join(t.TempDir(), "nope", "kc.yaml"))...)
		if err == nil || !isUsageError(err) || kcPosts(api) != 0 {
			t.Errorf("err = %v, POSTs = %d", err, kcPosts(api))
		}
	})
}

func TestKubeconfigCreate_ForceReplacesTheFile(t *testing.T) {
	t.Parallel()
	api := kcAPI()
	file := filepath.Join(t.TempDir(), "kc.yaml")
	if err := os.WriteFile(file, []byte("old"), 0o644); err != nil { //nolint:gosec // a loose mode proves the replacement is 0600
		t.Fatal(err)
	}
	_, errOut, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(file, "--force")...)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, file); got != kcYAML {
		t.Errorf("file = %q", got)
	}
	assertMode(t, file, 0o600)
	if want := "still valid. Run `verda mk8s kubeconfig list --cluster prod -l FIN-03`"; !strings.Contains(errOut, want) {
		t.Errorf("stderr misses the hint %q: %q", want, errOut)
	}

	// --force with no file in the way says nothing.
	_, errOut, err = runMK8s(t, jsonFactory(newFakeClient(t, kcAPI())), kcArgs(filepath.Join(t.TempDir(), "new.yaml"), "--force")...)
	if err != nil || strings.Contains(errOut, "still valid") {
		t.Errorf("err=%v stderr=%q", err, errOut)
	}
}

func TestKubeconfigCreate_FileDashWritesOnlyTheYAMLToStdout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	api := kcAPI()
	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), kcArgs("-")...)
	if err != nil {
		t.Fatal(err)
	}
	if out != kcYAML {
		t.Errorf("stdout = %q, want exactly the YAML", out)
	}
	assertNoPrivateKey(t, "stderr", errOut)
	if _, err := os.Stat(filepath.Join(home, ".kube")); err == nil {
		t.Error("--file - must not write a file")
	}
}

func TestKubeconfigCreate_FileDashRejectedWithStructuredOutput(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*verda.Client) *cmdutil.TestFactory{
		"json": jsonFactory, "yaml": yamlFactory, "agent": agentFactory,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := kcAPI()
			out, _, err := runMK8s(t, mk(newFakeClient(t, api)), kcArgs("-")...)
			if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) {
				t.Fatalf("err = %v", err)
			}
			if out != "" || len(api.requests) != 0 {
				t.Errorf("stdout = %q, requests = %v: must fail before any API call", out, api.requests)
			}
		})
	}
}

func TestKubeconfigCreate_MissingFlags(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*verda.Client) *cmdutil.TestFactory{
		"table": func(c *verda.Client) *cmdutil.TestFactory { return tableFactory(c, nil) },
		"agent": agentFactory,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api := kcAPI()
			f := mk(newFakeClient(t, api))
			_, _, err := runMK8s(t, f, "kubeconfig", "create")
			ae := cmdutil.ClassifyError(err)
			if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
				t.Fatalf("err = %v", err)
			}
			if m, _ := ae.Details["missing"].([]string); !slices.Equal(m, []string{"--cluster", "--common-name"}) {
				t.Errorf("missing = %v", ae.Details)
			}
			if o, _ := ae.Details["one_of"].([]string); !slices.Equal(o, []string{"--admin", "--group"}) {
				t.Errorf("one_of = %v", ae.Details)
			}
			if want := "required flags not provided: --cluster, --common-name; set at least one of --admin, --group"; ae.Message != want {
				t.Errorf("message = %q, want %q", ae.Message, want)
			}
			// With the rest given, a missing access level alone is reported.
			_, _, err = runMK8s(t, f, "kubeconfig", "create", "--cluster", "prod", "--common-name", "alice")
			ae = cmdutil.ClassifyError(err)
			if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" || !strings.Contains(ae.Message, "--admin, --group") {
				t.Errorf("no access: err = %v", err)
			}
			// Without a common name only that is missing.
			_, _, err = runMK8s(t, f, "kubeconfig", "create", "--cluster", "prod", "--admin")
			if m, _ := cmdutil.ClassifyError(err).Details["missing"].([]string); !slices.Equal(m, []string{"--common-name"}) {
				t.Errorf("no common name: err = %v", err)
			}
			if len(api.requests) != 0 {
				t.Errorf("requests = %v", api.requests)
			}
		})
	}
}

func TestKubeconfigCreate_InvalidInput(t *testing.T) {
	t.Parallel()
	api := kcAPI()
	file := filepath.Join(t.TempDir(), "kc.yaml")
	_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(file, "--wait-timeout", "0s")...)
	if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) {
		t.Errorf("zero wait timeout: %v", err)
	}
	for name, extra := range map[string][]string{
		"duration under 1h":      {"--duration", "30m"},
		"system: common name":    {"--common-name", "system:admin"},
		"system: group":          {"--group", "system:nodes"},
		"newline in the display": {"--display-name", "a\nb"},
	} {
		_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(file, extra...)...)
		if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Input validation comes before the file check.
	if err := os.WriteFile(file, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(file, "--duration", "30m")...); err == nil || !strings.Contains(err.Error(), "invalid kubeconfig") {
		t.Errorf("a bad duration must be reported before the taken file: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if usage := NewCmdKubeconfigCreate(nil, cmdutil.IOStreams{}).Flags().Lookup(flagDuration).Usage; !strings.Contains(usage, "cannot be revoked before it expires") {
		t.Errorf("--duration help = %q", usage)
	}
	args := kcArgs(file)
	args[slices.Index(args, "prod")] = "ghost"
	if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), args...); agentErrorCode(err) != "NOT_FOUND" {
		t.Errorf("unknown cluster: %v", err)
	}
	if n := kcPosts(api); n != 0 {
		t.Errorf("%d POST(s) for invalid input", n)
	}
}

func TestKubeconfigCreate_FailureSendsOnePOST(t *testing.T) {
	t.Parallel()
	t.Run("502 warns once", func(t *testing.T) {
		t.Parallel()
		api := kcAPI()
		api.failures[kcCreateRoute] = apiFailure{Status: 502, Body: `{"code":"bad_gateway","message":"upstream failed"}`}
		file := filepath.Join(t.TempDir(), "kc.yaml")
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), kcArgs(file)...)
		if err == nil || !strings.Contains(err.Error(), "upstream failed") {
			t.Fatalf("err = %v", err)
		}
		if !strings.Contains(errOut, "The kubeconfig may have been created anyway.") || !strings.Contains(errOut, "kubeconfig list --cluster prod -l FIN-03") {
			t.Errorf("stderr = %q", errOut)
		}
		if n := kcPosts(api); n != 1 {
			t.Errorf("POSTs = %d, want exactly 1", n)
		}
		if _, err := os.Stat(file); err == nil {
			t.Error("no file may be written when the create failed")
		}
	})
	t.Run("502 in agent mode puts the warning in the details", func(t *testing.T) {
		t.Parallel()
		api := kcAPI()
		api.failures[kcCreateRoute] = apiFailure{Status: 502, Body: `{"code":"bad_gateway","message":"upstream failed"}`}
		_, errOut, err := runMK8s(t, agentFactory(newFakeClient(t, api)), kcArgs(filepath.Join(t.TempDir(), "kc.yaml"))...)
		ae := cmdutil.ClassifyError(err)
		if ae == nil || !strings.Contains(ae.Details["warning"].(string), "may have been created anyway") {
			t.Fatalf("err = %v", err)
		}
		if errOut != "" || kcPosts(api) != 1 {
			t.Errorf("stderr = %q, POSTs = %d", errOut, kcPosts(api))
		}
	})
	t.Run("400 has no warning", func(t *testing.T) {
		t.Parallel()
		api := kcAPI()
		api.failures[kcCreateRoute] = apiFailure{Status: 400, Body: `{"code":"bad_request","message":"bad common name"}`}
		_, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), kcArgs(filepath.Join(t.TempDir(), "kc.yaml"))...)
		if err == nil || strings.Contains(errOut, "may have been created") || kcPosts(api) != 1 {
			t.Errorf("err = %v, stderr = %q, POSTs = %d", err, errOut, kcPosts(api))
		}
	})
	t.Run("403 not enabled", func(t *testing.T) {
		t.Parallel()
		api := kcAPI()
		api.disabled = true
		_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), kcArgs(filepath.Join(t.TempDir(), "kc.yaml"), "-l", "FIN-03")...)
		if agentErrorCode(err) != "AUTH_ERROR" || !strings.Contains(err.Error(), "not enabled") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestKubeconfigCreate_WaitTimeoutGivesRecovery(t *testing.T) {
	fastPoll(t)
	for _, agent := range []bool{false, true} {
		api := seededClusters() // never ready: the kubeconfig stays pending
		file := filepath.Join(t.TempDir(), "my kc.yaml")
		runFactory := tableFactory(newFakeClient(t, api), nil)
		if agent {
			runFactory = agentFactory(newFakeClient(t, api))
		}
		_, _, err := runMK8s(t, runFactory, kcArgs(file, "--wait-timeout", "150ms", "--force")...)
		if err == nil {
			t.Fatal("want a timeout")
		}
		msg := err.Error()
		saveCmd := "verda mk8s kubeconfig save prod-kc-1 --cluster prod -l FIN-03 --file '" + file + "' --force"
		for _, want := range []string{"prod-kc-1", saveCmd, "running create again issues another certificate", "last status: pending"} {
			if !strings.Contains(strings.ToLower(msg), strings.ToLower(want)) {
				t.Errorf("agent=%v: message %q lacks %q", agent, msg, want)
			}
		}
		if _, statErr := os.Stat(file); statErr == nil {
			t.Error("no file may be written before the kubeconfig is ready")
		}
		if n := kcPosts(api); n != 1 {
			t.Errorf("POSTs = %d", n)
		}
		if agent {
			ae := cmdutil.ClassifyError(err)
			if ae.Details["kubeconfig_id"] != "prod-kc-1" || ae.Details["save_command"] != saveCmd {
				t.Errorf("details = %v", ae.Details)
			}
		}
		kcAssertErrNoKey(t, err)
	}
}

func TestKubeconfigCreate_WriteFailureAfterCreateGivesRecovery(t *testing.T) {
	fastPoll(t)
	api := seededClusters()
	file := filepath.Join(t.TempDir(), "kc.yaml")
	// The file appears after the path check passed: the exclusive create refuses it.
	api.onKubeconfigGet = func(k *verda.ManagedKubernetesKubeconfig) {
		k.Status, k.Kubeconfig = verda.ManagedKubernetesKubeconfigStatusReady, kcYAML
		_ = os.WriteFile(file, []byte("raced"), 0o600)
	}
	_, errOut, err := runMK8s(t, agentFactory(newFakeClient(t, api)), kcArgs(file)...)
	if err == nil {
		t.Fatal("want a write error")
	}
	saveCmd := "verda mk8s kubeconfig save prod-kc-1 --cluster prod -l FIN-03 --file " + file
	if !strings.Contains(err.Error(), saveCmd) || !strings.Contains(err.Error(), "another certificate") {
		t.Errorf("err = %v", err)
	}
	ae := cmdutil.ClassifyError(err)
	if ae.Details["kubeconfig_id"] != "prod-kc-1" || ae.Details["save_command"] != saveCmd {
		t.Errorf("details = %v", ae.Details)
	}
	if got := readFile(t, file); got != "raced" {
		t.Errorf("the other file was replaced: %q", got)
	}
	kcAssertErrNoKey(t, err)
	assertNoPrivateKey(t, "stderr", errOut)

	// For a person the same text is the plain error.
	api = seededClusters()
	file = filepath.Join(t.TempDir(), "kc.yaml")
	api.onKubeconfigGet = func(k *verda.ManagedKubernetesKubeconfig) {
		k.Status, k.Kubeconfig = verda.ManagedKubernetesKubeconfigStatusReady, kcYAML
		_ = os.WriteFile(file, []byte("raced"), 0o600)
	}
	_, _, err = runMK8s(t, tableFactory(newFakeClient(t, api), nil), kcArgs(file)...)
	if err == nil || cmdutil.IsAgentError(err) || !strings.Contains(err.Error(), "prod-kc-1") {
		t.Errorf("human err = %v", err)
	}
}

func TestKubeconfigCreate_PendingAndStaleReadsDoNotEndTheWait(t *testing.T) {
	fastPoll(t)
	api := seededClusters()
	reads := 0
	api.onKubeconfigGet = func(k *verda.ManagedKubernetesKubeconfig) {
		reads++
		switch reads {
		case 1:
			k.Status = verda.ManagedKubernetesKubeconfigStatusPending
		case 2:
			// Ready without the YAML is a stale read.
			k.Status = verda.ManagedKubernetesKubeconfigStatusReady
		default:
			k.Status, k.Kubeconfig = verda.ManagedKubernetesKubeconfigStatusReady, kcYAML
		}
	}
	file := filepath.Join(t.TempDir(), "kc.yaml")
	out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(file)...)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, file); got != kcYAML {
		t.Errorf("file = %q", got)
	}
	if reads < 3 {
		t.Errorf("reads = %d, the wait ended early", reads)
	}
	assertNoPrivateKey(t, "stdout", out)
}

func TestKubeconfigCreate_DebugNeverLogsTheKey(t *testing.T) {
	t.Parallel()
	api := kcAPI()
	f := tableFactory(newFakeClient(t, api), nil)
	f.DebugOverride = true
	file := filepath.Join(t.TempDir(), "kc.yaml")
	out, errOut, err := runMK8s(t, f, kcArgs(file)...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "DEBUG: Request payload:") || !strings.Contains(errOut, "DEBUG: API response:") {
		t.Errorf("no debug output: %q", errOut)
	}
	assertNoPrivateKey(t, "stdout", out)
	assertNoPrivateKey(t, "debug output", errOut)

	f = agentFactory(newFakeClient(t, api))
	f.DebugOverride = true
	out, errOut, err = runMK8s(t, f, kcArgs(filepath.Join(t.TempDir(), "kc2.yaml"))...)
	if err != nil {
		t.Fatal(err)
	}
	assertNoPrivateKey(t, "agent stdout", out)
	assertNoPrivateKey(t, "agent debug output", errOut)
}

func TestKubeconfigCreate_StdoutDashDebugKeepsStdoutPure(t *testing.T) {
	t.Parallel()
	f := tableFactory(newFakeClient(t, kcAPI()), nil)
	f.DebugOverride = true
	out, errOut, err := runMK8s(t, f, kcArgs("-")...)
	if err != nil {
		t.Fatal(err)
	}
	if out != kcYAML {
		t.Errorf("stdout = %q", out)
	}
	assertNoPrivateKey(t, "debug output", errOut)
}

// A fresh machine has no ~/.kube: the default path passes the prompt check, and
// the recovery command printed after a timed-out create (which leaves out --file)
// creates ~/.kube with 0700.
func TestKubeconfigCreate_FreshHomeDefaultPathAndRecovery(t *testing.T) {
	fastPoll(t)
	home := kcHome(t)
	def := filepath.Join(home, ".kube", "verda-prod.alice.yaml")
	if err := kubeconfigFileValidator("prod", "alice", false)(def); err != nil {
		t.Errorf("the pre-filled default path must pass the prompt check: %v", err)
	}
	var ready atomic.Bool
	api := seededClusters()
	api.onKubeconfigGet = func(k *verda.ManagedKubernetesKubeconfig) {
		if ready.Load() {
			k.Status, k.Kubeconfig = verda.ManagedKubernetesKubeconfigStatusReady, kcYAML
		}
	}
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), "kubeconfig", "create", "--cluster", "prod", "--common-name", "alice", "--admin", "--wait-timeout", "100ms")
	ae := cmdutil.ClassifyError(err)
	saveCmd, _ := ae.Details["save_command"].(string)
	if saveCmd == "" || strings.Contains(saveCmd, "--file") {
		t.Fatalf("save_command = %q, want one without --file", saveCmd)
	}
	ready.Store(true)
	if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), strings.Fields(saveCmd)[2:]...); err != nil {
		t.Fatalf("recovery command %q failed: %v", saveCmd, err)
	}
	if got := readFile(t, def); got != kcYAML {
		t.Errorf("file = %q", got)
	}
	assertMode(t, filepath.Dir(def), 0o700)
}

// A certificate was issued, so the recovery text leads the error even when the
// classified API message ("no access") is about the failed read.
func TestKubeconfigCreate_ReadFailureKeepsTheRecoveryText(t *testing.T) {
	fastPoll(t)
	api := seededClusters()
	api.failures["GET /mk8sclusters/{loc}/{name}/kubeconfigs/{id}"] = apiFailure{Status: 403, Body: `{"code":"forbidden","message":"no access"}`}
	file := filepath.Join(t.TempDir(), "kc.yaml")
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), kcArgs(file)...)
	ae := cmdutil.ClassifyError(err)
	saveCmd := "verda mk8s kubeconfig save prod-kc-1 --cluster prod -l FIN-03 --file " + file
	if ae == nil || ae.Code != "AUTH_ERROR" || ae.Details["kubeconfig_id"] != "prod-kc-1" || ae.Details["save_command"] != saveCmd {
		t.Fatalf("err = %v, details = %v", err, ae)
	}
	if !strings.Contains(ae.Message, "Running create again issues another certificate") || !strings.Contains(ae.Message, saveCmd) {
		t.Errorf("message = %q, want the recovery text", ae.Message)
	}
}
