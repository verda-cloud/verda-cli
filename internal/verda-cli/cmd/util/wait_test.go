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

package util

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestWaitOptionsAddFlags(t *testing.T) {
	t.Parallel()

	// Test default true.
	cmd := stubCommand()
	var opts WaitOptions
	opts.AddFlags(cmd.Flags(), true)

	if !opts.Wait {
		t.Fatal("expected --wait default to be true")
	}
	if opts.Timeout != 5*time.Minute {
		t.Fatalf("expected default timeout 5m, got %v", opts.Timeout)
	}

	// Test default false.
	cmd2 := stubCommand()
	var opts2 WaitOptions
	opts2.AddFlags(cmd2.Flags(), false)

	if opts2.Wait {
		t.Fatal("expected --wait default to be false")
	}
}

func TestPollReturnsDoneImmediately(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := WaitOptions{Wait: true, Timeout: 5 * time.Second}
	calls := 0

	status, err := Poll(context.Background(), &buf, 100*time.Millisecond, opts, func(ctx context.Context) (string, bool, error) {
		calls++
		return "running", true, nil
	})

	if err != nil {
		t.Fatalf("Poll() error: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected status 'running', got %q", status)
	}
	if calls != 1 {
		t.Fatalf("expected 1 poll call, got %d", calls)
	}
}

func TestPollWaitsUntilDone(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := WaitOptions{Wait: true, Timeout: 5 * time.Second}
	calls := 0

	status, err := Poll(context.Background(), &buf, 50*time.Millisecond, opts, func(ctx context.Context) (string, bool, error) {
		calls++
		if calls >= 3 {
			return "running", true, nil
		}
		return "provisioning", false, nil
	})

	if err != nil {
		t.Fatalf("Poll() error: %v", err)
	}
	if status != "running" {
		t.Fatalf("expected status 'running', got %q", status)
	}
	if calls < 3 {
		t.Fatalf("expected at least 3 poll calls, got %d", calls)
	}
}

func TestPollRespectsTimeout(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := WaitOptions{Wait: true, Timeout: 200 * time.Millisecond}

	_, err := Poll(context.Background(), &buf, 50*time.Millisecond, opts, func(ctx context.Context) (string, bool, error) {
		return "provisioning", false, nil
	})

	if err == nil {
		t.Fatal("expected error on timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestPollSilentWithNilWriter(t *testing.T) {
	t.Parallel()

	opts := WaitOptions{Wait: true, Timeout: 5 * time.Second}
	calls := 0

	status, err := Poll(context.Background(), nil, 50*time.Millisecond, opts, func(ctx context.Context) (string, bool, error) {
		calls++
		if calls >= 2 {
			return "done", true, nil
		}
		return "waiting", false, nil
	})

	if err != nil {
		t.Fatalf("Poll() error: %v", err)
	}
	if status != "done" {
		t.Fatalf("expected status 'done', got %q", status)
	}
}

func TestPollReturnsError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	opts := WaitOptions{Wait: true, Timeout: 5 * time.Second}

	_, err := Poll(context.Background(), &buf, 50*time.Millisecond, opts, func(ctx context.Context) (string, bool, error) {
		return "", false, context.DeadlineExceeded
	})

	if err == nil {
		t.Fatal("expected error from Poll()")
	}
}

// --- Poll{Instance,Volume}Status failure propagation ---
// Regression coverage for review NEW-3 (wait.go:131): an instance landing in
// "error"/"notfound" (or a volume in a failed status) must surface as an
// error, never as done-with-nil. Every mock below resolves on the first poll,
// so the hardcoded 5s poll interval never delays these tests.

func newPollTestClient(t *testing.T, mux *http.ServeMux) *verda.Client {
	t.Helper()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "test-token",
			"token_type":   "Bearer",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test-id"),
		verda.WithClientSecret("test-secret"),
	)
	if err != nil {
		t.Fatalf("creating client: %v", err)
	}
	return client
}

func instanceStatusClient(t *testing.T, status string) *verda.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /instances/inst-1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.Instance{ID: "inst-1", Status: status})
	})
	return newPollTestClient(t, mux)
}

