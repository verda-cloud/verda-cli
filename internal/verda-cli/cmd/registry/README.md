# `verda registry` -- Container Registry

Manage Verda Container Registry (VCR, `vccr.io`) credentials, browse repositories, push local Docker images, and copy images between registries. Credentials are stored separately from the main API credentials under `verda_registry_` prefixed keys in the shared profile system.

> **Pre-release.** The `registry` command tree is gated behind `VERDA_REGISTRY_ENABLED=1` and hidden from `verda --help`. Without the env var, `verda registry ...` returns "unknown command". When the feature ships GA, delete `registryEnabled()` in `internal/verda-cli/cmd/cmd.go`, drop the gate in `NewRootCommand`, and remove `Hidden: true` from `internal/verda-cli/cmd/registry/registry.go`.

## Commands

| Command | Purpose |
|---------|---------|
| `verda registry configure` | Save VCR credentials (paste `docker login` from the web UI, flags, or wizard) |
| `verda registry show` | Print credential status + expiry (no secrets) |
| `verda registry login` | Write `~/.docker/config.json` for `docker pull` / compose / helm / nerdctl |
| `verda registry ls` | List repositories visible to the active credentials |
| `verda registry tags <repo>` | List tags in a repository plus per-tag digest + size |
| `verda registry push [image...]` | Push local images (daemon / OCI layout / tarball); zero-arg launches interactive picker |
| `verda registry copy <src> [<dst>]` (alias `cp`) | Copy an image between registries |

The parent command also accepts the alias `vcr`, so `verda vcr ls` works identically to `verda registry ls`.

## Global flags (inherited)

`--profile`, `--credentials-file`, `--debug`, `--agent`, `-o json`/`yaml`, `--timeout`.

## Quick start

### 1. Create credentials in the web UI, then configure the CLI

```bash
# Paste the full docker login command the UI prints
VERDA_REGISTRY_ENABLED=1 verda registry configure \
  --paste "docker login -u vcr-<project-id>+<cred-name> -p <secret> vccr.io"
```

Or run without flags on a TTY to drive the interactive wizard, which asks for the same pasted string and then offers to also write `~/.docker/config.json`.

### 2. Verify

```bash
VERDA_REGISTRY_ENABLED=1 verda registry show
# registry_configured: true
# expires_at:          2026-05-20T00:00:00Z
# days_remaining:      30
```

### 3. Push a local image

```bash
# From the Docker daemon
VERDA_REGISTRY_ENABLED=1 verda registry push my-app:v1.0.0

# Or launch the interactive picker (no positional args) on a TTY
VERDA_REGISTRY_ENABLED=1 verda registry push
```

### 4. List what's in the registry

```bash
VERDA_REGISTRY_ENABLED=1 verda registry ls
VERDA_REGISTRY_ENABLED=1 verda registry tags my-app
```

### 5. Copy from another registry

```bash
# Copy a public image from Docker Hub to VCR, preserving repo/tag
VERDA_REGISTRY_ENABLED=1 verda registry copy docker.io/library/nginx:1.25

# Copy every tag
VERDA_REGISTRY_ENABLED=1 verda registry copy docker.io/library/nginx --all-tags

# Copy to a custom destination
VERDA_REGISTRY_ENABLED=1 verda registry copy gcr.io/my-project/app:v1 my-app:prod
```

## Configuration

Three input modes for `configure`:

```bash
# 1. Paste the docker login command (from the Verda web UI)
verda registry configure --paste "docker login -u vcr-abc+cli -p s3cret vccr.io"

# 2. Classic flag + stdin form
echo -n "$SECRET" | verda registry configure \
  --username vcr-abc+cli \
  --endpoint vccr.io \
  --password-stdin

# 3. Interactive wizard (no flags, on a TTY)
verda registry configure
```

`--expires-in <days>` overrides the 30-day default expiry. `--profile <name>` writes to a named profile section.

> **Credentials are write-once from the user's side.** Verda's API never returns the secret again — only the credential name. If the secret is lost, delete + recreate the credential in the web UI and re-run `configure`. The CLI cannot fetch the secret on your behalf.

## Login (docker config merge)

```bash
verda registry login                           # merge default profile into ~/.docker/config.json
verda registry login --profile staging         # merge a non-default profile
verda registry login --config /tmp/dc.json     # write to a non-default docker config path
```

`login` is a **local file merge** — it never talks to the registry. Existing entries for other registries and unknown top-level keys (`credsStore`, `credHelpers`, `HttpHeaders`, `psFormat`, ...) are preserved verbatim.

## Listing

```bash
verda registry ls                  # top 50 repos with tag counts
verda registry ls --limit 200      # raise the per-repo metadata cap
verda registry ls --all            # no cap
verda registry ls -o json          # structured payload

verda registry tags my-app         # tags + digest + size (default cap 50)
verda registry tags my-app --all
verda registry tags vccr.io/my-project/my-app   # fully qualified form works too
```

