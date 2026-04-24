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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// RepositoryInfo is the normalized per-repository row returned by
// RepositoryLister. The Name field is the repository path without the
// project prefix — e.g. "library/hello-world" for Harbor's raw
// "{project}/library/hello-world". FullName keeps the raw value for
// debugging / structured output.
type RepositoryInfo struct {
	Name          string    `json:"name" yaml:"name"`
	FullName      string    `json:"full_name" yaml:"full_name"`
	ArtifactCount int64     `json:"artifact_count" yaml:"artifact_count"`
	PullCount     int64     `json:"pull_count" yaml:"pull_count"`
	UpdateTime    time.Time `json:"update_time" yaml:"update_time"`
}

// ArtifactInfo is the normalized per-artifact row returned by
// RepositoryLister.ListArtifacts. It maps Harbor's `/artifacts` response
// to the subset the CLI renders in the repo drill-down view (digest,
// tags, size, push time, pull time).
//
// An "artifact" in Harbor is a unique manifest digest. Multiple tags can
// point at the same artifact (same content, renamed), so Tags is a slice.
// PullTime is time.Time{} (zero) when an artifact has never been pulled —
// Harbor reports this as "0001-01-01T00:00:00Z" which parses to the Go
// zero value; callers should IsZero()-check before formatting.
type ArtifactInfo struct {
	Digest            string    `json:"digest" yaml:"digest"`
	Tags              []string  `json:"tags" yaml:"tags"`
	Size              int64     `json:"size" yaml:"size"`
	PushTime          time.Time `json:"push_time" yaml:"push_time"`
	PullTime          time.Time `json:"pull_time" yaml:"pull_time"`
	ArtifactType      string    `json:"artifact_type,omitempty" yaml:"artifact_type,omitempty"`
	ManifestMediaType string    `json:"manifest_media_type,omitempty" yaml:"manifest_media_type,omitempty"`
}

// RepositoryLister enumerates repositories — and their artifacts — in a
// Verda (Harbor) project, and deletes them on request. It is intentionally
// separate from the ggcr-backed Registry interface: Harbor's REST endpoints
// live outside the Docker Registry v2 surface. Keeping them on a dedicated
// interface preserves the "Registry == Docker v2" discipline ggcrRegistry
// relies on.
//
// The interface name is a legacy artifact — the original contract only
// listed. Delete methods live here too because they share the same
// credentials, transport, host, and error-translation plumbing; splitting
// them into a sibling "RepositoryDeleter" would only duplicate wiring.
type RepositoryLister interface {
	// ListRepositories returns every repository in projectName. projectName
	// is the Harbor project *name* (for VCR: the UUID string stored on
	// RegistryCredentials.ProjectID), not Harbor's numeric project_id.
	ListRepositories(ctx context.Context, projectName string) ([]RepositoryInfo, error)

	// ListArtifacts returns every artifact in repoName inside projectName.
	// repoName is the project-relative repository path (e.g.
	// "library/hello-world"), matching RepositoryInfo.Name. Slashes in the
	// path are URL-escaped by the implementation.
	ListArtifacts(ctx context.Context, projectName, repoName string) ([]ArtifactInfo, error)

	// DeleteRepository removes repoName and every artifact / tag under it
	// in one call, matching the "Delete image repository" action in the
	// Harbor web UI. Idempotency is NOT guaranteed by the server — a
	// second DELETE against an already-deleted repo returns 404, which
	// bubbles up as registry_repo_not_found.
	DeleteRepository(ctx context.Context, projectName, repoName string) error

	// DeleteArtifact removes a single artifact — identified by reference,
	// which is either a "sha256:..." digest or a plain tag name. Harbor's
	// endpoint accepts both shapes; the CLI treats them identically. When
	// reference is a tag, Harbor deletes the underlying manifest along with
	// EVERY tag that pointed at it — this matches the web UI's "Delete
	// image" button. Callers that want to unlink only a single tag should
	// use a tag-scoped endpoint (not exposed on this interface in v1).
	DeleteArtifact(ctx context.Context, projectName, repoName, reference string) error
}

