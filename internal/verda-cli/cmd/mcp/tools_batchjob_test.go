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

package mcp

import (
	"maps"
	"net/http"
	"testing"

	"github.com/verda-cloud/verda-cli/tests/contract/mockapi"
)

func batchjobCreateArgs(extra map[string]any) map[string]any {
	a := map[string]any{
		"name":             "nightly",
		"image":            "ghcr.io/org/embed:v1",
		"compute":          mockapi.ComputeH100,
		"deadline_seconds": float64(1800),
		"confirm":          true,
	}
	maps.Copy(a, extra)
	return a
}

func TestCreateBatchjobConfirmGateAndWire(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)

	a := batchjobCreateArgs(nil)
	delete(a, "confirm")
	assertToolErrorCode(t, call(t, s.handleCreateBatchjob, "create_batchjob", a), "CONFIRMATION_REQUIRED")
	if n := mock.RequestCount(http.MethodPost, "/job-deployments"); n != 0 {
		t.Fatalf("refused create must have zero side effects; mock saw %d POSTs", n)
	}

	out := parseSuccess(t, call(t, s.handleCreateBatchjob, "create_batchjob", batchjobCreateArgs(nil)))
	if out["name"] != "nightly" || out["status"] != mockapi.JobStatusReady {
		t.Fatalf("create result = %v", out)
	}
	b := mock.JobCreateBodies()[0]
	if got := dig(t, b, "scaling", "deadline_seconds"); got != float64(1800) {
		t.Errorf("deadline_seconds = %v, want 1800", got)
	}
	if got := dig(t, b, "scaling", "max_replica_count"); got != float64(3) {
		t.Errorf("max_replica_count default = %v, want 3", got)
	}
	if got := dig(t, b, "containers", 0, "volume_mounts", 0, "type"); got != "scratch" {
		t.Errorf("mount type = %v, want scratch", got)
	}
	if _, spot := b["is_spot"]; spot {
		t.Error("batch jobs have no spot field; it must not be on the wire")
	}
}

func TestCreateBatchjobDeadlineValidation(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)

	a := batchjobCreateArgs(nil)
	delete(a, "deadline_seconds")
	assertToolErrorCode(t, call(t, s.handleCreateBatchjob, "create_batchjob", a), "MISSING_REQUIRED_FLAGS")

	assertFieldError(t, call(t, s.handleCreateBatchjob, "create_batchjob",
		batchjobCreateArgs(map[string]any{"deadline_seconds": float64(60)})), "deadline_seconds")
	assertFieldError(t, call(t, s.handleCreateBatchjob, "create_batchjob",
		batchjobCreateArgs(map[string]any{"deadline_seconds": "1800"})), "deadline_seconds")
	assertFieldError(t, call(t, s.handleCreateBatchjob, "create_batchjob",
		batchjobCreateArgs(map[string]any{"image": "ghcr.io/org/embed"})), "image")
	if n := mock.RequestCount(http.MethodPost, "/job-deployments"); n != 0 {
		t.Fatalf("validation must fail before any request; mock saw %d POSTs", n)
	}
}

func TestBatchjobActions(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedJobDeployment("nightly")

	// restart does not exist for jobs.
	assertFieldError(t, call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "nightly", "action": "restart"}), "action")

	assertToolErrorCode(t, call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "nightly", "action": "purge_queue"}), "CONFIRMATION_REQUIRED")
	assertToolErrorCode(t, call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "nightly", "action": "delete"}), "CONFIRMATION_REQUIRED")
	if mock.RequestCount(http.MethodPost, "/job-deployments/nightly/purge-queue")+mock.RequestCount(http.MethodDelete, "/job-deployments/nightly") != 0 {
		t.Fatal("refused actions must have zero side effects")
	}

	out := parseSuccess(t, call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "nightly", "action": "pause", "wait": true}))
	if out["status"] != statusCompleted || out["deployment_status"] != mockapi.JobStatusPaused {
		t.Fatalf("pause wait = %v", out)
	}
	out = parseSuccess(t, call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "nightly", "action": "purge_queue", "confirm": true}))
	if out["status"] != statusAccepted {
		t.Fatalf("purge_queue = %v, want accepted", out)
	}
	out = parseSuccess(t, call(t, s.handleBatchjobAction, "batchjob_action", map[string]any{"name": "nightly", "action": "delete", "confirm": true}))
	if out["status"] != statusCompleted || mock.HasJob("nightly") {
		t.Fatalf("delete = %v; job still present: %v", out, mock.HasJob("nightly"))
	}
}

func TestListAndDescribeBatchjob(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedJobDeployment("nightly")

	var jobs []map[string]any
	mustJSON(t, resultText(t, call(t, s.handleListBatchjobs, "list_batchjobs", map[string]any{})), &jobs)
	if len(jobs) != 1 || jobs[0]["name"] != "nightly" {
		t.Fatalf("list = %v", jobs)
	}

	out := parseSuccess(t, call(t, s.handleDescribeBatchjob, "describe_batchjob", map[string]any{"name": "nightly"}))
	if out["status"] != mockapi.JobStatusReady {
		t.Errorf("status = %v", out["status"])
	}
	if got := dig(t, out, "scaling", "deadline_seconds"); got != float64(1800) {
		t.Errorf("scaling.deadline_seconds = %v", got)
	}
	if res := call(t, s.handleDescribeBatchjob, "describe_batchjob", map[string]any{"name": "missing"}); !res.IsError {
		t.Error("unknown job must be a tool error")
	}
}
