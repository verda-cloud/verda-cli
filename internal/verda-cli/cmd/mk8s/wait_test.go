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
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// TestWaitFor feeds waitFor scripted reads: a positive number is a failed read
// with that status, a negative one a value (-3 is done).
func TestWaitFor(t *testing.T) {
	fastPoll(t)
	const ok1, done = -1, -3
	for name, tc := range map[string]struct {
		reads    []int
		known    int // > 0: the object was read before the wait
		readErr  bool
		lastSeen int
	}{
		"failures must be consecutive":   {reads: []int{502, ok1, 502, ok1, 502, done}},
		"2 in a row are tolerated":       {reads: []int{ok1, 502, 502, done}},
		"gives up after 3 in a row":      {reads: []int{ok1, 500, 500, 500, done}, readErr: true, lastSeen: 1},
		"408 is retried":                 {reads: []int{ok1, 408, done}},
		"429 is retried":                 {reads: []int{ok1, 429, done}},
		"404 after a good read retried":  {reads: []int{ok1, 404, done}},
		"400 ends at once":               {reads: []int{ok1, 400, done}, readErr: true, lastSeen: 1},
		"403 ends at once":               {reads: []int{ok1, 403, done}, readErr: true, lastSeen: 1},
		"404 before the first good read": {reads: []int{404, 404, 404, 404, done}},
		"404s after a good read fail":    {reads: []int{ok1, 404, 404, 404, done}, readErr: true, lastSeen: 1},
		"404s of a known object fail":    {reads: []int{404, 404, 404, done}, known: 2, readErr: true, lastSeen: 2},
	} {
		i := 0
		get := func(context.Context) (*int, error) {
			r := tc.reads[min(i, len(tc.reads)-1)]
			i++
			if r > 0 {
				return nil, &verda.APIError{StatusCode: r, Message: "x"}
			}
			v := -r
			return &v, nil
		}
		var known *int
		if tc.known > 0 {
			known = &tc.known
		}
		last, err := waitFor(t.Context(), &cmdutil.TestFactory{}, nil, 5*time.Second, known, get, func(v *int) (string, bool) { return "", *v == 3 })
		if _, isRead := errors.AsType[*pollReadError](err); isRead != tc.readErr || (!tc.readErr && err != nil) {
			t.Errorf("%s: err = %v", name, err)
		}
		if tc.readErr && (last == nil || *last != tc.lastSeen) {
			t.Errorf("%s: last = %v, want the last good read", name, last)
		}
	}
}

func TestWaitFailure_Messages(t *testing.T) {
	t.Parallel()
	info := waitFailureInfo{Subject: "thing x", Timeout: time.Minute, LastSeen: "last: y", TimeoutTail: "Tail.", ReadNote: "Note.", CheckCmd: "check it"}
	read := waitFailure(&pollReadError{err: errors.New("boom")}, &info).Error()
	for _, want := range []string{"thing x was accepted, but --wait could not read it (boom)", "check with `check it`. Note."} {
		if !strings.Contains(read, want) {
			t.Errorf("read failure %q misses %q", read, want)
		}
	}
	if got := waitFailure(errors.New("timed out waiting"), &info).Error(); got != "timed out after 1m0s waiting for thing x (last: y). Tail." {
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
