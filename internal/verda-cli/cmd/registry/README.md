# `verda registry` -- Container Registry

Manage Verda Container Registry (VCR, `vccr.io`) credentials, browse repositories, push local Docker images, and copy images between registries. Credentials are stored separately from the main API credentials under `verda_registry_` prefixed keys in the shared profile system.

> **Pre-release.** The `registry` command tree is gated behind `VERDA_REGISTRY_ENABLED=1` and hidden from `verda --help`. Without the env var, `verda registry ...` returns "unknown command". When the feature ships GA, delete `registryEnabled()` in `internal/verda-cli/cmd/cmd.go`, drop the gate in `NewRootCommand`, and remove `Hidden: true` from `internal/verda-cli/cmd/registry/registry.go`.

## Commands

| Command | Purpose |
|---------|---------|
| `verda registry configure` | Save VCR credentials (paste `docker login` from the web UI, flags, or wizard) |
| `verda registry show` | Print credential status + expiry (no secrets) |
| `verda registry login` | Write `~/.docker/config.json` for `docker pull` / compose / helm / nerdctl |
| `verda registry ls` | List repositories in the active Verda project |
| `verda registry tags <repo>` | List tags in a repository plus per-tag digest + size |
| `verda registry push [image...]` | Push local images (daemon / OCI layout / tarball); zero-arg launches interactive picker |
| `verda registry copy <src> [<dst>]` (alias `cp`) | Copy an image between registries |
| `verda registry delete [<target>]` (aliases `del`, `rm`) | Delete a repository or a single image (tag / digest); zero-arg launches interactive flow |

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

### 4. List repositories and tags

```bash
# Every repository in the active project, then pick one to see its image list
# (digest / tags / size / push / pull — the same view Harbor's web UI shows)
VERDA_REGISTRY_ENABLED=1 verda registry ls

# Scriptable (piping suppresses the picker; JSON/YAML do too)
VERDA_REGISTRY_ENABLED=1 verda registry ls | less
VERDA_REGISTRY_ENABLED=1 verda registry ls -o json

# Tags inside one repository (digest + size per tag)
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

### 6. Delete a repository or image

```bash
# Delete a single image (by tag or digest)
VERDA_REGISTRY_ENABLED=1 verda registry delete my-app:v1.2.3
VERDA_REGISTRY_ENABLED=1 verda registry delete my-app@sha256:abcdef...

# Delete an entire repository (all artifacts + tags)
VERDA_REGISTRY_ENABLED=1 verda registry delete my-app --yes

# Zero-arg: interactive picker + multi-select (Space/Ctrl+A/Enter)
VERDA_REGISTRY_ENABLED=1 verda registry delete
```

## Configuration

### Where the values come from

The web UI's "Registry credentials created" dialog shows three fields. Map them to `configure` flags:

| Web UI field | CLI flag |
|---|---|
| **Full credentials name** | `--username` |
| **Secret** | stdin (with `--password-stdin`) |
| **Registry authentication command** | `--paste` (copy the whole string verbatim) |

The third field is the **only** place the registry URL appears after you close the dialog. Pasting it is the most robust path — the host is extracted automatically. If you only kept the name and secret, `configure` defaults `--endpoint` to `vccr.io` on production; staging/custom deployments need `--endpoint` explicitly (once — subsequent rotations on the same profile reuse the saved host).

### Three input modes

```bash
# 1. (Recommended) Paste the full "Registry authentication command" from the UI
verda registry configure --paste "docker login -u vcr-abc+cli -p s3cret vccr.io"

# 2a. Production: flag + stdin; --endpoint defaults to vccr.io
echo -n "$SECRET" | verda registry configure \
  --username vcr-abc+cli \
  --password-stdin

# 2b. Staging / custom host: pass --endpoint explicitly the first time
echo -n "$SECRET" | verda registry configure \
  --username vcr-abc+cli \
  --password-stdin \
  --endpoint registry.staging.internal.datacrunch.io

