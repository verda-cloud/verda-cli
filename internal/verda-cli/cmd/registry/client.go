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

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// Registry is the narrow interface the subcommands use to talk to a
// Docker Registry v2-compatible endpoint. The concrete implementation
// lives in this file (ggcrRegistry) and is swappable via helper.go's
// clientBuilder for tests. Keeping all go-containerregistry imports in
// this file mirrors the s3 package's discipline of isolating SDK
// imports to client.go + errors.go.
type Registry interface {
	// Catalog returns all repository names visible to the current credentials.
	Catalog(ctx context.Context) ([]string, error)

	// Tags returns tag names for the given repository (e.g. "my-app" or "ns/app").
	Tags(ctx context.Context, repo string) ([]string, error)

	// Head returns the manifest descriptor (digest, size, mediaType) for a ref.
	// Used by tags/ls commands to fetch size without downloading layers.
	Head(ctx context.Context, ref string) (*v1.Descriptor, error)

	// Write pushes an image to ref with concurrency + optional progress channel.
	Write(ctx context.Context, ref string, img v1.Image, opts WriteOptions) error

	// Read fetches an image from a registry (used by cp source-side).
	Read(ctx context.Context, ref string) (v1.Image, error)
}

// WriteOptions controls concurrency and progress reporting for Registry.Write.
type WriteOptions struct {
	Jobs     int              // layer-level parallelism (0 => ggcr default)
	Progress chan<- v1.Update // optional; closed by ggcr on success/error
}

// ggcrRegistry is the production Registry implementation backed by
// google/go-containerregistry. It carries the target host so relative
// refs like "my-app:v1" can default to the configured registry.
type ggcrRegistry struct {
	host string // e.g. "vccr.io"
	auth authn.Authenticator
}

// newGGCRRegistry builds a ggcrRegistry from credentials. The host is
// taken verbatim from creds.Endpoint (host only — no scheme, no path).
func newGGCRRegistry(creds *options.RegistryCredentials) Registry {
	return &ggcrRegistry{
		host: creds.Endpoint,
		auth: authn.FromConfig(authn.AuthConfig{
			Username: creds.Username,
			Password: creds.Secret,
		}),
	}
}

// parseRef parses a user-supplied ref, defaulting the registry to g.host
// when the ref is relative (e.g. "my-app:v1"). Absolute refs are
// passed through unchanged.
func (g *ggcrRegistry) parseRef(ref string) (name.Reference, error) {
	r, err := name.ParseReference(ref, name.WithDefaultRegistry(g.host))
	if err != nil {
		return nil, fmt.Errorf("parse reference %q: %w", ref, err)
	}
	return r, nil
}

// parseRepo parses a repository path (no tag/digest) relative to g.host.
func (g *ggcrRegistry) parseRepo(repo string) (name.Repository, error) {
	r, err := name.NewRepository(repo, name.WithDefaultRegistry(g.host))
	if err != nil {
		return name.Repository{}, fmt.Errorf("parse repository %q: %w", repo, err)
	}
	return r, nil
}

// Catalog lists all repositories on the registry.
func (g *ggcrRegistry) Catalog(ctx context.Context) ([]string, error) {
	reg, err := name.NewRegistry(g.host)
	if err != nil {
		return nil, fmt.Errorf("parse registry %q: %w", g.host, err)
	}
	return remote.Catalog(ctx, reg, remote.WithAuth(g.auth), remote.WithContext(ctx))
}

// Tags lists all tags for a repository.
func (g *ggcrRegistry) Tags(ctx context.Context, repo string) ([]string, error) {
	r, err := g.parseRepo(repo)
	if err != nil {
		return nil, err
	}
	return remote.List(r, remote.WithAuth(g.auth), remote.WithContext(ctx))
}

// Head fetches the manifest descriptor for a ref without downloading
// layers. Callers use this to check existence and obtain sizes.
func (g *ggcrRegistry) Head(ctx context.Context, ref string) (*v1.Descriptor, error) {
	r, err := g.parseRef(ref)
	if err != nil {
		return nil, err
	}
	return remote.Head(r, remote.WithAuth(g.auth), remote.WithContext(ctx))
}

// Write pushes an image to ref. Jobs == 0 falls back to ggcr's default
// layer-level parallelism. Progress, when non-nil, is closed by ggcr on
// success or error.
func (g *ggcrRegistry) Write(ctx context.Context, ref string, img v1.Image, opts WriteOptions) error {
	r, err := g.parseRef(ref)
	if err != nil {
		return err
	}
	remoteOpts := []remote.Option{
		remote.WithAuth(g.auth),
		remote.WithContext(ctx),
	}
	if opts.Jobs > 0 {
		remoteOpts = append(remoteOpts, remote.WithJobs(opts.Jobs))
	}
	if opts.Progress != nil {
		remoteOpts = append(remoteOpts, remote.WithProgress(opts.Progress))
	}
	return remote.Write(r, img, remoteOpts...)
}

// Read fetches an image from the registry. Used by `cp` on the source
// side.
func (g *ggcrRegistry) Read(ctx context.Context, ref string) (v1.Image, error) {
	r, err := g.parseRef(ref)
	if err != nil {
		return nil, err
	}
	return remote.Image(r, remote.WithAuth(g.auth), remote.WithContext(ctx))
}
