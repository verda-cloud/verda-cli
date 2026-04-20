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
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// writeOCILayout writes a single-image OCI layout for the given v1.Image
// into a fresh temp dir and returns that dir.
func writeOCILayout(t *testing.T, img v1.Image) string {
	t.Helper()
	dir := t.TempDir()
	idx := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: img})
	if _, err := layout.Write(dir, idx); err != nil {
		t.Fatalf("layout.Write: %v", err)
	}
	return dir
}

// writeMultiManifestOCILayout writes an OCI layout containing two images.
func writeMultiManifestOCILayout(t *testing.T, imgs ...v1.Image) string {
	t.Helper()
	dir := t.TempDir()
	adds := make([]mutate.IndexAddendum, 0, len(imgs))
	for _, img := range imgs {
		adds = append(adds, mutate.IndexAddendum{Add: img})
	}
	idx := mutate.AppendManifests(empty.Index, adds...)
	if _, err := layout.Write(dir, idx); err != nil {
		t.Fatalf("layout.Write: %v", err)
	}
	return dir
}

// writeTarball writes a Docker-format tarball for the given image to a
// temp file and returns the path.
func writeTarball(t *testing.T, img v1.Image, basename string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), basename)
	if err := tarball.WriteToFile(p, name.MustParseReference("test:latest"), img); err != nil {
		t.Fatalf("tarball.WriteToFile: %v", err)
	}
	return p
}

// randomImage is a small wrapper with a fixed byte size / layer count
// appropriate for unit tests.
func randomImage(t *testing.T) v1.Image {
	t.Helper()
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	return img
}

// swapDaemonImageFunc replaces daemonImageFunc for the duration of a
// test and restores it on cleanup. Used so we never talk to a real
// docker socket from unit tests.
func swapDaemonImageFunc(t *testing.T, fn func(ref name.Reference, opts ...daemon.Option) (v1.Image, error)) {
	t.Helper()
	prev := daemonImageFunc
	daemonImageFunc = fn
	t.Cleanup(func() { daemonImageFunc = prev })
}

// ---- OCI layout ----

