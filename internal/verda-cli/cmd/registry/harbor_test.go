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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
	"github.com/verda-cloud/verda-cli/internal/verda-cli/options"
)

// newHarborServer spins up an httptest.Server that mimics Harbor's
// /api/v2.0/projects and /api/v2.0/repositories endpoints. Tests tweak
// serverBehavior to simulate auth failures, unknown projects, paged
// result sets, etc.
type serverBehavior struct {
	// projects keyed by name; responses from /api/v2.0/projects?name=X.
	projects map[string]harborProject
	// repositories keyed by project_id; responses from /api/v2.0/repositories?q=project_id=N.
	// Callers supply the FULL list; the server paginates.
	reposByProject map[int][]harborRepository

	// forceStatus, when non-zero, overrides 2xx responses with an error
	// status for every endpoint. Used to test 401/403/etc.
	forceStatus int

	// strictFilter emulates Harbor's real behavior: when false (the
	// default), the server honors q=project_id=N. When true, the
	// server ignores the filter and returns every repository — which
	// lets us verify ListRepositories' defensive client-side filter.
	leakSiblings bool

	// wantUsername / wantSecret: when set, the handler rejects requests
	// whose basic auth doesn't match. Empty values skip auth checking.
	wantUsername string
	wantSecret   string

	// callCount records /repositories calls for paging tests.
	repositoriesCalls int
	projectsCalls     int

	// artifactsByRepoPath maps `{projectName}/{urlEscapedRepoName}` →
	// full artifact list. The handler paginates. Key format mirrors the
	// path segments the handler sees, so url.PathEscape("library/hello-
	// world") = "library%2Fhello-world" is what tests put in the key.
	artifactsByRepoPath map[string][]harborArtifact
	artifactsCalls      int
	// artifactsCalledPaths captures the full request URL paths (post-
	// escape) so tests can assert url.PathEscape was applied correctly.
	artifactsCalledPaths []string

	// deleteStatus, when non-zero, makes every DELETE on the
	// /projects/… prefix return that status code. Used for 412
	// (registry_delete_blocked) translation tests.
	deleteStatus int
	// deleteRepoCalledPaths captures the escaped path of every repo-
	// level DELETE (no trailing /artifacts/... segment).
	deleteRepoCalledPaths []string
	// deleteArtifactCalledPaths captures the escaped path of every
	// artifact-level DELETE (.../artifacts/{ref}).
	deleteArtifactCalledPaths []string
}

func newHarborServer(t *testing.T, b *serverBehavior) (*httptest.Server, *options.RegistryCredentials) {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v2.0/projects", b.handleProjects)
	mux.HandleFunc("/api/v2.0/projects/", b.handleProjectsSubtree)
	mux.HandleFunc("/api/v2.0/repositories", b.handleRepositories)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Strip "http://" prefix so the creds.Endpoint matches the shape the
	// CLI stores (host only). We patch the scheme inside the harbor
	// client via the test-only endpoint override below.
	host := strings.TrimPrefix(srv.URL, "http://")
	creds := &options.RegistryCredentials{
		Username:  "vcr-abc+test",
		Secret:    "s3cret",
		Endpoint:  host,
		ProjectID: "abc",
	}
	if b.wantUsername != "" {
		creds.Username = b.wantUsername
	}
	if b.wantSecret != "" {
		creds.Secret = b.wantSecret
	}
	return srv, creds
}

// checkAuth enforces wantUsername / wantSecret when they are set. Empty
// values skip the check (tests that don't care about auth).
func (b *serverBehavior) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if b.wantUsername == "" && b.wantSecret == "" {
		return true
	}
	u, p, ok := r.BasicAuth()
	if !ok || u != b.wantUsername || p != b.wantSecret {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED","message":"bad creds"}]}`))
		return false
	}
	return true
}

// handleProjects serves GET /api/v2.0/projects — the project lookup by
// name that resolveProjectID uses to map {projectName} → integer id.
func (b *serverBehavior) handleProjects(w http.ResponseWriter, r *http.Request) {
	b.projectsCalls++
	if !b.checkAuth(w, r) {
		return
	}
	if b.forceStatus != 0 {
		w.WriteHeader(b.forceStatus)
		_, _ = w.Write([]byte(`{"errors":[{"code":"FORCED","message":"forced"}]}`))
		return
	}
	name := r.URL.Query().Get("name")
	var out []harborProject
	if p, ok := b.projects[name]; ok {
		out = []harborProject{p}
	}
	writeJSON(w, out)
}

