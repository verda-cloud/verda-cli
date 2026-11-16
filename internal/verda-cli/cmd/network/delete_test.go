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
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// emptyNetworkAPI: network beta has a subnet with a route whose gateway is gone, and no instances.
func emptyNetworkAPI() *fakeAPI {
	api := seededAPI()
	api.addSubnet("pn_b", "sn_b1", "db", "FIN-03", "10.1.0.0/24", true)
	api.addRoute("sn_b1", "rt_b1", "0.0.0.0/0", nil)
	return api
}

func deletes(api *fakeAPI) int { return len(api.requestsMatching("DELETE")) }

func TestDeleteRefusesWhileInstancesAttached(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "delete", "alpha", "--yes")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != cmdutil.CodeResourceInUse {
		t.Fatalf("err = %v, want %s", err, cmdutil.CodeResourceInUse)
	}
	for _, want := range []string{"gw-1", "worker", "i-gw", "FIN-03", "10.0.0.5", "verda vm action --action delete"} {
		if !strings.Contains(ae.Message, want) {
			t.Errorf("message lacks %q: %s", want, ae.Message)
		}
	}
	if insts, _ := ae.Details["blockers"].([]map[string]string); len(insts) != 2 {
		t.Errorf("details.instances = %v", ae.Details["blockers"])
	}
	if deletes(api) != 0 {
		t.Error("no DELETE may be sent while instances are attached")
	}
}

func TestDeleteAgentModeRequiresYes(t *testing.T) {
	t.Parallel()
	api := emptyNetworkAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "delete", "beta")
	if code := agentErrorCode(err); code != "CONFIRMATION_REQUIRED" {
		t.Fatalf("code = %q, want CONFIRMATION_REQUIRED", code)
	}
	if len(api.requests) != 0 {
		t.Errorf("no API call expected before --yes, got %v", api.requests)
	}
}

func TestDeleteConfirmed(t *testing.T) {
	t.Parallel()
	api := emptyNetworkAPI()
	client := newFakeClient(t, api)
	out, errOut, err := runNetwork(t, tableFactory(client, tuitest.New().AddConfirm(true)), "delete", "beta")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, want := range []string{"Delete private network beta", "Also removes 1 subnet(s)", "db (sn_b1, FIN-03, 10.1.0.0/24)", "route 0.0.0.0/0 via (deleted)"} {
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
		api := emptyNetworkAPI()
		client := newFakeClient(t, api)
		f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable, PrompterOverride: p}
		_, errOut, err := runNetwork(t, f, "delete", "beta")
		if err != nil || !strings.Contains(errOut, canceledMsg) {
			t.Errorf("%s: err = %v, stderr = %q", name, err, errOut)
		}
		if deletes(api) != 0 {
			t.Errorf("%s: no DELETE expected", name)
		}
	}
}

func TestDeleteConflictAddsRetryHint(t *testing.T) {
	t.Parallel()
	api := emptyNetworkAPI()
	api.failOn("DELETE /private-networks/pn_b", 409, "conflict", "Private network still has 1 attached instance(s); delete them first: i-9")
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "delete", "beta", "--yes")
	ae := cmdutil.ClassifyError(err)
	if ae.Code != "API_ERROR" || ae.Details["api_code"] != "conflict" || ae.Details["status"] != 409 {
		t.Fatalf("agent error = %+v", ae)
	}
	if !strings.Contains(ae.Message, "still has 1 attached instance") || !strings.Contains(ae.Message, conflictRetryHint) {
		t.Errorf("message = %q", ae.Message)
	}
}

func TestDeleteOtherAPIErrorsUnchanged(t *testing.T) {
	t.Parallel()
	err := withConflictHint(&verda.APIError{StatusCode: 500, Message: "boom"})
	if strings.Contains(err.Error(), conflictRetryHint) {
		t.Error("only a 409 gets the retry hint")
	}
}