func TestSourceLoader_OCILayoutHappy(t *testing.T) {
	img := randomImage(t)
	dir := writeOCILayout(t, img)

	loader := NewDefaultSourceLoader(func(context.Context) error { return nil })
	got, err := loader.Load(context.Background(), SourceOCI, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	gotDigest, err := got.Digest()
	if err != nil {
		t.Fatalf("got.Digest: %v", err)
	}
	wantDigest, err := img.Digest()
	if err != nil {
		t.Fatalf("img.Digest: %v", err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("digest mismatch: got %s, want %s", gotDigest, wantDigest)
	}
}

func TestSourceLoader_OCILayoutDirMissing(t *testing.T) {
	loader := NewDefaultSourceLoader(nil)
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := loader.Load(context.Background(), SourceOCI, missing)
	if err == nil {
		t.Fatalf("expected error for missing OCI dir")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if ae.Code != kindRegistryInvalidReference {
		t.Fatalf("expected %s, got %s", kindRegistryInvalidReference, ae.Code)
	}
}

func TestSourceLoader_OCILayoutMultiManifest(t *testing.T) {
	img1 := randomImage(t)
	img2 := randomImage(t)
	dir := writeMultiManifestOCILayout(t, img1, img2)

	loader := NewDefaultSourceLoader(nil)
	_, err := loader.Load(context.Background(), SourceOCI, dir)
	if err == nil {
		t.Fatalf("expected multi-manifest error")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if !strings.Contains(ae.Message, "multiple manifests") {
		t.Fatalf("expected 'multiple manifests' in message, got %q", ae.Message)
	}
	d1, _ := img1.Digest()
	d2, _ := img2.Digest()
	if !strings.Contains(ae.Message, d1.String()) || !strings.Contains(ae.Message, d2.String()) {
		t.Fatalf("expected both digests in message, got %q", ae.Message)
	}
}

// ---- Tarball ----

func TestSourceLoader_TarballHappy(t *testing.T) {
	img := randomImage(t)
	tarPath := writeTarball(t, img, "img.tar")

	loader := NewDefaultSourceLoader(func(context.Context) error { return nil })
	got, err := loader.Load(context.Background(), SourceTar, tarPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	gotDigest, err := got.Digest()
	if err != nil {
		t.Fatalf("got.Digest: %v", err)
	}
	wantDigest, err := img.Digest()
	if err != nil {
		t.Fatalf("img.Digest: %v", err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("digest mismatch: got %s, want %s", gotDigest, wantDigest)
	}
}

func TestSourceLoader_TarballMissing(t *testing.T) {
	loader := NewDefaultSourceLoader(nil)
	missing := filepath.Join(t.TempDir(), "not-there.tar")
	_, err := loader.Load(context.Background(), SourceTar, missing)
	if err == nil {
		t.Fatalf("expected error for missing tarball")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if ae.Code != kindRegistryInvalidReference {
		t.Fatalf("expected %s, got %s", kindRegistryInvalidReference, ae.Code)
	}
}

// ---- Auto dispatch ----

func TestSourceLoader_AutoDispatchesToOCIForDir(t *testing.T) {
	img := randomImage(t)
	dir := writeOCILayout(t, img)

	// Ping deliberately fails; auto should never call it when the ref
	// is an existing directory.
	pingCalled := false
	ping := func(context.Context) error {
		pingCalled = true
		return errors.New("ping should not be called")
	}
	loader := NewDefaultSourceLoader(ping)
	got, err := loader.Load(context.Background(), SourceAuto, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pingCalled {
		t.Fatalf("ping should NOT be called for an existing directory ref")
	}
	gotDigest, _ := got.Digest()
	wantDigest, _ := img.Digest()
	if gotDigest != wantDigest {
		t.Fatalf("digest mismatch: got %s, want %s", gotDigest, wantDigest)
	}
}

func TestSourceLoader_AutoDispatchesToTarForTarFile(t *testing.T) {
	img := randomImage(t)
	tarPath := writeTarball(t, img, "img.tar")

	pingCalled := false
	ping := func(context.Context) error {
		pingCalled = true
		return errors.New("ping should not be called")
	}
	loader := NewDefaultSourceLoader(ping)
	got, err := loader.Load(context.Background(), SourceAuto, tarPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pingCalled {
		t.Fatalf("ping should NOT be called for a .tar file ref")
	}
	gotDigest, _ := got.Digest()
	wantDigest, _ := img.Digest()
	if gotDigest != wantDigest {
		t.Fatalf("digest mismatch: got %s, want %s", gotDigest, wantDigest)
	}
}

func TestSourceLoader_AutoDispatchesToDaemonForNonPath(t *testing.T) {
	img := randomImage(t)

	// Stub the daemon-image func so no real docker is consulted.
	var sawRef string
	swapDaemonImageFunc(t, func(ref name.Reference, _ ...daemon.Option) (v1.Image, error) {
		sawRef = ref.Name()
		return img, nil
	})

	ping := func(context.Context) error { return nil }
	loader := NewDefaultSourceLoader(ping)
	got, err := loader.Load(context.Background(), SourceAuto, "nginx:latest")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if sawRef == "" {
		t.Fatalf("daemon loader was not invoked")
	}
	if !strings.Contains(sawRef, "nginx") {
		t.Fatalf("expected ref to contain nginx, got %q", sawRef)
	}
	gotDigest, _ := got.Digest()
	wantDigest, _ := img.Digest()
	if gotDigest != wantDigest {
		t.Fatalf("digest mismatch: got %s, want %s", gotDigest, wantDigest)
	}
}

func TestSourceLoader_AutoFailsWhenDaemonUnreachable(t *testing.T) {
	pingErr := errors.New("dial unix /var/run/docker.sock: connect: no such file or directory")
	ping := func(context.Context) error { return pingErr }
	loader := NewDefaultSourceLoader(ping)

	_, err := loader.Load(context.Background(), SourceAuto, "nginx:latest")
	if err == nil {
		t.Fatalf("expected error when daemon unreachable")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if ae.Code != kindRegistryNoImageSource {
		t.Fatalf("expected %s, got %s", kindRegistryNoImageSource, ae.Code)
	}
	for _, want := range []string{"oci", "tar", "install Docker", "nginx:latest"} {
		if !strings.Contains(ae.Message, want) {
			t.Fatalf("expected %q in message, got %q", want, ae.Message)
		}
	}
}

func TestSourceLoader_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	loader := NewDefaultSourceLoader(func(context.Context) error { return nil })
	_, err := loader.Load(ctx, SourceAuto, "nginx:latest")
	if err == nil {
		t.Fatalf("expected error for pre-canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// Guard against accidentally introducing a silent panic on zero-manifest
// layouts — this path is defensive and not individually required by the
// task, but keeps the OCI branch honest.
func TestSourceLoader_OCILayoutEmptyIndex(t *testing.T) {
	dir := t.TempDir()
	if _, err := layout.Write(dir, empty.Index); err != nil {
		t.Fatalf("layout.Write: %v", err)
	}
	loader := NewDefaultSourceLoader(nil)
	_, err := loader.Load(context.Background(), SourceOCI, dir)
	if err == nil {
		t.Fatalf("expected error for empty OCI index")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if !strings.Contains(ae.Message, "no manifests") {
		t.Fatalf("expected 'no manifests' message, got %q", ae.Message)
	}
}

// Exercise the unknown-source-kind branch so switch coverage is
// explicit.
func TestSourceLoader_UnknownSourceKind(t *testing.T) {
	loader := NewDefaultSourceLoader(nil)
	_, err := loader.Load(context.Background(), ImageSource("bogus"), "nginx:latest")
	if err == nil {
		t.Fatalf("expected error for unknown source kind")
	}
	var ae *cmdutil.AgentError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *cmdutil.AgentError, got %T: %v", err, err)
	}
	if ae.Code != kindRegistryInvalidReference {
		t.Fatalf("expected %s, got %s", kindRegistryInvalidReference, ae.Code)
	}
	if !strings.Contains(ae.Message, "bogus") {
		t.Fatalf("expected message to include bogus, got %q", ae.Message)
	}
}

// Assert the swap point exists and is wired to the default constructor
// — push (Task 19) will depend on this being reassignable without a
// compile error.
func TestSourceLoaderBuilder_Swappable(t *testing.T) {
	prev := sourceLoaderBuilder
	defer func() { sourceLoaderBuilder = prev }()

	img := randomImage(t)
	called := false
	sourceLoaderBuilder = func(_ func(ctx context.Context) error) SourceLoader {
		called = true
		return stubSourceLoader{img: img}
	}
	loader := sourceLoaderBuilder(nil)
	if !called {
		t.Fatalf("expected swapped builder to be called")
	}
	got, err := loader.Load(context.Background(), SourceAuto, "anything")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := got.Digest(); err != nil {
		t.Fatalf("got.Digest: %v", err)
	}
}

type stubSourceLoader struct {
	img v1.Image
}

func (s stubSourceLoader) Load(context.Context, ImageSource, string) (v1.Image, error) {
	return s.img, nil
}
