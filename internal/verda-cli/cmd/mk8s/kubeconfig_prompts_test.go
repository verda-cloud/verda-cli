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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type kcPrompt struct {
	msg         string
	def         string
	placeholder string
	hints       bool
	escLabel    string
}

// kcRecorder wraps a prompter and records what each prompt asked and how.
type kcRecorder struct {
	tui.Prompter
	texts   []kcPrompt
	selects []kcPrompt
}

func (r *kcRecorder) TextInput(ctx context.Context, msg string, opts ...tui.TextInputOption) (string, error) {
	var c tui.TextInputConfig
	for _, o := range opts {
		o(&c)
	}
	r.texts = append(r.texts, kcPrompt{msg: msg, def: c.Default, placeholder: c.Placeholder})
	return r.Prompter.TextInput(ctx, msg, opts...)
}

func (r *kcRecorder) Select(ctx context.Context, msg string, labels []string, opts ...tui.SelectOption) (int, error) {
	var c tui.SelectConfig
	for _, o := range opts {
		o(&c)
	}
	r.selects = append(r.selects, kcPrompt{msg: msg, hints: c.ShowHints, escLabel: c.RelabelByID["esc"]})
	return r.Prompter.Select(ctx, msg, labels, opts...)
}

// asked reports whether a text or select prompt started with prefix.
func (r *kcRecorder) asked(prefix string) bool {
	for _, p := range slices.Concat(r.texts, r.selects) {
		if strings.HasPrefix(p.msg, prefix) {
			return true
		}
	}
	return false
}

func kcTable(client *verda.Client, p tui.Prompter) *cmdutil.TestFactory {
	return &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable, PrompterOverride: p}
}

// kcHome points the default kubeconfig path at a temp dir and fixes the OS user name.
func kcHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	orig := currentUserName
	currentUserName = func() string { return "tester" }
	t.Cleanup(func() { currentUserName = orig })
	return home
}

var kcInteractiveBase = []string{"kubeconfig", "create", "--cluster", "prod"}

// kcAssertFullFlowPrompts checks the order, defaults and hints of the prompts of a full interactive create.
func kcAssertFullFlowPrompts(t *testing.T, rec *kcRecorder, home string) {
	t.Helper()
	msgs := make([]string, 0, len(rec.texts))
	for _, p := range rec.texts {
		msgs = append(msgs, p.msg)
	}
	wantOrder := []string{"Common name", "Display name", "Duration", "File to write"}
	if len(msgs) != len(wantOrder) {
		t.Fatalf("text prompts = %v", msgs)
	}
	for i, prefix := range wantOrder {
		if !strings.HasPrefix(msgs[i], prefix) {
			t.Errorf("prompt %d = %q, want prefix %q", i, msgs[i], prefix)
		}
	}
	if rec.texts[0].def != "tester" {
		t.Errorf("common name default = %q, want the OS user name", rec.texts[0].def)
	}
	if rec.texts[2].def != "" || !strings.Contains(rec.texts[2].placeholder, "90 days") {
		t.Errorf("duration prompt = %+v: the 90 day default is a hint only", rec.texts[2])
	}
	if want := filepath.Join(home, ".kube", "verda-prod.alice.yaml"); rec.texts[3].def != want {
		t.Errorf("file default = %q, want %q", rec.texts[3].def, want)
	}
	if len(rec.selects) != 1 || rec.selects[0].msg != "Access" || !rec.selects[0].hints || rec.selects[0].escLabel != "esc cancel" {
		t.Errorf("selects = %+v, want the access select with hints", rec.selects)
	}
}

func TestKubeconfigCreateInteractive_FullFlow(t *testing.T) {
	withTerminal(t)
	home := kcHome(t)
	file := filepath.Join(t.TempDir(), "kc.yaml")
	api := kcAPI()
	rec := &kcRecorder{Prompter: tuitest.New().
		AddTextInput("alice").AddTextInput("Alice laptop").AddTextInput("720h").AddTextInput(file).
		AddSelect(0).AddConfirm(true)}
	out, errOut, err := runMK8s(t, kcTable(newFakeClient(t, api), rec), kcInteractiveBase...)
	if err != nil {
		t.Fatal(err)
	}
	kcAssertFullFlowPrompts(t, rec, home)
	body := kcBody(t, api)
	if body["common_name"] != "alice" || body["display_name"] != "Alice laptop" || body["duration"] != "720h" {
		t.Errorf("body = %v", body)
	}
	if g, _ := body["groups"].([]any); len(g) != 1 || g[0] != groupAdmin {
		t.Errorf("groups = %v", body["groups"])
	}
	if got := readFile(t, file); got != kcYAML {
		t.Errorf("file = %q", got)
	}
	assertMode(t, file, 0o600)
	for _, want := range []string{"Create kubeconfig", "alice", "cannot be revoked", "Canceled."} {
		if want == "Canceled." {
			if strings.Contains(errOut, want) {
				t.Errorf("stderr has %q", want)
			}
			continue
		}
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q: %q", want, errOut)
		}
	}
	assertNoPrivateKey(t, "stdout", out)
	assertNoPrivateKey(t, "stderr", errOut)
}

