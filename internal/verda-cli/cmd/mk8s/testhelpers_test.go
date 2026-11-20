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
	"encoding/json"
	"os"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// TestMain makes every test start off a terminal, whatever runs the tests; withTerminal opts in.
func TestMain(m *testing.M) {
	isInteractiveTerminal = func() bool { return false }
	os.Exit(m.Run())
}

// addCluster seeds a cluster; a running one has an endpoint, like the API.
func (a *fakeAPI) addCluster(name, location, phase string) {
	c := verda.ManagedKubernetesCluster{
		Name:              name,
		KubernetesVersion: "1.37.0",
		Location:          location,
		NetworkID:         "pn-1",
		SubnetID:          "sn-1",
		PodCIDR:           "10.100.0.0/16",
		ServiceCIDR:       "10.200.0.0/16",
		Phase:             phase,
		CreatedAt:         testCreated,
	}
	if phase == verda.ManagedKubernetesClusterPhaseRunning {
		c.ExternalEndpoint = "https://" + name + ".example.test:6443"
	}
	a.clusters = append(a.clusters, c)
}

func (a *fakeAPI) addPool(cluster, name, state string, replicas, ready int) {
	a.pools[cluster] = append(a.pools[cluster], verda.ManagedKubernetesNodePool{
		Name:          name,
		Replicas:      replicas,
		ReadyReplicas: ready,
		InstanceType:  "1V100.6V",
		SSHKeyIDs:     []string{"key-1"},
		State:         state,
		CreatedAt:     testCreated,
	})
}

// paths returns the path of every recorded request with this method, in order.
func (a *fakeAPI) paths(method string) []string {
	reqs := a.requestsMatching(method)
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, r.Path)
	}
	return out
}

// cancelPrompter answers Select or Confirm with an error (Esc, Ctrl+C) when set.
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

// agentErrorCode is the code main reports, so an API 404 counts as NOT_FOUND.
func agentErrorCode(err error) string {
	if ae := cmdutil.ClassifyError(err); ae != nil {
		return ae.Code
	}
	return ""
}

// isUsageError: VALIDATION_ERROR for agents, and not an AgentError, which main prints as JSON for people too.
func isUsageError(err error) bool {
	return agentErrorCode(err) == "VALIDATION_ERROR" && !cmdutil.IsAgentError(err)
}

func decodeJSON[T any](t *testing.T, s string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return v
}

func yamlFactory(client *verda.Client) *cmdutil.TestFactory {
	return &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: "yaml"}
}

func conditionReady() verda.ManagedKubernetesCondition {
	return verda.ManagedKubernetesCondition{Type: "Ready", Status: "True", Reason: "AllGood", Message: "all components ready", LastTransitionTime: testCreated}
}

func contextCanceled() error { return context.Canceled }

func errInterrupted() error { return tui.ErrInterrupted }

func (a *fakeAPI) addNetwork(id, name string) {
	a.networks = append(a.networks, verda.PrivateNetwork{ID: id, Name: name, Mode: verda.NetworkModeCustom, CreatedAt: testCreated})
}

func (a *fakeAPI) addSubnet(networkID, id, name, location, cidr string) {
	a.subnets[networkID] = append(a.subnets[networkID], verda.Subnet{
		ID: id, Name: name, CIDR: cidr, Location: verda.Location{Code: location}, CreatedAt: testCreated,
	})
}

func confirmYes() *tuitest.Prompter { return tuitest.New().AddConfirm(true) }

func confirmNo() *tuitest.Prompter { return tuitest.New().AddConfirm(false) }

// textCancelPrompter answers TextInput with Esc.
type textCancelPrompter struct{ *tuitest.Prompter }

func (p *textCancelPrompter) TextInput(context.Context, string, ...tui.TextInputOption) (string, error) {
	return "", context.Canceled
}

// pickerAnswers queues Select answers (indices) for a picker.
func pickerAnswers(indices ...int) *tuitest.Prompter {
	p := tuitest.New()
	for _, i := range indices {
		p.AddSelect(i)
	}
	return p
}

// poolWizardAPI has two clusters, instance types per location and two SSH keys.
func poolWizardAPI() *fakeAPI {
	api := seededClusters()
	api.availability["FIN-03"] = []string{"2A100.80S", "1V100.6V", "CPU.4V.16G"}
	api.availability["FIN-01"] = []string{"1A100.22V"}
	api.sshKeys = []verda.SSHKey{
		{ID: "key-1", Name: "laptop", CreatedAt: testCreated},
		{ID: "key-2", Name: "ci", CreatedAt: testCreated},
	}
	return api
}

func postBodyAny(t *testing.T, api *fakeAPI) map[string]any {
	t.Helper()
	reqs := api.requestsMatching("POST")
	if len(reqs) != 1 {
		t.Fatalf("want one POST, got %v", reqs)
	}
	return decodeJSON[map[string]any](t, reqs[0].Body)
}

// addKubeconfig seeds a kubeconfig of a cluster.
func (a *fakeAPI) addKubeconfig(cluster string, k verda.ManagedKubernetesKubeconfig) {
	a.kubeconfigs[cluster] = append(a.kubeconfigs[cluster], k)
}