// handleProjectsSubtree serves everything under /api/v2.0/projects/… — the
// artifacts listing (GET) and the repo / artifact deletes (DELETE). Mounted
// on the prefix (rather than net/http 1.22 pattern routes) so tests can
// inspect the *raw* (pre-decoded) path — the "is the repo segment
// percent-encoded?" assertion is the whole point of several tests.
func (b *serverBehavior) handleProjectsSubtree(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.EscapedPath()
	const prefix = "/api/v2.0/projects/"
	if !strings.HasPrefix(raw, prefix) {
		http.NotFound(w, r)
		return
	}
	if !b.checkAuth(w, r) {
		return
	}
	if r.Method == http.MethodDelete {
		b.handleDelete(w, r, raw, prefix)
		return
	}
	b.handleArtifacts(w, r, raw, prefix)
}

// handleDelete services either repo-level
//
//	/api/v2.0/projects/{p}/repositories/{repo}
//
// or artifact-level
//
//	/api/v2.0/projects/{p}/repositories/{repo}/artifacts/{ref}
//
// DELETEs. Records the raw path in the matching call log so tests can
// assert url.PathEscape was applied.
func (b *serverBehavior) handleDelete(w http.ResponseWriter, r *http.Request, raw, prefix string) {
	mid := strings.TrimPrefix(raw, prefix)
	if !strings.Contains(mid, "/repositories/") {
		http.NotFound(w, r)
		return
	}
	if strings.Contains(mid, "/artifacts/") {
		b.deleteArtifactCalledPaths = append(b.deleteArtifactCalledPaths, raw)
	} else {
		b.deleteRepoCalledPaths = append(b.deleteRepoCalledPaths, raw)
	}
	status := b.deleteStatus
	if b.forceStatus != 0 {
		status = b.forceStatus
	}
	if status != 0 {
		w.WriteHeader(status)
		// 412 often carries a message body explaining the
		// matched retention / immutability rule — emulate
		// that so tests can assert the body gets surfaced.
		_, _ = w.Write([]byte(`{"errors":[{"code":"PRECONDITION","message":"matched rule X"}]}`))
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleArtifacts services GET requests on the .../artifacts tail of the
// projects subtree. Paginates the configured artifact pool by the page /
// page_size query params the client sends.
func (b *serverBehavior) handleArtifacts(w http.ResponseWriter, r *http.Request, raw, prefix string) {
	if !strings.HasSuffix(raw, "/artifacts") {
		http.NotFound(w, r)
		return
	}
	b.artifactsCalls++
	b.artifactsCalledPaths = append(b.artifactsCalledPaths, raw)
	if b.forceStatus != 0 {
		w.WriteHeader(b.forceStatus)
		_, _ = w.Write([]byte(`{"errors":[{"code":"FORCED","message":"forced"}]}`))
		return
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(raw, prefix), "/artifacts")
	parts := strings.SplitN(mid, "/repositories/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	key := parts[0] + "/" + parts[1]
	pool := b.artifactsByRepoPath[key]
	start, end := pageBounds(r, len(pool))
	writeJSON(w, pool[start:end])
}

// handleRepositories serves GET /api/v2.0/repositories — the top-level
// repository listing, filtered by q=project_id=N in production.
func (b *serverBehavior) handleRepositories(w http.ResponseWriter, r *http.Request) {
	b.repositoriesCalls++
	if !b.checkAuth(w, r) {
		return
	}
	if b.forceStatus != 0 {
		w.WriteHeader(b.forceStatus)
		_, _ = w.Write([]byte(`{"errors":[{"code":"FORCED","message":"forced"}]}`))
		return
	}
	q := r.URL.Query().Get("q")
	wantPID := -1
	if strings.HasPrefix(q, "project_id=") {
		if n, err := strconv.Atoi(strings.TrimPrefix(q, "project_id=")); err == nil {
			wantPID = n
		}
	}
	var pool []harborRepository
	if b.leakSiblings {
		for _, v := range b.reposByProject {
			pool = append(pool, v...)
		}
	} else if wantPID != -1 {
		pool = b.reposByProject[wantPID]
	}
	start, end := pageBounds(r, len(pool))
	writeJSON(w, pool[start:end])
}

// pageBounds parses the page / page_size query params and clamps start /
// end into [0, total]. Shared by the artifacts and repositories handlers
// so pagination stays consistent across both.
func pageBounds(r *http.Request, total int) (start, end int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize <= 0 {
		pageSize = 10
	}
	start = (page - 1) * pageSize
	end = start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	return start, end
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// newHarborClientForTest builds a harborClient but overrides the URL
// scheme to http:// so we can target httptest's plaintext server without
// shipping a test-only TLS cert. Production harborClient always uses
// https://; the scheme lives in a single fmt.Sprintf in ListRepositories.
// We work around this by swapping the transport's RoundTrip to rewrite
// the scheme on the fly.
func newHarborClientForTest(creds *options.RegistryCredentials) *harborClient {
	c := &harborClient{
		host:     creds.Endpoint,
		username: creds.Username,
		secret:   creds.Secret,
		http: &http.Client{
			Transport: &schemeRewriteTransport{inner: http.DefaultTransport, scheme: "http"},
		},
	}
	return c
}

// schemeRewriteTransport rewrites the request URL's scheme. Tests use
// this to let harborClient hit an httptest.Server over plain HTTP while
// production code continues to build https:// URLs.
type schemeRewriteTransport struct {
	inner  http.RoundTripper
	scheme string
}

func (t *schemeRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.scheme
	return t.inner.RoundTrip(req)
}

// ---------- tests ----------

// TestHarbor_ListRepositories_HappyPath covers the common flow: resolve
// project name to integer id, then page through /repositories until a
// short page terminates the loop.
func TestHarbor_ListRepositories_HappyPath(t *testing.T) {
	updated := time.Date(2026, 4, 22, 13, 40, 5, 0, time.UTC).Format(time.RFC3339)
	b := &serverBehavior{
		projects: map[string]harborProject{
			"abc": {ProjectID: 42, Name: "abc"},
		},
		reposByProject: map[int][]harborRepository{
			42: {
				{Name: "abc/library/hello-world", ProjectID: 42, ArtifactCount: 1, PullCount: 0, UpdateTime: updated},
				{Name: "abc/public/autoscaler", ProjectID: 42, ArtifactCount: 3, PullCount: 42, UpdateTime: updated},
			},
		},
	}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	repos, err := c.ListRepositories(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("len(repos) = %d, want 2: %+v", len(repos), repos)
	}
	// Repo names should be stripped of the "abc/" project prefix.
	wantNames := map[string]bool{"library/hello-world": true, "public/autoscaler": true}
	for _, r := range repos {
		if !wantNames[r.Name] {
			t.Errorf("unexpected stripped Name %q", r.Name)
		}
		if !strings.HasPrefix(r.FullName, "abc/") {
			t.Errorf("FullName = %q, want abc/ prefix", r.FullName)
		}
	}
}

// TestHarbor_ListRepositories_SiblingLeakDefended emulates Harbor silently
// ignoring the project filter (as it did with the ?project_id=N form).
// The client-side filter in ListRepositories must drop sibling rows.
func TestHarbor_ListRepositories_SiblingLeakDefended(t *testing.T) {
	b := &serverBehavior{
		projects: map[string]harborProject{
			"abc": {ProjectID: 42, Name: "abc"},
		},
		reposByProject: map[int][]harborRepository{
			42: {{Name: "abc/library/keep", ProjectID: 42, ArtifactCount: 1}},
			99: {{Name: "other/leaked/drop", ProjectID: 99, ArtifactCount: 1}},
		},
		leakSiblings: true,
	}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	repos, err := c.ListRepositories(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "library/keep" {
		t.Errorf("expected single library/keep row after defensive filter, got %+v", repos)
	}
}

// TestHarbor_ListRepositories_Paging drives the loop across multiple pages.
// Page size is harborListerPageSize (100); we seed 101 rows so the loop
// issues at least two GETs and terminates on the short page.
func TestHarbor_ListRepositories_Paging(t *testing.T) {
	var seeded []harborRepository
	for i := 0; i < harborListerPageSize+1; i++ {
		seeded = append(seeded, harborRepository{
			Name:          fmt.Sprintf("abc/repo-%03d", i),
			ProjectID:     42,
			ArtifactCount: 1,
		})
	}
	b := &serverBehavior{
		projects:       map[string]harborProject{"abc": {ProjectID: 42, Name: "abc"}},
		reposByProject: map[int][]harborRepository{42: seeded},
	}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	repos, err := c.ListRepositories(context.Background(), "abc")
	if err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
	if len(repos) != harborListerPageSize+1 {
		t.Errorf("len(repos) = %d, want %d", len(repos), harborListerPageSize+1)
	}
	if b.repositoriesCalls < 2 {
		t.Errorf("repositoriesCalls = %d, expected >= 2 paged GETs", b.repositoriesCalls)
	}
}

// TestHarbor_ListRepositories_UnknownProject translates "no project with
// that name" into registry_repo_not_found rather than a confusing 200
// with empty data or a naked 404.
func TestHarbor_ListRepositories_UnknownProject(t *testing.T) {
	b := &serverBehavior{projects: map[string]harborProject{}}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	_, err := c.ListRepositories(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryRepoNotFound {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryRepoNotFound)
	}
}

// TestHarbor_ListRepositories_Unauthorized maps 401 to registry_auth_failed
// and surfaces the two-step recovery (rotate credential → contact support).
// The in-expiry-window 401 means the credential was revoked server-side,
// so the message deliberately avoids blaming expiry.
func TestHarbor_ListRepositories_Unauthorized(t *testing.T) {
	b := &serverBehavior{forceStatus: http.StatusUnauthorized}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	_, err := c.ListRepositories(context.Background(), "abc")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryAuthFailed)
	}
	for _, want := range []string{
		"authentication failed",
		"Verda web UI",
		"verda registry configure",
		"support@verda.cloud",
	} {
		if !strings.Contains(agentErr.Message, want) {
			t.Errorf("Message missing %q; got:\n%s", want, agentErr.Message)
		}
	}
}

// TestHarbor_ListRepositories_Forbidden maps 403 to registry_access_denied
// with a user-actionable recovery message. The message must walk the
// user through the two recovery steps (rotate credential, then escalate
// to support) — this is the regression guard for the "ls says the user
// has no permission but doesn't tell them how to fix it" class of bugs.
func TestHarbor_ListRepositories_Forbidden(t *testing.T) {
	b := &serverBehavior{forceStatus: http.StatusForbidden}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	_, err := c.ListRepositories(context.Background(), "abc")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryAccessDenied {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryAccessDenied)
	}
	// The message must surface both recovery paths — the permission
	// story is too confusing for a terse "denied" to be useful.
	for _, want := range []string{
		"does not have permission",
		"Verda web UI",
		"verda registry configure",
		"support@verda.cloud",
	} {
		if !strings.Contains(agentErr.Message, want) {
			t.Errorf("Message missing %q; got:\n%s", want, agentErr.Message)
		}
	}
}

// TestHarbor_ListRepositories_BasicAuth asserts the Authorization header
// is populated with the credential's username and secret.
func TestHarbor_ListRepositories_BasicAuth(t *testing.T) {
	b := &serverBehavior{
		projects:       map[string]harborProject{"abc": {ProjectID: 42, Name: "abc"}},
		reposByProject: map[int][]harborRepository{42: nil},
		wantUsername:   "vcr-abc+test",
		wantSecret:     "s3cret",
	}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	if _, err := c.ListRepositories(context.Background(), "abc"); err != nil {
		t.Fatalf("ListRepositories: %v", err)
	}
}

// TestHarbor_ListRepositories_WrongSecret: if the handler rejects the
// basic-auth credentials we return 401 → registry_auth_failed.
func TestHarbor_ListRepositories_WrongSecret(t *testing.T) {
	b := &serverBehavior{
		wantUsername: "vcr-abc+test",
		wantSecret:   "correct",
	}
	_, creds := newHarborServer(t, b)
	creds.Secret = "wrong"
	c := newHarborClientForTest(creds)

	_, err := c.ListRepositories(context.Background(), "abc")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryAuthFailed)
	}
}

