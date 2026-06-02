# verda s3 -- S3-compatible object storage

AWS-CLI-style object storage commands for Verda's S3-compatible endpoint. Uses a separate credential set (keys prefixed `verda_s3_`) so object-storage access is independent of the main API credentials while still sharing the profile system.

## Quick reference

| Command | Description |
|---------|-------------|
| `verda s3 configure` | Set up S3 credentials (wizard or flags) |
| `verda s3 show` | Print active S3 credential status (no secrets) |
| `verda s3 ls` | List buckets, or list keys under a prefix |
| `verda s3 cp` | Copy between local and S3, or S3 and S3 |
| `verda s3 mv` | Move (copy + delete source) |
| `verda s3 rm` | Delete one or many keys |
| `verda s3 sync` | Sync a directory and a prefix both directions |
| `verda s3 mb` | Make bucket |
| `verda s3 rb` | Remove bucket (optionally force-empty first) |
| `verda s3 presign` | Generate a time-limited GET URL for a key |

## Configuration

First create an access key in the Verda dashboard: **log in → select your
project → Project management → Credentials → Object Storage Access Keys.**

Interactive wizard — the endpoint and region come pre-filled with defaults
(`https://objects.fin-03.verda.storage`, `us-east-1`), so you normally just pick
a profile and paste the access key + secret:
```bash
verda s3 configure
```

Non-interactive (endpoint defaults if omitted; pass `--endpoint` for another region):
```bash
verda s3 configure \
  --access-key AKIA... \
  --secret-key ...
```

Show the active configuration (no secrets are printed):
```bash
verda s3 show
verda s3 show --profile staging
```

## Listing

```bash
verda s3 ls                              # list all buckets
verda s3 ls s3://my-bucket               # top-level keys + prefixes (delimiter /)
verda s3 ls s3://my-bucket --recursive   # every key under the bucket
verda s3 ls s3://my-bucket --human-readable --summarize
```

## Copying

```bash
# upload
verda s3 cp ./local-file s3://my-bucket/key.txt

# download
verda s3 cp s3://my-bucket/key.txt ./local-file

# server-side copy (S3 -> S3, no data traverses the client)
verda s3 cp s3://src-bucket/key s3://dst-bucket/key

# recursive directory upload
verda s3 cp ./dir s3://my-bucket/prefix/ --recursive

# with include/exclude filters (match against relative path, * does not cross /)
verda s3 cp ./dir s3://my-bucket/prefix/ --recursive \
  --include '*.go' --exclude '*_test.go'

# override content-type (otherwise inferred from extension)
verda s3 cp ./file s3://my-bucket/key --content-type 'application/json'

# preview what would happen
verda s3 cp ./dir s3://my-bucket/prefix/ --recursive --dryrun

# tune throughput for large transfers (uploads and single-object downloads)
verda s3 cp ./big.bin s3://my-bucket/big.bin --concurrency 16 --part-size 32MiB
```

### Resumable large transfers

Single-file uploads and single-object downloads larger than the part size are
multipart, parallel (5 concurrent parts by default), and **resumable**. If a
transfer is interrupted (network drop, Ctrl+C, crash), **re-run the exact same
command** and it continues — only the missing parts are sent/fetched:

```bash
# upload; if it breaks, run the SAME command again to resume
verda s3 cp ./model.safetensors s3://my-bucket/models/model.safetensors

# download; re-run to resume (a partial <dest>.part is kept until it completes)
verda s3 cp s3://my-bucket/models/model.safetensors ./model.safetensors

# force a fresh transfer, ignoring any saved progress
verda s3 cp s3://my-bucket/models/model.safetensors ./model.safetensors --no-resume
```

Resume reuses the **same part size** that the interrupted run used. Passing a
different `--part-size` on the resume (or changing the file) is detected and the
transfer restarts cleanly rather than mixing incompatible part boundaries. Part
sizes accept binary (`MiB`, `GiB`) and the loose `MB`/`M` forms — all treated as
binary (`1MB` = 1048576 bytes).

How it works:

- Resume state lives locally: uploads under `~/.verda/s3-uploads/` (+ the
  server-side multipart parts); downloads under `~/.verda/s3-downloads/` (+ a
  `<dest>.part` file). The key is a hash of the **source path + destination**, so
  resume requires re-running with the same source and destination.
- Uploads reconcile against the server (`ListParts`); downloads guard against the
  object changing with an `If-Match` ETag check (a changed object restarts cleanly).
- A same-host lock prevents two transfers of the same object running at once.
- For incomplete **uploads** specifically, `verda s3 ls-uploads` lists them and
  lets you pick one to resume; the staged parts cost storage until completed or
  aborted (`verda s3 abort-uploads`).
