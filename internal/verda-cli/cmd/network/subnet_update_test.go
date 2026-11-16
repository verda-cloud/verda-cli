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
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// twoSubnetAPI: alpha has two FIN-03 subnets, fin-03-1 is the default.
func twoSubnetAPI() *fakeAPI {
	api := seededAPI()
	api.addSubnet("pn_a", "sn_a3", "fin-03-2", "FIN-03", "10.0.2.0/24", false)
	return api
}

func TestSubnetUpdateFlagsExactBody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args []string
		path string
		want string
	}{
		{[]string{"sn_a3", "--name", "web"}, "/private-networks/pn_a/subnets/sn_a3", `{"name":"web"}`},
		{[]string{"fin-03-2", "--network", "alpha", "--default"}, "/private-networks/pn_a/subnets/sn_a3", `{"is_default":true}`},
		{[]string{"sn_a3", "--name", "web", "--default"}, "/private-networks/pn_a/subnets/sn_a3", `{"name":"web","is_default":true}`},
	}
	for _, tt := range tests {
		api := twoSubnetAPI()
		client := newFakeClient(t, api)
		if _, _, err := runNetwork(t, agentFactory(client), append([]string{"subnet", "update"}, tt.args...)...); err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		patches := api.requestsMatching("PATCH")
		if len(patches) != 1 || patches[0].Body != tt.want || patches[0].Path != tt.path {
			t.Errorf("%v: PATCH = %+v, want %s %s", tt.args, patches, tt.path, tt.want)
		}
	}
}

func TestSubnetUpdateRejectsDefaultFalseAndMissingFlags(t *testing.T) {
	t.Parallel()
	api := twoSubnetAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "update", "sn_a3", "--default=false")
	if code := agentErrorCode(err); code != "VALIDATION_ERROR" {
		t.Errorf("--default=false: code = %q", code)
	}
	_, _, err = runNetwork(t, agentFactory(client), "subnet", "update", "sn_a3")
	if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("no change flags: code = %q", code)
	}
	if len(api.requestsMatching("PATCH")) != 0 {
		t.Error("no PATCH expected")
	}
}

func TestSubnetUpdateInteractive(t *testing.T) {
	withTerminal(t)
	withEngineResults(t, wizard.MultiSelectResult([]int{1}))
	api := twoSubnetAPI()
	client := newFakeClient(t, api)
	// Picker index 2 is fin-03-2 (after fin-03-1 and us-1).
	_, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddSelect(2).AddConfirm(true)), "subnet", "update")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := patchBodies(api); len(got) != 1 || got[0] != `{"is_default":true}` {
		t.Errorf("PATCH = %q", got)
	}
}

func TestSubnetUpdateInteractiveCancelSendsNothing(t *testing.T) {
	cases := []struct {
		name     string
		prompter *cancelPrompter
		args     []string
		results  []wizard.TestResult
	}{
		{name: "esc at picker", prompter: &cancelPrompter{Prompter: tuitest.New(), selectErr: context.Canceled}},
		{name: "ctrl+c at picker", prompter: &cancelPrompter{Prompter: tuitest.New(), selectErr: tui.ErrInterrupted}},
		{name: "esc at changes", prompter: &cancelPrompter{Prompter: tuitest.New()}, args: []string{"sn_a3"}, results: []wizard.TestResult{wizard.BackResult()}},
		{name: "ctrl+c at changes", prompter: &cancelPrompter{Prompter: tuitest.New()}, args: []string{"sn_a3"}, results: []wizard.TestResult{wizard.ExitResult()}},
		{name: "ctrl+c at name", prompter: &cancelPrompter{Prompter: tuitest.New()}, args: []string{"sn_a3"}, results: []wizard.TestResult{wizard.MultiSelectResult([]int{0}), wizard.ExitResult()}},
		{name: "esc at confirm", prompter: &cancelPrompter{Prompter: tuitest.New(), confirmErr: context.Canceled}, args: []string{"sn_a3"}, results: []wizard.TestResult{wizard.MultiSelectResult([]int{1})}},
		{name: "ctrl+c at confirm", prompter: &cancelPrompter{Prompter: tuitest.New(), confirmErr: tui.ErrInterrupted}, args: []string{"sn_a3"}, results: []wizard.TestResult{wizard.MultiSelectResult([]int{1})}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withTerminal(t)
			withEngineResults(t, tc.results...)
			api := twoSubnetAPI()
			client := newFakeClient(t, api)
			f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable, PrompterOverride: tc.prompter}
			_, _, err := runNetwork(t, f, append([]string{"subnet", "update"}, tc.args...)...)
			if err != nil && !cmdutil.IsPromptCancel(err) {
				t.Fatalf("want a clean cancel, got %v", err)
			}
			if got := patchBodies(api); len(got) != 0 {
				t.Errorf("no PATCH expected, got %q", got)
			}
		})
	}
}