// TestHarbor_ListArtifacts_HappyPath covers the common flow: one artifact
// with one tag, non-zero push time, zero pull time (renders as zero in
// the returned ArtifactInfo).
func TestHarbor_ListArtifacts_HappyPath(t *testing.T) {
	pushed := time.Date(2026, 4, 22, 13, 40, 5, 0, time.UTC).Format(time.RFC3339)
	b := &serverBehavior{
		artifactsByRepoPath: map[string][]harborArtifact{
			// url.PathEscape("abc") = "abc"; url.PathEscape("library/hello-world") = "library%2Fhello-world"
			"abc/library%2Fhello-world": {{
				Digest:            "sha256:d1a8d0a4eeb63aff09f5f34d4d80505e0ba81905f36158cc3970d8e07179e59e",
				Size:              4015,
				PushTime:          pushed,
				PullTime:          "0001-01-01T00:00:00Z",
				ArtifactType:      "application/vnd.oci.image.config.v1+json",
				ManifestMediaType: "application/vnd.oci.image.manifest.v1+json",
				Tags:              []harborArtifactTag{{Name: "latest"}},
			}},
		},
	}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	arts, err := c.ListArtifacts(context.Background(), "abc", "library/hello-world")
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("len(arts) = %d, want 1", len(arts))
	}
	a := arts[0]
	if !strings.HasPrefix(a.Digest, "sha256:d1a8d0a4") {
		t.Errorf("Digest = %q", a.Digest)
	}
	if len(a.Tags) != 1 || a.Tags[0] != "latest" {
		t.Errorf("Tags = %v, want [latest]", a.Tags)
	}
	if a.Size != 4015 {
		t.Errorf("Size = %d, want 4015", a.Size)
	}
	if a.PushTime.IsZero() {
		t.Errorf("expected non-zero PushTime, got %v", a.PushTime)
	}
	// Harbor's "0001-01-01" sentinel => Go zero value.
	if !a.PullTime.IsZero() {
		t.Errorf("expected zero PullTime (never pulled), got %v", a.PullTime)
	}
}

