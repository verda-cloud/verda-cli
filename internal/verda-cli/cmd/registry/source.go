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
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// ImageSource identifies where to read an image from. The values match
// the user-facing --source flag choices for `verda registry push`.
type ImageSource string

const (
	// SourceAuto lets loadImageFromSource inspect the reference and pick
	// oci (directory), tar (file with .tar/.tar.gz extension), or daemon
	// (after a successful ping).
	SourceAuto ImageSource = "auto"
	// SourceDaemon resolves the reference against the local Docker daemon
	// via ggcr's pkg/v1/daemon.
	SourceDaemon ImageSource = "daemon"
	// SourceOCI reads an OCI image layout from a directory path.
	SourceOCI ImageSource = "oci"
	// SourceTar reads a Docker/OCI tarball from a file path.
	SourceTar ImageSource = "tar"
)

// SourceLoader loads a v1.Image from a user-provided reference / path.
// Implementations are constructed via the package-level
// sourceLoaderBuilder (swappable in tests).
type SourceLoader interface {
	Load(ctx context.Context, src ImageSource, ref string) (v1.Image, error)
}

// daemonImageFunc is the production entry point to ggcr's
// daemon.Image. It is assigned to a package variable so tests can
// stub the daemon branch without a running docker socket. Production
// code never reassigns it.
var daemonImageFunc = daemon.Image

// defaultSourceLoader is the production SourceLoader. It dispatches on
// ImageSource and, in auto mode, probes the daemon through the caller-
// supplied ping function to avoid a circular import between source.go
// and push (Task 19).
type defaultSourceLoader struct {
	ping func(ctx context.Context) error
}

// NewDefaultSourceLoader returns a SourceLoader that reads from the
// Docker daemon (for daemon), a filesystem OCI layout directory (for
// oci), or a tarball file (for tar). The ping function is only called
// for SourceAuto when the reference is not an on-disk path.
func NewDefaultSourceLoader(ping func(ctx context.Context) error) SourceLoader {
	return &defaultSourceLoader{ping: ping}
}

// Load dispatches to the daemon / OCI layout / tarball loaders based on
// src. In auto mode it inspects ref on the filesystem before falling
// back to the daemon, and returns a friendly multi-option error when
// the daemon is unreachable.
func (l *defaultSourceLoader) Load(ctx context.Context, src ImageSource, ref string) (v1.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	switch src {
	case SourceDaemon:
		return loadFromDaemon(ctx, ref)
	case SourceOCI:
		return loadFromOCILayout(ref)
	case SourceTar:
		return loadFromTarball(ref)
	case SourceAuto, "":
		return l.loadAuto(ctx, ref)
	default:
		return nil, &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("unknown image source %q; valid values are auto, daemon, oci, tar", string(src)),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
}

// loadAuto implements SourceAuto dispatch. It stats ref first; if ref
// is a directory, we use the OCI layout loader; if it's a regular file
// with a tar-ish extension, the tarball loader; otherwise we try the
// daemon after a successful ping.
func (l *defaultSourceLoader) loadAuto(ctx context.Context, ref string) (v1.Image, error) {
	info, statErr := os.Stat(ref)
	if statErr == nil {
		if info.IsDir() {
			return loadFromOCILayout(ref)
		}
		if info.Mode().IsRegular() && hasTarExtension(ref) {
			return loadFromTarball(ref)
		}
	}

	// Fall through to daemon. Probe first so unreachable daemons
	// produce a friendly multi-option error instead of whatever raw
	// network message ggcr would bubble up.
	if l.ping != nil {
		if err := l.ping(ctx); err != nil {
			return nil, daemonUnreachableError(ref, err)
		}
	}
	return loadFromDaemon(ctx, ref)
}

// hasTarExtension reports whether path has a recognized tarball
// extension. .tgz is intentionally accepted as an alias for .tar.gz —
// Docker itself writes both.
func hasTarExtension(path string) bool {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".tar"),
		strings.HasSuffix(lower, ".tar.gz"),
		strings.HasSuffix(lower, ".tgz"):
		return true
	}
	return false
}

