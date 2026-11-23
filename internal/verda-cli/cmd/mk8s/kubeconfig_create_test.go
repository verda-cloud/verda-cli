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
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// kcYAML is the YAML the fake serves; kcPrivateKeyMarker stands for the private key.
const kcYAML = "apiVersion: v1\nkind: Config\nusers:\n- name: alice\n  user:\n    client-key-data: " + kcPrivateKeyMarker + "\n"

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

func TestKubeconfigCreate_WritesFileInEveryOutputMode(t *testing.T) {
	t.Parallel()
	for name, mk := range map[string]func(*verda.Client) *cmdutil.TestFactory{
		"table": func(c *verda.Client) *cmdutil.TestFactory { return tableFactory(c, nil) },
		"json":  jsonFactory,
	} {
		api := kcAPI()
		file := filepath.Join(t.TempDir(), "kc.yaml")
		out, errOut, err := runMK8s(t, mk(newFakeClient(t, api)), kcArgs(file)...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := readFile(t, file); got != kcYAML {
			t.Errorf("%s: file = %q", name, got)
		}
		assertMode(t, file, 0o600)
		if n := kcPosts(api); n != 1 {
			t.Errorf("%s: POSTs = %d, want 1", name, n)
		}
		if name == "table" {
			if !strings.Contains(out, "Kubeconfig prod-kc-1 saved to "+file) || !strings.Contains(errOut, "export KUBECONFIG="+file) {
				t.Errorf("out=%q stderr=%q", out, errOut)
			}
			continue
		}
		v := decodeJSON[KubeconfigView](t, out)
		if v.File != file || v.CommonName != "alice" || !slices.Equal(v.Groups, []string{groupAdmin}) || v.NotAfter == nil || errOut != "" {
			t.Errorf("view = %+v, stderr = %q", v, errOut)
		}
	}
}

func TestKubeconfigCreate_RequestBody(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		flags  []string
		groups []any
		extra  map[string]any
	}{
		"admin and a group": {flags: []string{"--admin", "--group", "dev", "--group", groupAdmin}, groups: []any{groupAdmin, "dev"}},
		"duration":          {flags: []string{"--admin", "--duration", "720h"}, groups: []any{groupAdmin}, extra: map[string]any{"duration": "720h"}},
		"display name":      {flags: []string{"--group", "dev", "--display-name", "Alice laptop"}, groups: []any{"dev"}, extra: map[string]any{"display_name": "Alice laptop"}},
	} {
		api := kcAPI()
		args := []string{"kubeconfig", "create", "--cluster", "prod", "--common-name", "alice", "--file", filepath.Join(t.TempDir(), "kc.yaml")}
		if _, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), append(args, tc.flags...)...); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		body := postBodyAny(t, api)
		if got, _ := body["groups"].([]any); body["common_name"] != "alice" || !slices.Equal(got, tc.groups) {
			t.Errorf("%s: body = %v", name, body)
		}
		for _, key := range []string{"duration", "display_name"} {
			want, has := tc.extra[key]
			if got, ok := body[key]; ok != has || (has && got != want) {
				t.Errorf("%s: %s = %v (present %v), want %v (present %v)", name, key, got, ok, want, has)
			}
		}
		if reqs := api.requestsMatching("POST"); reqs[0].Path != "/mk8sclusters/FIN-03/prod/kubeconfigs" {
			t.Errorf("%s: path = %s", name, reqs[0].Path)
		}
	}
}

func TestKubeconfigCreate_DefaultPath(t *testing.T) {
	home := setHome(t)
	args := []string{"kubeconfig", "create", "--cluster", "prod", "--common-name", "alice", "--admin"}
	out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, kcAPI()), nil), args...)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".kube", "verda-prod.alice.yaml")
	if got := readFile(t, want); got != kcYAML {
		t.Errorf("file = %q", got)
	}
	assertMode(t, want, 0o600)
	assertMode(t, filepath.Dir(want), 0o700)
	if !strings.Contains(out, want) || !strings.Contains(errOut, "export KUBECONFIG="+want) {
		t.Errorf("out=%q err=%q", out, errOut)
	}
}

// A path that cannot be written stops the command before a certificate is issued.
func TestKubeconfigCreate_ExistingTargetStopsBeforeAnyPOST(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "kc.yaml")
	if err := os.WriteFile(file, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"file without --force": {kcArgs(file), "--force"},
		"directory":            {kcArgs(t.TempDir(), "--force"), "is a directory"},
	} {
		api := kcAPI()
		_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !isUsageError(err) || kcPosts(api) != 0 {
			t.Errorf("%s: err = %v, POSTs = %d", name, err, kcPosts(api))
		}
	}
	if got := readFile(t, file); got != "old" {
		t.Errorf("file = %q", got)
	}
}

func TestKubeconfigCreate_ForceReplacesTheFile(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "kc.yaml")
	if err := os.WriteFile(file, []byte("old"), 0o644); err != nil { //nolint:gosec // a loose mode proves the replacement is 0600
		t.Fatal(err)
	}
	_, errOut, err := runMK8s(t, jsonFactory(newFakeClient(t, kcAPI())), kcArgs(file, "--force")...)
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

