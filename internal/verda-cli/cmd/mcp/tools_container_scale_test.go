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
	"encoding/json"
	"net/http"
	"testing"
)

func mustJSON(t *testing.T, text string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(text), v); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
}

func TestScaleContainerConfirmGate(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")

	res := call(t, s.handleScaleContainer, "scale_container", map[string]any{"name": "api", "min_replicas": float64(1)})
	assertToolErrorCode(t, res, "CONFIRMATION_REQUIRED")
	if n := mock.RequestCount(http.MethodPatch, "/container-deployments/api/scaling"); n != 0 {
		t.Fatalf("refused scale must not PATCH; mock saw %d", n)
	}

	// No option at all is a missing-argument error, listed before the gate.
	res = call(t, s.handleScaleContainer, "scale_container", map[string]any{"name": "api", "confirm": true})
	d := toolErrorDetails(t, res, "MISSING_REQUIRED_FLAGS")
	if d["one_of"] == nil {
		t.Fatalf("details.one_of missing: %v", d)
	}

	// Limits come from the scaling package; the field is reported in argument form.
	assertFieldError(t, call(t, s.handleScaleContainer, "scale_container", map[string]any{
		"name": "api", "confirm": true, "max_replicas": float64(0),
	}), "max_replicas")
	assertFieldError(t, call(t, s.handleScaleContainer, "scale_container", map[string]any{
		"name": "api", "confirm": true, "min_replicas": float64(5), "max_replicas": float64(2),
	}), "min_replicas")
	assertFieldError(t, call(t, s.handleScaleContainer, "scale_container", map[string]any{
		"name": "api", "confirm": true, "concurrency": "2",
	}), "concurrency")
}

func TestScaleContainerAcceptedThenWait(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")

	out := parseSuccess(t, call(t, s.handleScaleContainer, "scale_container", map[string]any{
		"name": "api", "confirm": true, "min_replicas": float64(1), "queue_preset": "cost-saver",
	}))
	if out["status"] != statusAccepted {
		t.Fatalf("without wait the status is accepted: %v", out)
	}
	if got := dig(t, out, "previous", "min_replica_count"); got != float64(0) {
		t.Errorf("previous.min_replica_count = %v, want 0", got)
	}
	if got := dig(t, out, "expected", "min_replica_count"); got != float64(1) {
		t.Errorf("expected.min_replica_count = %v, want 1", got)
	}
	bodies := mock.ScalingPatchBodies()
	if len(bodies) != 1 {
		t.Fatalf("want 1 PATCH, got %d", len(bodies))
	}
	if _, sentMax := bodies[0]["max_replica_count"]; sentMax {
		t.Error("an omitted bound must not be copied from the read into the PATCH")
	}
	if got := dig(t, bodies[0], "scaling_triggers", "queue_load", "threshold"); got != float64(6) {
		t.Errorf("cost-saver threshold = %v, want 6", got)
	}

	out = parseSuccess(t, call(t, s.handleScaleContainer, "scale_container", map[string]any{
		"name": "api", "confirm": true, "max_replicas": float64(4), "wait": true,
	}))
	if out["status"] != statusCompleted {
		t.Fatalf("with wait the status is completed once reads show it: %v", out)
	}
	if got := dig(t, out, "current", "max_replica_count"); got != float64(4) {
		t.Errorf("current.max_replica_count = %v, want 4", got)
	}
}

// TestScaleContainerAPIRejectsMinAboveStoredMax: one bound alone is sent and
// the backend decides (CC-1024); the tool reports the API error, not a guess.
func TestScaleContainerAPIRejectsMinAboveStoredMax(t *testing.T) {
	t.Parallel()
	s, mock := newMockBackedServer(t)
	mock.SeedContainerDeployment("api")

	res := call(t, s.handleScaleContainer, "scale_container", map[string]any{
		"name": "api", "confirm": true, "min_replicas": float64(9),
	})
	if !res.IsError {
		t.Fatalf("expected the API 400 to surface, got %s", resultText(t, res))
	}
}