// harborListerPageSize is the page size used for `/api/v2.0/repositories`.
// Harbor caps page_size at 100; we request the max so large projects
// fetch in as few round-trips as possible.
const harborListerPageSize = 100

// harborClient is the production RepositoryLister. It talks to Harbor's
// REST API v2.0 using Basic auth with the robot credentials minted for
// the project. A single *http.Client is reused across calls so the
// retrying transport's connection pool is shared.
type harborClient struct {
	host     string // e.g. "vccr.io"; no scheme
	username string
	secret   string
	http     *http.Client
}

// newHarborClient builds a harborClient from credentials. A zero
// RetryConfig disables retries (the stdlib default transport is used).
// Retries are safe: every call site uses idempotent GETs.
func newHarborClient(creds *options.RegistryCredentials, cfg RetryConfig) RepositoryLister {
	rt := http.DefaultTransport
	if cfg.enabled() {
		if base, ok := http.DefaultTransport.(*http.Transport); ok {
			rt = NewRetryingTransport(base.Clone(), cfg)
		} else {
			rt = NewRetryingTransport(http.DefaultTransport, cfg)
		}
	}
	return &harborClient{
		host:     creds.Endpoint,
		username: creds.Username,
		secret:   creds.Secret,
		// Leave Timeout unset — command layer imposes the ctx deadline.
		http: &http.Client{Transport: rt},
	}
}

// harborProject mirrors the subset of fields we need from
// `/api/v2.0/projects`. Harbor returns many more; json.Unmarshal ignores
// the rest.
type harborProject struct {
	ProjectID int    `json:"project_id"`
	Name      string `json:"name"`
}

// harborRepository mirrors the subset we need from
// `/api/v2.0/repositories`. Raw time strings are parsed into time.Time
// once at the edge.
type harborRepository struct {
	Name          string `json:"name"`
	ProjectID     int    `json:"project_id"`
	ArtifactCount int64  `json:"artifact_count"`
	PullCount     int64  `json:"pull_count"`
	UpdateTime    string `json:"update_time"`
}

// ListRepositories resolves the project name to Harbor's numeric
// project_id (one GET), then pages through `/api/v2.0/repositories`
// filtered by project_id. Stops when a page returns fewer than
// page_size rows.
//
// Why not `/api/v2.0/projects/{name}/repositories`? Because VCR's
// web-UI-minted robot accounts currently lack the `list repository`
// project-scoped permission that endpoint requires, but DO have
// read access via the top-level `/repositories` endpoint filtered by
// project_id. See registry/CLAUDE.md "List Repositories (ls)".
func (c *harborClient) ListRepositories(ctx context.Context, projectName string) ([]RepositoryInfo, error) {
	projectID, err := c.resolveProjectID(ctx, projectName)
	if err != nil {
		return nil, err
	}

	prefix := projectName + "/"
	var out []RepositoryInfo
	for page := 1; ; page++ {
		repos, err := c.fetchRepositoriesPage(ctx, projectID, page)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			// Defensive client-side filter: the q= query syntax filters
			// server-side, but Harbor has silently ignored the plain
			// ?project_id=N form in the past — keeping this check means
			// a future regression can't leak sibling-project rows.
			if r.ProjectID != projectID {
				continue
			}
			info := RepositoryInfo{
				Name:          strings.TrimPrefix(r.Name, prefix),
				FullName:      r.Name,
				ArtifactCount: r.ArtifactCount,
				PullCount:     r.PullCount,
			}
			if r.UpdateTime != "" {
				if t, perr := time.Parse(time.RFC3339, r.UpdateTime); perr == nil {
					info.UpdateTime = t
				}
			}
			out = append(out, info)
		}
		if len(repos) < harborListerPageSize {
			return out, nil
		}
	}
}

// harborArtifact mirrors the subset of fields we need from
// `/api/v2.0/projects/{project}/repositories/{repo}/artifacts`. Harbor's
// full response carries build history, SBOM links, vulnerability
// scanner results, accessories, and more — json.Unmarshal drops the rest.
type harborArtifact struct {
	Digest            string              `json:"digest"`
	Size              int64               `json:"size"`
	PushTime          string              `json:"push_time"`
	PullTime          string              `json:"pull_time"`
	ArtifactType      string              `json:"artifact_type"`
	ManifestMediaType string              `json:"manifest_media_type"`
	Tags              []harborArtifactTag `json:"tags"`
}

