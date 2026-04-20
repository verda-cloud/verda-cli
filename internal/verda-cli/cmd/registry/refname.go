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

// Package registry's refname.go is a thin, pure wrapper around
// go-containerregistry's name.ParseReference that:
//
//   - splits parsed references into an explicit Host/Project/Repository/Tag/Digest
//     struct (Ref), so callers don't have to poke at ggcr types;
//   - understands "short" VCR-style refs (e.g. "my-app:v1") and expands them to
//     "<endpoint>/<project>/<name>:<tag>" via the user's RegistryCredentials;
//   - keeps all ggcr types confined to this file so the rest of the package sees
//     only the plain Ref struct.
//
// Two entry points:
//
//   - Parse(raw) strictly parses a fully qualified reference. Short refs fail.
//   - Normalize(raw, creds) parses either form, expanding short refs with creds.

package registry

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// Ref is a parsed image reference broken into logical components.
// Exactly one of Tag or Digest is non-empty (Tag defaults to "latest" when
// neither is supplied in the source string).
type Ref struct {
	Host       string // e.g. "vccr.io", "index.docker.io", "registry.local:5000"
	Project    string // first path segment for VCR-style refs (matches creds.Endpoint); "" otherwise
	Repository string // path minus the Project segment (or full path when Project is "")
	Tag        string // "latest" when unspecified and no digest given
	Digest     string // "sha256:..." when supplied; "" otherwise
}

// String renders the Ref as a canonical, parseable reference. With Digest it
// returns "<host>/<path>@<digest>"; otherwise "<host>/<path>:<tag>". The path
// is "<Project>/<Repository>" when Project is non-empty, else just "<Repository>".
func (r Ref) String() string {
	path := r.Repository
	if r.Project != "" {
		path = r.Project + "/" + r.Repository
	}
	base := r.Host + "/" + path
	if r.Digest != "" {
		return base + "@" + r.Digest
	}
	return base + ":" + r.Tag
}

// IsVCR reports whether the reference's host equals the given credentials'
// Endpoint. Returns false when creds is nil or has an empty Endpoint.
//
//nolint:gocritic // hugeParam: Ref is an immutable value type; the contract in refname.go uses value receivers uniformly.
func (r Ref) IsVCR(creds *options.RegistryCredentials) bool {
	if creds == nil || creds.Endpoint == "" {
		return false
	}
	return r.Host == creds.Endpoint
}

// FullRepository returns "<Project>/<Repository>" or just "<Repository>" when
// Project is empty. This is the canonical repository path the Registry
// interface (e.g. Registry.Tags) expects.
//
//nolint:gocritic // hugeParam: Ref is an immutable value type; the contract in refname.go uses value receivers uniformly.
func (r Ref) FullRepository() string {
	if r.Project == "" {
		return r.Repository
	}
	return r.Project + "/" + r.Repository
}

// hasProjectNamespace reports whether a registry host is conventionally
// organized as "<namespace>/<repo>" (Docker Hub, VCR, GHCR-style public
// hosts) rather than an opaque self-hosted registry.
//
// Heuristic: any host that has no port and isn't the bare "localhost" alias.
// We don't maintain a lookup table — the colon-in-authority test is enough
// to catch every registry-with-port we've seen in the wild (localhost:5000,
// 127.0.0.1:5000, registry.local:5000, foo.example.com:443, ...).
func hasProjectNamespace(host string) bool {
	if host == "" || host == "localhost" {
		return false
	}
	if strings.ContainsRune(host, ':') {
		return false
	}
	return true
}

// isShortRef reports whether raw looks like a "short" reference with no
// registry host. The classic Docker heuristic:
//
//   - No "/" at all — definitely short (e.g. "my-app", "my-app:v1").
//   - Otherwise, the first "/"-delimited segment is treated as a host iff it
//     contains "." or ":" or equals "localhost". In all other cases the whole
//     string is a (possibly multi-segment) repository under the default
//     registry, so we still treat it as short.
//
// We only inspect the first segment; trailing tag/digest colons never appear
// before the first "/" in a valid reference, so no pre-stripping is needed.
func isShortRef(raw string) bool {
	slash := strings.IndexByte(raw, '/')
	if slash < 0 {
		return true
	}
	first := raw[:slash]
	if first == "localhost" {
		return false
	}
	return !strings.ContainsAny(first, ".:")
}

