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

package serverless

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
)

// envServerConfig drives newEnvServer: which routes fail, what the env GET
// groups are keyed on, and which deployments the picker sees.
type envServerConfig struct {
	// fail maps a route key to the HTTP status it returns instead of succeeding.
	// Keys: "GET list", "GET deployment", "GET env", "POST env", "PATCH env", "DELETE env".
	fail map[string]int
	// groupContainer overrides container_name in the env GET group; "" means "<deployment>-0".
	groupContainer string
	// deployments is what GET /container-deployments lists for the picker.
	deployments []string
	envVars     []verda.ContainerEnvVar
}

func (c envServerConfig) failed(w http.ResponseWriter, key string) bool {
	status, ok := c.fail[key]
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": "test_failure", "message": key + " failed"})
	return true
}

func newEnvServer(t *testing.T, cfg envServerConfig) *envRecordingServer {
	t.Helper()
	rec := &envRecordingServer{envVars: cfg.envVars}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "test-token", "token_type": "Bearer"})
	})

	mux.HandleFunc("GET /container-deployments", func(w http.ResponseWriter, _ *http.Request) {
		if cfg.failed(w, "GET list") {
			return
		}
		list := make([]verda.ContainerDeployment, 0, len(cfg.deployments))
		for _, name := range cfg.deployments {
			list = append(list, verda.ContainerDeployment{Name: name})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	})

	mux.HandleFunc("GET /container-deployments/{name}", func(w http.ResponseWriter, r *http.Request) {
		if cfg.failed(w, "GET deployment") {
			return
		}
		name := r.PathValue("name")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.ContainerDeployment{
			Name:       name,
			Containers: []verda.DeploymentContainer{{Name: name + "-0"}},
		})
	})

	mux.HandleFunc("GET /container-deployments/{name}/environment-variables", func(w http.ResponseWriter, r *http.Request) {
		if cfg.failed(w, "GET env") {
			return
		}
		group := cfg.groupContainer
		if group == "" {
			group = r.PathValue("name") + "-0"
		}
		rec.mu.Lock()
		v := rec.envVars
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"container_name": group, "env": v}})
	})

	record := func(key string, slot *[]byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if cfg.failed(w, key) {
				return
			}
			body, _ := io.ReadAll(r.Body)
			rec.mu.Lock()
			*slot = body
			rec.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		}
	}
	mux.HandleFunc("POST /container-deployments/{name}/environment-variables", record("POST env", &rec.addBody))
	mux.HandleFunc("PATCH /container-deployments/{name}/environment-variables", record("PATCH env", &rec.patchBody))
	mux.HandleFunc("DELETE /container-deployments/{name}/environment-variables", record("DELETE env", &rec.delBody))

	rec.srv = httptest.NewServer(mux)
	t.Cleanup(rec.srv.Close)
	return rec
}

