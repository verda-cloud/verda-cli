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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	"github.com/verda-cloud/verda-cli/pkg/tui"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

var testCreated = time.Date(2026, 11, 19, 9, 30, 0, 0, time.UTC)

type recordedRequest struct {
	Method string
	Path   string
	Body   string
}

// apiFailure makes a route answer with a fixed status and body.
type apiFailure struct {
	Status int
	Body   string
}

// fakeAPI is an in-memory mk8s API. Pools and kubeconfigs are keyed by cluster name.
type fakeAPI struct {
	mu          sync.Mutex
	locations   []string
	clusters    []verda.ManagedKubernetesCluster
	pools       map[string][]verda.ManagedKubernetesNodePool
	kubeconfigs map[string][]verda.ManagedKubernetesKubeconfig
	// networks and subnets (keyed by network ID) serve the wizard's pickers.
	networks []verda.PrivateNetwork
	subnets  map[string][]verda.Subnet
	// availability lists instance types per location; sshKeys serve the pickers.
	availability map[string][]string
	sshKeys      []verda.SSHKey
	// onPoolGet runs before each pool GET answers, to move a pool through its
	// states. It runs while the fake holds its lock: it must not call the
	// fake's locking helpers (requestsMatching, paths); read a.requests directly.
	onPoolGet func(p *verda.ManagedKubernetesNodePool)
	// onKubeconfigGet runs before each kubeconfig GET answers (same locking rule as onPoolGet),
	// to turn a pending kubeconfig ready and give it its YAML.
	onKubeconfigGet func(k *verda.ManagedKubernetesKubeconfig)
	requests        []recordedRequest
	// failures: route pattern (for example "POST /mk8sclusters") -> forced answer.
	failures map[string]apiFailure
	// disabled answers every route with 403 feature_not_enabled.
	disabled bool
	// onClusterGet runs before each cluster GET answers, so a test can move a
	// cluster through its phases while a command waits.
	onClusterGet func(c *verda.ManagedKubernetesCluster)
	nextID       int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		locations:    []string{"FIN-01", "FIN-03"},
		pools:        map[string][]verda.ManagedKubernetesNodePool{},
		kubeconfigs:  map[string][]verda.ManagedKubernetesKubeconfig{},
		subnets:      map[string][]verda.Subnet{},
		availability: map[string][]string{},
		failures:     map[string]apiFailure{},
	}
}

