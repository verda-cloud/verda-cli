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
	"encoding/json"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func deletes(api *fakeAPI) int { return len(api.requestsMatching("DELETE")) }

func TestDeleteAgentModeRequiresYes(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "delete", "pn_b")
	if code := agentErrorCode(err); code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("code = %q, want CONFIRMATION_REQUIRED", code)
	}
	if len(api.requests) != 0 {
		t.Errorf("no API call expected before --yes, got %v", api.requests)
	}
}

func TestDeleteConfirmedListsSubnets(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	api.addSubnet("pn_b", "sn_b1", "db", "FIN-03", "10.1.0.0/24", true)
	client := newFakeClient(t, api)
	out, errOut, err := runNetwork(t, tableFactory(client, tuitest.New().AddConfirm(true)), "delete", "pn_b")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, want := range []string{"Delete private network beta (pn_b)", "Also removes 1 subnet(s)", "db (sn_b1, FIN-03, 10.1.0.0/24)", "cannot be undone"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("warning lacks %q:\n%s", want, errOut)
		}
	}
	if !strings.Contains(out, "Deleted network beta (pn_b)") {
		t.Errorf("out = %q", out)
	}
	if d := api.requestsMatching("DELETE"); len(d) != 1 || d[0].Path != "/private-networks/pn_b" {
		t.Errorf("DELETE = %v", d)
	}
}

func TestDeleteCanceled(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]*cancelPrompter{
		"declined": {Prompter: tuitest.New().AddConfirm(false)},
		"esc":      {Prompter: tuitest.New(), confirmErr: context.Canceled},
		"ctrl+c":   {Prompter: tuitest.New(), confirmErr: tui.ErrInterrupted},
	} {
		api := seededAPI()
		client := newFakeClient(t, api)
		f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable, PrompterOverride: p}
		_, errOut, err := runNetwork(t, f, "delete", "pn_b")
		if err != nil || !strings.Contains(errOut, canceledMsg) {
			t.Errorf("%s: err = %v, stderr = %q", name, err, errOut)
		}
		if deletes(api) != 0 {
			t.Errorf("%s: no DELETE expected", name)
		}
	}
}

func TestDeleteAgentYesWritesResult(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	out, _, err := runNetwork(t, agentFactory(client), "rm", "pn_b", "--yes")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	want := map[string]string{"resource": "network", "id": "pn_b", "name": "beta", "action": "delete", "status": "completed"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if deletes(api) != 1 {
		t.Errorf("want one DELETE, got %d", deletes(api))
	}
}

func TestDeleteNotFound(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	_, _, err := runNetwork(t, agentFactory(client), "delete", "nope", "--yes")
	if code := agentErrorCode(err); code != "NOT_FOUND" {
		t.Errorf("code = %q (err %v), want NOT_FOUND", code, err)
	}
}

func TestDeleteMissingArgOffTerminal(t *testing.T) {
	t.Parallel()
	client := newFakeClient(t, seededAPI())
	_, _, err := runNetwork(t, tableFactory(client, nil), "delete")
	if code := agentErrorCode(err); code != "MISSING_REQUIRED_FLAGS" {
		t.Errorf("code = %q (err %v), want MISSING_REQUIRED_FLAGS", code, err)
	}
}

func TestDeletePickerOnTerminal(t *testing.T) {
	withTerminal(t)
	api := seededAPI()
	client := newFakeClient(t, api)
	out, _, err := runNetwork(t, tableFactory(client, tuitest.New().AddSelect(1).AddConfirm(true)), "delete")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(out, "Deleted network beta (pn_b)") {
		t.Errorf("picker choice 1 should delete beta: %q", out)
	}
}

func TestDeletePickerCancel(t *testing.T) {
	withTerminal(t)
	for name, p := range map[string]*cancelPrompter{
		"cancel row": {Prompter: tuitest.New().AddSelect(2)},
		"esc":        {Prompter: tuitest.New(), selectErr: context.Canceled},
	} {
		api := seededAPI()
		client := newFakeClient(t, api)
		f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable, PrompterOverride: p}
		out, errOut, err := runNetwork(t, f, "delete")
		if err != nil || out != "" || !strings.Contains(errOut, canceledMsg) || deletes(api) != 0 {
			t.Errorf("%s: want %q on stderr only; out=%q stderr=%q err=%v deletes=%d", name, canceledMsg, out, errOut, err, deletes(api))
		}
	}
}