// loadFromDaemon resolves ref against the local Docker daemon via the
// swappable daemonImageFunc. Parse errors on ref are surfaced as
// registry_invalid_reference so agent-mode consumers see a structured
// envelope instead of a raw ggcr string.
func loadFromDaemon(ctx context.Context, ref string) (v1.Image, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return nil, &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("invalid image reference %q: %s", ref, err.Error()),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	return daemonImageFunc(parsed, daemon.WithContext(ctx))
}

// loadFromOCILayout reads an OCI image layout directory at path and
// returns the first (and, for v1, only supported) image in its index.
// Multi-manifest layouts produce a descriptive error listing the
// available digests and any tag annotations so users can narrow down
// with a future --ref-digest flag.
func loadFromOCILayout(path string) (v1.Image, error) {
	idx, err := layout.ImageIndexFromPath(path)
	if err != nil {
		return nil, &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("cannot read OCI layout at %q: %s", path, err.Error()),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("cannot parse OCI index at %q: %s", path, err.Error()),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	if len(manifest.Manifests) == 0 {
		return nil, &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("OCI layout at %q contains no manifests", path),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	if len(manifest.Manifests) > 1 {
		return nil, multiManifestError(path, manifest.Manifests)
	}
	img, err := idx.Image(manifest.Manifests[0].Digest)
	if err != nil {
		return nil, &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("cannot load image from OCI layout at %q: %s", path, err.Error()),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	return img, nil
}

// multiManifestError builds the user-facing "multiple manifests" error
// for an OCI layout. The message lists each available digest alongside
// its ref-name / tag annotation (when present) so the user can decide
// which one to pick.
func multiManifestError(path string, manifests []v1.Descriptor) error {
	var b strings.Builder
	fmt.Fprintf(&b, "OCI layout at %q contains multiple manifests; select one with a digest or tag:\n", path)
	for i := range manifests {
		m := &manifests[i]
		tag := ""
		if m.Annotations != nil {
			// org.opencontainers.image.ref.name is the canonical tag
			// annotation written by `oci-image-tool` and Buildah.
			if v, ok := m.Annotations["org.opencontainers.image.ref.name"]; ok && v != "" {
				tag = v
			}
		}
		if tag != "" {
			fmt.Fprintf(&b, "  - %s (tag: %s)\n", m.Digest.String(), tag)
		} else {
			fmt.Fprintf(&b, "  - %s\n", m.Digest.String())
		}
	}
	return &cmdutil.AgentError{
		Code:     kindRegistryInvalidReference,
		Message:  strings.TrimRight(b.String(), "\n"),
		ExitCode: cmdutil.ExitBadArgs,
	}
}

// loadFromTarball reads a Docker/OCI tarball at path. The tag selector
// is nil, which instructs ggcr to return the first image in the
// archive; explicit tag selection is a future enhancement.
func loadFromTarball(path string) (v1.Image, error) {
	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		return nil, &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  fmt.Sprintf("cannot read tarball at %q: %s", path, err.Error()),
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	return img, nil
}

// daemonUnreachableError builds the friendly auto-mode fallback error
// when ping(ctx) fails. The spirit of the Task 19 design doc is
// preserved: lead with the underlying cause, then list the alternative
// --source options, then link to Docker install docs.
func daemonUnreachableError(ref string, underlying error) error {
	msg := fmt.Sprintf(
		"no image source available for %q.\n"+
			"  - Docker daemon: not reachable (%s)\n"+
			"Provide a source explicitly:\n"+
			"  verda registry push --source oci ./image-layout\n"+
			"  verda registry push --source tar ./image.tar\n"+
			"Or install Docker: https://docs.docker.com/get-docker/",
		ref, underlying.Error(),
	)
	return &cmdutil.AgentError{
		Code:    kindRegistryNoImageSource,
		Message: msg,
		Details: map[string]any{
			"reference":    ref,
			"daemon_error": underlying.Error(),
		},
		ExitCode: cmdutil.ExitAPI,
	}
}