func (a *fakeAPI) requestsMatching(method string) []recordedRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []recordedRequest
	for _, r := range a.requests {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

func (a *fakeAPI) id(prefix string) string {
	a.nextID++
	return fmt.Sprintf("%s-%d", prefix, a.nextID)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter, what string) {
	writeJSON(w, http.StatusNotFound, map[string]string{"code": "not_found", "message": what + " not found"})
}

func (a *fakeAPI) findCluster(location, name string) int {
	for i := range a.clusters {
		if a.clusters[i].Name == name && (location == "" || a.clusters[i].Location == location) {
			return i
		}
	}
	return -1
}

func (a *fakeAPI) findPool(cluster, name string) int {
	for i := range a.pools[cluster] {
		if a.pools[cluster][i].Name == name {
			return i
		}
	}
	return -1
}

func (a *fakeAPI) findKubeconfig(cluster, id string) int {
	for i := range a.kubeconfigs[cluster] {
		if a.kubeconfigs[cluster][i].ID == id {
			return i
		}
	}
	return -1
}

// clusterOr404 resolves {location}/{name}; on a miss it answers 404 and returns -1.
func (a *fakeAPI) clusterOr404(w http.ResponseWriter, r *http.Request) int {
	i := a.findCluster(r.PathValue("loc"), r.PathValue("name"))
	if i < 0 {
		notFound(w, "cluster")
	}
	return i
}

func (a *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	route := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if fl, ok := a.failures[pattern]; ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(fl.Status)
				_, _ = io.WriteString(w, fl.Body)
				return
			}
			h(w, r)
		})
	}

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"access_token": "test-token", "token_type": "Bearer"})
	})
	a.networkRoutes(route)
	route("GET /mk8sclusters/enabled_locations", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, verda.ManagedKubernetesEnabledLocations{Locations: a.locations})
	})
	route("GET /mk8sclusters", func(w http.ResponseWriter, _ *http.Request) {
		clusters := a.clusters
		if clusters == nil {
			clusters = []verda.ManagedKubernetesCluster{}
		}
		writeJSON(w, http.StatusOK, clusters)
	})
	route("POST /mk8sclusters", a.createCluster)
	route("GET /mk8sclusters/{loc}/{name}", func(w http.ResponseWriter, r *http.Request) {
		if i := a.clusterOr404(w, r); i >= 0 {
			if a.onClusterGet != nil {
				a.onClusterGet(&a.clusters[i])
			}
			writeJSON(w, http.StatusOK, a.clusters[i])
		}
	})
	route("PATCH /mk8sclusters/{loc}/{name}", func(w http.ResponseWriter, r *http.Request) {
		i := a.clusterOr404(w, r)
		if i < 0 {
			return
		}
		var req verda.UpdateManagedKubernetesClusterRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		a.clusters[i].KubernetesVersion = req.KubernetesVersion
		writeJSON(w, http.StatusOK, a.clusters[i])
	})
	route("DELETE /mk8sclusters/{loc}/{name}", func(w http.ResponseWriter, r *http.Request) {
		i := a.clusterOr404(w, r)
		if i < 0 {
			return
		}
		name := a.clusters[i].Name
		a.clusters = append(a.clusters[:i], a.clusters[i+1:]...)
		delete(a.pools, name)
		delete(a.kubeconfigs, name)
		w.WriteHeader(http.StatusAccepted)
	})

	route("GET /mk8sclusters/{loc}/{name}/pools", func(w http.ResponseWriter, r *http.Request) {
		pools := a.pools[r.PathValue("name")]
		if pools == nil {
			pools = []verda.ManagedKubernetesNodePool{}
		}
		writeJSON(w, http.StatusOK, pools)
	})
	route("POST /mk8sclusters/{loc}/{name}/pools", a.createPool)
	route("GET /mk8sclusters/{loc}/{name}/pools/{pool}", func(w http.ResponseWriter, r *http.Request) {
		i := a.findPool(r.PathValue("name"), r.PathValue("pool"))
		if i < 0 {
			notFound(w, "node pool")
			return
		}
		if a.onPoolGet != nil {
			a.onPoolGet(&a.pools[r.PathValue("name")][i])
		}
		writeJSON(w, http.StatusOK, a.pools[r.PathValue("name")][i])
	})
	route("PATCH /mk8sclusters/{loc}/{name}/pools/{pool}", a.updatePool)
	route("DELETE /mk8sclusters/{loc}/{name}/pools/{pool}", func(w http.ResponseWriter, r *http.Request) {
		cluster := r.PathValue("name")
		i := a.findPool(cluster, r.PathValue("pool"))
		if i < 0 {
			notFound(w, "node pool")
			return
		}
		a.pools[cluster] = append(a.pools[cluster][:i], a.pools[cluster][i+1:]...)
		w.WriteHeader(http.StatusAccepted)
	})

	a.kubeconfigRoutes(route)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		a.mu.Lock()
		defer a.mu.Unlock()
		if r.URL.Path != "/oauth2/token" {
			a.requests = append(a.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Body: string(body)})
			if a.disabled {
				writeJSON(w, http.StatusForbidden, map[string]string{"code": "forbidden", "message": "forbidden", "details": reasonFeatureNotEnabled})
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *fakeAPI) createCluster(w http.ResponseWriter, r *http.Request) {
	var req verda.CreateManagedKubernetesClusterRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	c := verda.ManagedKubernetesCluster{
		Name: req.Name, KubernetesVersion: req.KubernetesVersion, Location: req.Location,
		NetworkID: req.NetworkID, SubnetID: req.SubnetID, PodCIDR: req.PodCIDR, ServiceCIDR: req.ServiceCIDR,
		Phase: verda.ManagedKubernetesClusterPhaseProvisioning, CreatedAt: testCreated,
	}
	a.clusters = append(a.clusters, c)
	writeJSON(w, http.StatusCreated, c)
}

func (a *fakeAPI) createPool(w http.ResponseWriter, r *http.Request) {
	cluster := r.PathValue("name")
	var req verda.CreateManagedKubernetesNodePoolRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	replicas := 1
	if req.Replicas != nil {
		replicas = *req.Replicas
	}
	p := verda.ManagedKubernetesNodePool{
		Name: req.Name, Replicas: replicas, InstanceType: req.InstanceType, SSHKeyIDs: req.SSHKeyIDs,
		State: verda.ManagedKubernetesNodePoolStateCreating, CreatedAt: testCreated,
	}
	a.pools[cluster] = append(a.pools[cluster], p)
	writeJSON(w, http.StatusCreated, p)
}