// TestHarbor_ListArtifacts_URLEscapesRepoSlash is the regression guard
// that matters most: Harbor routes the path segments, so the slash in
// "library/hello-world" MUST be percent-encoded to "library%2Fhello-
// world". If a future refactor drops url.PathEscape, this test fails.
func TestHarbor_ListArtifacts_URLEscapesRepoSlash(t *testing.T) {
	b := &serverBehavior{
		artifactsByRepoPath: map[string][]harborArtifact{
			"abc/library%2Fhello-world": {},
		},
	}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	if _, err := c.ListArtifacts(context.Background(), "abc", "library/hello-world"); err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(b.artifactsCalledPaths) == 0 {
		t.Fatalf("artifacts endpoint was not called")
	}
	got := b.artifactsCalledPaths[0]
	if !strings.Contains(got, "library%2Fhello-world") {
		t.Errorf("expected percent-encoded repo segment in path, got %q", got)
	}
	if strings.Contains(got, "library/hello-world") {
		t.Errorf("repo slash leaked unencoded into path: %q", got)
	}
}

// TestHarbor_ListArtifacts_Paging: seed > page-size and assert the
// client issues multiple GETs and terminates on the short page.
func TestHarbor_ListArtifacts_Paging(t *testing.T) {
	var seeded []harborArtifact
	for i := 0; i < harborListerPageSize+1; i++ {
		seeded = append(seeded, harborArtifact{
			Digest: fmt.Sprintf("sha256:%064d", i),
			Size:   int64(i),
		})
	}
	b := &serverBehavior{
		artifactsByRepoPath: map[string][]harborArtifact{
			"abc/my%2Frepo": seeded,
		},
	}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	arts, err := c.ListArtifacts(context.Background(), "abc", "my/repo")
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(arts) != harborListerPageSize+1 {
		t.Errorf("len(arts) = %d, want %d", len(arts), harborListerPageSize+1)
	}
	if b.artifactsCalls < 2 {
		t.Errorf("artifactsCalls = %d, expected >= 2 paged GETs", b.artifactsCalls)
	}
}

