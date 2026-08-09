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

package vm

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	tuitest "github.com/verda-cloud/verda-cli/pkg/tui/testing"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// deleteMux serves an instance with an OS volume and one data volume, plus a
// capturing PUT /instances handler so tests can assert on the volume_ids the
// client sent (nil → API default deletes the OS volume; [] → delete none).
type deleteMux struct {
	lastAction  atomic.Value // map[string]any of the last PUT /instances body
	getCalls    atomic.Int32
	status      atomic.Value // instance status returned by GET /instances/{id}
	afterAction atomic.Value // status the instance lands in after an action
}

func newDeleteMux() (*http.ServeMux, *deleteMux) {
	d := &deleteMux{}
	d.status.Store("running")
	d.afterAction.Store("offline")

	mux := baseMux()
	mux.HandleFunc("GET /instances/{id}", func(w http.ResponseWriter, _ *http.Request) {
		d.getCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":             "inst-1",
			"hostname":       "test-vm",
			"status":         d.status.Load(),
			"instance_type":  "1V100.6V",
			"location":       "FIN-01",
			"os_volume_id":   "vol-os",
			"volume_ids":     []string{"vol-data"},
			"price_per_hour": 1.5,
		})
	})
	mux.HandleFunc("GET /volumes/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		isOS := id == "vol-os"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           id,
			"name":         id,
			"size":         100,
			"type":         "NVMe",
			"status":       "attached",
			"is_os_volume": isOS,
		})
	})
	mux.HandleFunc("PUT /instances", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		d.lastAction.Store(body)
		if a, _ := body["action"].(string); a == "shutdown" {
			d.status.Store(d.afterAction.Load())
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux, d
}

// capturedVolumeIDs returns the volume_ids of the last action request,
// distinguishing null (API default) from an explicit empty list.
func (d *deleteMux) capturedVolumeIDs(t *testing.T) (ids []string, explicit bool) {
	t.Helper()
	v := d.lastAction.Load()
	if v == nil {
		t.Fatal("no action request captured")
	}
	raw, ok := v.(map[string]any)["volume_ids"]
	if !ok || raw == nil {
		return nil, false
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("volume_ids has unexpected type %T", raw)
	}
	for _, id := range list {
		ids = append(ids, id.(string))
	}
	return ids, true
}

// TestDeleteFlow_NoVolumesSelectedKeepsVolumes is the H7 regression test for
// the interactive single delete: selecting no volumes in the picker must send
// an explicit empty volume_ids, not nil (nil = API default deletes the OS
// volume, contradicting the "keeps billing" warning).
func TestDeleteFlow_NoVolumesSelectedKeepsVolumes(t *testing.T) {
	t.Parallel()

	mux, d := newDeleteMux()
	srv := newTestHarness(t, mux)
	srv.Factory.AgentModeOverride = false
	srv.Factory.OutputFormatOverride = ""
	srv.Factory.PrompterOverride = tuitest.New().
		AddMultiSelect([]int{}). // no volumes selected
		AddConfirm(true)

	cmd := NewCmdAction(srv.Factory, srv.IOStreams)
	cmd.SetArgs([]string{"--id", "inst-1", "--action", "delete"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute() returned error: %v\nStderr: %s", err, srv.Stderr.String())
	}

	ids, explicit := d.capturedVolumeIDs(t)
	if !explicit {
		t.Fatal("volume_ids was null/absent — nil invokes the API default (OS volume deleted)")
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty volume_ids (nothing selected), got %v", ids)
	}
	if !strings.Contains(srv.Stderr.String(), "continue to charge") {
		t.Error("expected the keep-billing warning when not all volumes are selected")
	}
}

// TestDeleteFlow_SelectedVolumesDeleted: exactly the picked volumes are sent.
// fetchInstanceVolumes fetches concurrently, so assert order-insensitively.
func TestDeleteFlow_SelectedVolumesDeleted(t *testing.T) {
	t.Parallel()

	mux, d := newDeleteMux()
	srv := newTestHarness(t, mux)
	srv.Factory.AgentModeOverride = false
	srv.Factory.OutputFormatOverride = ""
	srv.Factory.PrompterOverride = tuitest.New().
		AddMultiSelect([]int{0, 1}). // both volumes
		AddConfirm(true)

	cmd := NewCmdAction(srv.Factory, srv.IOStreams)
	cmd.SetArgs([]string{"--id", "inst-1", "--action", "delete"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute() returned error: %v\nStderr: %s", err, srv.Stderr.String())
	}

	ids, explicit := d.capturedVolumeIDs(t)
	slices.Sort(ids)
	if !explicit || !slices.Equal(ids, []string{"vol-data", "vol-os"}) {
		t.Fatalf("expected volume_ids=[vol-data vol-os], got %v (explicit=%v)", ids, explicit)
	}
	if strings.Contains(srv.Stderr.String(), "continue to charge") {
		t.Error("keep-billing warning shown although all volumes were selected")
	}
}

// TestDeleteAgent_DefaultKeepsVolumes mirrors the batch contract: without
// --with-volumes, agent-mode single delete sends volume_ids=[] and reports the
// batch JSON shape.
func TestDeleteAgent_DefaultKeepsVolumes(t *testing.T) {
	t.Parallel()

	mux, d := newDeleteMux()
	srv := newTestHarness(t, mux) // agent mode + JSON by default

	cmd := NewCmdAction(srv.Factory, srv.IOStreams)
	cmd.SetArgs([]string{"--id", "inst-1", "--action", "delete", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute() returned error: %v\nStderr: %s", err, srv.Stderr.String())
	}

	ids, explicit := d.capturedVolumeIDs(t)
	if !explicit || len(ids) != 0 {
		t.Fatalf("expected explicit empty volume_ids, got %v (explicit=%v)", ids, explicit)
	}

	var out map[string]any
	if err := json.Unmarshal(srv.Stdout.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, srv.Stdout.String())
	}
	if out["action"] != "delete" || out["total"] != float64(1) || out["succeeded"] != float64(1) {
		t.Errorf("unexpected batch-shaped output: %v", out)
	}
	results, ok := out["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("expected one result entry, got %v", out["results"])
	}
	entry := results[0].(map[string]any)
	if entry["instance_id"] != "inst-1" || entry["status"] != "success" {
		t.Errorf("unexpected result entry: %v", entry)
	}
	if _, leaked := out["volumes_deleted"]; leaked {
		t.Error("volumes_deleted leaked into batch-shaped output")
	}
}

// TestDeleteAgent_WithVolumesDeletesAll: --with-volumes names every attached
// volume (OS first, then data, deduplicated — same helper as batch).
func TestDeleteAgent_WithVolumesDeletesAll(t *testing.T) {
	t.Parallel()

	mux, d := newDeleteMux()
	srv := newTestHarness(t, mux)

	cmd := NewCmdAction(srv.Factory, srv.IOStreams)
	cmd.SetArgs([]string{"--id", "inst-1", "--action", "delete", "--yes", "--with-volumes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute() returned error: %v\nStderr: %s", err, srv.Stderr.String())
	}

	ids, explicit := d.capturedVolumeIDs(t)
	if !explicit || len(ids) != 2 || ids[0] != "vol-os" || ids[1] != "vol-data" {
		t.Fatalf("expected volume_ids=[vol-os vol-data], got %v (explicit=%v)", ids, explicit)
	}
}

func TestDeleteAgent_RequiresYes(t *testing.T) {
	t.Parallel()

	mux, _ := newDeleteMux()
	srv := newTestHarness(t, mux)

	cmd := NewCmdAction(srv.Factory, srv.IOStreams)
	cmd.SetArgs([]string{"--id", "inst-1", "--action", "delete"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected CONFIRMATION_REQUIRED, got nil")
	}
	if !cmdutil.IsAgentError(err) || !strings.Contains(err.Error(), "CONFIRMATION_REQUIRED") {
		t.Fatalf("expected CONFIRMATION_REQUIRED agent error, got %v", err)
	}
}

// TestWithVolumesRejectedForNonDeleteSingle: same validation as batch.
func TestWithVolumesRejectedForNonDeleteSingle(t *testing.T) {
	t.Parallel()

	mux, _ := newDeleteMux()
	srv := newTestHarness(t, mux)

	cmd := NewCmdAction(srv.Factory, srv.IOStreams)
	cmd.SetArgs([]string{"--id", "inst-1", "--action", "shutdown", "--yes", "--with-volumes"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --with-volumes on non-delete action")
	}
	if !strings.Contains(err.Error(), "--with-volumes is only valid with the delete action") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestActionAgent_DefaultReportsAccepted: agent-mode actions tell the truth —
// the API accepted the action; nothing is polled unless --wait is explicit
// (--wait defaults true but locks in before --agent is parsed).
func TestActionAgent_DefaultReportsAccepted(t *testing.T) {
	t.Parallel()

	mux, d := newDeleteMux()
	srv := newTestHarness(t, mux)

	cmd := NewCmdAction(srv.Factory, srv.IOStreams)
	cmd.SetArgs([]string{"--id", "inst-1", "--action", "shutdown", "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute() returned error: %v\nStderr: %s", err, srv.Stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(srv.Stdout.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, srv.Stdout.String())
	}
	if out["status"] != "accepted" {
		t.Errorf("status = %v, want accepted (no --wait, no polling claim)", out["status"])
	}
	// Exactly one GET (the pre-action fetch); polling would add more.
	if got := d.getCalls.Load(); got != 1 {
		t.Errorf("GET /instances/{id} calls = %d, want 1 (no polling by default)", got)
	}
}

// TestActionAgent_ExplicitWaitPollsToCompleted: an explicit --wait polls to
// the expected status and reports completed.
func TestActionAgent_ExplicitWaitPollsToCompleted(t *testing.T) {
	t.Parallel()

	mux, d := newDeleteMux()
	srv := newTestHarness(t, mux)

	cmd := NewCmdAction(srv.Factory, srv.IOStreams)
	cmd.SetArgs([]string{"--id", "inst-1", "--action", "shutdown", "--yes", "--wait"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute() returned error: %v\nStderr: %s", err, srv.Stderr.String())
	}

	var out map[string]any
	if err := json.Unmarshal(srv.Stdout.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput: %s", err, srv.Stdout.String())
	}
	if out["status"] != "completed" {
		t.Errorf("status = %v, want completed after --wait poll", out["status"])
	}
	if got := d.getCalls.Load(); got < 2 {
		t.Errorf("GET /instances/{id} calls = %d, want >= 2 (fetch + poll)", got)
	}
}

// TestActionAgent_WaitFailureIsError: a failed transition during an explicit
// --wait surfaces as an error, not as a completed success (agents key on the
// exit code).
func TestActionAgent_WaitFailureIsError(t *testing.T) {
	t.Parallel()

	mux, d := newDeleteMux()
	srv := newTestHarness(t, mux)
	d.afterAction.Store("error") // the shutdown action fails server-side

	cmd := NewCmdAction(srv.Factory, srv.IOStreams)
	cmd.SetArgs([]string{"--id", "inst-1", "--action", "shutdown", "--yes", "--wait"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when instance transitions to error during --wait")
	}
	if !strings.Contains(err.Error(), "error") {
		t.Fatalf("unexpected error: %v", err)
	}
}