// runEnv executes `verda container env <args...>` and returns stdout, stderr, err.
func runEnv(t *testing.T, f cmdutil.Factory, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := NewCmdContainer(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(append([]string{"env"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// --- no client / no name ---

// Every subcommand surfaces the missing-credentials error from the factory
// instead of panicking or prompting.
func TestEnv_NoClient(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"list":  {"list", "my-endpoint"},
		"set":   {"set", "my-endpoint", "--env", "A=1"},
		"unset": {"unset", "--deployment", "my-endpoint", "--yes", "A"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := &cmdutil.TestFactory{OutputFormatOverride: "json"}
			_, _, err := runEnv(t, f, args...)
			if !errors.Is(err, cmdutil.ErrNoClient) {
				t.Fatalf("want ErrNoClient, got %v", err)
			}
		})
	}
}

// Without a terminal (here: json output) and without a name, list and set
// cannot open the picker and must report the missing argument.
func TestEnv_MissingName_NonInteractive(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"list":  {"list"},
		"set":   {"set", "--env", "A=1"},
		"unset": {"unset", "A"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := newEnvServer(t, envServerConfig{})
			f := newTestFactory(t, rec.srv.URL)
			f.AgentModeOverride = false
			_, _, err := runEnv(t, f, args...)
			if agentErrorCode(err) != "MISSING_REQUIRED_FLAGS" {
				t.Fatalf("want MISSING_REQUIRED_FLAGS, got %v", err)
			}
		})
	}
}

// --- API failures ---

func TestEnvList_FetchError(t *testing.T) {
	t.Parallel()
	rec := newEnvServer(t, envServerConfig{fail: map[string]int{"GET env": http.StatusInternalServerError}})
	f := newTestFactory(t, rec.srv.URL)
	stdout, _, err := runEnv(t, f, "list", "my-endpoint")
	if err == nil {
		t.Fatal("want error from env GET, got nil")
	}
	if stdout != "" {
		t.Errorf("want no stdout on failure, got %q", stdout)
	}
}

// list resolves the container through the deployment GET like set and unset,
// so an unknown deployment fails there instead of reading someone's env GET.
func TestEnvList_DeploymentLookupError(t *testing.T) {
	t.Parallel()
	rec := newEnvServer(t, envServerConfig{fail: map[string]int{"GET deployment": http.StatusNotFound}})
	f := newTestFactory(t, rec.srv.URL)
	stdout, _, err := runEnv(t, f, "list", "missing")
	if err == nil {
		t.Fatal("want error from deployment GET, got nil")
	}
	if stdout != "" {
		t.Errorf("want no stdout on failure, got %q", stdout)
	}
}

func TestEnvSet_DeploymentLookupError(t *testing.T) {
	t.Parallel()
	rec := newEnvServer(t, envServerConfig{fail: map[string]int{"GET deployment": http.StatusNotFound}})
	f := newTestFactory(t, rec.srv.URL)
	_, _, err := runEnv(t, f, "set", "missing", "--env", "A=1")
	if err == nil {
		t.Fatal("want error from deployment GET, got nil")
	}
	if rec.getAddBody() != nil || rec.getPatchBody() != nil {
		t.Error("want no env write after a failed deployment lookup")
	}
}

func TestEnvSet_EnvFetchError(t *testing.T) {
	t.Parallel()
	rec := newEnvServer(t, envServerConfig{fail: map[string]int{"GET env": http.StatusInternalServerError}})
	f := newTestFactory(t, rec.srv.URL)
	_, _, err := runEnv(t, f, "set", "my-endpoint", "--env", "A=1")
	if err == nil {
		t.Fatal("want error from env GET, got nil")
	}
	if rec.getAddBody() != nil || rec.getPatchBody() != nil {
		t.Error("want no env write after a failed env fetch")
	}
}

// The env GET and the deployment GET are separate reads; when the resolved
// container has no group yet, every variable counts as new and is added.
func TestEnvSet_ContainerMissingFromGroups(t *testing.T) {
	t.Parallel()
	rec := newEnvServer(t, envServerConfig{
		groupContainer: "someone-else-0",
		envVars:        []verda.ContainerEnvVar{{Name: "A", Type: "plain", ValueOrReferenceToSecret: "old"}},
	})
	f := newTestFactory(t, rec.srv.URL)
	stdout, _, err := runEnv(t, f, "set", "my-endpoint", "--env", "A=new")
	if err != nil {
		t.Fatalf("env set: %v", err)
	}
	if rec.getPatchBody() != nil {
		t.Error("want no PATCH: A is not known on my-endpoint-0")
	}
	var add struct {
		ContainerName string `json:"container_name"`
	}
	if err := json.Unmarshal(rec.getAddBody(), &add); err != nil {
		t.Fatalf("unmarshal add body: %v", err)
	}
	if add.ContainerName != "my-endpoint-0" {
		t.Errorf("add container_name: got %q, want my-endpoint-0", add.ContainerName)
	}
	if !strings.Contains(stdout, `"added": 1`) || !strings.Contains(stdout, `"updated": 0`) {
		t.Errorf("want added=1 updated=0, got %s", stdout)
	}
}

func TestEnvSet_InvalidEnvFlag(t *testing.T) {
	t.Parallel()
	rec := newEnvServer(t, envServerConfig{})
	f := newTestFactory(t, rec.srv.URL)
	_, _, err := runEnv(t, f, "set", "my-endpoint", "--env", "lower=1")
	if err == nil {
		t.Fatal("want validation error for lowercase env name, got nil")
	}
	if rec.getAddBody() != nil {
		t.Error("want no API write on a validation error")
	}
}

func TestEnvSet_AddError(t *testing.T) {
	t.Parallel()
	rec := newEnvServer(t, envServerConfig{fail: map[string]int{"POST env": http.StatusBadRequest}})
	f := newTestFactory(t, rec.srv.URL)
	stdout, _, err := runEnv(t, f, "set", "my-endpoint", "--env", "A=1")
	if err == nil {
		t.Fatal("want error from POST, got nil")
	}
	if stdout != "" {
		t.Errorf("want no success output, got %q", stdout)
	}
}

func TestEnvSet_UpdateError(t *testing.T) {
	t.Parallel()
	rec := newEnvServer(t, envServerConfig{
		fail:    map[string]int{"PATCH env": http.StatusBadRequest},
		envVars: []verda.ContainerEnvVar{{Name: "A", Type: "plain", ValueOrReferenceToSecret: "old"}},
	})
	f := newTestFactory(t, rec.srv.URL)
	stdout, _, err := runEnv(t, f, "set", "my-endpoint", "--env", "A=new", "--env", "B=1")
	if err == nil {
		t.Fatal("want error from PATCH, got nil")
	}
	if rec.getAddBody() == nil {
		t.Error("want the add to have been sent before the failing update")
	}
	if stdout != "" {
		t.Errorf("want no success output, got %q", stdout)
	}
}

func TestEnvUnset_DeploymentLookupError(t *testing.T) {
	t.Parallel()
	rec := newEnvServer(t, envServerConfig{fail: map[string]int{"GET deployment": http.StatusNotFound}})
	f := newTestFactory(t, rec.srv.URL)
	_, _, err := runEnv(t, f, "unset", "--deployment", "missing", "--yes", "A")
	if err == nil {
		t.Fatal("want error from deployment GET, got nil")
	}
	if rec.getDelBody() != nil {
		t.Error("want no DELETE after a failed deployment lookup")
	}
}

func TestEnvUnset_DeleteError(t *testing.T) {
	t.Parallel()
	rec := newEnvServer(t, envServerConfig{fail: map[string]int{"DELETE env": http.StatusInternalServerError}})
	f := newTestFactory(t, rec.srv.URL)
	stdout, _, err := runEnv(t, f, "unset", "--deployment", "my-endpoint", "--yes", "A")
	if err == nil {
		t.Fatal("want error from DELETE, got nil")
	}
	if stdout != "" {
		t.Errorf("want no success output, got %q", stdout)
	}
}

// --- unset confirmation ---

func TestEnvUnset_Confirm(t *testing.T) {
	t.Parallel()
	t.Run("accepted sends the delete", func(t *testing.T) {
		t.Parallel()
		rec := newEnvServer(t, envServerConfig{})
		f := newTestFactory(t, rec.srv.URL)
		f.AgentModeOverride = false
		f.PrompterOverride = tuitest.New().AddConfirm(true)
		_, stderr, err := runEnv(t, f, "unset", "--deployment", "my-endpoint", "A", "B")
		if err != nil {
			t.Fatalf("env unset: %v", err)
		}
		if !strings.Contains(stderr, "Remove 2 variable(s) from my-endpoint") {
			t.Errorf("want warning heading on stderr, got %q", stderr)
		}
		var del struct {
			ContainerName string   `json:"container_name"`
			Env           []string `json:"env"`
		}
		if err := json.Unmarshal(rec.getDelBody(), &del); err != nil {
			t.Fatalf("unmarshal delete body: %v", err)
		}
		if del.ContainerName != "my-endpoint-0" || len(del.Env) != 2 {
			t.Errorf("delete body: got %+v", del)
		}
	})
	t.Run("declined is a clean no-op", func(t *testing.T) {
		t.Parallel()
		rec := newEnvServer(t, envServerConfig{})
		f := newTestFactory(t, rec.srv.URL)
		f.AgentModeOverride = false
		f.PrompterOverride = tuitest.New().AddConfirm(false)
		stdout, _, err := runEnv(t, f, "unset", "--deployment", "my-endpoint", "A")
		if err != nil {
			t.Fatalf("want nil on decline, got %v", err)
		}
		if rec.getDelBody() != nil {
			t.Error("want no DELETE when the confirm is declined")
		}
		if stdout != "" {
			t.Errorf("want no output on decline, got %q", stdout)
		}
	})
	t.Run("prompter failure is surfaced", func(t *testing.T) {
		t.Parallel()
		rec := newEnvServer(t, envServerConfig{})
		f := newTestFactory(t, rec.srv.URL)
		f.AgentModeOverride = false
		f.PrompterOverride = tuitest.New() // no confirm queued → real error
		_, _, err := runEnv(t, f, "unset", "--deployment", "my-endpoint", "A")
		if err == nil || cmdutil.IsPromptCancel(err) {
			t.Fatalf("want a real prompter error, got %v", err)
		}
		if rec.getDelBody() != nil {
			t.Error("want no DELETE after a prompter failure")
		}
	})
}

// --- interactive picker ---

// interactiveEnvFactory: table output, non-agent, scripted prompter; pair with withTerminal.
func interactiveEnvFactory(t *testing.T, rec *envRecordingServer, p *tuitest.Prompter) *cmdutil.TestFactory {
	t.Helper()
	f := newTestFactory(t, rec.srv.URL)
	f.AgentModeOverride = false
	f.OutputFormatOverride = outputTable
	f.PrompterOverride = p
	return f
}

var pickerDeployments = []string{"alpha", "beta"}

// The picker path mutates the package-level terminal probe, so these tests
// are not parallel; the subtests share the override.
func TestEnvList_InteractivePicker(t *testing.T) {
	withTerminal(t)
	deployments := pickerDeployments

	t.Run("list picks a deployment", func(t *testing.T) {
		rec := newEnvServer(t, envServerConfig{
			deployments: deployments,
			envVars:     []verda.ContainerEnvVar{{Name: "A", Type: "plain", ValueOrReferenceToSecret: "1"}},
		})
		stdout, _, err := runEnv(t, interactiveEnvFactory(t, rec, tuitest.New().AddSelect(1)), "list")
		if err != nil {
			t.Fatalf("env list: %v", err)
		}
		if !strings.Contains(stdout, "A") || !strings.Contains(stdout, "plain") {
			t.Errorf("want table with A, got %q", stdout)
		}
	})
	t.Run("list cancel at picker exits cleanly", func(t *testing.T) {
		rec := newEnvServer(t, envServerConfig{deployments: deployments})
		stdout, _, err := runEnv(t, interactiveEnvFactory(t, rec, tuitest.New().AddSelect(len(deployments))), "list")
		if err != nil {
			t.Fatalf("want nil on cancel, got %v", err)
		}
		if stdout != "" {
			t.Errorf("want no output on cancel, got %q", stdout)
		}
	})
	t.Run("list with no deployments", func(t *testing.T) {
		rec := newEnvServer(t, envServerConfig{})
		stdout, _, err := runEnv(t, interactiveEnvFactory(t, rec, tuitest.New()), "list")
		if err != nil {
			t.Fatalf("want nil on empty list, got %v", err)
		}
		if !strings.Contains(stdout, "No container deployments found") {
			t.Errorf("want empty-list notice, got %q", stdout)
		}
	})
	t.Run("list picker load error", func(t *testing.T) {
		rec := newEnvServer(t, envServerConfig{fail: map[string]int{"GET list": http.StatusInternalServerError}})
		if _, _, err := runEnv(t, interactiveEnvFactory(t, rec, tuitest.New()), "list"); err == nil {
			t.Fatal("want error from deployments GET, got nil")
		}
	})
}

func TestEnvWrite_InteractivePicker(t *testing.T) {
	withTerminal(t)
	deployments := pickerDeployments

	t.Run("set picks a deployment and writes to its container", func(t *testing.T) {
		rec := newEnvServer(t, envServerConfig{deployments: deployments})
		stdout, _, err := runEnv(t, interactiveEnvFactory(t, rec, tuitest.New().AddSelect(0)), "set", "--env", "A=1")
		if err != nil {
			t.Fatalf("env set: %v", err)
		}
		var add struct {
			ContainerName string `json:"container_name"`
		}
		if err := json.Unmarshal(rec.getAddBody(), &add); err != nil {
			t.Fatalf("unmarshal add body: %v", err)
		}
		if add.ContainerName != "alpha-0" {
			t.Errorf("add container_name: got %q, want alpha-0", add.ContainerName)
		}
		if !strings.Contains(stdout, `deployment "alpha"`) {
			t.Errorf("want success line naming alpha, got %q", stdout)
		}
	})
	t.Run("set cancel at picker exits cleanly", func(t *testing.T) {
		rec := newEnvServer(t, envServerConfig{deployments: deployments})
		_, _, err := runEnv(t, interactiveEnvFactory(t, rec, tuitest.New().AddSelect(len(deployments))), "set", "--env", "A=1")
		if err != nil {
			t.Fatalf("want nil on cancel, got %v", err)
		}
		if rec.getAddBody() != nil {
			t.Error("want no API write on cancel")
		}
	})
	t.Run("unset picks a deployment then confirms", func(t *testing.T) {
		rec := newEnvServer(t, envServerConfig{deployments: deployments})
		stdout, _, err := runEnv(t, interactiveEnvFactory(t, rec, tuitest.New().AddSelect(1).AddConfirm(true)), "unset", "A")
		if err != nil {
			t.Fatalf("env unset: %v", err)
		}
		var del struct {
			ContainerName string `json:"container_name"`
		}
		if err := json.Unmarshal(rec.getDelBody(), &del); err != nil {
			t.Fatalf("unmarshal delete body: %v", err)
		}
		if del.ContainerName != "beta-0" {
			t.Errorf("delete container_name: got %q, want beta-0", del.ContainerName)
		}
		if !strings.Contains(stdout, `deployment "beta"`) {
			t.Errorf("want success line naming beta, got %q", stdout)
		}
	})
	t.Run("unset cancel at picker exits cleanly", func(t *testing.T) {
		rec := newEnvServer(t, envServerConfig{deployments: deployments})
		_, _, err := runEnv(t, interactiveEnvFactory(t, rec, tuitest.New().AddSelect(len(deployments))), "unset", "A")
		if err != nil {
			t.Fatalf("want nil on cancel, got %v", err)
		}
		if rec.getDelBody() != nil {
			t.Error("want no DELETE on cancel")
		}
	})
	t.Run("unset picker load error", func(t *testing.T) {
		rec := newEnvServer(t, envServerConfig{fail: map[string]int{"GET list": http.StatusInternalServerError}})
		if _, _, err := runEnv(t, interactiveEnvFactory(t, rec, tuitest.New()), "unset", "A"); err == nil {
			t.Fatal("want error from deployments GET, got nil")
		}
	})
}

// --- helpers ---

func TestEnvVarsOf(t *testing.T) {
	t.Parallel()
	a := []verda.ContainerEnvVar{{Name: "A"}}
	b := []verda.ContainerEnvVar{{Name: "B"}}
	groups := []envVarGroup{{ContainerName: "x-0", Env: a}, {ContainerName: "y-0", Env: b}}

	if got := envVarsOf(nil, "x-0"); got != nil {
		t.Errorf("no groups: want nil, got %v", got)
	}
	if got := envVarsOf(groups, "x-0"); len(got) != 1 || got[0].Name != "A" {
		t.Errorf("first group: want A, got %v", got)
	}
	if got := envVarsOf(groups, "y-0"); len(got) != 1 || got[0].Name != "B" {
		t.Errorf("named group: want B, got %v", got)
	}
	if got := envVarsOf(groups, "z-0"); got != nil {
		t.Errorf("unknown container: want nil, got %v", got)
	}
}
