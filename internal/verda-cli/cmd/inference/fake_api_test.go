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

package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// fakeAPI is an in-memory inference API; it never reaches a real endpoint.
type fakeAPI struct {
	mu         sync.Mutex
	models     []verda.InferenceModel
	subs       []verda.InferenceSubscription
	keys       []verda.InferenceAPIKey
	plaintext  map[string]string
	nextID     int
	created    []string
	deleted    []string
	subscribed []string
	failCreate bool
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		plaintext: map[string]string{},
		models: []verda.InferenceModel{
			{
				Code: "qwen3-coder", EndpointModel: "Qwen/Qwen3-Coder", Name: "Qwen3 Coder", Category: "text",
				Status: verda.InferenceModelStatusAvailable, AcceptNewSubscriptions: true,
				ServiceTiers: []string{"standard", "flex"}, ContextLength: new(int64(262144)), MaxOutputLength: new(int64(65536)),
			},
			{
				Code: "kimi-k2", EndpointModel: "moonshotai/Kimi-K2", Name: "Kimi K2", Category: "text",
				Status: verda.InferenceModelStatusBeta, AcceptNewSubscriptions: true,
				ServiceTiers: []string{"standard"}, ContextLength: new(int64(131072)),
			},
			{
				Code: "flux", EndpointModel: "flux", Name: "Flux", Category: "image",
				Status: verda.InferenceModelStatusAvailable, AcceptNewSubscriptions: true, ServiceTiers: []string{"standard"},
			},
			{
				Code: "future", EndpointModel: "future", Name: "Future", Category: "text",
				Status: verda.InferenceModelStatusComingSoon, ServiceTiers: []string{"standard"},
			},
		},
		subs: []verda.InferenceSubscription{{ModelCode: "kimi-k2", ServiceTier: "standard", QuotaTier: 1, RPM: new(int64(60))}},
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]string{"access_token": "test-token", "token_type": "Bearer"})
	})
	mux.HandleFunc("GET /inference/models", func(w http.ResponseWriter, _ *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		writeJSON(w, map[string]any{"data": a.models})
	})
	mux.HandleFunc("GET /inference/models/{code}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, m := range a.models {
			if m.Code == r.PathValue("code") {
				writeJSON(w, verda.InferenceModelDetail{InferenceModel: m, QuotaTier: 1})
				return
			}
		}
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("GET /inference/subscriptions", func(w http.ResponseWriter, _ *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		writeJSON(w, a.subs)
	})
	mux.HandleFunc("GET /inference/subscriptions/status", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, verda.InferenceTenantStatus{Status: "active", HasMinimumBalance: true})
	})
	mux.HandleFunc("PUT /inference/subscriptions/{code}/{tier}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		s := verda.InferenceSubscription{ModelCode: r.PathValue("code"), ServiceTier: r.PathValue("tier")}
		a.subs = append(a.subs, s)
		a.subscribed = append(a.subscribed, s.ModelCode+"/"+s.ServiceTier)
		writeJSON(w, s)
	})
	mux.HandleFunc("DELETE /inference/subscriptions/{code}/{tier}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.subs = slices.DeleteFunc(a.subs, func(s verda.InferenceSubscription) bool {
			return s.ModelCode == r.PathValue("code") && s.ServiceTier == r.PathValue("tier")
		})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /inference/api-keys", func(w http.ResponseWriter, _ *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		writeJSON(w, a.keys)
	})
	mux.HandleFunc("POST /inference/api-keys", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.failCreate {
			http.Error(w, `{"message":"boom"}`, http.StatusBadRequest)
			return
		}
		var req verda.CreateInferenceAPIKeyRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		a.nextID++
		id := fmt.Sprintf("key-%d", a.nextID)
		k := verda.InferenceAPIKey{ID: id, Label: req.Label, MaskedKey: "dcs_…" + id, CreatedAt: time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)}
		a.keys = append(a.keys, k)
		a.created = append(a.created, req.Label)
		a.plaintext[id] = fmt.Sprintf("dcs_plaintext%04d", a.nextID)
		writeJSON(w, verda.CreatedInferenceAPIKey{InferenceAPIKey: k, Key: a.plaintext[id]})
	})
	mux.HandleFunc("DELETE /inference/api-keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		id := r.PathValue("id")
		n := len(a.keys)
		a.keys = slices.DeleteFunc(a.keys, func(k verda.InferenceAPIKey) bool { return k.ID == id })
		if len(a.keys) == n {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			return
		}
		a.deleted = append(a.deleted, id)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /inference/usage", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, verda.InferenceUsage{
			Rows:   []verda.InferenceUsageRow{{Day: "2026-11-22", Model: "qwen3-coder", InferenceUsageTotals: verda.InferenceUsageTotals{Requests: 3, InputTokens: 100, OutputTokens: 50}}},
			Totals: verda.InferenceUsageTotals{Requests: 3, InputTokens: 100, OutputTokens: 50},
			Range:  verda.InferenceUsageRange{Start: r.URL.Query().Get("start"), End: r.URL.Query().Get("end")},
		})
	})
	mux.HandleFunc("GET /inference/rate-limits", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, verda.InferenceTier{Tier: 1, RateLimits: []verda.InferenceRateLimit{{ModelCode: "kimi-k2", ServiceTier: "standard", RPM: new(int64(60))}}})
	})
	mux.HandleFunc("GET /inference/tiers", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []verda.InferenceTier{{Tier: 1}, {Tier: 2}})
	})
	return mux
}