// parseWithDefault turns a raw string into a Ref, using defaultHost as the
// registry when no host is present in raw. The caller decides (via isShortRef)
// whether expansion is desired — parseWithDefault just invokes ggcr.
//
// For already-qualified refs (defaultHost == ""), we first peel the host
// segment off and pass the remainder with WithDefaultRegistry(host). This
// sidesteps a ggcr corner case: bare "localhost/..." is otherwise treated as
// a namespace under docker.io because ggcr only recognizes the first segment
// as a host if it contains "." or ":".
func parseWithDefault(raw, defaultHost string) (Ref, error) {
	effectiveHost := defaultHost
	parseInput := raw
	if defaultHost == "" {
		// Full ref: extract the first "/" segment as the host so ggcr can
		// apply it via WithDefaultRegistry even for hostless-looking names
		// like "localhost".
		if slash := strings.IndexByte(raw, '/'); slash > 0 {
			effectiveHost = raw[:slash]
			parseInput = raw[slash+1:]
		}
	}

	opts := []name.Option{}
	if effectiveHost != "" {
		opts = append(opts, name.WithDefaultRegistry(effectiveHost))
	}
	ref, err := name.ParseReference(parseInput, opts...)
	if err != nil {
		return Ref{}, fmt.Errorf("parse reference %q: %w", raw, err)
	}

	out := Ref{
		Host: ref.Context().RegistryStr(),
	}
	repoStr := ref.Context().RepositoryStr()

	// Split the leading "project/namespace" segment off the repository path
	// only for registries that have a meaningful top-level namespace concept:
	// public hosted registries like vccr.io and index.docker.io. Arbitrary
	// self-hosted registries (registry.local:5000, 127.0.0.1:5000, localhost)
	// keep the full path in Repository because their naming scheme is opaque.
	//
	// Heuristic: a host is "project-structured" iff it has no port and is not
	// the bare "localhost" alias. This matches all the known registries that
	// organize content as <namespace>/<repo> (Docker Hub's "library/" prefix,
	// VCR's tenant projects, GHCR-style /<org>/, etc.) without needing a
	// lookup table.
	if hasProjectNamespace(out.Host) {
		if slash := strings.IndexByte(repoStr, '/'); slash > 0 {
			out.Project = repoStr[:slash]
			out.Repository = repoStr[slash+1:]
		} else {
			out.Repository = repoStr
		}
	} else {
		out.Repository = repoStr
	}

	switch r := ref.(type) {
	case name.Tag:
		out.Tag = r.TagStr()
	case name.Digest:
		out.Digest = r.DigestStr()
	default:
		// name.ParseReference only ever returns Tag or Digest; this branch
		// exists so a future ggcr addition surfaces loudly rather than
		// silently defaulting.
		return Ref{}, fmt.Errorf("unexpected reference kind %T for %q", ref, raw)
	}
	return out, nil
}

// Parse parses a fully qualified reference (e.g. "vccr.io/proj/app:v1") with
// no defaulting. Short refs are rejected — callers that want expansion should
// use Normalize.
func Parse(raw string) (Ref, error) {
	if isShortRef(raw) {
		return Ref{}, fmt.Errorf("reference %q requires a host (e.g. vccr.io/project/name:tag)", raw)
	}
	return parseWithDefault(raw, "")
}

// Normalize parses a reference that may be short (no host). Short refs are
// expanded to "<creds.Endpoint>/<creds.ProjectID>/<raw>". Fails if raw is
// short and creds is nil or missing Endpoint/ProjectID.
func Normalize(raw string, creds *options.RegistryCredentials) (Ref, error) {
	if !isShortRef(raw) {
		return parseWithDefault(raw, "")
	}
	if creds == nil || creds.Endpoint == "" || creds.ProjectID == "" {
		return Ref{}, fmt.Errorf("no registry configured; cannot expand short reference %q (run `verda registry configure` or pass a fully qualified reference)", raw)
	}
	expanded := creds.Endpoint + "/" + creds.ProjectID + "/" + raw
	return parseWithDefault(expanded, creds.Endpoint)
}