func volumeStatusClient(t *testing.T, status string) *verda.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /volumes/vol-1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verda.Volume{ID: "vol-1", Status: status})
	})
	return newPollTestClient(t, mux)
}

func TestPollInstanceStatusReachesTarget(t *testing.T) {
	t.Parallel()
	client := instanceStatusClient(t, verda.StatusRunning)

	inst, err := PollInstanceStatus(context.Background(), nil, client, "inst-1",
		WaitOptions{Wait: true, Timeout: time.Minute}, verda.StatusRunning)
	if err != nil {
		t.Fatalf("PollInstanceStatus() error: %v", err)
	}
	if inst == nil || inst.Status != verda.StatusRunning {
		t.Fatalf("got %+v, want running instance", inst)
	}
}

func TestPollInstanceStatusErrorIsFailure(t *testing.T) {
	t.Parallel()
	client := instanceStatusClient(t, verda.StatusError)

	_, err := PollInstanceStatus(context.Background(), nil, client, "inst-1",
		WaitOptions{Wait: true, Timeout: time.Minute}, verda.StatusRunning)
	if err == nil {
		t.Fatal("PollInstanceStatus() = nil error, want failure on 'error' status")
	}
	if !strings.Contains(err.Error(), `"error"`) {
		t.Fatalf("error %q should name the failed status", err)
	}
}

func TestPollInstanceStatusNotFoundIsFailure(t *testing.T) {
	t.Parallel()
	client := instanceStatusClient(t, verda.StatusNotFound)

	_, err := PollInstanceStatus(context.Background(), nil, client, "inst-1",
		WaitOptions{Wait: true, Timeout: time.Minute}, verda.StatusRunning)
	if err == nil {
		t.Fatal("PollInstanceStatus() = nil error, want failure on 'notfound' status")
	}
}

func TestPollInstanceStatusNoTargetErrorIsFailure(t *testing.T) {
	t.Parallel()
	client := instanceStatusClient(t, verda.StatusError)

	// No expected status: waits for any terminal status, still must not
	// report success (vm create --wait exited 0 on error before the fix).
	_, err := PollInstanceStatus(context.Background(), nil, client, "inst-1",
		WaitOptions{Wait: true, Timeout: time.Minute})
	if err == nil {
		t.Fatal("PollInstanceStatus() without target = nil error, want failure on 'error' status")
	}
}

func TestPollInstanceStatusNoTargetTerminalOK(t *testing.T) {
	t.Parallel()
	client := instanceStatusClient(t, verda.StatusRunning)

	inst, err := PollInstanceStatus(context.Background(), nil, client, "inst-1",
		WaitOptions{Wait: true, Timeout: time.Minute})
	if err != nil {
		t.Fatalf("PollInstanceStatus() error: %v", err)
	}
	if inst == nil {
		t.Fatal("expected last polled instance")
	}
}

func TestPollVolumeStatusReachesTarget(t *testing.T) {
	t.Parallel()
	client := volumeStatusClient(t, verda.VolumeStatusDetached)

	vol, err := PollVolumeStatus(context.Background(), nil, client, "vol-1",
		WaitOptions{Wait: true, Timeout: time.Minute}, verda.VolumeStatusDetached)
	if err != nil {
		t.Fatalf("PollVolumeStatus() error: %v", err)
	}
	if vol == nil || vol.Status != verda.VolumeStatusDetached {
		t.Fatalf("got %+v, want detached volume", vol)
	}
}

func TestPollVolumeStatusFailedTerminatesImmediately(t *testing.T) {
	t.Parallel()
	for _, status := range []string{verda.VolumeStatusCanceled, verda.VolumeStatusDeleted, "error"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			client := volumeStatusClient(t, status)

			start := time.Now()
			_, err := PollVolumeStatus(context.Background(), nil, client, "vol-1",
				WaitOptions{Wait: true, Timeout: 5 * time.Minute}, verda.VolumeStatusDetached)
			if err == nil {
				t.Fatalf("PollVolumeStatus() = nil error, want failure on %q status", status)
			}
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Fatalf("poll took %s; failed status must terminate immediately, not burn the timeout", elapsed)
			}
			if !strings.Contains(err.Error(), status) {
				t.Fatalf("error %q should name the failed status %q", err, status)
			}
		})
	}
}
