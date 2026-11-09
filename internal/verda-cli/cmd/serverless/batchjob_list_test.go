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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func newBatchjobListServer(t *testing.T, jobs []verda.JobDeploymentShortInfo) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token", "token_type": "Bearer",
		})
	})

	mux.HandleFunc("GET /job-deployments", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jobs)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestBatchjobList_Table(t *testing.T) {
	t.Parallel()
	jobs := []verda.JobDeploymentShortInfo{
		{
			Name:      "training-run",
			CreatedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
			Compute:   &verda.ContainerCompute{Name: "H100", Size: 2},
		},
		{
			Name:      "inference-batch",
			CreatedAt: time.Date(2026, 8, 5, 14, 0, 0, 0, time.UTC),
			Compute:   &verda.ContainerCompute{Name: "A100", Size: 1},
		},
	}
	srv := newBatchjobListServer(t, jobs)
	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable

	stdout, _, err := runBatchjob(t, f, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	for _, want := range []string{"NAME", "COMPUTE", "SIZE", "CREATED", "training-run", "H100", "inference-batch"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

func TestBatchjobList_JSON(t *testing.T) {
	t.Parallel()
	jobs := []verda.JobDeploymentShortInfo{
		{
			Name:      "my-job",
			CreatedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
			Compute:   &verda.ContainerCompute{Name: "H100", Size: 1},
		},
	}
	srv := newBatchjobListServer(t, jobs)
	f := newTestFactory(t, srv.URL)

	stdout, _, err := runBatchjob(t, f, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var got []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if len(got) != 1 || got[0].Name != "my-job" {
		t.Errorf("got %+v", got)
	}
}

func TestBatchjobList_Empty(t *testing.T) {
	t.Parallel()
	srv := newBatchjobListServer(t, []verda.JobDeploymentShortInfo{})
	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable

	stdout, _, err := runBatchjob(t, f, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout, "No batch-job deployments found") {
		t.Errorf("expected empty message; got:\n%s", stdout)
	}
}

func TestBatchjobList_NilCompute(t *testing.T) {
	t.Parallel()
	jobs := []verda.JobDeploymentShortInfo{
		{
			Name:      "no-compute",
			CreatedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		},
	}
	srv := newBatchjobListServer(t, jobs)
	f := newTestFactory(t, srv.URL)
	f.OutputFormatOverride = outputTable

	stdout, _, err := runBatchjob(t, f, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout, "no-compute") {
		t.Errorf("expected deployment name in output; got:\n%s", stdout)
	}
}