# 3. Interactive wizard (no flags, on a TTY)
verda registry configure
```

When `--endpoint` is omitted, the CLI prints a one-line `Using registry endpoint "…" (<source>)` notice to stderr so non-production users immediately see whether they got the saved host or the production default.

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
# Interactive: pick a repo to see its image list (like Harbor's web UI)
verda registry ls

# Non-interactive table (pipe or redirect => no picker, deterministic output)
verda registry ls | less
verda registry ls -o json           # structured payload for scripts
verda registry ls --profile staging # use a non-default credentials profile

# Tags inside one repository
verda registry tags my-app          # tags + digest + size (default cap 50)
verda registry tags my-app --all
verda registry tags vccr.io/my-project/my-app   # fully qualified form works too
```

On a terminal, `ls` prints one row per repository and lets you pick one; selecting a row fetches that repo's per-artifact detail and renders an image-list card (**DIGEST**, **TAGS**, **SIZE**, **PUSHED**, **PULLED**) — the same view Harbor's UI shows when you expand a repository row. Pick "Exit" (or press Ctrl-C at the picker) to quit.

When `ls` is piped or redirected, or when `-o json` / `-o yaml` is set, the picker is suppressed and a single deterministic document is emitted instead. This is what scripts and CI should rely on.

### Artifacts vs. tags (what the columns mean)

- **Artifact** (`ARTIFACTS` column in `ls`, rows in the drill-down card) = a **unique manifest digest**. Uploading the same content under a second tag does not create a second artifact; re-pushing with different content under an existing tag does.
- **Tag** = a mutable label pointing to one artifact at a time (like a Git tag or branch tip). `verda registry tags <repo>` is still the right command when you want a tag-centric view.

So an `ARTIFACTS` count of `1` with two tags means both tags point at the same pushed content; a count of `2` with two tags means the tags point at different content.

### How listing talks to Harbor

`ls` uses Harbor's REST API (`/api/v2.0/projects`, `/api/v2.0/repositories`, and `/api/v2.0/projects/{project}/repositories/{repo}/artifacts`), **not** the Docker Registry v2 catalog endpoint — `_catalog` is admin-only on Harbor and inaccessible to the project-scoped robot accounts VCR issues. Repository names are shown with the project prefix stripped (e.g. `library/hello-world`, not `<project-uuid>/library/hello-world`); the full name is preserved in the JSON/YAML output under `full_name` for scripts that need it.

`tags` always fetches the full tag list; `--limit` bounds only the per-row metadata HEADs that follow. Rows past the cap are still listed by name, with `--` in the metadata columns.

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

### Source authentication (private images)

`copy` has **two independent auth chains**:

```
[source registry] --(source auth)--> [your local CLI] --(VCR creds)--> [VCR]
```

The destination side always uses the Verda credentials from `~/.verda/credentials` (whatever `vccr configure` wrote). The **source** side is configurable via `--src-auth`:

| `--src-auth` value | Behavior | When to use |
|---|---|---|
| `docker-config` (default) | `authn.DefaultKeychain`: reads `~/.docker/config.json`, honors `credsStore` / `credHelpers`, falls back to anonymous | You already ran `docker login` (or a cloud-CLI equivalent) for the source host |
| `anonymous` | Skips the keychain entirely; sends no Authorization header | Public source, or you want to bypass a stale / wrong docker-config entry |
| `basic` | Sends Basic auth using `--src-username` and the secret read from stdin | Automation / CI with no persistent docker-config; one-off credentials you don't want on disk |

**Rule of thumb: if `docker pull <src>` works, `vccr copy <src>` will read it** — the default keychain is identical.

#### 1. You already ran `docker login` for the source

No extra flags — this is the default path:

```bash
echo "$GITHUB_PAT" | docker login ghcr.io -u USERNAME --password-stdin

VERDA_REGISTRY_ENABLED=1 verda registry copy \
  ghcr.io/acme/private-app:v1 \
  acme/private-app:v1
```

#### 2. Inline basic auth (CI-friendly, no docker-config mutation)

