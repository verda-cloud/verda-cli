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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// JobDeploymentShortInfo carries a time.Time CreatedAt, so a deployment the API
// sent no created_at for used to be reported as created on 0001-01-01.
func TestBatchjobListOmitsZeroCreatedAt(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("GET /job-deployments", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"job-a"},` +
			`{"name":"job-b","created_at":"2026-08-11T18:51:12Z"}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var stdout, stderr bytes.Buffer
	f := newTestFactory(t, srv.URL)
	cmd := newCmdBatchjobList(f, cmdutil.IOStreams{Out: &stdout, ErrOut: &stderr})
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("batchjob list: %v\nstderr:\n%s", err, stderr.String())
	}

	out := stdout.String()
	if strings.Contains(out, "0001-01-01") {
		t.Errorf("batchjob list emits a zero timestamp as data:\n%s", out)
	}

	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2:\n%s", len(got), out)
	}
	if _, ok := got[0]["created_at"]; ok {
		t.Errorf("undated deployment carries created_at: %v", got[0])
	}
	if got[1]["created_at"] != "2026-08-11T18:51:12Z" {
		t.Errorf("dated deployment lost created_at: %v", got[1])
	}
	if got[0]["name"] != "job-a" {
		t.Errorf("name dropped by the view: %v", got[0])
	}
}