func (a *fakeAPI) client(t *testing.T) *verda.Client {
	t.Helper()
	srv := httptest.NewServer(a.handler())
	t.Cleanup(srv.Close)
	c, err := verda.NewClient(verda.WithBaseURL(srv.URL), verda.WithClientID("id"), verda.WithClientSecret("secret"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (a *fakeAPI) keyCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.keys)
}

type testHost struct {
	env       *hostEnv
	vars      map[string]string
	installed []string
	version   string
}

// newTestHost: a fake machine rooted in t.TempDir(); nothing outside it is touched.
func newTestHost(t *testing.T) *testHost {
	t.Helper()
	home := t.TempDir()
	h := &testHost{vars: map[string]string{"SHELL": "/bin/zsh"}}
	h.env = &hostEnv{
		Home:     home,
		VerdaDir: filepath.Join(home, ".verda"),
		GOOS:     "linux",
		Getenv:   func(k string) string { return h.vars[k] },
		LookPath: func(bin string) (string, error) {
			if slices.Contains(h.installed, bin) {
				return "/usr/bin/" + bin, nil
			}
			return "", errors.New("not found")
		},
		Hostname: func() (string, error) { return "testhost", nil },
		Version: func(context.Context, string) (string, error) {
			if h.version == "" {
				return "", errors.New("not installed")
			}
			return h.version, nil
		},
	}
	h.initKimi()
	return h
}

// initKimi simulates a Kimi Code that has been launched once.
func (h *testHost) initKimi() {
	_ = os.MkdirAll(h.path(".kimi-code"), 0o700)
}

func (h *testHost) path(rel string) string { return filepath.Join(h.env.Home, rel) }

type runOpts struct {
	format   string
	agent    bool
	prompter *tuitest.Prompter
}

func runInference(t *testing.T, api *fakeAPI, h *testHost, o runOpts, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	ioStreams := cmdutil.IOStreams{Out: &out, ErrOut: &errOut}
	f := &cmdutil.TestFactory{OutputFormatOverride: o.format, AgentModeOverride: o.agent}
	if api != nil {
		f.ClientOverride = api.client(t)
	}
	if o.prompter != nil {
		f.PrompterOverride = o.prompter
	}
	root := &cobra.Command{Use: "verda", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newCmdInference(f, ioStreams, func() (*hostEnv, error) { return h.env, nil }))
	root.SetArgs(append([]string{"inference"}, args...))
	err = root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func errCode(err error) string {
	if err == nil {
		return ""
	}
	return cmdutil.ClassifyError(err).Code
}
