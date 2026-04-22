# Registry Command Knowledge

## Quick Reference

- Parent: `verda registry` (aliases: `vccr` canonical, `vcr` legacy)
- Subcommands: `configure`, `show`, `login`, `ls`, `tags`, `push`, `copy` (alias `cp`)
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
  - `format.go` -- `formatBytes`, `formatMMSS`, `pluralS`, small string helpers shared by `tags`/`push`/`copy`.
  - `ls.go` -- List repos + per-repo tag count via `_catalog` + `Tags`. `isStructuredFormat` lives here.
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

### Expiry Handling

- `RegistryCredentials.ExpiresAt` defaults to `now + 30 days` at configure time. `--expires-in <days>` overrides.
- `checkExpiry(creds)` (`expiry.go`) is the pre-flight helper. **Every command RunE calls it BEFORE dialing the registry** (ls, tags, push, copy, login). Skipping it means the user gets a generic 401 from ggcr instead of an actionable `registry_credential_expired`.
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

**Audit before adding new files.** Command files (`ls.go`, `tags.go`, `configure.go`, `show.go`, `login.go`, `push_picker.go`, etc.) never import ggcr directly — they go through the `Registry` interface and the `Ref` value type.

### Swap Points (for Test Injection)

- `clientBuilder` (`helper.go`) -- swap with a fake `Registry` backed by an in-process test server.
- `daemonListerBuilder` (`helper.go`) -- swap with a fake `DaemonLister` to avoid touching the host's docker socket.
- `sourceLoaderBuilder` (`helper.go`) -- swap with a fake `SourceLoader`.
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
