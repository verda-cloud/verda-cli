# Registry Command Knowledge

> Go house style lives in the root `CLAUDE.md` § "Go House Style". This file carries registry-specific idioms only — see below for swap points, ggcr isolation rules, and the pre-translated AgentError contract that do not apply elsewhere.

## Quick Reference

- Parent: `verda registry` (aliases: `vccr` canonical, `vcr` legacy)
- Subcommands: `configure`, `show`, `login`, `ls`, `tags`, `push`, `copy` (alias `cp`), `delete` (aliases `del`, `rm`)
- Files:
  - `registry.go` -- Parent command registration. `Hidden: true`; register gated on `VERDA_REGISTRY_ENABLED=1` in `cmd/cmd.go`.
  - `configure.go` -- Credential setup (three input modes: `--paste`, `--username/--password-stdin/--endpoint`, interactive wizard).
  - `wizard.go` -- Bubbletea wizard steps for `configure`.
  - `show.go` -- Credential status readout (no secrets); near-expiry warning when < 7 days.
  - `login.go` -- Writes `~/.docker/config.json` so docker/compose/helm/nerdctl can auth against VCR. Pure file merge; no registry call.
  - `client.go` -- `Registry` interface + `ggcrRegistry` production implementation. All `remote.*` and `authn`/`name` imports isolated here + a few siblings.
  - `helper.go` -- `clientBuilder` / `daemonListerBuilder` / `sourceLoaderBuilder` swap points. `loadCredsFromFactory` + default-profile fallback.
  - `path.go` -- Credentials file path resolution (`--credentials-file` > `VERDA_REGISTRY_CREDENTIALS_FILE` > `options.DefaultCredentialsFilePath()`).
  - `errors.go` -- `translateError` + `translateErrorWithExpiry` mapping ggcr `transport.Error` + network errors -> `cmdutil.AgentError`.
  - `expiry.go` -- `checkExpiry(creds)` pre-flight helper.
  - `refname.go` -- `Ref` value type + `Parse`, `Normalize`, `hasProjectNamespace`, `isShortRef`. `pkg/name` import lives here.
  - `loginparse.go` -- `parseDockerLogin` turns a web-UI-supplied `docker login ...` line into `(username, secret, host, project-id)`.
  - `docker_daemon.go` -- `DaemonLister` + `DaemonImage` talking to the local Docker socket over HTTP (no shell-out to `docker`).
  - `source.go` -- `SourceLoader` + `ImageSource` (auto/daemon/oci/tar). `daemonImageFunc` swap point. ggcr daemon/layout/tarball imports live here.
  - `retry_transport.go` -- `RetryConfig` + `retryTransport` wrapping the HTTP transport used by ggcr.
  - `progress.go` -- Sliding-window throughput meter used by bubbletea push view.
  - `format.go` -- `formatBytes`, `formatMMSS`, `pluralS`, `isStructuredFormat`, small string helpers shared by `tags`/`push`/`copy`.
  - `harbor.go` -- `RepositoryLister` interface (`ListRepositories` + `ListArtifacts` + `DeleteRepository` + `DeleteArtifact`) + `harborClient` implementation. Talks to Harbor's REST API v2.0 (`/api/v2.0/projects`, `/api/v2.0/repositories`, `/api/v2.0/projects/{p}/repositories/{r}[/artifacts[/{ref}]]`) using Basic auth. Intentionally separate from the `Registry` interface (which is Docker Registry v2 / ggcr shaped); see "List Repositories (ls)" and "Delete (`delete`)" below.
  - `ls.go` -- List repositories in the active Verda project via `harborClient`. Non-TTY + structured output render a flat table/document; TTY output routes through `f.Prompter().Select` so the user can pick a repo to drill into, at which point `ListArtifacts` fetches per-artifact detail (digest / tags / size / push / pull) for a Harbor-UI-style image card.
  - `delete.go` -- Delete a repository (all artifacts / tags) or a single image (artifact) in the active project via `harborClient`. Positional target shapes: `REPOSITORY`, `REPOSITORY:TAG`, `REPOSITORY@DIGEST`. TTY + no arg enters an interactive flow (pick repo → sub-menu → `MultiSelect` over artifacts for batch delete, or whole-repo delete). Agent mode requires `--yes`; missing `--yes` returns `CONFIRMATION_REQUIRED`. Reuses `Normalize` from `refname.go` plus a local `classifyTarget` to distinguish "bare repo" vs "artifact by tag" (because `Normalize` defaults `Tag` to `"latest"` for push/copy semantics — that default would smuggle a tag-delete otherwise).
  - `tags.go` -- List tags + per-tag digest/size via `Head`.
  - `push.go` -- Push local images (daemon/oci/tar). Handles zero-arg interactive picker. `runPickerFn` swap point.
  - `push_view.go` -- Bubbletea model + renderer for the push/copy progress view. `isTerminalFn` swap point.
  - `push_picker.go` -- Bubbletea model for the interactive daemon-image picker (selectable list + `formatAgo` helper).
  - `copy.go` -- Copy single ref / `--all-tags` between registries. `sourceKeychainBuilder` / `sourceRegistryBuilder` swap points; worker pool + overwrite guard.
  - `*_test.go` alongside each file.

