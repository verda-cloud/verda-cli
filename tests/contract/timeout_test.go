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

package contract

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// TestTimeoutControlPlaneFailsFast pins review H2's control-plane half: with
// the client-level http.Client.Timeout gone, a wedged API endpoint must still
// be bounded by --timeout (per-call WithTimeout), not hang. The mock blocks
// /locations until the client cancels, so a missing bound fails the suite's
// own safety net (cliTimeout) rather than this quick assert.
func TestTimeoutControlPlaneFailsFast(t *testing.T) {
	t.Parallel()

	srv := newServer(t)
	srv.HangRoute("/locations")

	r := runCLI(t, srv, "--timeout", "1s", "locations")
	if r.ExitCode == 0 {
		t.Fatalf("exit code = 0 against a hung endpoint\nstdout: %s\nstderr: %s", r.Stdout, r.Stderr)
	}
	if r.Duration > 10*time.Second {
		t.Fatalf("--timeout 1s did not bound the call: took %s\nstdout: %s\nstderr: %s",
			r.Duration, r.Stdout, r.Stderr)
	}
}

// TestTimeoutTransferNotClamped pins review H2's data-plane half: a transfer
// that outlives --timeout must succeed (bounded ctx is for control-plane
// calls only). The source registry delays every layer-blob GET by blobDelay,
// well past the 900ms --timeout; pre-fix the copy died mid-transfer with
// context deadline exceeded.
func TestTimeoutTransferNotClamped(t *testing.T) {
	t.Parallel()

	const blobDelay = 1500 * time.Millisecond

	// Source: in-memory docker v2 registry with delayed blob pulls.
	inner := ggcrregistry.New()
	srcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/blobs/") {
			timer := time.NewTimer(blobDelay)
			select {
			case <-timer.C:
			case <-r.Context().Done():
				timer.Stop()
				return
			}
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srcSrv.Close)

	// Destination: same registry, no delay.
	dstSrv := httptest.NewServer(ggcrregistry.New())
	t.Cleanup(dstSrv.Close)

	srcHost := hostOf(t, srcSrv)
	dstHost := hostOf(t, dstSrv)

	// Prime the source with a small image (writes are not delayed).
	srcRef, err := name.ParseReference(srcHost + "/lib/app:v1")
	if err != nil {
		t.Fatalf("parse src ref: %v", err)
	}
	img, err := random.Image(2048, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	if err := remote.Write(srcRef, img); err != nil {
		t.Fatalf("prime source image: %v", err)
	}

	credsFile := filepath.Join(t.TempDir(), "credentials")
	credsBody := fmt.Sprintf("[default]\nverda_registry_username = u\nverda_registry_secret = p\nverda_registry_endpoint = %s\nverda_registry_project_id = proj\n", dstHost)
	if err := os.WriteFile(credsFile, []byte(credsBody), 0o600); err != nil {
		t.Fatalf("write registry creds: %v", err)
	}

	srv := newServer(t)
	r := runCLIEnv(t, srv, []string{"VERDA_REGISTRY_CREDENTIALS_FILE=" + credsFile},
		"--timeout", "900ms", "registry", "copy",
		srcHost+"/lib/app:v1", dstHost+"/proj/app:v1",
		"--src-auth", "anonymous",
	)
	requireExit(t, r, 0)
	if r.Duration < blobDelay {
		t.Fatalf("copy finished in %s, under the blob delay %s — transfer never hit the slow path\nstdout: %s\nstderr: %s",
			r.Duration, blobDelay, r.Stdout, r.Stderr)
	}

	dstRef, err := name.ParseReference(dstHost + "/proj/app:v1")
	if err != nil {
		t.Fatalf("parse dst ref: %v", err)
	}
	if _, err := remote.Head(dstRef); err != nil {
		t.Fatalf("image missing at destination: %v\nstdout: %s\nstderr: %v", err, r.Stdout, r.Stderr)
	}
}

func hostOf(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	return u.Host
}
