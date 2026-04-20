# verda s3 -- S3-compatible object storage

AWS-CLI-style object storage commands for Verda's S3-compatible endpoint. Uses a separate credential set (keys prefixed `verda_s3_`) so object-storage access is independent of the main API credentials while still sharing the profile system.

> **Pre-release.** The `s3` command tree is gated behind `VERDA_S3_ENABLED=1` and hidden from `verda --help`. Without the env var, `verda s3 ...` returns "unknown command". When the feature ships GA, drop the gate in `internal/verda-cli/cmd/cmd.go` (`s3Enabled` + the `if`) and remove `Hidden: true` from `internal/verda-cli/cmd/s3/s3.go`.

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

Interactive wizard (prompts for access key, secret key, endpoint, region):
```bash
verda s3 configure
```

Non-interactive:
```bash
verda s3 configure \
  --access-key AKIA... \
  --secret-key ... \
  --endpoint https://objects.lab.verda.storage
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
```

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

Only `configure` has an interactive wizard. Every other subcommand is one-shot: it takes positional URIs + flags and either succeeds or returns a structured error.

- **`configure` wizard**: triggers when any of `--access-key`, `--secret-key`, `--endpoint` is missing. Supply all three (plus optionally `--profile`, `--region`, `--credentials-file`) to skip the wizard entirely.
- **Destructive prompts** (`rb`, `rm`): an interactive `prompter.Confirm()` warns before deletion unless `--yes` is passed. `cp`, `mv`, `sync`, `sync --delete` do not prompt (AWS convention — the verb itself is the commitment).
- **Agent mode** (`--agent`): disables every interactive prompt, implies `--output json`, and requires `--yes` for any destructive operation. Without `--yes`, destructive subcommands return `cmdutil.AgentError{Code: "CONFIRMATION_REQUIRED"}` so calling agents know exactly what to add.

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

1. Profile name (default `default`)
2. S3 access key ID (masked)
3. S3 secret access key (password prompt)
4. S3 endpoint URL (must start with `http://` or `https://`)
5. S3 region (default `us-east-1`)

All five steps are skipped individually when the corresponding flag is already set — so `verda s3 configure --access-key X --endpoint Y` only prompts for the secret and region.