## Domain-Specific Logic

### Credential Gap (CRITICAL for future Claude sessions)

Registry credentials are **write-once from the user's side**. The Verda API's `GetRegistryCredentials` returns only the credential *name*, never the secret. Practical consequences:

- `configure` has to be **paste-driven**: the user copies the ready-made `docker login ...` line from the Verda web UI and pastes it into `--paste` (or the wizard prompt). `parseDockerLogin` (`loginparse.go`) extracts `username`, `secret`, `host`, and `project-id` from that string. The project-id comes out of the `vcr-<project>+<name>` username shape.
- If a user loses their secret, the only recovery is **delete + recreate in the UI** (there is no server-side "show me again" endpoint to fall back to).
- This is the reason the wizard exists at all — a password-prompt flow alone would fail on the endpoint and project-id fields.
- **Endpoint default (for users who only grabbed name+secret):** the `--username/--password-stdin` flag path accepts a missing `--endpoint` and resolves it in this order: explicit flag → `verda_registry_endpoint` already saved for the active profile → `defaultRegistryEndpoint` ("vccr.io") in `configure.go`. When the fallback kicks in we emit a `Using registry endpoint "…" (<source>)` line on stderr (non-agent only) so staging users don't silently get the production host. The paste path is unaffected — the host comes out of the pasted string verbatim. See `resolveEndpointForFlags` / `loadSavedEndpoint`.

### Expiry Handling

- `RegistryCredentials.ExpiresAt` defaults to `now + 30 days` at configure time. `--expires-in <days>` overrides.
- `checkExpiry(creds)` (`expiry.go`) is the pre-flight helper. **Every command RunE calls it BEFORE dialing the registry** (tags, push, copy, login). Skipping it means the user gets a generic 401 from ggcr instead of an actionable `registry_credential_expired`.
- `translateErrorWithExpiry(err, creds)` (`errors.go`) maps a server-side 401 to `registry_credential_expired` when `creds.ExpiresAt` is in the past, otherwise to `registry_auth_failed`. This is the preferred error-translation entry point for any command that has creds in hand.
- Legacy rows with `ExpiresAt.IsZero()` are treated as "not expired" — the server is authoritative. This keeps pre-Task-9 credentials working without a migration.

### Profile Fallback (registry-specific)

Registry commands are in `skipCredentialResolution` (see `cmd/cmd.go`), so `Options.Complete()` never runs and `AuthOptions.Profile` stays empty. `loadCredsFromFactory` in `helper.go` therefore falls back to `defaultProfileName` (`"default"`) when the profile is blank. Without this fallback, `LoadRegistryCredentialsForProfile(path, "")` would resolve ini.v1's synthetic `DEFAULT` section instead of the user's `[default]` section, and every registry command would falsely report "not configured" right after a successful `verda registry configure`. This mirrors the s3 package's pattern.

### ggcr Isolation Discipline

All `github.com/google/go-containerregistry` imports live in a narrow set of files:

- `client.go` (`remote`, `authn`, `name`, `v1`)
- `errors.go` (`v1/remote/transport`)
- `source.go` (`v1`, `daemon`, `layout`, `tarball`, `name`)
- `copy.go` (`authn`, `v1`)
- `push.go` (`v1`)
- `push_view.go` (`v1`)
- `refname.go` (`name`)