func (a *fakeAPI) updatePool(w http.ResponseWriter, r *http.Request) {
	cluster := r.PathValue("name")
	i := a.findPool(cluster, r.PathValue("pool"))
	if i < 0 {
		notFound(w, "node pool")
		return
	}
	var req verda.UpdateManagedKubernetesNodePoolRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Replicas != nil {
		a.pools[cluster][i].Replicas = *req.Replicas
	}
	writeJSON(w, http.StatusOK, a.pools[cluster][i])
}

// createKubeconfig answers pending with a server-chosen ID, as the API does.
func (a *fakeAPI) createKubeconfig(w http.ResponseWriter, r *http.Request) {
	cluster := r.PathValue("name")
	var req verda.CreateManagedKubernetesKubeconfigRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	k := verda.ManagedKubernetesKubeconfig{
		ID: a.id(cluster + "-kc"), DisplayName: req.DisplayName, CommonName: req.CommonName,
		Groups: req.Groups, Status: verda.ManagedKubernetesKubeconfigStatusPending,
	}
	a.kubeconfigs[cluster] = append(a.kubeconfigs[cluster], k)
	k.Kubeconfig = ""
	writeJSON(w, http.StatusCreated, k)
}

// newFakeClient serves api and returns an SDK client pointed at it.
func newFakeClient(t *testing.T, api *fakeAPI) *verda.Client {
	t.Helper()
	srv := httptest.NewServer(api.handler())
	t.Cleanup(srv.Close)
	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("verda.NewClient: %v", err)
	}
	return client
}

// Factory shapes for the modes the commands are tested in.
func tableFactory(client *verda.Client, p *tuitest.Prompter) *cmdutil.TestFactory {
	f := &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: outputTable}
	if p != nil {
		f.PrompterOverride = p
	}
	return f
}

func jsonFactory(client *verda.Client) *cmdutil.TestFactory {
	return &cmdutil.TestFactory{ClientOverride: client, OutputFormatOverride: "json"}
}

func agentFactory(client *verda.Client) *cmdutil.TestFactory {
	return &cmdutil.TestFactory{ClientOverride: client, AgentModeOverride: true, OutputFormatOverride: "json"}
}

// runMK8s runs `verda mk8s <args>` and returns stdout and stderr.
func runMK8s(t *testing.T, f cmdutil.Factory, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := NewCmdMK8s(f, cmdutil.IOStreams{Out: &out, ErrOut: &errOut})
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err = cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// withTerminal makes interactiveTTY see a terminal. Never call t.Parallel in or under a
// test that uses it, fastPoll or withWizardAnswers: they change package variables.
func withTerminal(t *testing.T) {
	t.Helper()
	orig := isInteractiveTerminal
	isInteractiveTerminal = func() bool { return true }
	t.Cleanup(func() { isInteractiveTerminal = orig })
}

type routeFunc func(pattern string, h http.HandlerFunc)

// networkRoutes serve the private networks and subnets the wizard lists.
func (a *fakeAPI) networkRoutes(route routeFunc) {
	route("GET /instance-availability", func(w http.ResponseWriter, r *http.Request) {
		var out []verda.LocationAvailability
		for loc, types := range a.availability {
			if want := r.URL.Query().Get("location_code"); want == "" || want == loc {
				out = append(out, verda.LocationAvailability{LocationCode: loc, Availabilities: types})
			}
		}
		if out == nil {
			out = []verda.LocationAvailability{}
		}
		writeJSON(w, http.StatusOK, out)
	})
	route("GET /ssh-keys", func(w http.ResponseWriter, _ *http.Request) {
		keys := a.sshKeys
		if keys == nil {
			keys = []verda.SSHKey{}
		}
		writeJSON(w, http.StatusOK, keys)
	})
	route("GET /private-networks", func(w http.ResponseWriter, _ *http.Request) {
		networks := a.networks
		if networks == nil {
			networks = []verda.PrivateNetwork{}
		}
		writeJSON(w, http.StatusOK, networks)
	})
	route("GET /private-networks/{id}/subnets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.subnets[r.PathValue("id")])
	})
}