func TestKubeconfigCreateInteractive_CustomGroupsAndDefaultPath(t *testing.T) {
	withTerminal(t)
	home := kcHome(t)
	api := kcAPI()
	def := filepath.Join(home, ".kube", "verda-prod.alice.yaml")
	rec := &kcRecorder{Prompter: tuitest.New().
		AddTextInput("alice").AddTextInput("dev, ops, dev").AddTextInput("").AddTextInput("").AddTextInput(def).
		AddSelect(1).AddConfirm(true)}
	if _, _, err := runMK8s(t, kcTable(newFakeClient(t, api), rec), kcInteractiveBase...); err != nil {
		t.Fatal(err)
	}
	body := kcBody(t, api)
	if g, _ := body["groups"].([]any); len(g) != 2 || g[0] != "dev" || g[1] != "ops" {
		t.Errorf("groups = %v", body["groups"])
	}
	for _, key := range []string{"duration", "display_name"} {
		if _, ok := body[key]; ok {
			t.Errorf("%s sent although the answer was empty: %v", key, body)
		}
	}
	// The default path keeps default-path behavior: ~/.kube is created with 0700.
	if got := readFile(t, def); got != kcYAML {
		t.Errorf("file = %q", got)
	}
	assertMode(t, filepath.Dir(def), 0o700)
}

func TestKubeconfigCreateInteractive_EachFlagSkipsItsPrompt(t *testing.T) {
	withTerminal(t)
	file := filepath.Join(t.TempDir(), "flag.yaml")
	cases := []struct {
		name   string
		flags  []string
		prefix string
	}{
		{"common name", []string{"--common-name", "bob"}, "Common name"},
		{"admin", []string{"--admin"}, "Access"},
		{"group", []string{"--group", "dev"}, "Access"},
		{"display name", []string{"--display-name", "x"}, "Display name"},
		{"duration", []string{"--duration", "720h"}, "Duration"},
		{"file", []string{"--file", file}, "File to write"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := kcHome(t)
			def := filepath.Join(home, ".kube", "verda-prod.bob.yaml")
			// Answers in prompt order, minus the prompt whose flag is set.
			p := tuitest.New().AddSelect(0).AddConfirm(true)
			for _, a := range []struct{ prefix, answer string }{{"Common name", "bob"}, {"Display name", ""}, {"Duration", ""}, {"File to write", def}} {
				if a.prefix != tc.prefix {
					p.AddTextInput(a.answer)
				}
			}
			rec := &kcRecorder{Prompter: p}
			args := append(slices.Clone(kcInteractiveBase), tc.flags...)
			if _, _, err := runMK8s(t, kcTable(newFakeClient(t, kcAPI()), rec), args...); err != nil {
				t.Fatal(err)
			}
			if rec.asked(tc.prefix) {
				t.Errorf("%q was asked although its flag is set", tc.prefix)
			}
		})
	}
}

func TestKubeconfigCreateInteractive_AllFlagsOnlyConfirm(t *testing.T) {
	withTerminal(t)
	kcHome(t)
	api := kcAPI()
	file := filepath.Join(t.TempDir(), "kc.yaml")
	rec := &kcRecorder{Prompter: tuitest.New().AddConfirm(true)}
	args := append(slices.Clone(kcInteractiveBase), "--common-name", "bob", "--group", "dev", "--display-name", "x", "--duration", "2h", "--file", file)
	if _, _, err := runMK8s(t, kcTable(newFakeClient(t, api), rec), args...); err != nil {
		t.Fatal(err)
	}
	if len(rec.texts) != 0 || len(rec.selects) != 0 {
		t.Errorf("prompts asked: texts=%v selects=%v", rec.texts, rec.selects)
	}
	if body := kcBody(t, api); body["duration"] != "2h" || body["display_name"] != "x" {
		t.Errorf("body = %v", body)
	}
}