Both `ls` and `tags` always fetch the full catalog / tag list; `--limit` bounds only the per-row metadata HEADs that follow. Rows past the cap are still listed by name, with `--` in the metadata columns.

## Pushing

```bash
# Single image from the Docker daemon
verda registry push my-app:v1.0.0

# Multiple images (sequential in v1)
verda registry push my-app:v1 worker:v1 edge:v1

# Override destination repo / tag (single-image only)
verda registry push my-app:latest --repo team/api --tag prod

# Non-daemon sources
verda registry push --source oci ./build/image-layout
verda registry push --source tar ./out/image.tar

# Zero-arg interactive picker (TTY only)
verda registry push

# Tuning
verda registry push my-app:v1 --jobs 4 --retries 5 --progress plain
```

`--source auto` (default) picks `oci` for directories, `tar` for `*.tar` / `*.tar.gz` / `*.tgz` files, otherwise probes the Docker daemon. If the daemon is unreachable in auto mode you get a structured `registry_no_image_source` error listing the `--source oci|tar` alternatives.

## Copying

```bash
# Single ref, default destination (VCR with src repo + tag preserved)
verda registry copy docker.io/library/nginx:1.25

# Custom destination
verda registry copy gcr.io/project/app:v1 my-app:prod

# Every tag in the source repository
verda registry copy docker.io/library/nginx --all-tags

# Dry run (resolves + sizes; no writes)
verda registry copy docker.io/library/nginx --all-tags --dry-run

# Source-side basic auth (secret on stdin)
echo "$SRC_PASSWORD" | verda registry copy private.example.com/app:v1 \
  --src-auth basic --src-username jdoe --src-password-stdin

# Bypass docker-config keychain entirely (public source)
verda registry copy docker.io/library/nginx --src-auth anonymous

# Overwrite an existing destination tag without prompting
verda registry copy docker.io/library/nginx:1.25 --overwrite
verda registry copy docker.io/library/nginx:1.25 --yes        # --yes implies --overwrite

# Tuning
verda registry copy docker.io/library/nginx --all-tags \
  --jobs 4 --image-jobs 2 --progress plain
```

`--all-tags` copies with partial-success semantics: one failing tag never cancels siblings. A non-zero exit with `registry_copy_partial_failure` is returned when at least one tag failed; the structured payload carries `total`/`succeeded`/`failed`/`skipped` counts.

## Flags per subcommand

### `configure`
`--paste`, `--username`, `--password-stdin`, `--endpoint`, `--expires-in`, `--profile`, `--credentials-file`, `--docker-config`.

### `show`
`--profile`, `--credentials-file`.

### `login`
`--profile`, `--credentials-file`, `--config`.

### `ls`
`--profile`, `--credentials-file`, `--limit`, `--all`.

### `tags`
`--profile`, `--credentials-file`, `--limit`, `--all`.

### `push`
`--profile`, `--credentials-file`, `--repo`, `--tag`, `--source` (`auto|daemon|oci|tar`), `--jobs`, `--image-jobs`, `--retries`, `--progress` (`auto|plain|json|none`), `--no-mount` (currently a no-op; prints a notice).

### `copy` (alias `cp`)
`--profile`, `--credentials-file`, `--all-tags`, `--jobs`, `--image-jobs`, `--retries`, `--progress`, `--dry-run`, `--overwrite`, `--yes`, `--src-auth` (`docker-config|anonymous|basic`), `--src-username`, `--src-password-stdin`.

## Output formats

All commands honour the global output flags:

- `-o table` (default) -- human-readable
- `-o json` / `-o yaml` -- single structured payload; progress lines are suppressed so the output stays parseable
- `--agent` -- disables interactive prompts, implies structured output, and requires `--yes` / `--overwrite` for any destructive `copy`
- `--debug` -- dumps registry request/response metadata to stderr

Progress output for `push` and `copy` is controlled by `--progress`:

- `auto` (default) -- bubbletea TUI on a TTY, plain text otherwise
- `plain` -- flat-text lines to stderr
- `json` -- reserved; falls through to plain today
- `none` -- suppress progress entirely

Bubbletea output always goes to **stderr** so stdout stays clean for scripted consumers.

## Exit codes

- `0` -- success
- `1` -- runtime error (auth, network, etc.; `--agent` returns a structured `cmdutil.AgentError`)
- `130` -- canceled (Ctrl-C)
- Non-zero with error code `registry_copy_partial_failure` -- at least one tag in a `copy --all-tags` failed

## Environment

- `VERDA_REGISTRY_ENABLED=1` -- **required** to register any `registry` subcommand (pre-release gate)
- `VERDA_REGISTRY_CREDENTIALS_FILE` -- override the default credentials file path (`~/.verda/credentials`); useful in tests and CI
- `DOCKER_CONFIG` -- honoured by `verda registry login` when `--config` is not passed
- `DOCKER_HOST` -- honoured by the daemon source in `push --source daemon` and `--source auto`

