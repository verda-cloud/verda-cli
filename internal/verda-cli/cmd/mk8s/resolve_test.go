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
	"slices"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestResolve_LocationGivenMakesNoListCall(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "describe", "prod", "-l", "FIN-03"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(api.paths("GET"), "/mk8sclusters") {
		t.Errorf("-l must skip the List call: %v", api.paths("GET"))
	}
}

func TestResolve_NotFound(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, seededClusters()), nil), "cluster", "describe", "ghost")
	if err == nil || agentErrorCode(err) != "NOT_FOUND" || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolve_ErrorsArePlainSoMainPrintsThemAsText(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	api.addCluster("prod", "FIN-01", "running")
	for name, args := range map[string][]string{
		"not found": {"cluster", "describe", "ghost"},
		"ambiguous": {"cluster", "describe", "prod"},
	} {
		_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), args...)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		// main prints an AgentError as a JSON envelope even for a person.
		if cmdutil.IsAgentError(err) {
			t.Errorf("%s: %v is an AgentError, a person would get JSON", name, err)
		}
	}
}

func TestResolve_TrimsNameAndLocation(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	if _, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "describe", " prod ", "-l", " FIN-03 "); err != nil {
		t.Fatal(err)
	}
	if got := api.paths("GET"); got[0] != "/mk8sclusters/FIN-03/prod" {
		t.Errorf("requests = %v", got)
	}
}

func TestResolve_ExplicitLocationWrongIsAPI404(t *testing.T) {
	t.Parallel()
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, seededClusters()), nil), "cluster", "describe", "prod", "-l", "FIN-01")
	if err == nil || agentErrorCode(err) != "NOT_FOUND" {
		t.Fatalf("err = %v", err)
	}
}

func TestResolve_NameInTwoLocationsAsksForLocation(t *testing.T) {
	t.Parallel()
	api := seededClusters()
	api.addCluster("prod", "FIN-01", "running")
	_, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), nil), "cluster", "describe", "prod")
	if err == nil || agentErrorCode(err) != "VALIDATION_ERROR" {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"FIN-01", "FIN-03", "-l"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error misses %q: %v", want, err)
		}
	}
}

func TestResolve_MissingClusterOffTerminal(t *testing.T) {
	t.Parallel()
	for name, f := range map[string]*cmdutil.TestFactory{
		"table": tableFactory(newFakeClient(t, seededClusters()), nil),
		"agent": agentFactory(newFakeClient(t, seededClusters())),
	} {
		for _, args := range [][]string{{"cluster", "describe"}, {"cluster", "describe", "  "}} {
			_, _, err := runMK8s(t, f, args...)
			ae := cmdutil.ClassifyError(err)
			if ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
				t.Errorf("%s %v: err = %v", name, args, err)
			}
		}
	}
}

func TestResolve_Picker(t *testing.T) {
	withTerminal(t)

	t.Run("picks the second cluster", func(t *testing.T) {
		api := seededClusters()
		out, _, err := runMK8s(t, tableFactory(newFakeClient(t, api), tuitest.New().AddSelect(1)), "cluster", "describe")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "staging") || strings.Contains(out, "prod.example") {
			t.Errorf("describe output = %s", out)
		}
		if got := api.paths("GET"); !slices.Contains(got, "/mk8sclusters/FIN-01/staging") {
			t.Errorf("requests = %v", got)
		}
	})

	t.Run("limited to -l", func(t *testing.T) {
		var labels []string
		p := &recordingPrompter{Prompter: tuitest.New().AddSelect(0), labels: &labels}
		f := tableFactory(newFakeClient(t, seededClusters()), nil)
		f.PrompterOverride = p
		if _, _, err := runMK8s(t, f, "cluster", "describe", "-l", "FIN-01"); err != nil {
			t.Fatal(err)
		}
		if len(labels) != 2 || !strings.HasPrefix(labels[0], "staging") || labels[1] != cancelLabel {
			t.Errorf("labels = %v, want staging and Cancel only", labels)
		}
	})

	t.Run("cancel row, Esc and Ctrl+C end cleanly", func(t *testing.T) {
		for name, p := range map[string]*cancelPrompter{
			"cancel row": {Prompter: tuitest.New().AddSelect(2)},
			"esc":        {Prompter: tuitest.New(), selectErr: context.Canceled},
			"ctrl+c":     {Prompter: tuitest.New(), selectErr: tui.ErrInterrupted},
		} {
			api := seededClusters()
			f := tableFactory(newFakeClient(t, api), nil)
			f.PrompterOverride = p
			out, errOut, err := runMK8s(t, f, "cluster", "describe")
			if err != nil || out != "" || !strings.Contains(errOut, canceledMsg) {
				t.Errorf("%s: out=%q err=%q error=%v", name, out, errOut, err)
			}
			if len(api.paths("GET")) != 1 {
				t.Errorf("%s: only the List call may happen, got %v", name, api.paths("GET"))
			}
		}
	})
}

func TestResolve_PickerEdgeCases(t *testing.T) {
	withTerminal(t)

	t.Run("a picker error that is not a cancel is returned", func(t *testing.T) {
		f := tableFactory(newFakeClient(t, seededClusters()), nil)
		f.PrompterOverride = &cancelPrompter{Prompter: tuitest.New(), selectErr: errors.New("terminal broke")}
		if _, _, err := runMK8s(t, f, "cluster", "describe"); err == nil || !strings.Contains(err.Error(), "terminal broke") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("no cluster in the chosen location", func(t *testing.T) {
		out, _, err := runMK8s(t, tableFactory(newFakeClient(t, seededClusters()), tuitest.New()), "cluster", "describe", "-l", "FIN-99")
		if err != nil || !strings.Contains(out, "No clusters found in FIN-99.") {
			t.Errorf("out=%q err=%v", out, err)
		}
	})

	t.Run("no clusters", func(t *testing.T) {
		out, errOut, err := runMK8s(t, tableFactory(newFakeClient(t, newFakeAPI()), tuitest.New()), "cluster", "describe")
		if err != nil || !strings.Contains(out, "No clusters found.") || !strings.Contains(errOut, "cluster create") {
			t.Errorf("out=%q err=%q error=%v", out, errOut, err)
		}
	})

	t.Run("not offered in agent mode", func(t *testing.T) {
		_, _, err := runMK8s(t, agentFactory(newFakeClient(t, seededClusters())), "cluster", "describe")
		if ae := cmdutil.ClassifyError(err); ae == nil || ae.Code != "MISSING_REQUIRED_FLAGS" {
			t.Errorf("err = %v", err)
		}
	})
}

// recordingPrompter records the labels of every Select.
type recordingPrompter struct {
	*tuitest.Prompter
	labels *[]string
}

func (p *recordingPrompter) Select(ctx context.Context, msg string, labels []string, opts ...tui.SelectOption) (int, error) {
	*p.labels = append(*p.labels, labels...)
	return p.Prompter.Select(ctx, msg, labels, opts...)
}
