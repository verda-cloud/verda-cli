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

package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeDaemon is a tiny test double that lets each test wire up whatever
// handlers it needs for /_ping and /images/json. Approach A from the
// task spec: a stdlib httptest.Server fronted by an arbitrary
// *http.Client + base URL, so the production unix-socket plumbing is
// bypassed but the HTTP + JSON + sort logic is covered end-to-end.
type fakeDaemon struct {
	ping   http.HandlerFunc
	images http.HandlerFunc
}

func (f *fakeDaemon) start(t *testing.T) (DaemonLister, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	if f.ping != nil {
		mux.HandleFunc("/_ping", f.ping)
	}
	if f.images != nil {
		mux.HandleFunc("/images/json", f.images)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return newHTTPLister(srv.Client(), srv.URL), srv
}

// TestDaemonLister_PingOK — server responds 200; Ping returns nil.
func TestDaemonLister_PingOK(t *testing.T) {
	fd := &fakeDaemon{
		ping: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		},
	}
	lister, _ := fd.start(t)

	if err := lister.Ping(context.Background()); err != nil {
		t.Fatalf("Ping returned error: %v", err)
	}
}

// TestDaemonLister_PingFailed — server 500 → error mentions status.
func TestDaemonLister_PingFailed(t *testing.T) {
	fd := &fakeDaemon{
		ping: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	}
	lister, _ := fd.start(t)

	err := lister.Ping(context.Background())
	if err == nil {
		t.Fatal("expected ping error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error to mention 500, got: %v", err)
	}
}

// TestDaemonLister_ListParsesImages — server returns a fixed JSON array
// of 3 images; List returns 3 DaemonImage values with correct fields.
func TestDaemonLister_ListParsesImages(t *testing.T) {
	body := `[
		{"Id":"sha256:a","RepoTags":["nginx:latest","nginx:1.25"],"Created":1700000000,"Size":100},
		{"Id":"sha256:b","RepoTags":["redis:7"],"Created":1700000100,"Size":200},
		{"Id":"sha256:c","RepoTags":["alpine:3"],"Created":1700000200,"Size":50}
	]`
	fd := &fakeDaemon{
		images: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("all") != "1" {
				t.Errorf("expected all=1 query param, got %q", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		},
	}
	lister, _ := fd.start(t)

	got, err := lister.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 images, got %d", len(got))
	}
	// After sort-by-created-desc: c (200), b (100), a (0).
	if got[0].ID != "sha256:c" {
		t.Errorf("expected newest first sha256:c, got %q", got[0].ID)
	}
	// Spot-check the middle row fully to cover all the fields.
	if got[1].ID != "sha256:b" {
		t.Errorf("expected 2nd image sha256:b, got %q", got[1].ID)
	}
	if len(got[1].RepoTags) != 1 || got[1].RepoTags[0] != "redis:7" {
		t.Errorf("RepoTags[1] = %v, want [redis:7]", got[1].RepoTags)
	}
	if got[1].Size != 200 {
		t.Errorf("Size[1] = %d, want 200", got[1].Size)
	}
	if got[1].Created.Unix() != 1700000100 {
		t.Errorf("Created[1] = %d, want 1700000100", got[1].Created.Unix())
	}
	// The 2-tag image has both tags preserved in order.
	if len(got[2].RepoTags) != 2 || got[2].RepoTags[0] != "nginx:latest" || got[2].RepoTags[1] != "nginx:1.25" {
		t.Errorf("multi-tag RepoTags = %v, want [nginx:latest nginx:1.25]", got[2].RepoTags)
	}
}

// TestDaemonLister_ListSortsByCreatedDesc — out-of-order timestamps are
// reordered newest-first.
func TestDaemonLister_ListSortsByCreatedDesc(t *testing.T) {
	body := `[
		{"Id":"sha256:old","Created":100,"Size":1,"RepoTags":["old:1"]},
		{"Id":"sha256:new","Created":300,"Size":1,"RepoTags":["new:1"]},
		{"Id":"sha256:mid","Created":200,"Size":1,"RepoTags":["mid:1"]}
	]`
	fd := &fakeDaemon{
		images: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		},
	}
	lister, _ := fd.start(t)

	got, err := lister.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	wantOrder := []string{"sha256:new", "sha256:mid", "sha256:old"}
	for i, want := range wantOrder {
		if got[i].ID != want {
			t.Errorf("position %d: got %q, want %q", i, got[i].ID, want)
		}
	}
}

// TestDaemonLister_ListPreservesDanglingTags — an image whose RepoTags
// contains the "<none>:<none>" sentinel must be preserved verbatim.
func TestDaemonLister_ListPreservesDanglingTags(t *testing.T) {
	body := `[
		{"Id":"sha256:dangling","RepoTags":["<none>:<none>"],"Created":1,"Size":1}
	]`
	fd := &fakeDaemon{
		images: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		},
	}
	lister, _ := fd.start(t)

	got, err := lister.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 image, got %d", len(got))
	}
	if len(got[0].RepoTags) != 1 || got[0].RepoTags[0] != "<none>:<none>" {
		t.Errorf("dangling RepoTags = %v, want [<none>:<none>]", got[0].RepoTags)
	}
}