## Multiple profiles

Profiles work across API, S3, and registry credentials in the same `~/.verda/credentials` file. Create a named profile with:

```bash
verda registry configure --profile staging --paste "docker login ..."
```

Switch per-command with `--profile staging` or persist it with `verda auth use staging`.

## Interactive vs Non-Interactive

- `configure` has an interactive bubbletea wizard that drives the `--paste` flow plus expiry + docker-config options. Supply `--paste` or `--username/--password-stdin/--endpoint` to skip the wizard entirely.
- `push` with zero positional args launches an interactive daemon-image picker when stderr is a TTY and `--agent` is off. Under `--agent` or a non-TTY, zero-arg push returns a structured "interactive push requires a TTY" error.
- `copy` prompts for overwrite confirmation when the destination tag already exists. Pass `--overwrite` / `--yes` to skip the prompt, or run under `--agent` to force the caller to make the decision (agent mode returns `CONFIRMATION_REQUIRED` rather than auto-confirming).
- Every other subcommand is one-shot.

## Architecture notes

Key files:

- `registry.go` -- parent command + subcommand registration
- `configure.go`, `wizard.go`, `loginparse.go`, `path.go` -- credential setup (paste / wizard / flags), docker-login parser, credentials file path resolution
- `show.go` -- status readout (near-expiry warning when < 7 days)
- `login.go` -- merge VCR credentials into `~/.docker/config.json` (pure JSON manipulation; preserves unknown keys)
- `client.go` -- `Registry` interface (narrow, mockable), `ggcrRegistry` production implementation, `newGGCRRegistryForSource` for `cp`'s source side
- `helper.go` -- `clientBuilder` / `daemonListerBuilder` / `sourceLoaderBuilder` swap points, profile fallback
- `errors.go` -- `translateError` + `translateErrorWithExpiry`: ggcr `transport.Error` + network errors mapped to `cmdutil.AgentError` with domain-specific kinds
- `expiry.go` -- `checkExpiry(creds)` pre-flight helper
- `refname.go` -- `Ref` value type, `Parse`, `Normalize`, `hasProjectNamespace` heuristic
- `retry_transport.go` -- `http.RoundTripper` with exponential backoff + `Retry-After` handling for idempotent methods
- `docker_daemon.go` -- HTTP client for the local Docker socket (no `docker` binary shell-out)
- `source.go` -- `SourceLoader` dispatcher: daemon / OCI layout / tarball, with filesystem-based auto-detect
- `push.go`, `push_view.go`, `push_picker.go`, `progress.go` -- push command + bubbletea progress view + interactive picker
- `copy.go` -- copy command: single ref, `--all-tags` worker pool, `--dry-run`, overwrite guard
- `format.go` -- byte formatting, `MM:SS`, pluralization helpers
- `ls.go`, `tags.go` -- list commands

Business logic highlights:

- **Credential resolution**: `--credentials-file` > `VERDA_REGISTRY_CREDENTIALS_FILE` > `~/.verda/credentials`; profile falls back to `default` when unset.
- **Pre-flight expiry check**: every command calls `checkExpiry(creds)` before dialing the registry. Legacy rows with zero `ExpiresAt` are treated as non-expiring; the server is authoritative.
- **Error translation**: every ggcr response error funneled through `translateError` / `translateErrorWithExpiry`. 401 against expired creds -> `registry_credential_expired`. Bare 404 (no Diagnostic body) -> `registry_tag_not_found` so `copy`'s overwrite pre-flight sees absent destinations correctly.
- **Overwrite guard (copy)**: `Head` the dst ref before each `Write`; decline-safe defaults (prompt on TTY, `CONFIRMATION_REQUIRED` in agent mode, skip on non-TTY non-agent). `--overwrite` / `--yes` bypass.
- **Partial-success semantics (`copy --all-tags`)**: raw `sync.WaitGroup` + channels (not `errgroup`) so one failing tag never cancels siblings. Summary + non-zero exit carry `registry_copy_partial_failure`.
- **Retries**: GET/HEAD/PUT/DELETE/PATCH retried on 408/429/500/502/503/504 and network timeouts. POST (blob-upload init) is never retried — a duplicate could orphan a partial upload. Across-invocation resume is free from the registry protocol (content-addressed dedup).
- **Worker pool (`copy --all-tags`)**: `--image-jobs` defaults auto-pick: `1` for < 4 tags, else `min(NumCPU/2, 4)`. User values are clamped to `min(hw, 8)`.

Wizard flow (`configure`):

1. Profile name (default `default`).
2. `docker login ...` paste string (parsed by `parseDockerLogin`).
3. Expiry window in days (default 30).
4. Whether to also write `~/.docker/config.json`.

The paste step is the only mandatory input; the others have sensible defaults and can be skipped with Enter.

## Links

- Design doc: `docs/plans/2026-04-20-container-registry-design.md`
- Implementation plan: `docs/plans/2026-04-20-container-registry-impl.md`
