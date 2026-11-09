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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type batchjobActionRecorder struct {
	mu      sync.Mutex
	actions []string
	srv     *httptest.Server
}

func newBatchjobActionServer(t *testing.T) *batchjobActionRecorder {
	t.Helper()
	rec := &batchjobActionRecorder{}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token", "token_type": "Bearer",
		})
	})

	mux.HandleFunc("POST /job-deployments/{name}/{action}", func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.actions = append(rec.actions, r.PathValue("action"))
		rec.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	rec.srv = httptest.NewServer(mux)
	t.Cleanup(rec.srv.Close)
	return rec
}

func runBatchjob(t *testing.T, f cmdutil.Factory, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := NewCmdBatchjob(f, cmdutil.IOStreams{Out: &out, ErrOut: &errOut})
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestBatchjobPause_AgentMode(t *testing.T) {
	t.Parallel()
	rec := newBatchjobActionServer(t)
	f := newTestFactory(t, rec.srv.URL)

	stdout, _, err := runBatchjob(t, f, "pause", "my-job")
	if err != nil {
		t.Fatalf("pause: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if got["name"] != "my-job" || got["action"] != "pause" || got["status"] != "accepted" {
		t.Errorf("got %v", got)
	}
}

func TestBatchjobResume_AgentMode(t *testing.T) {
	t.Parallel()
	rec := newBatchjobActionServer(t)
	f := newTestFactory(t, rec.srv.URL)

	stdout, _, err := runBatchjob(t, f, "resume", "my-job")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if got["action"] != "resume" || got["status"] != "accepted" {
		t.Errorf("got %v", got)
	}
}

func TestBatchjobPurgeQueue_AgentMode(t *testing.T) {
	t.Parallel()
	rec := newBatchjobActionServer(t)
	f := newTestFactory(t, rec.srv.URL)

	stdout, _, err := runBatchjob(t, f, "purge-queue", "my-job", "--yes")
	if err != nil {
		t.Fatalf("purge-queue: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if got["action"] != "purge-queue" || got["status"] != "accepted" {
		t.Errorf("got %v", got)
	}
}

func TestBatchjobPurgeQueue_AgentMode_RequiresYes(t *testing.T) {
	t.Parallel()
	rec := newBatchjobActionServer(t)
	f := newTestFactory(t, rec.srv.URL)

	_, _, err := runBatchjob(t, f, "purge-queue", "my-job")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected *AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != "CONFIRMATION_REQUIRED" {
		t.Errorf("code = %q, want CONFIRMATION_REQUIRED", agentErr.Code)
	}
}

func TestBatchjobPause_TableOutput(t *testing.T) {
	t.Parallel()
	rec := newBatchjobActionServer(t)
	f := newTestFactory(t, rec.srv.URL)
	f.AgentModeOverride = false
	f.OutputFormatOverride = outputTable

	stdout, _, err := runBatchjob(t, f, "pause", "my-job")
	if err != nil {
		t.Fatalf("pause: %v", err)
	}

	if !strings.Contains(stdout, "Paused") {
		t.Errorf("expected 'Paused' in output; got:\n%s", stdout)
	}
}