The secret **must** come via stdin; passing it as a flag is not supported:

```bash
echo "$SRC_PASSWORD" | VERDA_REGISTRY_ENABLED=1 verda registry copy \
  private.example.com/team/app:v1 \
  --src-auth basic \
  --src-username jdoe \
  --src-password-stdin
```

Works against any registry accepting Basic auth: Harbor, self-hosted `distribution/distribution`, GitLab Container Registry (`$CI_REGISTRY_PASSWORD`), GitHub Container Registry (PAT with `read:packages`), Quay robot tokens, JFrog, Docker Hub with a PAT, etc.

#### 3. Cloud registries with short-lived tokens

Cloud registries mint ephemeral tokens; use the cloud CLI once to seed docker-config, then use path #1:

```bash
# AWS ECR
aws ecr get-login-password --region us-east-1 | docker login \
  --username AWS --password-stdin 123456789012.dkr.ecr.us-east-1.amazonaws.com
verda registry copy 123456789012.dkr.ecr.us-east-1.amazonaws.com/team/app:v1 acme/app:v1

# GCP Artifact Registry / GCR
gcloud auth configure-docker us-docker.pkg.dev   # one-time
verda registry copy us-docker.pkg.dev/my-project/repo/app:v1 acme/app:v1

# Azure Container Registry
az acr login --name myregistry                   # token into docker-config
verda registry copy myregistry.azurecr.io/team/app:v1 acme/app:v1
```

All three are `--src-auth docker-config` under the hood — the cloud CLI is just writing to `~/.docker/config.json` for you.

#### Debugging a private-source copy

```bash
# Sanity-check: can docker pull the same ref?
docker pull ghcr.io/acme/private-app:v1

# Inspect the keychain
cat ~/.docker/config.json

# Force anonymous to isolate keychain issues from actual access
verda registry copy ghcr.io/acme/private-app:v1 --src-auth anonymous --dry-run

# Request/response metadata on stderr
verda registry copy ... --debug
```

Diagnosis map:

| docker pull works? | `--src-auth anonymous` works? | Likely cause |
|---|---|---|
| ✅ | ✅ | Source is actually public |
| ✅ | ❌ 401 | Source is private; use default `--src-auth docker-config` |
| ❌ | — | Your docker-config / keychain isn't set up for this registry yet |

#### Caveats

- Private sources behind OIDC/SSO need the corresponding cloud/tooling CLI to mint tokens first. `copy` doesn't drive those flows — it just consumes what the keychain returns.
- Source-side rate limits are **not** ours. Docker Hub anonymous = 100 pulls / 6h / IP; authenticate (`docker login docker.io`) to get the 200/6h tier.
- `--src-auth basic` credentials stay in-process — the CLI never writes them to `~/.docker/config.json` or any other file. `--debug` may still print HTTP metadata that includes an `Authorization` header; avoid `--debug` on shared terminals or log-capturing CI when using `basic`.
- Destination is always VCR (the robot account from `vccr configure`); `--src-auth` only governs the pull leg.

## Deleting

```bash
# Delete a whole repository (all artifacts and tags, irreversible)
verda registry delete my-app
verda registry delete library/hello-world

# Delete one image by tag
verda registry delete my-app:v1.2.3

# Delete one image by digest
verda registry delete my-app@sha256:abcdef...

# Fully-qualified refs work too (as long as the project matches the active credentials)
verda registry delete vccr.io/<project>/my-app:v1

# Skip the confirmation prompt
verda registry delete my-app:v1 --yes
verda registry delete my-app --yes          # also skips the repo-delete "⚠" dialog

# Zero-arg interactive flow on a TTY
#  1. Pick a repository from the list
#  2. Choose "Delete image(s)…" or "Delete repository …"
#  3. For images: multi-select with Space, Ctrl+A selects all, Enter confirms
verda registry delete

# Aliases: del, rm
verda registry del my-app:v1
verda registry rm  my-app:v1 --yes
```

### Confirmation contract

