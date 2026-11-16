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

package network

import (
	"context"
	"io"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// cancelPrompter returns err from its Select or Confirm; the rest is scripted.
type cancelPrompter struct {
	*tuitest.Prompter
	selectErr  error
	confirmErr error
}

func (p *cancelPrompter) Select(ctx context.Context, msg string, labels []string, opts ...tui.SelectOption) (int, error) {
	if p.selectErr != nil {
		return -1, p.selectErr
	}
	return p.Prompter.Select(ctx, msg, labels, opts...)
}

func (p *cancelPrompter) Confirm(ctx context.Context, msg string, opts ...tui.ConfirmOption) (bool, error) {
	if p.confirmErr != nil {
		return false, p.confirmErr
	}
	return p.Prompter.Confirm(ctx, msg, opts...)
}

// withEngineResults makes runChangeFlow use scripted results; not parallel-safe.
func withEngineResults(t *testing.T, results ...wizard.TestResult) {
	t.Helper()
	orig := newEngine
	newEngine = func(_ tui.Prompter, _ tui.Status, _ io.Writer) *wizard.Engine {
		return wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(results...))
	}
	t.Cleanup(func() { newEngine = orig })
}

func patchBodies(api *fakeAPI) []string {
	patches := api.requestsMatching("PATCH")
	out := make([]string, 0, len(patches))
	for _, r := range patches {
		out = append(out, r.Body)
	}
	return out
}

func TestUpdateFlagsExactBody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"beta", "--name", "gamma"}, `{"name":"gamma"}`},
		{[]string{"beta", "--name", " gamma "}, `{"name":"gamma"}`},
		{[]string{"pn_b", "--default"}, `{"is_default":true}`},
		{[]string{"beta", "--name", "gamma", "--default"}, `{"name":"gamma","is_default":true}`},
	}
	for _, tt := range tests {
		api := seededAPI()
		client := newFakeClient(t, api)
		if _, _, err := runNetwork(t, agentFactory(client), append([]string{"update"}, tt.args...)...); err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		if got := patchBodies(api); len(got) != 1 || got[0] != tt.want {
			t.Errorf("%v: PATCH bodies = %q, want %s", tt.args, got, tt.want)
		}
	}
}

func TestUpdateRejectsDefaultFalse(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "update", "alpha", "--default=false")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" {
		t.Fatalf("code = %q (err %v), want VALIDATION_ERROR", code, err)
	}
	if len(api.requests) != 0 {
		t.Errorf("no API call expected, got %v", api.requests)
	}
}

func TestUpdateNoChangeFlagsOffTerminal(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	_, _, err := runNetwork(t, agentFactory(client), "update", "alpha")
	var ae *cmdutil.AgentError
	if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" {
		t.Fatalf("code = %q, want MISSING_REQUIRED_FLAGS", code)
	}
	ae = cmdutil.ClassifyError(err)
	if oneOf, _ := ae.Details["one_of"].([]string); len(oneOf) != 2 {
		t.Errorf("one_of = %v", ae.Details["one_of"])
	}
	_, _, err = runNetwork(t, agentFactory(client), "update")
	ae = cmdutil.ClassifyError(err)
	if missing, _ := ae.Details["missing"].([]string); len(missing) != 1 || missing[0] != argNetwork {
		t.Errorf("missing = %v, want [<network>]", ae.Details["missing"])
	}
}

func TestUpdateInteractiveRenameAndDefault(t *testing.T) {
	withTerminal(t)
	withEngineResults(t, wizard.MultiSelectResult([]int{0, 1}), wizard.TextResult("gamma"))
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddSelect(1).AddConfirm(true)), "update")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := patchBodies(api); len(got) != 1 || got[0] != `{"name":"gamma","is_default":true}` {
		t.Errorf("PATCH = %q", got)
	}
}