// TestHarbor_ListArtifacts_Unauthorized and _Forbidden confirm the same
// error-translation contract that ListRepositories uses, since both
// methods funnel through translateHarborError.
func TestHarbor_ListArtifacts_Unauthorized(t *testing.T) {
	b := &serverBehavior{forceStatus: http.StatusUnauthorized}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	_, err := c.ListArtifacts(context.Background(), "abc", "library/hello-world")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryAuthFailed)
	}
}

func TestHarbor_ListArtifacts_Forbidden(t *testing.T) {
	b := &serverBehavior{forceStatus: http.StatusForbidden}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	_, err := c.ListArtifacts(context.Background(), "abc", "library/hello-world")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryAccessDenied {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryAccessDenied)
	}
}

// ---------- DeleteRepository / DeleteArtifact ----------

// TestHarbor_DeleteRepository_HappyPath verifies the URL shape and that
// a 200/204 from Harbor returns nil. The URL assertions are what keep
// future refactors honest — repoName must be percent-encoded exactly
// like the list-artifacts endpoint.
func TestHarbor_DeleteRepository_HappyPath(t *testing.T) {
	b := &serverBehavior{}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	if err := c.DeleteRepository(context.Background(), "abc", "library/hello-world"); err != nil {
		t.Fatalf("DeleteRepository: %v", err)
	}
	if len(b.deleteRepoCalledPaths) != 1 {
		t.Fatalf("deleteRepoCalledPaths = %v, want 1 call", b.deleteRepoCalledPaths)
	}
	got := b.deleteRepoCalledPaths[0]
	if !strings.Contains(got, "/api/v2.0/projects/abc/repositories/library%2Fhello-world") {
		t.Errorf("expected percent-encoded repo segment, got %q", got)
	}
	if strings.HasSuffix(got, "/artifacts") {
		t.Errorf("repo-level delete leaked into artifact endpoint: %q", got)
	}
}