**Audit before adding new files.** Command files (`tags.go`, `configure.go`, `show.go`, `login.go`, `push_picker.go`, etc.) never import ggcr directly — they go through the `Registry` interface and the `Ref` value type.

### Swap Points (for Test Injection)

- `clientBuilder` (`helper.go`) -- swap with a fake `Registry` backed by an in-process test server.
- `daemonListerBuilder` (`helper.go`) -- swap with a fake `DaemonLister` to avoid touching the host's docker socket.
- `sourceLoaderBuilder` (`helper.go`) -- swap with a fake `SourceLoader`.
- `harborListerBuilder` (`helper.go`) -- swap with a fake `RepositoryLister` for `ls` tests. Separate from `clientBuilder` because Harbor REST and Docker Registry v2 are two different interfaces; see `harbor.go` + "List Repositories (ls)".
- `sourceKeychainBuilder` (`copy.go`) -- swap the source-side keychain; production is `authn.DefaultKeychain`.
- `sourceRegistryBuilder` (`copy.go`) -- swap the source-side `Registry` builder; production is `newGGCRRegistryForSource`.
- `runPickerFn` (`push.go`) -- swap the interactive picker driver; production is `runPickerTUI`.
- `isTerminalFn` (`push_view.go`) -- swap TTY detection; production is `isTerminalFD`.
- `daemonImageFunc` (`source.go`) -- swap ggcr's `daemon.Image` so tests don't need a docker socket.

### Docker is Optional (NOT a Dependency)

- There is **no shell-out to the `docker` CLI binary** anywhere.
- The Docker **daemon** is consulted only as one of three possible `push` sources (`--source daemon`, `--source auto`, or the zero-arg interactive picker). OCI layouts and tarballs are first-class alternatives.
- `login` writes `~/.docker/config.json` directly via pure JSON manipulation. `credsStore`, `credHelpers`, `HttpHeaders`, `psFormat`, and any unknown top-level keys are preserved verbatim.
- `cp` reads `~/.docker/config.json` via `authn.DefaultKeychain` if the file exists, but doesn't require it — public images pull anonymously when the keychain is absent or unconfigured.

### Cross-Repo Blob Mount

ggcr auto-emits `mount=<digest>&from=<existing-repo>` hints when it sees a layer already present on the destination registry. Big win for ML images with shared base layers. The `--no-mount` flag on `push` is accepted but **currently a no-op** — we print a one-line note on `ErrOut` when it's set. Wiring it requires a `remote.WithMount` option that doesn't cleanly exist yet; see the comment in `push.go`.

### Retry + Resume

- `retry_transport.go` wraps ggcr's HTTP transport. Retries idempotent methods (GET/HEAD/PUT/DELETE/PATCH — **not POST**) on 408/429/500/502/503/504 and network timeouts. Respects `Retry-After` on 429/503 (seconds or HTTP-date).
- Across-invocation resume is free from the registry protocol: re-running the same `push`/`copy` skips layers already present on the destination (content-addressed dedup). No local state file needed.
- Chunk-level resume (resuming a single layer mid-upload) is **not implemented** — deferred. Transient errors mid-layer restart that layer from zero but the rest of the image is unaffected.

### Worker Pool Semantics

- `--jobs` = layer-level parallelism within one image. Routed into `remote.WithJobs`. `0` means ggcr default.
- `--image-jobs` = image-level parallelism across tags in `copy --all-tags`. Implemented as raw `sync.WaitGroup` + channels (NOT `errgroup`) because we want **partial-success** semantics: one tag failure must not cancel sibling writes.
- `resolveImageJobs(userValue, tagCount, hwConcurrency)` (`copy.go`): user value wins (clamped to `min(hw, 8)`); auto-mode picks `1` when < 4 tags, otherwise `hw/2` capped at 4. The cap prevents a 32-core laptop from DoSing a small registry.
- `push --image-jobs` is currently hardcoded to 1 (stored on `pushOptions` for a future parallel push); push today is sequential across multiple positional args.

### Source Authentication (copy)

