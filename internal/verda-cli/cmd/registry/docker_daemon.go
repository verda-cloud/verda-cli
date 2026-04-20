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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
)

// DaemonImage is a summary of one image visible to the local Docker daemon.
// Derived from GET /images/json with minimal transformation.
type DaemonImage struct {
	// ID is the full sha256:... image ID reported by the daemon.
	ID string
	// RepoTags is the set of tags the daemon has for this image — e.g.
	// ["nginx:latest", "nginx:1.25"]. May be empty for purely dangling
	// images, or contain the sentinel "<none>:<none>" for images whose
	// tags have been shadowed. Preserved as-is; callers decide how to
	// present or filter these.
	RepoTags []string
	// Size is the image size in bytes as reported by the daemon.
	Size int64
	// Created is the image creation timestamp. Derived from the daemon's
	// Unix-seconds field and therefore in the local timezone's Unix clock
	// (time.Unix coerces to UTC-based instants internally).
	Created time.Time
}

// DaemonLister abstracts the Docker daemon API so we can swap it in tests.
type DaemonLister interface {
	// Ping returns nil if the daemon is reachable.
	Ping(ctx context.Context) error

	// List returns all images the daemon knows about, including dangling
	// ones, sorted by Created descending (newest first).
	List(ctx context.Context) ([]DaemonImage, error)
}

// NewDaemonLister constructs the default real implementation pointing at
// the user's local Docker socket (or DOCKER_HOST if set).
//
// Resolution order:
//
//  1. DOCKER_HOST=unix:///path → unix socket at /path.
//  2. DOCKER_HOST=tcp://host:port → TCP dial (plain HTTP, no TLS).
//  3. DOCKER_HOST=anything-else → error "unsupported DOCKER_HOST scheme".
//  4. Linux/macOS with DOCKER_HOST unset → /var/run/docker.sock.
//  5. Windows with DOCKER_HOST unset → error; verda does not yet support
//     Windows named pipes. Users can still push via --source oci|tar or
//     set DOCKER_HOST=tcp://... to reach a remote daemon.
func NewDaemonLister() (DaemonLister, error) {
	transport, baseURL, socketPath, err := resolveDaemonTransport(os.Getenv("DOCKER_HOST"), runtime.GOOS)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: transport}
	l := newHTTPLister(client, baseURL).(*httpLister)
	l.socketPath = socketPath
	return l, nil
}

// httpLister is the concrete DaemonLister implementation. It is kept
// small and transport-agnostic so tests can inject an httptest.Server's
// TCP client instead of wiring a real unix socket.
type httpLister struct {
	c       *http.Client
	baseURL string
	// socketPath is surfaced in "daemon not reachable" errors so the
	// message tells users exactly where verda tried to look. Empty for
	// TCP / test backends.
	socketPath string
}

// newHTTPLister builds an httpLister around an arbitrary *http.Client +
// base URL. Both the production socket path and tests go through this
// constructor.
func newHTTPLister(c *http.Client, baseURL string) DaemonLister {
	return &httpLister{c: c, baseURL: strings.TrimRight(baseURL, "/")}
}

// unixDialer returns a DialContext that ignores the HTTP network/addr
// pair (we always pass "http://unix/..." as the URL) and dials the given
// unix socket path instead.
func unixDialer(socketPath string) func(ctx context.Context, _, _ string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socketPath)
	}
}

// resolveDaemonTransport picks an http.RoundTripper + base URL + socket
// path hint for the given DOCKER_HOST value and GOOS. Split out of
// NewDaemonLister so tests can table-drive the logic without touching
// env state. socketPath is empty for TCP transports.
func resolveDaemonTransport(dockerHost, goos string) (tr http.RoundTripper, baseURL, socketPath string, err error) {
	socketPath, err = resolveSocket(dockerHost, goos)
	if err != nil {
		return nil, "", "", err
	}
	if strings.HasPrefix(socketPath, "tcp://") {
		// For TCP we hand the host straight to net/http; the default
		// transport is fine. Rewrite scheme to plain http — TLS isn't
		// supported yet and Docker TCP endpoints without TLS are
		// addressable as http://host:port/....
		u, parseErr := url.Parse(socketPath)
		if parseErr != nil {
			return nil, "", "", fmt.Errorf("parse DOCKER_HOST: %w", parseErr)
		}
		return http.DefaultTransport, "http://" + u.Host, "", nil
	}
	// Unix socket path.
	return &http.Transport{DialContext: unixDialer(socketPath)}, "http://unix", socketPath, nil
}

