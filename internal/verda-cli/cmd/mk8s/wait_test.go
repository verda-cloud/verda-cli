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

package mk8s

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// scriptedGet answers each read from a list: an error, or a value.
type step struct {
	err error
	val int
}

func scriptedGet(steps []step) func(context.Context) (*int, error) {
	i := 0
	return func(context.Context) (*int, error) {
		s := steps[min(i, len(steps)-1)]
		i++
		if s.err != nil {
			return nil, s.err
		}
		v := s.val
		return &v, nil
	}
}

func apiErrStatus(code int) error { return &verda.APIError{StatusCode: code, Message: "x"} }

func wait(t *testing.T, steps []step, timeout time.Duration) (*int, error) {
	t.Helper()
	fastPoll(t)
	return waitFor(t.Context(), &cmdutil.TestFactory{}, nil, timeout, scriptedGet(steps),
		func(v *int) (string, bool) { return "reading", *v >= 3 })
}

func TestWaitFor_FailuresMustBeConsecutive(t *testing.T) {
	// fail, ok, fail, ok, fail, ok: never maxReadFailures in a row, so the wait goes on to done.
	steps := []step{{err: apiErrStatus(502)}, {val: 1}, {err: apiErrStatus(502)}, {val: 2}, {err: apiErrStatus(502)}, {val: 3}}
	last, err := wait(t, steps, 5*time.Second)
	if err != nil || last == nil || *last != 3 {
		t.Fatalf("last=%v err=%v", last, err)
	}
}

func TestWaitFor_GivesUpAfterConsecutiveFailures(t *testing.T) {
	steps := []step{{val: 1}, {err: apiErrStatus(500)}, {err: apiErrStatus(500)}, {err: apiErrStatus(500)}, {val: 3}}
	last, err := wait(t, steps, 5*time.Second)
	if _, ok := errors.AsType[*pollReadError](err); !ok {
		t.Fatalf("err = %v, want a pollReadError", err)
	}
	if last == nil || *last != 1 {
		t.Errorf("last = %v, want the last good read", last)
	}
}

func TestWaitFor_RetriableAndFatalStatuses(t *testing.T) {
	for _, code := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusNotFound, http.StatusBadGateway} {
		// one failure then done: retriable statuses are tolerated
		if _, err := wait(t, []step{{val: 1}, {err: apiErrStatus(code)}, {val: 3}}, 5*time.Second); err != nil {
			t.Errorf("status %d must be retried: %v", code, err)
		}
	}
	for _, code := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		if _, err := wait(t, []step{{val: 1}, {err: apiErrStatus(code)}, {val: 3}}, 5*time.Second); err == nil {
			t.Errorf("status %d must end the wait at once", code)
		}
	}
}

func TestWaitFor_A404BeforeTheFirstGoodReadIsWaitedOut(t *testing.T) {
	steps := []step{{err: apiErrStatus(404)}, {err: apiErrStatus(404)}, {err: apiErrStatus(404)}, {err: apiErrStatus(404)}, {val: 3}}
	if _, err := wait(t, steps, 5*time.Second); err != nil {
		t.Errorf("a lagging read after a create must not fail the wait: %v", err)
	}
	// After a good read, the same 404s are failures.
	steps = []step{{val: 1}, {err: apiErrStatus(404)}, {err: apiErrStatus(404)}, {err: apiErrStatus(404)}, {val: 3}}
	if _, err := wait(t, steps, 5*time.Second); err == nil {
		t.Error("404s after a good read count as failures")
	}
}

