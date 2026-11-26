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

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
)

// kcHome points the default kubeconfig path at a temp dir and fixes the OS user name.
func kcHome(t *testing.T) string {
	t.Helper()
	home := setHome(t)
	orig := currentUserName
	currentUserName = func() string { return "tester" }
	t.Cleanup(func() { currentUserName = orig })
	return home
}

var kcInteractiveBase = []string{"kubeconfig", "create", "--cluster", "prod"}

// Each flag skips its prompt; an empty display name or duration is left out of the request.
func TestKubeconfigCreateInteractive(t *testing.T) {
	withTerminal(t)
	for name, tc := range map[string]struct {
		prompter func(file string) *tuitest.Prompter
		flags    func(file string) []string
		file     func(home, dir string) string
		groups   []any
		extra    map[string]any
	}{
		"admin by prompt": {
			prompter: func(file string) *tuitest.Prompter {
				return tuitest.New().AddTextInput("alice").AddSelect(0).AddTextInput("Alice laptop").AddTextInput("720h").AddTextInput(file).AddConfirm(true)
			},
			file:   func(_, dir string) string { return filepath.Join(dir, "kc.yaml") },
			groups: []any{groupAdmin},
			extra:  map[string]any{"display_name": "Alice laptop", "duration": "720h"},
		},
		"custom groups, default path": {
			prompter: func(file string) *tuitest.Prompter {
				return tuitest.New().AddTextInput("alice").AddSelect(1).AddTextInput("dev, ops, dev").AddTextInput("").AddTextInput("").AddTextInput(file).AddConfirm(true)
			},
			file:   func(home, _ string) string { return filepath.Join(home, ".kube", "verda-prod.alice.yaml") },
			groups: []any{"dev", "ops"},
		},
		"~/ in the file answer is the home directory": {
			prompter: func(string) *tuitest.Prompter {
				return tuitest.New().AddTextInput("alice").AddSelect(0).AddTextInput("").AddTextInput("").AddTextInput("~/kc.yaml").AddConfirm(true)
			},
			file:   func(home, _ string) string { return filepath.Join(home, "kc.yaml") },
			groups: []any{groupAdmin},
		},
		"all flags: only the confirm": {
			prompter: func(string) *tuitest.Prompter { return tuitest.New().AddConfirm(true) },
			flags: func(file string) []string {
				return []string{"--common-name", "alice", "--group", "dev", "--display-name", "x", "--duration", "2h", "--file", file}
			},
			file:   func(_, dir string) string { return filepath.Join(dir, "kc.yaml") },
			groups: []any{"dev"},
			extra:  map[string]any{"display_name": "x", "duration": "2h"},
		},
	} {
		home := kcHome(t)
		file := tc.file(home, t.TempDir())
		args := slices.Clone(kcInteractiveBase)
		if tc.flags != nil {
			args = append(args, tc.flags(file)...)
		}
		api := kcAPI()
		out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, api), tc.prompter(file)), args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		body := postBodyAny(t, api)
		if g, _ := body["groups"].([]any); body["common_name"] != "alice" || !slices.Equal(g, tc.groups) {
			t.Errorf("%s: body = %v", name, body)
		}
		for _, key := range []string{"duration", "display_name"} {
			if got, ok := body[key]; got != tc.extra[key] || ok != (tc.extra[key] != nil) {
				t.Errorf("%s: %s = %v", name, key, got)
			}
		}
		if got := readFile(t, file); got != kcYAML {
			t.Errorf("%s: file = %q", name, got)
		}
		assertMode(t, file, 0o600)
		// The access prompt prints the revoke note; --group skips it.
		if !strings.Contains(errOut, "Create kubeconfig") || strings.Contains(errOut, "cannot be revoked") != (tc.flags == nil) {
			t.Errorf("%s: stderr = %q", name, errOut)
		}
		assertNoPrivateKey(t, "stdout", out)
		assertNoPrivateKey(t, "stderr", errOut)
	}
}

func TestKubeconfigCreateInteractive_CancelAndDecline(t *testing.T) {
	withTerminal(t)
	kcHome(t)
	full := append(slices.Clone(kcInteractiveBase), "--common-name", "bob", "--admin", "--display-name", "", "--duration", "", "--file", filepath.Join(t.TempDir(), "kc.yaml"))
	for name, tc := range map[string]struct {
		prompter tui.Prompter
		args     []string
		err      string // a prompt failure that is not a cancel is returned
	}{
		"Esc at a text prompt": {&textCancelPrompter{Prompter: tuitest.New()}, kcInteractiveBase, ""},
		"Ctrl+C at the select": {&cancelPrompter{Prompter: tuitest.New(), selectErr: errInterrupted()}, append(slices.Clone(kcInteractiveBase), "--common-name", "bob"), ""},
		"decline the summary":  {confirmNo(), full, ""},
		"prompt failure":       {tuitest.New(), kcInteractiveBase, "no text input responses queued"},
	} {
		api := kcAPI()
		f := tableFactory(newFakeClient(t, api), nil)
		f.PrompterOverride = tc.prompter
		out, errOut, err := runMK8s(t, f, tc.args...)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) || strings.Contains(errOut, canceledMsg) || kcPosts(api) != 0 {
				t.Errorf("%s: err = %v, stderr = %q", name, err, errOut)
			}
			continue
		}
		if err != nil || !strings.Contains(errOut, canceledMsg) || out != "" || kcPosts(api) != 0 {
			t.Errorf("%s: err = %v, stdout = %q, stderr = %q, POSTs = %d", name, err, out, errOut, kcPosts(api))
		}
	}
}

func TestKubeconfigPromptValidators(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.yaml")
	if err := os.WriteFile(existing, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		err  error
		want string // "" means accepted
	}{
		"- prints the key on the terminal": {kubeconfigFileValidator("prod", "alice", false)("-"), "enter a file path"},
		"existing file":                    {kubeconfigFileValidator("prod", "alice", false)(existing), "--force"},
		"existing file with --force":       {kubeconfigFileValidator("prod", "alice", true)(existing), ""},
		"missing parent":                   {kubeconfigFileValidator("prod", "alice", false)(filepath.Join(dir, "no", "new.yaml")), "does not exist"},
		"empty duration: API default":      {validateDurationInput("  "), ""},
		"duration":                         {validateDurationInput("720h"), ""},
		"duration under 1h":                {validateDurationInput("30m"), "1h"},
	} {
		if (tc.want == "") != (tc.err == nil) || (tc.err != nil && (!strings.Contains(tc.err.Error(), tc.want) || strings.HasSuffix(tc.err.Error(), "."))) {
			t.Errorf("%s: err = %v, want %q", name, tc.err, tc.want)
		}
	}
}

func TestKubeconfigFileValidator_ExpandsHome(t *testing.T) {
	home := setHome(t)
	if err := os.WriteFile(filepath.Join(home, "taken.yaml"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	validate := kubeconfigFileValidator("prod", "alice", false)
	if err := validate("~/kc.yaml"); err != nil {
		t.Errorf("~/kc.yaml: %v", err)
	}
	if err := validate("~/taken.yaml"); err == nil || !strings.Contains(err.Error(), filepath.Join(home, "taken.yaml")) {
		t.Errorf("~/taken.yaml: %v, want the existing file in the home directory", err)
	}
}