func TestUpdateInteractiveOnlyDefaultSendsOnlyDefault(t *testing.T) {
	withTerminal(t)
	withEngineResults(t, wizard.MultiSelectResult([]int{1}))
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddConfirm(true)), "update", "beta")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := patchBodies(api); len(got) != 1 || got[0] != `{"is_default":true}` {
		t.Errorf("PATCH = %q", got)
	}
}

func TestUpdateInteractiveCancelSendsNothing(t *testing.T) {
	cases := []struct {
		name     string
		prompter *cancelPrompter
		args     []string
		results  []wizard.TestResult
	}{
		{name: "esc at picker", prompter: &cancelPrompter{Prompter: tuitest.New(), selectErr: context.Canceled}},
		{name: "ctrl+c at picker", prompter: &cancelPrompter{Prompter: tuitest.New(), selectErr: tui.ErrInterrupted}},
		{name: "esc at changes", prompter: &cancelPrompter{Prompter: tuitest.New()}, args: []string{"beta"}, results: []wizard.TestResult{wizard.BackResult()}},
		{name: "ctrl+c at changes", prompter: &cancelPrompter{Prompter: tuitest.New()}, args: []string{"beta"}, results: []wizard.TestResult{wizard.ExitResult()}},
		{name: "esc at name, then ctrl+c", prompter: &cancelPrompter{Prompter: tuitest.New()}, args: []string{"beta"}, results: []wizard.TestResult{wizard.MultiSelectResult([]int{0}), wizard.BackResult(), wizard.ExitResult()}},
		{name: "ctrl+c at name", prompter: &cancelPrompter{Prompter: tuitest.New()}, args: []string{"beta"}, results: []wizard.TestResult{wizard.MultiSelectResult([]int{0}), wizard.ExitResult()}},
		{name: "esc at confirm", prompter: &cancelPrompter{Prompter: tuitest.New(), confirmErr: context.Canceled}, args: []string{"beta"}, results: []wizard.TestResult{wizard.MultiSelectResult([]int{1})}},
		{name: "ctrl+c at confirm", prompter: &cancelPrompter{Prompter: tuitest.New(), confirmErr: tui.ErrInterrupted}, args: []string{"beta"}, results: []wizard.TestResult{wizard.MultiSelectResult([]int{1})}},
		{name: "decline at confirm", prompter: &cancelPrompter{Prompter: tuitest.New().AddConfirm(false)}, args: []string{"beta"}, results: []wizard.TestResult{wizard.MultiSelectResult([]int{1})}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withTerminal(t)
			withEngineResults(t, tc.results...)
			api := seededAPI()
			client := newFakeClient(t, api)
			f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable, PrompterOverride: tc.prompter}
			_, _, err := runNetwork(t, f, append([]string{"update"}, tc.args...)...)
			if err != nil && !cmdutil.IsPromptCancel(err) {
				t.Fatalf("want a clean cancel, got %v", err)
			}
			if got := patchBodies(api); len(got) != 0 {
				t.Errorf("no PATCH expected, got %q", got)
			}
		})
	}
}

func TestUpdateHidesMakeDefaultForTheDefault(t *testing.T) {
	withTerminal(t)
	// alpha is already the default: index 1 does not exist, so only Rename is offered.
	withEngineResults(t, wizard.MultiSelectResult([]int{0}), wizard.TextResult("alpha2"))
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddConfirm(true)), "update", "alpha")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := patchBodies(api); len(got) != 1 || got[0] != `{"name":"alpha2"}` {
		t.Errorf("PATCH = %q", got)
	}
}

func TestChangeChoicesHideMakeDefaultForTheDefault(t *testing.T) {
	t.Parallel()
	values := func(isDefault bool) []string {
		var out []string
		for _, c := range changeChoices(isDefault) {
			out = append(out, c.Value)
		}
		return out
	}
	if got := values(true); len(got) != 1 || got[0] != changeRename {
		t.Errorf("default target: choices = %v, want only %s", got, changeRename)
	}
	if got := values(false); len(got) != 2 || got[1] != changeMakeDefault {
		t.Errorf("non-default target: choices = %v, want rename and %s", got, changeMakeDefault)
	}
}