func TestWaitFor_TimeoutWhileAReadIsInFlight(t *testing.T) {
	fastPoll(t)
	hang := func(ctx context.Context) (*int, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	_, err := waitFor(t.Context(), &cmdutil.TestFactory{}, nil, 50*time.Millisecond, hang, func(*int) (string, bool) { return "", false })
	if !errors.Is(err, errWaitTimeout) {
		t.Errorf("err = %v, want errWaitTimeout, not a read failure", err)
	}
}

func TestWaitFailure_Messages(t *testing.T) {
	t.Parallel()
	info := waitFailureInfo{Subject: "thing x", Timeout: time.Minute, LastSeen: "last: y", TimeoutTail: "Tail.", ReadNote: "Note.", CheckCmd: "check it"}
	read := waitFailure(&pollReadError{err: errors.New("boom")}, &info).Error()
	for _, want := range []string{"thing x was accepted, but --wait could not read it (boom)", "check with `check it`", "Note."} {
		if !strings.Contains(read, want) {
			t.Errorf("read failure %q misses %q", read, want)
		}
	}
	if got := waitFailure(errWaitTimeout, &info).Error(); got != "timed out after 1m0s waiting for thing x (last: y). Tail." {
		t.Errorf("timeout = %q", got)
	}
	info.ReadNote = ""
	if read := waitFailure(&pollReadError{err: errors.New("boom")}, &info).Error(); strings.Contains(read, ". ") && strings.Contains(read, "Note") {
		t.Errorf("no ReadNote, no note: %q", read)
	}
}

func TestPoolConditions(t *testing.T) {
	t.Parallel()
	pool := func(state string, replicas, ready int) *verda.ManagedKubernetesNodePool {
		return &verda.ManagedKubernetesNodePool{State: state, Replicas: replicas, ReadyReplicas: ready}
	}
	type c struct {
		name string
		p    *verda.ManagedKubernetesNodePool
		want int
		done bool
	}
	for _, tc := range []c{
		{"active and ready", pool("active", 3, 3), 3, true},
		{"creating at the count", pool("creating", 3, 3), 3, false},
		{"degraded at the count", pool("degraded", 3, 3), 3, false},
		{"active, short of the count", pool("active", 3, 2), 3, false},
		{"zero workers, active", pool("active", 0, 0), 0, true},
		{"zero workers, still creating", pool("creating", 0, 0), 0, false},
	} {
		if _, got := poolCreated(tc.want)(tc.p); got != tc.done {
			t.Errorf("poolCreated %s = %v, want %v", tc.name, got, tc.done)
		}
	}
	for _, tc := range []c{
		{"new count, all ready", pool("active", 5, 5), 5, true},
		{"old count, all ready", pool("active", 3, 3), 5, false},
		{"ready at target, replicas not", pool("active", 3, 5), 5, false},
		{"new count, degraded", pool("degraded", 5, 5), 5, false},
		{"to zero, stale workers", pool("active", 0, 3), 0, false},
		{"to zero, done", pool("active", 0, 0), 0, true},
	} {
		if _, got := poolScaled(tc.want)(tc.p); got != tc.done {
			t.Errorf("poolScaled %s = %v, want %v", tc.name, got, tc.done)
		}
	}
	if s, _ := poolCreated(3)(&verda.ManagedKubernetesNodePool{State: "degraded", Replicas: 3, ReadyReplicas: 1, Errors: []verda.ManagedKubernetesNodePoolError{{Reason: "insufficient_capacity"}, {Reason: "insufficient_capacity"}, {Reason: "cloud_api_error"}}}); s != "degraded (insufficient_capacity, cloud_api_error), 1/3 workers ready" {
		t.Errorf("status = %q", s)
	}
	if s, done := clusterReady(&verda.ManagedKubernetesCluster{Phase: "running"}); done || s != "running" {
		t.Errorf("running without an endpoint: %q %v", s, done)
	}
	if _, done := clusterReady(&verda.ManagedKubernetesCluster{Phase: "running", ExternalEndpoint: "https://x"}); !done {
		t.Error("running with an endpoint is ready")
	}
	if _, done := clusterReady(&verda.ManagedKubernetesCluster{Phase: "degraded", ExternalEndpoint: "https://x"}); done {
		t.Error("degraded is not ready")
	}
}

func TestKubeconfigReady(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status, yaml string
		done         bool
	}{
		"pending without YAML": {verda.ManagedKubernetesKubeconfigStatusPending, "", false},
		"pending with YAML":    {verda.ManagedKubernetesKubeconfigStatusPending, "y", false},
		"ready without YAML":   {verda.ManagedKubernetesKubeconfigStatusReady, "", false},
		"ready with YAML":      {verda.ManagedKubernetesKubeconfigStatusReady, "y", true},
	} {
		status, done := kubeconfigReady(&verda.ManagedKubernetesKubeconfig{Status: tc.status, Kubeconfig: tc.yaml})
		if status != tc.status || done != tc.done {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", name, status, done, tc.status, tc.done)
		}
	}
}