// harborArtifactTag is the per-tag sub-record attached to an artifact
// response when the request includes `with_tag=true`. Only Name is used
// downstream; Immutable / PushTime / PullTime are intentionally ignored
// (the Harbor UI doesn't surface them per-tag either).
type harborArtifactTag struct {
	Name string `json:"name"`
}

// ListArtifacts pages through
// `/api/v2.0/projects/{project}/repositories/{repo}/artifacts?with_tag=true`
// and returns normalized ArtifactInfo rows.
//
// Robot-account access: unlike `ls`, this endpoint *does* work for the
// project-scoped robot accounts VCR mints — confirmed against staging.
// If that changes, translateHarborError maps the 401/403 response to the
// appropriate registry_* AgentError.
//
// URL-escaping: repoName is a project-relative path that often contains
// a slash (e.g. "library/hello-world"). Harbor's routing requires the
// slash to be percent-encoded in the path segment — `url.PathEscape`
// does exactly that (turns "/" into "%2F" while leaving other chars
// alone). Plain path concatenation would be interpreted as two nested
// path segments and 404.
func (c *harborClient) ListArtifacts(ctx context.Context, projectName, repoName string) ([]ArtifactInfo, error) {
	escapedRepo := url.PathEscape(repoName)
	var out []ArtifactInfo
	for page := 1; ; page++ {
		arts, err := c.fetchArtifactsPage(ctx, projectName, escapedRepo, page)
		if err != nil {
			return nil, err
		}
		for _, a := range arts {
			info := ArtifactInfo{
				Digest:            a.Digest,
				Size:              a.Size,
				ArtifactType:      a.ArtifactType,
				ManifestMediaType: a.ManifestMediaType,
			}
			if a.PushTime != "" {
				if t, perr := time.Parse(time.RFC3339, a.PushTime); perr == nil && !t.IsZero() {
					info.PushTime = t
				}
			}
			if a.PullTime != "" {
				if t, perr := time.Parse(time.RFC3339, a.PullTime); perr == nil {
					// Harbor emits "0001-01-01T00:00:00Z" for
					// never-pulled artifacts; time.Time zero stays zero.
					info.PullTime = t
				}
			}
			for _, tag := range a.Tags {
				if tag.Name != "" {
					info.Tags = append(info.Tags, tag.Name)
				}
			}
			out = append(out, info)
		}
		if len(arts) < harborListerPageSize {
			return out, nil
		}
	}
}

// fetchArtifactsPage issues a single paged GET against Harbor's project-
// scoped artifacts endpoint. Callers detect end-of-list by observing a
// short page (same pattern as fetchRepositoriesPage).
func (c *harborClient) fetchArtifactsPage(ctx context.Context, projectName, escapedRepo string, page int) ([]harborArtifact, error) {
	q := url.Values{}
	q.Set("with_tag", "true")
	q.Set("page", strconv.Itoa(page))
	q.Set("page_size", strconv.Itoa(harborListerPageSize))
	reqURL := fmt.Sprintf("https://%s/api/v2.0/projects/%s/repositories/%s/artifacts?%s",
		c.host, url.PathEscape(projectName), escapedRepo, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, translateHarborError(err, 0, nil, nil)
	}
	req.SetBasicAuth(c.username, c.secret)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, translateHarborError(err, 0, nil, nil)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, translateHarborError(nil, resp.StatusCode, body, resp)
	}

	var arts []harborArtifact
	if err := json.Unmarshal(body, &arts); err != nil {
		return nil, &cmdutil.AgentError{
			Code:     kindRegistryInternalError,
			Message:  "decode /artifacts response: " + err.Error(),
			ExitCode: cmdutil.ExitAPI,
		}
	}
	return arts, nil
}