func (a *fakeAPI) kubeconfigRoutes(route routeFunc) {
	route("GET /mk8sclusters/{loc}/{name}/kubeconfigs", func(w http.ResponseWriter, r *http.Request) {
		list := make([]verda.ManagedKubernetesKubeconfig, 0, len(a.kubeconfigs[r.PathValue("name")]))
		for _, k := range a.kubeconfigs[r.PathValue("name")] {
			k.Kubeconfig = "" // the list never carries the YAML
			list = append(list, k)
		}
		writeJSON(w, http.StatusOK, list)
	})
	route("POST /mk8sclusters/{loc}/{name}/kubeconfigs", a.createKubeconfig)
	route("GET /mk8sclusters/{loc}/{name}/kubeconfigs/{id}", func(w http.ResponseWriter, r *http.Request) {
		i := a.findKubeconfig(r.PathValue("name"), r.PathValue("id"))
		if i < 0 {
			notFound(w, "kubeconfig")
			return
		}
		if a.onKubeconfigGet != nil {
			a.onKubeconfigGet(&a.kubeconfigs[r.PathValue("name")][i])
		}
		writeJSON(w, http.StatusOK, a.kubeconfigs[r.PathValue("name")][i])
	})
	route("DELETE /mk8sclusters/{loc}/{name}/kubeconfigs/{id}", func(w http.ResponseWriter, r *http.Request) {
		cluster := r.PathValue("name")
		i := a.findKubeconfig(cluster, r.PathValue("id"))
		if i < 0 {
			notFound(w, "kubeconfig")
			return
		}
		a.kubeconfigs[cluster] = append(a.kubeconfigs[cluster][:i], a.kubeconfigs[cluster][i+1:]...)
		w.WriteHeader(http.StatusNoContent)
	})
}

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

func conditionReady() verda.ManagedKubernetesCondition {
	return verda.ManagedKubernetesCondition{Type: "Ready", Status: "True", Reason: "AllGood", Message: "all components ready", LastTransitionTime: testCreated}
}

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

// setHome points HOME (USERPROFILE on Windows) at a new temp dir and returns it.
func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func seededClusters() *fakeAPI {
	api := newFakeAPI()
	api.addCluster("prod", "FIN-03", "running")
	api.addCluster("staging", "FIN-01", "provisioning")
	return api
}

// fastPoll shortens the wait loop. Never call t.Parallel in or under a test that
// uses it, withTerminal or withWizardAnswers: they change package variables.
func fastPoll(t *testing.T) {
	t.Helper()
	orig := pollInterval
	pollInterval = 5 * time.Millisecond
	t.Cleanup(func() { pollInterval = orig })
}

// withWizardAnswers makes the wizard take canned answers instead of a terminal.
func withWizardAnswers(t *testing.T, results ...wizard.TestResult) {
	t.Helper()
	orig := newWizardEngine
	newWizardEngine = func(cmdutil.Factory, cmdutil.IOStreams) *wizard.Engine {
		return wizard.NewEngine(nil, nil, wizard.WithOutput(io.Discard), wizard.WithTestResults(results...))
	}
	t.Cleanup(func() { newWizardEngine = orig })
}

// poolAPI has the clusters of seededClusters and two pools in "prod": workers (3/3, active) and gpu (1/2, degraded).
func poolAPI() *fakeAPI {
	api := seededClusters()
	api.addPool("prod", "workers", "active", 3, 3)
	api.addPool("prod", "gpu", "degraded", 2, 1)
	api.pools["prod"][1].Errors = []verda.ManagedKubernetesNodePoolError{
		{Reason: "insufficient_capacity", Message: "no free 1V100.6V", Since: testCreated},
	}
	return api
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: reads a file the test wrote under its own temp dir
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// assertMode checks the POSIX mode; Windows has none, so only that check is skipped.
func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", path, got, want)
	}
}

func assertNoPrivateKey(t *testing.T, label string, outs ...string) {
	t.Helper()
	for _, s := range outs {
		if strings.Contains(s, kcPrivateKeyMarker) || strings.Contains(s, "client-key-data") || strings.Contains(s, "apiVersion: v1") {
			t.Errorf("%s: the private key reached the output:\n%s", label, s)
		}
	}
}
