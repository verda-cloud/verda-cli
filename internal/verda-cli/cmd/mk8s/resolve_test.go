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
	"errors"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// With -l there is no List call; name and location are trimmed, the location upper-cased.
func TestResolve_TrimsNameAndLocation(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "describe", " prod ", "-l", " fin-03 "); err != nil {
		t.Fatal(err)
	}
	if got := api.paths("GET"); got[0] != "/mk8sclusters/FIN-03/prod" {
		t.Errorf("requests = %v", got)
	}
}

// Lookup errors are plain, not AgentErrors: main prints an AgentError as JSON even for a person.
func TestResolve_Errors(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	api.addCluster("twice", "FIN-01", "running")
	api.addCluster("twice", "FIN-03", "running")
	for name, tc := range map[string]struct {
		args     []string
		code     string
		agentErr bool
		mentions []string
	}{
		"missing":   {[]string{}, "MISSING_REQUIRED_FLAGS", true, nil},
		"blank":     {[]string{"  "}, "MISSING_REQUIRED_FLAGS", true, nil},
		"not found": {[]string{"ghost"}, "NOT_FOUND", false, []string{"ghost"}},
		"wrong -l":  {[]string{"staging", "-l", "FIN-03"}, "NOT_FOUND", false, nil},
		"ambiguous": {[]string{"twice"}, "VALIDATION_ERROR", false, []string{"FIN-01", "FIN-03", "-l"}},
	} {
		_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), append([]string{"cluster", "describe"}, tc.args...)...)
		if agentErrorCode(err) != tc.code || cmdutil.IsAgentError(err) != tc.agentErr {
			t.Errorf("%s: err = %v", name, err)
		}
		for _, want := range tc.mentions {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error misses %q: %v", name, want, err)
			}
		}
	}
}

func TestResolve_Picker(t *testing.T) {
	withTerminal(t)
	for name, tc := range map[string]struct {
		api         *fakeAPI
		prompter    *cancelPrompter
		args        []string
		out, errOut string
		err         string
	}{
		"picks the second":     {seededClusters(), &cancelPrompter{Prompter: tuitest.New().AddSelect(1)}, nil, "staging", "", ""},
		"limited to -l":        {seededClusters(), &cancelPrompter{Prompter: tuitest.New().AddSelect(0)}, []string{"-l", "fin-01"}, "staging", "", ""},
		"cancel row":           {seededClusters(), &cancelPrompter{Prompter: tuitest.New().AddSelect(2)}, nil, "", canceledMsg, ""},
		"esc":                  {seededClusters(), &cancelPrompter{Prompter: tuitest.New(), selectErr: context.Canceled}, nil, "", canceledMsg, ""},
		"ctrl+c":               {seededClusters(), &cancelPrompter{Prompter: tuitest.New(), selectErr: tui.ErrInterrupted}, nil, "", canceledMsg, ""},
		"other picker error":   {seededClusters(), &cancelPrompter{Prompter: tuitest.New(), selectErr: errors.New("terminal broke")}, nil, "", "", "terminal broke"},
		"none in the location": {seededClusters(), &cancelPrompter{Prompter: tuitest.New()}, []string{"-l", "FIN-99"}, "No clusters found in FIN-99.", "", ""},
		"no clusters":          {newFakeAPI(), &cancelPrompter{Prompter: tuitest.New()}, nil, "No clusters found.", "cluster create", ""},
	} {
		f := tableFactory(newFakeClient(t, tc.api), nil)
		f.PrompterOverride = tc.prompter
		out, errOut, err := runMK8s(t, f, append([]string{"cluster", "describe"}, tc.args...)...)
		if (tc.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.err)) ||
			!strings.Contains(out, tc.out) || !strings.Contains(errOut, tc.errOut) || (tc.out == "" && out != "") {
			t.Errorf("%s: out=%q stderr=%q err=%v", name, out, errOut, err)
		}
		if tc.out == "staging" && strings.Contains(out, "prod.example") {
			t.Errorf("%s: described prod: %s", name, out)
		}
	}
}