// TestHarbor_DeleteRepository_NotFound surfaces a 404 as
// registry_repo_not_found (shared with ListRepositories' unknown-
// project branch — Harbor reuses the same code).
func TestHarbor_DeleteRepository_NotFound(t *testing.T) {
	b := &serverBehavior{deleteStatus: http.StatusNotFound}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	err := c.DeleteRepository(context.Background(), "abc", "library/missing")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryRepoNotFound {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryRepoNotFound)
	}
}

// TestHarbor_DeleteRepository_PreconditionFailed is the regression
// guard for 412 translation: Harbor returns 412 when a project policy
// (Tag Immutability / Retention) blocks the delete. The CLI must
// surface registry_delete_blocked with the recovery message.
func TestHarbor_DeleteRepository_PreconditionFailed(t *testing.T) {
	b := &serverBehavior{deleteStatus: http.StatusPreconditionFailed}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	err := c.DeleteRepository(context.Background(), "abc", "library/hello-world")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryDeleteBlocked {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryDeleteBlocked)
	}
	for _, want := range []string{
		"blocked by a Verda project policy",
		"Tag Immutability",
		"Tag Retention",
		"support@verda.cloud",
	} {
		if !strings.Contains(agentErr.Message, want) {
			t.Errorf("Message missing %q; got:\n%s", want, agentErr.Message)
		}
	}
}

// TestHarbor_DeleteRepository_Unauthorized confirms 401 translation
// travels through the same shared message as ListRepositories.
func TestHarbor_DeleteRepository_Unauthorized(t *testing.T) {
	b := &serverBehavior{deleteStatus: http.StatusUnauthorized}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	err := c.DeleteRepository(context.Background(), "abc", "library/hello-world")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryAuthFailed {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryAuthFailed)
	}
}

