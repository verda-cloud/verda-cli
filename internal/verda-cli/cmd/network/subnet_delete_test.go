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

func TestSubnetDeleteRefusesWithInstances(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "delete", "sn_a1", "--yes")
	ae := cmdutil.ClassifyError(err)
	if ae == nil || ae.Code != cmdutil.CodeResourceInUse || !strings.Contains(ae.Message, "Subnet fin-03-1 has 2 instance(s)") {
		t.Fatalf("err = %v", err)
	}
	if deletes(api) != 0 {
		t.Error("no DELETE expected")
	}
}

func TestSubnetDeleteAgentRequiresYes(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "delete", "sn_a2")
	if code := agentErrorCode(err); code != "CONFIRMATION_REQUIRED" {
		t.Errorf("code = %q", code)
	}
}

func TestSubnetDeleteConfirmedListsRoutes(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	pub := "1.2.3.4"
	api.addRoute("sn_a2", "rt_1", "0.0.0.0/0", &verda.AttachedInstance{ID: "i-x", Hostname: "nat-1", PublicIP: &pub})
	client := newFakeClient(t, api)
	out, errOut, err := runNetwork(t, tableFactory(client, tuitest.New().AddConfirm(true)), "subnet", "delete", "us-1", "--network", "alpha")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, want := range []string{"Delete subnet us-1 (sn_a2, US-SOUTHWEST-1, 10.0.1.0/24) of network alpha", "rt_1: 0.0.0.0/0 via nat-1"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("warning lacks %q:\n%s", want, errOut)
		}
	}
	if d := api.requestsMatching("DELETE"); len(d) != 1 || d[0].Path != "/private-networks/pn_a/subnets/sn_a2" {
		t.Errorf("DELETE = %v", d)
	}
	if !strings.Contains(out, "Deleted subnet us-1 (sn_a2)") {
		t.Errorf("out = %q", out)
	}
}

func TestSubnetDeleteCanceled(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]*cancelPrompter{
		"declined": {Prompter: tuitest.New().AddConfirm(false)},
		"esc":      {Prompter: tuitest.New(), confirmErr: context.Canceled},
		"ctrl+c":   {Prompter: tuitest.New(), confirmErr: tui.ErrInterrupted},
	} {
		api := seededAPI()
		client := newFakeClient(t, api)
		f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable, PrompterOverride: p}
		_, errOut, err := runNetwork(t, f, "subnet", "delete", "sn_a2")
		if err != nil || !strings.Contains(errOut, canceledMsg) || deletes(api) != 0 {
			t.Errorf("%s: err = %v, stderr = %q, deletes = %d", name, err, errOut, deletes(api))
		}
	}
}

func TestSubnetDeleteConflict(t *testing.T) {
	t.Parallel()
	api := seededAPI()
	api.failOn("DELETE /private-networks/pn_a/subnets/sn_a2", 409, "conflict", "Subnet still has 1 attached instance(s); delete them first: i-9")
	client := newFakeClient(t, api)
	_, _, err := runNetwork(t, agentFactory(client), "subnet", "delete", "sn_a2", "--yes")
	ae := cmdutil.ClassifyError(err)
	if ae.Details["api_code"] != "conflict" || !strings.Contains(ae.Message, conflictRetryHint) {
		t.Errorf("agent error = %+v", ae)
	}
}