`delete` is destructive and prompts before every operation unless `--yes` / `-y` is passed:

- **Interactive TTY (default)** — renders a Harbor-style warning dialog:
  - For repositories: `⚠ Delete image repository` + an info line with the artifact count (best-effort) + "cannot be undone".
  - For images: `⚠ Delete image` + a DIGEST / TAG / SIZE row.
  - Declining keeps you in the interactive menu; accepting triggers the delete.
- **Non-TTY without `--yes`** — the command refuses and asks for `--yes`. Scripts must be explicit.
- **Agent mode (`--agent`)** — `--yes` is **mandatory**. Without it the command emits a structured `CONFIRMATION_REQUIRED` error (exit code `ExitBadArgs`) so the caller can surface the prompt to its user before retrying.

### What actually gets deleted

- `delete <repo>` removes every artifact and every tag in the repository in one server-side call. Re-running returns a `registry_repo_not_found` error (idempotent from the user's perspective).
- `delete <repo>:<tag>` and `delete <repo>@<digest>` delete the underlying **artifact** (manifest + layers that are no longer referenced). Every tag pointing at that manifest is removed in the process — Harbor does not offer a tag-scoped "unlink just this one tag" endpoint. If the repository ends up with zero remaining artifacts, Harbor will keep the empty repository row; you can follow up with `delete <repo>` to drop it entirely.
- Cross-project targets (a ref whose project segment does not match the active credentials) are rejected locally with `registry_invalid_reference` — rather than letting Harbor return an opaque 403 later.

### Policy-blocked deletes

If Harbor rejects the request because of a **Tag Immutability** or **Tag Retention** rule, the CLI surfaces a dedicated `registry_delete_blocked` error (HTTP 412 from Harbor). The message quotes Harbor's reason and explains the two recovery paths:

1. Open the project in the Verda web UI, review the matching Tag Immutability / Tag Retention rule, and adjust or remove it.
2. Retry `verda registry delete`, or contact Verda support (`support@verda.cloud`) if the rule is required and the artifact still needs to go.

### Structured output (agent mode)

`delete` emits a single JSON / YAML document per invocation:

```json
{
  "action": "delete_repository",
  "repository": "library/hello-world",
  "deleted_artifacts": 3,
  "status": "completed"
}
```

```json
{
  "action": "delete_artifact",
  "repository": "library/hello-world",
  "reference": "v1.2.3",
  "digest": "sha256:…",
  "removed_tags": ["v1.2.3", "v1"],
  "status": "completed"
}
```

The artifact-count / digest / removed-tags fields come from a best-effort pre-delete `ListArtifacts` probe — if that probe fails, the delete still proceeds and the fields are omitted rather than set to zero or `null`. Scripts should treat them as optional metadata.

## Flags per subcommand

### `configure`
`--paste`, `--username`, `--password-stdin`, `--endpoint` (optional — defaults to the profile's saved host or `vccr.io`), `--expires-in`, `--profile`, `--credentials-file`, `--docker-config`.

### `show`
`--profile`, `--credentials-file`.

### `login`
`--profile`, `--credentials-file`, `--config`.

### `ls`
`--profile`, `--credentials-file`.

### `tags`
`--profile`, `--credentials-file`, `--limit`, `--all`.

### `push`
`--profile`, `--credentials-file`, `--repo`, `--tag`, `--source` (`auto|daemon|oci|tar`), `--jobs`, `--image-jobs`, `--retries`, `--progress` (`auto|plain|json|none`), `--no-mount` (currently a no-op; prints a notice).

### `copy` (alias `cp`)
`--profile`, `--credentials-file`, `--all-tags`, `--jobs`, `--image-jobs`, `--retries`, `--progress`, `--dry-run`, `--overwrite`, `--yes`, `--src-auth` (`docker-config|anonymous|basic`), `--src-username`, `--src-password-stdin`.

### `delete` (aliases `del`, `rm`)
`--profile`, `--credentials-file`, `--yes` / `-y`.

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
- Non-zero with error code `registry_delete_blocked` -- Harbor rejected the delete because of a Tag Immutability / Tag Retention rule (HTTP 412)
- Non-zero with error code `CONFIRMATION_REQUIRED` -- `delete` invoked under `--agent` without `--yes` (exit `ExitBadArgs`)

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
- `delete` with zero positional args on a TTY launches the interactive repo picker + sub-menu + multi-select image flow (Space to toggle, Ctrl+A to select all, Enter to confirm). With a positional target, it shows a one-shot confirmation dialog; `--yes` skips it. Under `--agent`, `--yes` is mandatory — missing it returns `CONFIRMATION_REQUIRED`.
- Every other subcommand is one-shot.

## Architecture notes

Key files:

- `registry.go` -- parent command + subcommand registration
- `configure.go`, `wizard.go`, `loginparse.go`, `path.go` -- credential setup (paste / wizard / flags), docker-login parser, credentials file path resolution
- `show.go` -- status readout (near-expiry warning when < 7 days)
- `login.go` -- merge VCR credentials into `~/.docker/config.json` (pure JSON manipulation; preserves unknown keys)
- `client.go` -- `Registry` interface (narrow, mockable, Docker Registry v2 shaped), `ggcrRegistry` production implementation, `newGGCRRegistryForSource` for `cp`'s source side
- `harbor.go` -- `RepositoryLister` interface + `harborClient`: Harbor REST v2.0 (`/api/v2.0/projects`, `/api/v2.0/repositories`, repo + artifact DELETE endpoints). Separate from `Registry` because Harbor REST and Docker Registry v2 are different surfaces
- `ls.go` -- repository listing using `harborClient`
- `delete.go` -- repository / artifact deletion using `harborClient`: positional targets (`repo` / `repo:tag` / `repo@digest`), TTY confirmation dialogs, zero-arg interactive flow with multi-select + select-all, agent-mode `CONFIRMATION_REQUIRED` contract
- `helper.go` -- `clientBuilder` / `daemonListerBuilder` / `sourceLoaderBuilder` / `harborListerBuilder` swap points, profile fallback
- `errors.go` -- `translateError` + `translateErrorWithExpiry`: ggcr `transport.Error` + network errors mapped to `cmdutil.AgentError` with domain-specific kinds
- `expiry.go` -- `checkExpiry(creds)` pre-flight helper
- `refname.go` -- `Ref` value type, `Parse`, `Normalize`, `hasProjectNamespace` heuristic
- `retry_transport.go` -- `http.RoundTripper` with exponential backoff + `Retry-After` handling for idempotent methods
- `docker_daemon.go` -- HTTP client for the local Docker socket (no `docker` binary shell-out)
- `source.go` -- `SourceLoader` dispatcher: daemon / OCI layout / tarball, with filesystem-based auto-detect
- `push.go`, `push_view.go`, `push_picker.go`, `progress.go` -- push command + bubbletea progress view + interactive picker
- `copy.go` -- copy command: single ref, `--all-tags` worker pool, `--dry-run`, overwrite guard
- `format.go` -- byte formatting, `MM:SS`, pluralization helpers, `isStructuredFormat`
- `tags.go` -- list tags in a repository (per-tag digest + size)

Business logic highlights:

- **Credential resolution**: `--credentials-file` > `VERDA_REGISTRY_CREDENTIALS_FILE` > `~/.verda/credentials`; profile falls back to `default` when unset.
- **Pre-flight expiry check**: every command calls `checkExpiry(creds)` before dialing the registry. Legacy rows with zero `ExpiresAt` are treated as non-expiring; the server is authoritative.
- **Error translation**: every ggcr response error funneled through `translateError` / `translateErrorWithExpiry`. 401 against expired creds -> `registry_credential_expired`. Bare 404 (no Diagnostic body) -> `registry_tag_not_found` so `copy`'s overwrite pre-flight sees absent destinations correctly.
- **Overwrite guard (copy)**: `Head` the dst ref before each `Write`; decline-safe defaults (prompt on TTY, `CONFIRMATION_REQUIRED` in agent mode, skip on non-TTY non-agent). `--overwrite` / `--yes` bypass.
- **Partial-success semantics (`copy --all-tags`)**: raw `sync.WaitGroup` + channels (not `errgroup`) so one failing tag never cancels siblings. Summary + non-zero exit carry `registry_copy_partial_failure`.
- **Retries**: GET/HEAD/PUT/DELETE/PATCH retried on 408/429/500/502/503/504 and network timeouts. POST (blob-upload init) is never retried — a duplicate could orphan a partial upload. Across-invocation resume is free from the registry protocol (content-addressed dedup).
- **Worker pool (`copy --all-tags`)**: `--image-jobs` defaults auto-pick: `1` for < 4 tags, else `min(NumCPU/2, 4)`. User values are clamped to `min(hw, 8)`.
- **Listing via Harbor REST, not `_catalog`**: `/v2/_catalog` is admin-only on Harbor and returns 401 for the project-scoped robot accounts VCR issues. `ls` therefore talks to Harbor's REST API (`/api/v2.0/projects`, `/api/v2.0/repositories`, and `/api/v2.0/projects/{project}/repositories/{repo}/artifacts`) using the rich-query filter `q=project_id=N` for the repo page — the plain `?project_id=N` parameter is silently ignored by Harbor and must not be used. `ListRepositories` also applies a client-side `r.ProjectID == projectID` filter as belt-and-suspenders. `ListArtifacts` percent-encodes the repository path segment (`url.PathEscape`) so names containing `/` route correctly. See `harbor.go` for the full implementation and the "List Repositories / artifacts" section in the package CLAUDE.md for the permission history.
- **Interactive `ls` drill-down**: on a TTY, `ls` routes through `f.Prompter().Select` to let the user pick a repository, then calls `RepositoryLister.ListArtifacts` and renders a per-artifact card (digest / tags / size / push / pull). Non-TTY output is always the flat repo table; structured output (`-o json|yaml`) never enters the picker. TTY detection is swappable via `isTerminalFn` (shared with `copy` / `push_view.go`), which also lets tests exercise the picker path without a real terminal.
- **Delete target classification**: `classifyTarget` inspects the raw positional argument's last path segment for `@` or `:` to distinguish bare repositories from artifact references, *before* calling `Normalize`. `Normalize` defaults the tag to `"latest"` for push/copy semantics — reusing that default for delete would silently convert `delete library/hello-world` into `delete library/hello-world:latest`, which is the wrong intent. Cross-project targets are rejected locally with `registry_invalid_reference` so the user gets a useful message instead of a 403 from Harbor.
- **Policy-blocked deletes (HTTP 412)**: Harbor returns 412 when a project Tag Immutability / Tag Retention rule forbids the operation. `translateHarborError` maps that to `registry_delete_blocked` with a recovery message walking the user through editing the policy in the web UI or escalating to support; the Harbor response body (usually `"matched rule X"`) is folded into the message verbatim.
- **Interactive delete flow**: on a TTY with no positional arg, `delete` drives a two-level menu — outer picker over repositories (same `formatRepoRow` as `ls`), inner menu per repo (delete image(s) / delete repository / back / exit). The image-delete step uses `prompter.MultiSelect`, which natively supports Ctrl+A "select all" (see `verdagostack/pkg/tui/bubbletea/multiselect.go`); the prompt label advertises the keystroke. Batches run sequentially with partial-success reporting — one failing artifact never cancels siblings.

Wizard flow (`configure`):

1. Profile name (default `default`).
2. `docker login ...` paste string (parsed by `parseDockerLogin`).
3. Expiry window in days (default 30).
4. Whether to also write `~/.docker/config.json`.

The paste step is the only mandatory input; the others have sensible defaults and can be skipped with Enter.

## Links

- Design doc: `docs/plans/2026-04-20-container-registry-design.md`
- Implementation plan: `docs/plans/2026-04-20-container-registry-impl.md`