// TestHarbor_DeleteArtifact_ByDigest verifies the URL shape for the
// artifact-delete endpoint when reference is a digest. The colon in
// "sha256:" must be percent-encoded to stay a valid URI component.
func TestHarbor_DeleteArtifact_ByDigest(t *testing.T) {
	b := &serverBehavior{}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	const digest = "sha256:d1a8d0a4eeb63aff09f5f34d4d80505e0ba81905f36158cc3970d8e07179e59e"
	if err := c.DeleteArtifact(context.Background(), "abc", "library/hello-world", digest); err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}
	if len(b.deleteArtifactCalledPaths) != 1 {
		t.Fatalf("deleteArtifactCalledPaths = %v, want 1 call", b.deleteArtifactCalledPaths)
	}
	got := b.deleteArtifactCalledPaths[0]
	// Repo slash MUST be escaped (Harbor routes that segment); the
	// ":" in the digest is a valid pchar per RFC 3986 and
	// url.PathEscape leaves it alone — so we check the full expected
	// suffix verbatim rather than trying to assert on colons.
	wantSuffix := "/repositories/library%2Fhello-world/artifacts/" + digest
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("path does not end with %q; got %q", wantSuffix, got)
	}
}

// TestHarbor_DeleteArtifact_ByTag confirms the same endpoint accepts a
// plain tag name (no prefix) as the reference — matches Harbor's docs.
func TestHarbor_DeleteArtifact_ByTag(t *testing.T) {
	b := &serverBehavior{}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	if err := c.DeleteArtifact(context.Background(), "abc", "library/hello-world", "latest"); err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}
	if len(b.deleteArtifactCalledPaths) != 1 {
		t.Fatalf("deleteArtifactCalledPaths = %v, want 1 call", b.deleteArtifactCalledPaths)
	}
	got := b.deleteArtifactCalledPaths[0]
	if !strings.HasSuffix(got, "/artifacts/latest") {
		t.Errorf("expected tag ref at path tail, got %q", got)
	}
}

// TestHarbor_DeleteArtifact_EmptyReference fails fast — we don't want
// a bogus DELETE against the bare /artifacts collection (which some
// Harbor versions interpret as "delete all").
func TestHarbor_DeleteArtifact_EmptyReference(t *testing.T) {
	b := &serverBehavior{}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	err := c.DeleteArtifact(context.Background(), "abc", "library/hello-world", "")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryInvalidReference {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryInvalidReference)
	}
	if len(b.deleteArtifactCalledPaths) != 0 {
		t.Errorf("empty reference should not hit the server, got calls %v", b.deleteArtifactCalledPaths)
	}
}

// TestHarbor_DeleteArtifact_PreconditionFailed mirrors the repo-level
// 412 test, exercising the same translateHarborError branch from the
// artifact-delete path.
func TestHarbor_DeleteArtifact_PreconditionFailed(t *testing.T) {
	b := &serverBehavior{deleteStatus: http.StatusPreconditionFailed}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	err := c.DeleteArtifact(context.Background(), "abc", "library/hello-world", "latest")
	var agentErr *cmdutil.AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("expected AgentError, got %T: %v", err, err)
	}
	if agentErr.Code != kindRegistryDeleteBlocked {
		t.Errorf("Code = %q, want %q", agentErr.Code, kindRegistryDeleteBlocked)
	}
	// The body from the fake server ("matched rule X") should be
	// folded into the message.
	if !strings.Contains(agentErr.Message, "matched rule X") {
		t.Errorf("expected upstream body in message, got:\n%s", agentErr.Message)
	}
}

// TestHarbor_ListArtifacts_EmptyTagList preserves the "untagged" signal
// — ListArtifacts must not silently drop artifacts whose Tags field is
// missing or empty (SBOMs, dangling referrer manifests).
func TestHarbor_ListArtifacts_EmptyTagList(t *testing.T) {
	b := &serverBehavior{
		artifactsByRepoPath: map[string][]harborArtifact{
			"abc/repo": {{
				Digest: "sha256:deadbeef000000000000000000000000000000000000000000000000000000ff",
				Size:   1024,
				// Tags omitted on purpose.
			}},
		},
	}
	_, creds := newHarborServer(t, b)
	c := newHarborClientForTest(creds)

	arts, err := c.ListArtifacts(context.Background(), "abc", "repo")
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("len(arts) = %d, want 1", len(arts))
	}
	if len(arts[0].Tags) != 0 {
		t.Errorf("expected zero tags, got %v", arts[0].Tags)
	}
}
