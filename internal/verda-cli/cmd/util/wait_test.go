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
	"strings"
	"testing"
	"time"
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