- Interactive downloads from the `verda s3 ls` browser (per-object **Download**
  or the **Download files here…** multi-select) use the same resumable path —
  re-selecting Download on an interrupted object resumes from its `.part`. They
  save to your **Downloads folder** (`~/Downloads`, created if missing; falls
  back to the current directory if the home dir can't be resolved) and pause on a
  *Back to list / Exit* summary so the result stays on screen. `cp` keeps writing
  to the explicit destination you pass.
  - **No silent overwrites:** if a file of the same name already exists locally,
    the download is saved as `name-2.ext`, `name-3.ext`, … instead of clobbering
    it. A genuine resume of the *same* object keeps its original name (so its
    `.part` is continued). Multi-select is scoped to a single folder, so a batch
    never spans folders.
- Recursive (`--recursive`), `sync`, and `mv` transfers are not yet resumable
  per-file.

## Moving

Same flag surface as `cp`; source is removed on success.
```bash
verda s3 mv ./tmpfile s3://my-bucket/final-name
verda s3 mv s3://my-bucket/old-key s3://my-bucket/new-key
```

## Syncing

```bash
# local -> S3
verda s3 sync ./local-dir s3://my-bucket/prefix/

# S3 -> local
verda s3 sync s3://my-bucket/prefix/ ./local-dir

# remove destination files that don't exist in source (AWS-convention --delete)
verda s3 sync ./a s3://bucket/ --delete

# treat any mtime difference as "changed" (not just newer-source)
verda s3 sync ./a s3://bucket/ --exact-timestamps --dryrun
```

## Removing

```bash
verda s3 rm s3://bucket/key
verda s3 rm s3://bucket/prefix/ --recursive
verda s3 rm s3://bucket/prefix/ --recursive --include '*.log' --yes
verda s3 rm s3://bucket/prefix/ --recursive --dryrun
```

Destructive; prompts for confirmation unless `--yes` is passed. In agent mode, `--yes` is mandatory.

## Buckets

```bash
verda s3 mb s3://new-bucket-name

verda s3 rb s3://old-bucket              # only works if empty
verda s3 rb s3://old-bucket --force      # empty the bucket first, then remove
```

`rb` prompts for confirmation unless `--yes`; `--force` implies a recursive delete so be sure.

## Presigned URLs

```bash
verda s3 presign s3://bucket/key                    # default 1h
verda s3 presign s3://bucket/key --expires-in 15m
verda s3 presign s3://bucket/key --expires-in 24h
```

The URL is printed to stdout (pipe-friendly); the expiration hint goes to stderr, e.g.:
```bash
verda s3 presign s3://bucket/key --expires-in 30m | pbcopy
```

## Output formats

All commands honour the global output flags:

- `--output table` (default)
- `--output json` / `--output yaml` -- single structured payload at the end
- `--agent` -- disables interactive prompts, implies JSON, and requires `--yes` for destructive operations
- `--debug` -- dumps SDK request/response metadata to stderr

Per-file progress lines (`uploaded`, `downloaded`, `copied`, `moved`, `deleted`) are only emitted when the format is `table`. Structured output produces exactly one payload so it stays parseable.

## Environment

- `VERDA_SHARED_CREDENTIALS_FILE` -- override the default credentials path (`~/.verda/credentials`)

## Multiple profiles

Profiles work across both API and S3 credentials. Create a second profile via:
```bash
verda s3 configure --profile staging
```
Switch with `--profile staging` on any command, or persist it with `verda auth use staging`.

## Interactive vs Non-Interactive

Every command works two ways: **non-interactively** with positional URIs + flags
(scripts, `--agent`, pipes), and **interactively** with a TUI when you omit the
target on a terminal. The interactive path triggers only when stdout is a TTY,
not in `--agent` mode, and the output format is the default `table`; otherwise an
omitted target returns the command help (or a structured error in `--agent`).

| Command | Interactive trigger (on a TTY) | Flow |
|---------|--------------------------------|------|
| `configure` | any of `--access-key`/`--secret-key`/`--endpoint` missing | credential wizard |
| `ls` | no argument | folder browser (drill in, per-object actions, multi-download) |
| `cp` | no destination (and not a bare `s3://` download) | upload wizard (source → bucket → folder → confirm) |
| `mb` | no argument | prompts for the new bucket name |
| `rb` | no argument | bucket picker, then the destructive confirm |
| `rm` | no argument | folder browser; tick files at a level to delete (red confirm + preview) |
| `mv` | no args, or a single `s3://` source | S3→S3 move/rename wizard (source → dest bucket → dest key → confirm) |

Notes:

- **`configure` wizard**: triggers when `--access-key` or `--secret-key` is missing. Endpoint and region default (so `configure --access-key X --secret-key Y` is fully non-interactive); pass `--endpoint`/`--region`/`--profile`/`--credentials-file` to override.
- **Destructive prompts** (`rb`, `rm`): an interactive `prompter.Confirm()` with a red warning + preview runs before deletion unless `--yes` is passed — in both the flag and TUI paths. `cp`, `mv`, `sync`, `sync --delete` do not prompt (AWS convention — the verb itself is the commitment), though the interactive `mv` wizard adds a final confirm.
- **`mv` interactive scope**: the wizard covers S3→S3 moves/renames only; local↔S3 moves still require both explicit arguments (a local path can't be picked in the TUI).
- **`rm` interactive scope**: multi-select is scoped to one folder level; drill into subfolders to delete within them, or use `rm <prefix> --recursive` for bulk deletes across a whole prefix.
- **Navigation**: `Esc` steps back (ascends a folder / returns to the previous wizard step), `Ctrl+C` exits immediately — never a confirmation dialog on either.
- **Agent mode** (`--agent`): disables every interactive prompt, implies `--output json`, and requires `--yes` for any destructive operation. Without `--yes`, destructive subcommands return `cmdutil.AgentError{Code: "CONFIRMATION_REQUIRED"}` so calling agents know exactly what to add. With no target, bucket/object-targeting commands return a `MISSING_REQUIRED_FLAGS` error rather than prompting.

## Architecture Notes

Key files:

- `s3.go` — parent command registration
- `configure.go`, `wizard.go`, `path.go` — credential setup wizard + flag mode + credentials file path resolution
- `show.go` — credential status readout (no secrets)
- `client.go` — `API` interface (mockable SDK subset), `NewClient` factory, `resolveEndpoint`, `validateAuthMode`
- `helper.go` — `clientBuilder` swap point for tests, `sdkS3Client` newtype wrapping `*s3.Client` into the `API` interface
- `errors.go` — `translateError` mapping smithy error codes to `cmdutil.AgentError`
- `uri.go` — `s3://bucket/key` parser (`Parse`, `URI`, `IsS3URI`)
- `transfer.go` — `Transporter` interface (upload + download), `Copier`, `safeJoin` (path-traversal guard), `inferContentType`
- `ls.go`, `cp.go`, `mv.go`, `rm.go`, `sync.go`, `mb.go`, `rb.go`, `presign.go` — one subcommand each

SDK usage (AWS SDK v2):

- `ListBuckets`, `ListObjectsV2`, `HeadBucket`, `HeadObject`, `GetObject`, `PutObject`, `DeleteObject`, `DeleteObjects`, `CreateBucket`, `DeleteBucket`, `CopyObject` — through the `API` interface
- `feature/s3/manager.Uploader/Downloader` — multipart upload/download (wrapped by `Transporter`)
- `s3.NewPresignClient` — presigned GET URLs (wrapped by `Presigner`)

Business logic:

- **Credential resolution**: per-invocation flags > profile keys (`verda_s3_*`) > `DefaultEndpoint` fallback for endpoint
- **Filter matching**: `filepath.Match` against the relative path (not basename). `*` does not cross `/`. Shared `matchFilters` lives in `rm.go`.
- **Batching**: `DeleteObjects` in groups of 1000 (`maxDeleteBatch` in `rb.go`) for `rm --recursive` and `rb --force`.
- **Pagination**: `ContinuationToken` loop for every `ListObjectsV2`, with a defensive break if a server returns `IsTruncated=true` but an empty `NextContinuationToken`.
- **CopySource encoding**: per-component `url.PathEscape(bucket) + "/" + url.PathEscape(key)` — never escape the whole thing as one string.
- **Sync comparison**: default is "src size differs OR src newer"; `--exact-timestamps` flips to "src size differs OR any mtime difference".
- **Path traversal guard**: `safeJoin` on every s3-to-local download, so adversarial keys containing `../` cannot escape the destination directory.

Wizard flow (`configure`):

1. Profile — **pick an existing profile** (each tagged "S3 configured" / "no S3 credentials yet") **or "+ Create new profile…"**. The selection defaults to the active profile (`--profile` / `VERDA_PROFILE` / `verda auth use`).
2. New profile name — only when "Create new" was chosen.
3. S3 access key ID
4. S3 secret access key (password prompt)
5. S3 endpoint URL (pre-filled with `https://objects.fin-03.verda.storage`; must start with `http://`/`https://`)
6. S3 region (default `us-east-1`)

Steps are skipped individually when the corresponding flag is already set — so `verda s3 configure --access-key X --endpoint Y` only prompts for the secret and region, and `--profile staging` skips the profile picker entirely (targeting `[staging]`).

> Note: `configure` writes to the profile you pick/name here; it does **not** auto-follow the active profile the way the read commands (`ls`/`cp`/…) do. The picker defaulting to the active profile keeps the two aligned, but if you create credentials for a non-active profile, pass `--profile` to the read commands or `verda auth use <name>` to switch.