func TestKubeconfigCreateInteractive_FileDashSkipsFilePromptAndPrintsYAML(t *testing.T) {
	withTerminal(t)
	kcHome(t)
	rec := &kcRecorder{Prompter: tuitest.New().AddConfirm(true)}
	args := append(slices.Clone(kcInteractiveBase), "--common-name", "bob", "--admin", "--display-name", "", "--duration", "", "--file", "-")
	out, errOut, err := runMK8s(t, kcTable(newFakeClient(t, kcAPI()), rec), args...)
	if err != nil {
		t.Fatal(err)
	}
	if out != kcYAML || len(rec.texts) != 0 {
		t.Errorf("stdout = %q, text prompts = %v", out, rec.texts)
	}
	assertNoPrivateKey(t, "stderr", errOut)
	if !strings.Contains(errOut, "File:          stdout") {
		t.Errorf("summary = %q", errOut)
	}
}

func TestKubeconfigCreateInteractive_PicksTheClusterWhenOmitted(t *testing.T) {
	withTerminal(t)
	kcHome(t)
	api := kcAPI()
	file := filepath.Join(t.TempDir(), "kc.yaml")
	rec := &kcRecorder{Prompter: tuitest.New().AddSelect(0).AddConfirm(true)}
	args := []string{"kubeconfig", "create", "--common-name", "bob", "--admin", "--display-name", "", "--duration", "", "--file", file}
	if _, _, err := runMK8s(t, kcTable(newFakeClient(t, api), rec), args...); err != nil {
		t.Fatal(err)
	}
	if len(rec.selects) != 1 || rec.selects[0].msg != "Select cluster" || !rec.selects[0].hints || rec.selects[0].escLabel != "esc cancel" {
		t.Errorf("selects = %+v", rec.selects)
	}
	if reqs := api.requestsMatching("POST"); len(reqs) != 1 || reqs[0].Path != "/mk8sclusters/FIN-03/prod/kubeconfigs" {
		t.Errorf("POSTs = %v", reqs)
	}
}

func TestKubeconfigCreateInteractive_CancelAndDecline(t *testing.T) {
	withTerminal(t)
	kcHome(t)
	full := func() []string {
		return append(slices.Clone(kcInteractiveBase), "--common-name", "bob", "--admin", "--display-name", "", "--duration", "", "--file", filepath.Join(t.TempDir(), "kc.yaml"))
	}
	cases := map[string]struct {
		prompter tui.Prompter
		args     []string
	}{
		"Esc at a text prompt":    {&textCancelPrompter{Prompter: tuitest.New()}, kcInteractiveBase},
		"Ctrl+C at the select":    {&cancelPrompter{Prompter: tuitest.New(), selectErr: errInterrupted()}, append(slices.Clone(kcInteractiveBase), "--common-name", "bob")},
		"Esc at the confirm":      {&cancelPrompter{Prompter: tuitest.New(), confirmErr: contextCanceled()}, full()},
		"Ctrl+C at the confirm":   {&cancelPrompter{Prompter: tuitest.New(), confirmErr: errInterrupted()}, full()},
		"decline the summary":     {confirmNo(), full()},
		"Esc at the cluster pick": {&cancelPrompter{Prompter: tuitest.New(), selectErr: contextCanceled()}, []string{"kubeconfig", "create", "--common-name", "bob", "--admin"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := kcAPI()
			out, errOut, err := runMK8s(t, kcTable(newFakeClient(t, api), tc.prompter), tc.args...)
			if err != nil {
				t.Fatalf("a cancel is a clean exit, got %v", err)
			}
			if !strings.Contains(errOut, "Canceled.") || out != "" {
				t.Errorf("stdout = %q, stderr = %q", out, errOut)
			}
			if n := kcPosts(api); n != 0 {
				t.Errorf("%d POST(s) after a cancel", n)
			}
		})
	}
}

func TestKubeconfigCreateInteractive_PromptFailureIsReturned(t *testing.T) {
	withTerminal(t)
	kcHome(t)
	// No answers queued: the fake prompter fails, which is not a cancel.
	_, _, err := runMK8s(t, kcTable(newFakeClient(t, kcAPI()), tuitest.New()), kcInteractiveBase...)
	if err == nil || strings.Contains(err.Error(), "Canceled") {
		t.Errorf("err = %v", err)
	}
}