// resolveProjectID looks up the integer Harbor project_id for a project
// name. /api/v2.0/projects?name=X performs a case-insensitive substring
// match, so we filter exact matches client-side.
func (c *harborClient) resolveProjectID(ctx context.Context, projectName string) (int, error) {
	q := url.Values{}
	q.Set("name", projectName)
	q.Set("page", "1")
	q.Set("page_size", "100")
	reqURL := fmt.Sprintf("https://%s/api/v2.0/projects?%s", c.host, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return 0, translateHarborError(err, 0, nil, nil)
	}
	req.SetBasicAuth(c.username, c.secret)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, translateHarborError(err, 0, nil, nil)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, translateHarborError(nil, resp.StatusCode, body, resp)
	}

	var projects []harborProject
	if err := json.Unmarshal(body, &projects); err != nil {
		return 0, &cmdutil.AgentError{
			Code:     kindRegistryInternalError,
			Message:  "decode /api/v2.0/projects response: " + err.Error(),
			ExitCode: cmdutil.ExitAPI,
		}
	}
	for _, p := range projects {
		if p.Name == projectName {
			return p.ProjectID, nil
		}
	}
	return 0, &cmdutil.AgentError{
		Code:     kindRegistryRepoNotFound,
		Message:  fmt.Sprintf("Project %q not found on registry %s.", projectName, c.host),
		ExitCode: cmdutil.ExitNotFound,
	}
}

// DeleteRepository issues `DELETE /api/v2.0/projects/{project}/repositories/{repo}`.
// A 2xx response means the whole repo — every artifact and every tag —
// has been removed. 404 surfaces as registry_repo_not_found; 412 as
// registry_delete_blocked (Harbor uses 412 for Tag Immutability /
// Retention rule blocks).
//
// repoName is url.PathEscape'd exactly once (same discipline as
// ListArtifacts); concatenating slashes without escaping would make
// Harbor's router interpret the path as nested segments and return 404.
func (c *harborClient) DeleteRepository(ctx context.Context, projectName, repoName string) error {
	reqURL := fmt.Sprintf("https://%s/api/v2.0/projects/%s/repositories/%s",
		c.host, url.PathEscape(projectName), url.PathEscape(repoName))
	return c.doDelete(ctx, reqURL)
}

// DeleteArtifact issues `DELETE /api/v2.0/projects/{p}/repositories/{r}/artifacts/{ref}`.
// reference is either a digest ("sha256:...") or a tag name — Harbor's
// route handles both without disambiguation. We escape each path segment
// independently so colons in digests and slashes in repo paths are
// rendered as valid URI components.
func (c *harborClient) DeleteArtifact(ctx context.Context, projectName, repoName, reference string) error {
	if reference == "" {
		return &cmdutil.AgentError{
			Code:     kindRegistryInvalidReference,
			Message:  "Artifact reference must be a tag name or a \"sha256:...\" digest.",
			ExitCode: cmdutil.ExitBadArgs,
		}
	}
	reqURL := fmt.Sprintf("https://%s/api/v2.0/projects/%s/repositories/%s/artifacts/%s",
		c.host,
		url.PathEscape(projectName),
		url.PathEscape(repoName),
		url.PathEscape(reference),
	)
	return c.doDelete(ctx, reqURL)
}

// doDelete is the shared DELETE helper for the two delete endpoints.
// Factored out because both endpoints have identical auth / error /
// body-handling semantics; the only difference is the URL. 2xx responses
// — including 204 No Content — return nil; everything else funnels
// through translateHarborError.
func (c *harborClient) doDelete(ctx context.Context, reqURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, http.NoBody)
	if err != nil {
		return translateHarborError(err, 0, nil, nil)
	}
	req.SetBasicAuth(c.username, c.secret)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return translateHarborError(err, 0, nil, nil)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Drain the body so the retrying transport can reuse the
		// connection even when Harbor sends a small JSON envelope on
		// 200 (some deployments do, most send 204 with no body).
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return translateHarborError(nil, resp.StatusCode, body, resp)
}