`copy` runs two independent auth chains: VCR credentials on the destination (always the robot account from `~/.verda/credentials`), and a pluggable source-side chain selected by `--src-auth`:

- `docker-config` (default) -- `authn.DefaultKeychain` via `sourceKeychainBuilder`. Reads `~/.docker/config.json`, honors `credsStore` / `credHelpers`, anonymous fallback. **If `docker pull <src>` works, `vccr copy <src>` reads the same creds.**
- `anonymous` -- sends no Authorization header. Used to bypass a stale docker-config entry, or to prove a source is actually public.
- `basic` -- takes `--src-username` + secret via `--src-password-stdin`. The CLI never persists these to disk. `--debug` may still emit `Authorization` headers in its HTTP trace, so avoid `--debug` on shared terminals when using `basic`.

Cloud registries (ECR / GCR / Artifact Registry / ACR / ...) don't need a separate code path — their CLIs (`aws ecr get-login-password`, `gcloud auth configure-docker`, `az acr login`) write tokens into `~/.docker/config.json`, after which the default `docker-config` mode picks them up. The user-facing guide lives in `README.md` under "Source authentication (private images)" — point users there rather than re-explaining in chat when possible.

`sourceKeychainBuilder` and `sourceRegistryBuilder` (`copy.go`) are the swap points for tests; production wires `authn.DefaultKeychain` and `newGGCRRegistryForSource` respectively.

### `--yes` Implies `--overwrite` (copy)

Matches s3's `rm`/`rb` pattern. Both flags can be set; if either is set, `copy` proceeds through pre-existing destination tags without prompting. In the `runCopy` body we normalize `opts.Yes -> opts.Overwrite` once, so everywhere downstream reads only `Overwrite`.

### Overwrite Guard (copy)

Before each `Write`, we `Head` the destination ref:

- Not found (translated error has `Code` in {`registry_tag_not_found`, `registry_repo_not_found`} per `isNotFoundError`) -> proceed; no prompt.
- Found AND `!opts.Overwrite`:
  - Interactive TTY -> `prompter.Confirm` (declined -> `skipped` status, not failed).
  - Agent mode -> `cmdutil.NewConfirmationRequiredError("copy")`.
  - Non-TTY non-agent -> safe default: skip (never destructive writes when we can't ask).
- Found AND `opts.Overwrite` -> proceed; ggcr's `Write` is idempotent on manifest replace.

`--dry-run` skips the guard entirely by design (it's asking "what would happen", not doing).

### Feature Gate

- Parent command hidden via `Hidden: true` in `registry.go` so `verda --help` doesn't advertise it pre-GA.
- Registration in `cmd/cmd.go` gated by `registryEnabled()` which reads `VERDA_REGISTRY_ENABLED=1` / `=true`. Without the env var, the command tree isn't registered at all and `verda registry ...` returns "unknown command".
- **GA flip**: delete `registryEnabled()`, drop the gate in `NewRootCommand`, and remove `Hidden: true` from `registry.go`.

## Gotchas & Edge Cases

- `Ref.Project` is **only populated** for hosts "conventionally organized as project/repo" — `vccr.io`, `docker.io`, `gcr.io`, etc. Self-hosted registries with ports (`registry.local:5000/team/app`) have `Project=""` and the full path lives on `Repository="team/app"`. See `hasProjectNamespace` in `refname.go` — the heuristic is "no port in host and not bare localhost".
- `name.ParseReference` rewrites `docker.io` -> `index.docker.io` internally. Tests that inspect the parsed host need `strings.HasSuffix(host, "docker.io")`, not `host == "docker.io"`.
- 1-character repo names are rejected by ggcr's `pkg/name`. Tests use at least 2 chars (e.g. `"app"`, not `"a"`).
- `formatAgo` in `push_picker.go` treats sub-60s durations (including negative skew from clock drift) as `"just now"`.
- `TestPush_InteractiveMode_HappyPath` (and similar) uses `--progress plain` to bypass the TUI branch; the real bubbletea path is hard to drive in unit tests, so it's covered by model-level tests in `push_view_test.go` / `push_picker_test.go` instead.
- `pushViewModel` (`push_view.go`) is **reused by `copy`** for both single-ref and `--all-tags`. The heading still says "Pushing N images" even in copy mode — a minor label mismatch, acceptable for v1; easy to swap in a dedicated copy header later.
- The pre-flight `Head` on a copy destination relies on `translateTransportError` mapping a bare 404 (no structured `Diagnostic` body) to `registry_tag_not_found`. Without that fallthrough, non-existent destinations would surface as `registry_internal_error` and the overwrite guard would bail out before the first write.
- `configure --docker-config` is accepted but not yet wired — it prints a TODO notice pointing at `verda registry login`.
- Bubbletea output always goes to `ioStreams.ErrOut`. Stdout stays clean for structured / scripted consumption.
- `splitLocalRef` in `push.go` intentionally does **not** use `Normalize()` — Normalize prefixes with `creds.ProjectID`, which is correct for VCR destinations but would corrupt a local `my-app:v1` source ref. The host heuristic mirrors `isShortRef` (first segment is a host iff it contains `.` / `:` or is `localhost`).

## Relationships

- `cmdutil` (`internal/verda-cli/cmd/util`) -- `Factory`, `IOStreams`, `DebugJSON`, `WriteStructured`, `AgentError`, `NewConfirmationRequiredError`, `LongDesc`, `Examples`, `UsageErrorf`, exit-code constants.
- `options` -- `RegistryCredentials`, `LoadRegistryCredentialsForProfile`, `WriteRegistryCredentialsToProfile`, `DefaultCredentialsFilePath`, `EnsureVerdaDir`.
- `verdagostack/pkg/tui/wizard` -- imported by `configure.go` only (the credential-setup wizard).
- `verdagostack/pkg/tui` -- `WithConfirmDefault` for the overwrite prompt in `copy`.
- `google/go-containerregistry` -- `pkg/v1`, `pkg/v1/remote`, `pkg/v1/remote/transport`, `pkg/v1/daemon`, `pkg/v1/layout`, `pkg/v1/tarball`, `pkg/name`, `pkg/authn` (plus `pkg/registry` as the in-process test server in `*_test.go`).
- `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2` -- progress view, picker, and wizard styling.
- `github.com/charmbracelet/x/term` -- `IsTerminal` wired through `isTerminalFn`.
- `github.com/spf13/cobra` -- command definitions.

## List Repositories (`ls`)

`ls` was removed once (robot accounts had no list permission) and re-added. The current implementation lives in `harbor.go` + `ls.go`. Essential facts a future session needs to avoid reopening old bug reports:

### Endpoint choice

- **NOT** `/v2/_catalog` — admin-only on Harbor; returns 401 for project robots forever (not a permission the robot can be granted).
- **NOT** `/api/v2.0/projects/{project}/repositories` — requires a project-scoped permission that the Verda-minted robots do *not* carry today; returns 403 even when the top-level listing endpoint works.
- **YES** `/api/v2.0/repositories?q=project_id=N` — the top-level listing endpoint, filtered server-side with Harbor's rich-query syntax. The robot permission granted in Apr 2026 unlocks this path.

### `q=` vs `?project_id=N`

Harbor's top-level `/repositories` endpoint has *two* filter syntaxes that look equivalent but aren't:

- `?project_id=N` is **silently ignored**. The server returns every repository the robot can see, across every project.
- `?q=project_id=N` (URL-encoded to `q=project_id%3D24`) is **strict**. Use this.

`harborClient.fetchRepositoriesPage` uses the `q=` form, and `ListRepositories` still applies a client-side filter (`r.ProjectID == projectID`) as belt-and-suspenders. If the server starts honoring the bare form in the future, the client-side filter becomes a no-op, not a regression.

### Name mapping

- Harbor's `project.name` for VCR is the project UUID string (`"00000000-0000-4000-a000-..."`). That's what `creds.ProjectID` stores despite the key name — see `loginparse.go`.
- `/api/v2.0/repositories` returns fully-qualified names like `"{project-uuid}/library/hello-world"`. `ListRepositories` strips the `{projectName}/` prefix so the human table shows just `library/hello-world`. The unstripped value is preserved in `RepositoryInfo.FullName` for structured output.
- `resolveProjectID` maps the project name to Harbor's integer `project_id` (needed for the `q=` filter). It filters the `/projects?name=X` results by exact name match — the `name` query parameter does a case-insensitive substring match, not exact.

### Why a separate interface

`Registry` (in `client.go`) is intentionally Docker Registry v2 / ggcr shaped. Harbor REST lives on a different surface. Mixing them would force every ggcr swap point in tests to also stub repo listing — a lot of churn for a contract that doesn't actually need to be unified. See `harbor.go` for `RepositoryLister` + `harborClient`, and `helper.go` for the dedicated `harborListerBuilder` swap point.

### Pre-translated AgentErrors

`harborClient` returns `*cmdutil.AgentError` directly (via `translateHarborError`) rather than bubbling raw transport errors. `translateError` / `translateErrorWithExpiry` in `errors.go` detect a pre-translated AgentError and pass it through unchanged — without this guard, harbor's structured `registry_access_denied` would get re-wrapped as `registry_internal_error` by the ggcr-shaped fallback. Any future non-ggcr client added to this package should follow the same contract: emit a fully-populated AgentError and let the helpers pass it through.

### Interactive drill-down (TTY only)

`runLs` forks three ways:

1. `-o json|yaml` → `WriteStructured` → return. The picker never runs; scripts always get a deterministic document.
2. `!isTerminalFn(ioStreams.Out)` → `renderLsHuman` → return. Same table the pre-drill-down `ls` used to produce. Piping to `less` / `jq` / CI logs hits this path.
3. TTY → `runLsInteractive` loops the user through `prompter.Select` → `ListArtifacts(project, repo.Name)` → `renderRepoArtifacts`. Selecting "Exit" (the appended trailing label) or a prompter cancel error returns nil cleanly, same contract as `cmd/vm/list.go`. A transient `ListArtifacts` error is printed to ErrOut and the picker stays open — matching `vm list`'s "one flaky fetch doesn't kick the user out" behavior.

`ListArtifacts` percent-encodes the repo name (`url.PathEscape`) because Harbor routes the path; the regression test `TestHarbor_ListArtifacts_URLEscapesRepoSlash` asserts the encoded form reaches the server. Unlike the repository-listing endpoint, the project-scoped `/repositories/{r}/artifacts` endpoint *does* work for the robot accounts VCR mints today (confirmed against staging, Apr 2026). If that changes, `translateHarborError` already maps 401/403 to the appropriate `registry_*` AgentError.

The picker's per-row label is built by `formatRepoRow` (repo name padded to 48 cols + right-aligned artifact/pull counts + short-form "UPDATED" date). The image-list card is rendered by `renderRepoArtifacts`, columns mirror Harbor's web UI (`DIGEST` / `TAGS` / `SIZE` / `PUSHED` / `PULLED`). Zero-valued `PullTime` (Harbor's "never pulled" sentinel is the 0001-01-01 epoch) renders as `--`. Untagged artifacts (SBOMs, referrer manifests) show `<untagged>` in the TAGS column.

## Delete (`delete` / aliases `del`, `rm`)

`verda registry delete` shares the `RepositoryLister` plumbing with `ls` — same credentials, same host, same error translation — and adds two write methods on the interface: `DeleteRepository` and `DeleteArtifact`. Essentials for future sessions:

### Positional parsing (`classifyTarget` + `Normalize`)

The command accepts three target shapes:

- `REPOSITORY` → whole-repo delete (`/projects/{p}/repositories/{r}`).
- `REPOSITORY:TAG` → artifact delete (`/projects/{p}/repositories/{r}/artifacts/{tag}`).
- `REPOSITORY@DIGEST` → artifact delete via digest.

`classifyTarget` inspects only the path's **last "/"-delimited segment** for `@` or `:` — this prevents `host:port` (`registry.local:5000/...`) from mis-classifying as a tagged delete. We do NOT rely on the `Tag`/`Digest` fields populated by `Normalize`: `Normalize` defaults `Tag` to `"latest"` for bare repo refs (correct for push/copy, wrong here — it would silently smuggle a tag delete into `registry delete library/hello-world`).

Cross-project refs (e.g. `vccr.io/other-project/...` when the active cred is for `abc`) are rejected up front with a `registry_invalid_reference` error. The robot wouldn't be able to service the call anyway, but a clear local error beats a confusing 403 from Harbor.

### Harbor delete endpoints

- `DELETE /api/v2.0/projects/{p}/repositories/{r}` — whole-repo delete. Removes every artifact and every tag in one call. Idempotent from the user's perspective; the server returns 404 on a second call which maps to `registry_repo_not_found`.
- `DELETE /api/v2.0/projects/{p}/repositories/{r}/artifacts/{ref}` — artifact delete. `ref` is either a digest (`sha256:...`) or a tag name; Harbor's router accepts both shapes and deletes the underlying manifest plus every tag pointing at it. The CLI does not expose a tag-scoped "unlink just this tag" endpoint in v1 — matches the Harbor UI's "Delete image" button.
- Both repo and artifact paths `url.PathEscape` each segment independently, so repos containing `/` (Harbor's project-relative path style) and digests containing `:` route cleanly. The regression test for repo escaping is `TestHarbor_DeleteRepository_HappyPath`.

### 412 Precondition Failed → `registry_delete_blocked`

Harbor returns HTTP 412 when a project policy — **Tag Immutability** or **Tag Retention** — forbids the delete. `translateHarborError` maps that to a new `registry_delete_blocked` AgentError (`registryDeleteBlockedRecoveryMessage`) that walks the user through (1) editing the policy in the web UI, (2) escalating to support. The Harbor body (usually `"matched rule X"`) is folded into the message verbatim so the user can tell which rule triggered.

### Confirmation contract

- `--yes` / `-y` skips the prompt. In agent mode it is **mandatory**; a missing `--yes` short-circuits to `cmdutil.NewConfirmationRequiredError("delete")` (`CONFIRMATION_REQUIRED`, exit `ExitBadArgs`) — mirrors `vm delete`.
- In TTY mode without `--yes`, the repo flow renders the Harbor-style "⚠ Delete image repository" dialog (red header + info line showing artifact count from a best-effort `ListArtifacts` probe + "cannot be undone"). The artifact flow renders the matching "Delete image" dialog (DIGEST / TAG / SIZE row). Both terminate in a plain `Prompter.Confirm` — same yes/no contract the user asked for.
- Non-TTY stdout with no positional arg is refused (same `registry_invalid_reference` as the agent-mode miss) — scripts must always pass a target.

### Interactive flow

`runDeleteInteractive` mirrors `ls`'s picker but lives on a two-level menu:

1. Outer picker: select a repository (or Exit).
2. Inner menu: "Delete image(s) from X", "Delete repository X (all images)", "Back to repository list", "Exit".
3. The image-delete sub-flow uses `prompter.MultiSelect` with a label that surfaces the **Ctrl+A** "select all" keystroke — the bubbletea `MultiSelect` already supports it natively (see `verdagostack/pkg/tui/bubbletea/multiselect.go`), so we just advertise it in the prompt label and tests emulate it via `AddMultiSelect([]int{0, 1, ..., n-1})`.
4. The image batch runs sequentially (Harbor has no bulk-delete endpoint). Failures on individual artifacts are collected and reported at the end; survivors still get deleted — users generally want partial progress, not all-or-nothing.

### Structured output (agent mode)

`deleteResult` shape keyed on `Action`:

- `delete_repository`: `repository`, `deleted_artifacts` (best-effort count from pre-delete `ListArtifacts`), `status=completed`.
- `delete_artifact`: `repository`, `reference`, `digest` + `removed_tags` (best-effort from pre-delete `ListArtifacts`), `status=completed`.

Pre-delete `ListArtifacts` failures degrade gracefully — the delete proceeds, the count/digest/tags fields are omitted (never zero-filled or lying).

### Test coverage

- `TestHarbor_Delete{Repository,Artifact}_*` in `harbor_test.go` — URL encoding, happy paths, 404/401/412.
- `TestDelete_*` in `delete_test.go` — positional shapes (repo / tag / digest), cross-project rejection, non-TTY no-target rejection, agent mode (`CONFIRMATION_REQUIRED`, JSON envelope), interactive flow (menu, confirm decline keeps state, image batch, Ctrl+A-equivalent select-all, empty repos).
- `fakeLister` (in `testhelpers_test.go`) records `deletedRepos` + `deletedArtifacts` so tests can assert without setting up an httptest server.