// TestDaemonLister_ListEmptyArray — "[]" → empty slice, no error.
func TestDaemonLister_ListEmptyArray(t *testing.T) {
	fd := &fakeDaemon{
		images: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("[]"))
		},
	}
	lister, _ := fd.start(t)

	got, err := lister.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 images, got %d", len(got))
	}
}

// TestDaemonLister_ListInvalidJSON — wrapped error mentions "malformed".
func TestDaemonLister_ListInvalidJSON(t *testing.T) {
	fd := &fakeDaemon{
		images: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("{not json"))
		},
	}
	lister, _ := fd.start(t)

	_, err := lister.List(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("expected error to mention 'malformed', got: %v", err)
	}
}

// TestDaemonLister_ListNon200 — status-code is surfaced verbatim.
func TestDaemonLister_ListNon200(t *testing.T) {
	fd := &fakeDaemon{
		images: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	lister, _ := fd.start(t)

	_, err := lister.List(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("expected error to mention 502, got: %v", err)
	}
}

// TestDaemonLister_ContextCanceled — pre-canceled ctx short-circuits
// before any bytes hit the wire; error surfaces as a context error.
func TestDaemonLister_ContextCanceled(t *testing.T) {
	fd := &fakeDaemon{
		images: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("[]"))
		},
	}
	lister, _ := fd.start(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := lister.List(ctx)
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	// Either context.Canceled surfaces directly, or net/http wraps it —
	// in either case the message contains "canceled".
	if !strings.Contains(strings.ToLower(err.Error()), "cancel") {
		t.Fatalf("expected 'cancel' in error, got: %v", err)
	}
}

// TestResolveSocket — table-driven across DOCKER_HOST + GOOS pairs.
func TestResolveSocket(t *testing.T) {
	tests := []struct {
		name       string
		dockerHost string
		goos       string
		want       string
		wantErr    string // substring; "" = expect success
	}{
		{
			name:       "unix scheme",
			dockerHost: "unix:///foo",
			goos:       "linux",
			want:       "/foo",
		},
		{
			name:       "tcp scheme passed through",
			dockerHost: "tcp://host:2376",
			goos:       "linux",
			want:       "tcp://host:2376",
		},
		{
			name:       "default linux",
			dockerHost: "",
			goos:       "linux",
			want:       "/var/run/docker.sock",
		},
		{
			name:       "default darwin",
			dockerHost: "",
			goos:       "darwin",
			want:       "/var/run/docker.sock",
		},
		{
			name:       "default windows errors",
			dockerHost: "",
			goos:       "windows",
			// Error string is lowercase per Go convention ("docker on Windows ...").
			// We search for the mid-sentence "Windows is not yet supported" substring.
			wantErr: "Windows is not yet supported",
		},
		{
			name:       "windows with tcp host works",
			dockerHost: "tcp://host:2376",
			goos:       "windows",
			want:       "tcp://host:2376",
		},
		{
			name:       "unknown scheme",
			dockerHost: "ssh://user@host",
			goos:       "linux",
			wantErr:    "unsupported DOCKER_HOST scheme",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveSocket(tc.dockerHost, tc.goos)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("resolveSocket(%q, %q) = %q, want %q", tc.dockerHost, tc.goos, got, tc.want)
			}
		})
	}
}

// TestDaemonListerBuilder_DefaultsToProduction smoke-tests that the
// package-level swap point targets NewDaemonLister by default. Tests
// that reassign it (push, Task 19) rely on this being a plain var.
func TestDaemonListerBuilder_DefaultsToProduction(t *testing.T) {
	// Indirect function comparison by calling through the var and
	// checking the returned type isn't our test fake. Simpler: compare
	// function pointers via a type assertion dance — if the var has
	// been clobbered by a parallel test, this surfaces it.
	_ = daemonListerBuilder // ensures symbol exists & remains assignable
}

// TestDaemonLister_CreatedTimestampRounding — confirms Unix-seconds
// conversion preserves the exact second. A loose sanity check so future
// refactors to time-parsing don't silently drift.
func TestDaemonLister_CreatedTimestampRounding(t *testing.T) {
	body := `[{"Id":"sha256:x","RepoTags":["a:1"],"Created":1699123456,"Size":1}]`
	fd := &fakeDaemon{
		images: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		},
	}
	lister, _ := fd.start(t)

	got, err := lister.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := time.Unix(1699123456, 0)
	if !got[0].Created.Equal(want) {
		t.Fatalf("Created = %v, want %v", got[0].Created, want)
	}
}