func TestKubeconfigCreateInteractive_ExistingFileStopsBeforeConfirmAndPOST(t *testing.T) {
	withTerminal(t)
	home := kcHome(t)
	def := filepath.Join(home, ".kube", "verda-prod.bob.yaml")
	if err := os.MkdirAll(filepath.Dir(def), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(def, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := kcAPI()
	// No confirm queued: reaching it would fail with a different error.
	p := tuitest.New().AddTextInput("").AddTextInput("").AddTextInput(def)
	args := append(slices.Clone(kcInteractiveBase), "--common-name", "bob", "--admin")
	_, _, err := runMK8s(t, kcTable(newFakeClient(t, api), p), args...)
	if err == nil || !strings.Contains(err.Error(), "--force") || kcPosts(api) != 0 {
		t.Errorf("err = %v, POSTs = %d", err, kcPosts(api))
	}
	if got := readFile(t, def); got != "old" {
		t.Errorf("file = %q", got)
	}
}

// A named --file that is taken stops the command before any prompt or API call.
func TestKubeconfigCreateInteractive_ExistingFlagFileStopsBeforePrompts(t *testing.T) {
	withTerminal(t)
	file := filepath.Join(t.TempDir(), "kc.yaml")
	if err := os.WriteFile(file, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := kcAPI()
	rec := &kcRecorder{Prompter: tuitest.New()}
	_, _, err := runMK8s(t, kcTable(newFakeClient(t, api), rec), "kubeconfig", "create", "--cluster", "prod", "--file", file)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("err = %v", err)
	}
	if len(rec.texts)+len(rec.selects) != 0 || len(api.requests) != 0 {
		t.Errorf("prompts = %v %v, requests = %v: want none", rec.texts, rec.selects, api.requests)
	}
}

// A blank --cluster is an error in create and save, as in list and describe: no picker.
func TestKubeconfigCreateAndSave_BlankClusterNeverPicks(t *testing.T) {
	withTerminal(t)
	for _, args := range [][]string{{"kubeconfig", "create", "--cluster", ""}, {"kubeconfig", "save", "prod-kc-1", "--cluster", ""}} {
		rec := &kcRecorder{Prompter: tuitest.New()}
		_, _, err := runMK8s(t, kcTable(newFakeClient(t, kcAPI()), rec), args...)
		if agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" || len(rec.texts)+len(rec.selects) != 0 {
			t.Errorf("%v: err = %v, prompts = %v %v", args, err, rec.texts, rec.selects)
		}
	}
}

func TestKubeconfigCreate_NeverPromptsOffTheTable(t *testing.T) {
	withTerminal(t)
	for name, f := range map[string]*cmdutil.TestFactory{
		"json":  jsonFactory(newFakeClient(t, kcAPI())),
		"agent": agentFactory(newFakeClient(t, kcAPI())),
	} {
		rec := &kcRecorder{Prompter: tuitest.New()}
		f.PrompterOverride = rec
		_, _, err := runMK8s(t, f, "kubeconfig", "create")
		if agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" {
			t.Errorf("%s: err = %v", name, err)
		}
		if len(rec.texts)+len(rec.selects) != 0 {
			t.Errorf("%s prompted", name)
		}
	}
}

func TestKubeconfigFileValidator(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.yaml")
	if err := os.WriteFile(existing, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := kubeconfigFileValidator("prod", "alice", false)("-"); err == nil {
		t.Error("- must be refused: it prints the key on the terminal")
	}
	if err := kubeconfigFileValidator("prod", "alice", false)(existing); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("existing file without force: %v", err)
	}
	if err := kubeconfigFileValidator("prod", "alice", true)(existing); err != nil {
		t.Errorf("existing file with force: %v", err)
	}
	if err := kubeconfigFileValidator("prod", "alice", false)(filepath.Join(dir, "new.yaml")); err != nil {
		t.Errorf("new file: %v", err)
	}
	if err := kubeconfigFileValidator("prod", "alice", false)(filepath.Join(dir, "no", "such", "new.yaml")); err == nil {
		t.Error("a missing parent directory must be refused")
	}
}

func TestValidateDurationInput(t *testing.T) {
	t.Parallel()
	for in, ok := range map[string]bool{"": true, "  ": true, "720h": true, "1h": true, "5m": false, "30m": false, "abc": false, "-1h": false} {
		err := validateDurationInput(in)
		if (err == nil) != ok {
			t.Errorf("validateDurationInput(%q) = %v, ok=%v", in, err, ok)
		}
		if err != nil && strings.HasSuffix(err.Error(), ".") {
			t.Errorf("validateDurationInput(%q) = %q, trailing dot", in, err)
		}
	}
}

func TestCurrentUserName(t *testing.T) {
	t.Parallel()
	if currentUserName() == "" {
		t.Skip("no OS user name in this environment")
	}
}