// --file - writes only the YAML to stdout, also with --debug, and no file.
func TestKubeconfigCreate_FileDashWritesOnlyTheYAML(t *testing.T) {
	home := setHome(t)
	f := tableFactory(newFakeClient(t, kcAPI()), nil)
	f.DebugOverride = true
	out, errOut, err := runMK8s(t, f, kcArgs("-")...)
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

// --file - would share stdout with the JSON: it fails before any API call.
func TestKubeconfigCreate_FileDashRejectedWithStructuredOutput(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"create": kcArgs("-"),
		"save":   {"kubeconfig", "save", "prod-kc-1", "--cluster", "prod", "--file", "-"},
	} {
		api := kcAPI()
		out, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), args...)
		if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) || out != "" || len(api.requests) != 0 {
			t.Errorf("%s: err = %v, stdout = %q, requests = %v", name, err, out, api.requests)
		}
	}
}

func TestKubeconfigCreate_MissingFlags(t *testing.T) {
	t.Parallel()
	api := kcAPI()
	_, _, err := runMK8s(t, agentFactory(newFakeClient(t, api)), "kubeconfig", "create")
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
	if len(api.requests) != 0 {
		t.Errorf("requests = %v", api.requests)
	}
}

func TestKubeconfigCreate_InvalidInput(t *testing.T) {
	t.Parallel()
	api := kcAPI()
	file := filepath.Join(t.TempDir(), "kc.yaml")
	for name, extra := range map[string][]string{
		"zero wait timeout": {"--wait-timeout", "0s"},
		"duration under 1h": {"--duration", "30m"},
		"system: group":     {"--group", "system:nodes"},
	} {
		_, _, err := runMK8s(t, jsonFactory(newFakeClient(t, api)), kcArgs(file, extra...)...)
		if agentErrorCode(err) != "VALIDATION_ERROR" || cmdutil.IsAgentError(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if n := len(api.requests); n != 0 {
		t.Errorf("%d request(s) for invalid input", n)
	}
}

// After CreateKubeconfig succeeded, a failed wait, read or write returns the
// kubeconfig ID and the exact save command; agents also get them in the details.
func TestKubeconfigCreate_RecoveryAfterCreate(t *testing.T) {
	fastPoll(t)
	raced := func(file string) func(*verda.ManagedKubernetesKubeconfig) {
		// The file appears after the path check passed: the exclusive create refuses it.
		return func(k *verda.ManagedKubernetesKubeconfig) {
			k.Status, k.Kubeconfig = verda.ManagedKubernetesKubeconfigStatusReady, kcYAML
			_ = os.WriteFile(file, []byte("raced"), 0o600)
		}
	}
	for name, tc := range map[string]struct {
		agent     bool
		base      string
		extra     []string
		hook      func(file string) func(*verda.ManagedKubernetesKubeconfig)
		readFails bool
		quoted    string // the --file part of the save command
		want      string
		fileLeft  string // "" means no file
	}{
		"wait timeout":        {agent: true, base: "my kc.yaml", extra: []string{"--wait-timeout", "150ms", "--force"}, want: "last status: pending"},
		"write failure":       {agent: true, base: "kc.yaml", hook: raced, want: "is ready but writing it failed", fileLeft: "raced"},
		"write failure human": {base: "kc.yaml", hook: raced, want: "is ready but writing it failed", fileLeft: "raced"},
		"read failure":        {agent: true, base: "kc.yaml", readFails: true, want: "its status could not be read (API error 403: no access)"},
	} {
		api := seededClusters() // never ready unless hook says so
		file := filepath.Join(t.TempDir(), tc.base)
		if tc.hook != nil {
			api.onKubeconfigGet = tc.hook(file)
		}
		if tc.readFails {
			api.failures["GET /mk8sclusters/{loc}/{name}/kubeconfigs/{id}"] = apiFailure{Status: 403, Body: `{"code":"forbidden","message":"no access"}`}
		}
		f := tableFactory(newFakeClient(t, api), nil)
		if tc.agent {
			f = agentFactory(newFakeClient(t, api))
		}
		_, errOut, err := runMK8s(t, f, kcArgs(file, tc.extra...)...)
		saveCmd := "verda mk8s kubeconfig save prod-kc-1 --cluster prod -l FIN-03 --file " + file
		if strings.Contains(file, " ") {
			saveCmd = "verda mk8s kubeconfig save prod-kc-1 --cluster prod -l FIN-03 --file '" + file + "' --force"
		}
		if err == nil || !strings.Contains(err.Error(), saveCmd) || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), kubeconfigAgainWarning) {
			t.Errorf("%s: err = %v", name, err)
		}
		if ae := cmdutil.ClassifyError(err); tc.agent != cmdutil.IsAgentError(err) || (tc.agent && (ae.Details["kubeconfig_id"] != "prod-kc-1" || ae.Details["save_command"] != saveCmd)) {
			t.Errorf("%s: agent error = %v", name, ae)
		}
		if got, statErr := os.ReadFile(file); string(got) != tc.fileLeft || (tc.fileLeft == "") != (statErr != nil) { //nolint:gosec // G304: file is under t.TempDir()
			t.Errorf("%s: file = %q (%v)", name, got, statErr)
		}
		if n := kcPosts(api); n != 1 {
			t.Errorf("%s: POSTs = %d", name, n)
		}
		kcAssertErrNoKey(t, err)
		assertNoPrivateKey(t, "stderr", errOut)
	}
}

// Pending, then ready without the YAML (a stale read), then ready: the wait ends on the third read.
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
	if got := readFile(t, file); got != kcYAML || reads < 3 {
		t.Errorf("file = %q, reads = %d", got, reads)
	}
	assertNoPrivateKey(t, "stdout", out)
}