// resolveSocket turns a DOCKER_HOST + GOOS pair into either a unix
// socket path, a "tcp://host:port" literal (passed through for the TCP
// branch of resolveDaemonTransport), or an error describing the
// unsupported configuration.
func resolveSocket(dockerHost, goos string) (string, error) {
	if dockerHost != "" {
		switch {
		case strings.HasPrefix(dockerHost, "unix://"):
			return strings.TrimPrefix(dockerHost, "unix://"), nil
		case strings.HasPrefix(dockerHost, "tcp://"):
			return dockerHost, nil
		default:
			return "", fmt.Errorf("unsupported DOCKER_HOST scheme: %q", dockerHost)
		}
	}
	if goos == "windows" {
		return "", errors.New(
			"docker on Windows is not yet supported by verda registry; " +
				"set DOCKER_HOST=tcp://... to target a remote daemon, " +
				"or use --source oci|tar when pushing",
		)
	}
	return "/var/run/docker.sock", nil
}

// Ping hits /_ping and treats any 2xx as healthy. 500 and socket errors
// are translated to user-friendly messages.
func (l *httpLister) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.baseURL+"/_ping", http.NoBody)
	if err != nil {
		return fmt.Errorf("build ping request: %w", err)
	}
	resp, err := l.c.Do(req)
	if err != nil {
		return l.wrapDialErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("docker API returned status %d", resp.StatusCode)
	}
	return nil
}

// daemonImageJSON mirrors the subset of /images/json we care about.
type daemonImageJSON struct {
	ID       string   `json:"Id"`
	RepoTags []string `json:"RepoTags"`
	Created  int64    `json:"Created"`
	Size     int64    `json:"Size"`
}

// List fetches /images/json?all=1 and maps it into []DaemonImage sorted
// by Created descending.
func (l *httpLister) List(ctx context.Context) ([]DaemonImage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.baseURL+"/images/json?all=1", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build list request: %w", err)
	}
	resp, err := l.c.Do(req)
	if err != nil {
		return nil, l.wrapDialErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("docker API returned status %d", resp.StatusCode)
	}
	var raw []daemonImageJSON
	// We intentionally do NOT DisallowUnknownFields — Docker adds new
	// fields over time and we want to keep parsing older and newer
	// daemon responses interchangeably.
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("docker API returned malformed response: %w", err)
	}
	out := make([]DaemonImage, 0, len(raw))
	for _, r := range raw {
		out = append(out, DaemonImage{
			ID:       r.ID,
			RepoTags: r.RepoTags,
			Size:     r.Size,
			Created:  time.Unix(r.Created, 0),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Created.After(out[j].Created)
	})
	return out, nil
}

// wrapDialErr rewrites low-level "connection refused" / "no such file"
// socket errors into the user-facing form. Other errors are returned
// unchanged — timeouts and context cancellation should surface as-is so
// callers can distinguish them.
func (l *httpLister) wrapDialErr(err error) error {
	if err == nil {
		return nil
	}
	// Context errors are returned as-is; callers already know how to
	// render "canceled" / "deadline exceeded".
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	// net.OpError for unix sockets typically wraps syscall.ECONNREFUSED
	// or "no such file or directory". Match both.
	msg := err.Error()
	if strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "connect: no such file") {
		where := l.socketPath
		if where == "" {
			where = l.baseURL
		}
		return fmt.Errorf("docker daemon not reachable at %s; start Docker Desktop or the Docker daemon", where)
	}
	return err
}