// fetchRepositoriesPage issues a single paged GET against the top-level
// `/api/v2.0/repositories` endpoint. Harbor returns `[]` (not `null`) on
// a past-the-end page, so callers can detect end-of-list by observing a
// short page.
func (c *harborClient) fetchRepositoriesPage(ctx context.Context, projectID, page int) ([]harborRepository, error) {
	// Harbor's strict filter uses the rich-query syntax (`q=k=v`). The
	// bare `?project_id=N` query param is silently ignored and returns
	// every repository the robot has any access to — use q= instead.
	q := url.Values{}
	q.Set("q", "project_id="+strconv.Itoa(projectID))
	q.Set("page", strconv.Itoa(page))
	q.Set("page_size", strconv.Itoa(harborListerPageSize))
	reqURL := fmt.Sprintf("https://%s/api/v2.0/repositories?%s", c.host, q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, translateHarborError(err, 0, nil, nil)
	}
	req.SetBasicAuth(c.username, c.secret)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, translateHarborError(err, 0, nil, nil)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, translateHarborError(nil, resp.StatusCode, body, resp)
	}

	var repos []harborRepository
	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, &cmdutil.AgentError{
			Code:     kindRegistryInternalError,
			Message:  "decode /api/v2.0/repositories response: " + err.Error(),
			ExitCode: cmdutil.ExitAPI,
		}
	}
	return repos, nil
}

// translateHarborError maps a (transport-error, HTTP status, body) tuple
// to an *AgentError. Mirrors translateTransportError's classification for
// the JSON API path: 401/403/404 become the corresponding registry_*
// codes; everything else is registry_internal_error with the body included
// for debuggability.
//
// When transportErr is non-nil we delegate to translateError — it already
// handles net.DNSError, net.OpError, url.Error timeouts, and the "no such
// host" / "connection refused" string fallbacks.
func translateHarborError(transportErr error, status int, body []byte, _ *http.Response) error {
	if transportErr != nil {
		return translateError(transportErr)
	}

	// Short-circuit: 2xx paths never call this helper (see callers).
	switch status {
	case http.StatusUnauthorized:
		return &cmdutil.AgentError{
			Code:     kindRegistryAuthFailed,
			Message:  registryAuthFailedRecoveryMessage,
			ExitCode: cmdutil.ExitAuth,
		}
	case http.StatusForbidden:
		return &cmdutil.AgentError{
			Code:     kindRegistryAccessDenied,
			Message:  registryAccessDeniedRecoveryMessage,
			ExitCode: cmdutil.ExitAuth,
		}
	case http.StatusNotFound:
		return &cmdutil.AgentError{
			Code:     kindRegistryRepoNotFound,
			Message:  "Harbor endpoint not found (404). The registry may be misconfigured.",
			ExitCode: cmdutil.ExitNotFound,
		}
	case http.StatusPreconditionFailed:
		// Harbor returns 412 when a project policy (Tag Immutability or
		// Tag Retention) forbids the deletion. The body usually carries
		// a helpful "matched rule" hint; surface it alongside the
		// recovery message so users don't have to dig through API
		// docs to interpret the block.
		msg := registryDeleteBlockedRecoveryMessage
		if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
			if len(trimmed) > 256 {
				trimmed = trimmed[:256] + "…"
			}
			msg = "Deletion blocked by Harbor (HTTP 412): " + trimmed + "\n\n" + registryDeleteBlockedRecoveryMessage
		}
		return &cmdutil.AgentError{
			Code:     kindRegistryDeleteBlocked,
			Message:  msg,
			ExitCode: cmdutil.ExitAPI,
		}
	case http.StatusTooManyRequests:
		return &cmdutil.AgentError{
			Code:     kindRegistryRateLimited,
			Message:  "Rate limited by registry.",
			ExitCode: cmdutil.ExitAPI,
		}
	}
	// Fall-through: surface the HTTP status + a trimmed body so the raw
	// Harbor error string isn't completely hidden from the user.
	msg := fmt.Sprintf("Registry HTTP %d", status)
	if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
		// Cap body at 256 bytes so a huge HTML error page doesn't
		// saturate the terminal.
		if len(trimmed) > 256 {
			trimmed = trimmed[:256] + "…"
		}
		msg += ": " + trimmed
	}
	return &cmdutil.AgentError{
		Code:     kindRegistryInternalError,
		Message:  msg,
		ExitCode: cmdutil.ExitAPI,
	}
}
