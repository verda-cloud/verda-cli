# S3 Command Knowledge

## Quick Reference
- Parent: `verda s3`
- Subcommands: `configure`, `show`, `ls`, `cp`, `mv`, `rm`, `sync`, `mb`, `rb`, `presign`
- Files:
  - `s3.go` -- Parent command registration
  - `configure.go` -- Credential setup; wizard mode and flag mode
  - `show.go` -- Prints active S3 credential status (no secrets)
  - `path.go` -- Credentials file path resolution (env override + default)
  - `wizard.go` -- `configure` wizard step definitions
  - `client.go` -- `API` interface, SDK client factory, endpoint/auth-mode resolution
  - `helper.go` -- `clientBuilder` swap point + `sdkS3Client` newtype wrapping the SDK client
  - `errors.go` -- `translateError` smithy -> `cmdutil.AgentError` mapping
  - `uri.go` -- `s3://bucket/key` parser
  - `transfer.go` -- `Transporter` interface, `safeJoin`, `inferContentType` (shared by cp/mv/sync)
  - `ls.go`, `cp.go`, `mv.go`, `rm.go`, `sync.go`, `mb.go`, `rb.go`, `presign.go` -- one subcommand each

## Domain-Specific Logic

### Credential resolution order
Resolved in `client.go` `NewClient` + `resolveEndpoint`:
1. Per-invocation flag overrides: `--endpoint`, `--access-key`, `--secret-key`, `--region`
2. `~/.verda/credentials` profile, keys prefixed `verda_s3_`
3. `DefaultEndpoint` fallback for endpoint only (host/region are required via flag or profile)
4. `verda_s3_auth_mode`: `credentials` (implemented), `api` (stub -- not yet implemented)

### Profile fallback (s3-specific)
S3 commands are in `skipCredentialResolution` (see `cmd/cmd.go`), so `Options.Complete()` never runs and `AuthOptions.Profile` stays empty. `loadCredsFromFactory` in `helper.go` therefore falls back to `defaultProfileName` ("default") when `Profile == ""`. Without this, `LoadS3CredentialsForProfile(path, "")` would load ini.v1's synthetic `DEFAULT` section instead of the user's `[default]` section, and `s3 ls`/`cp`/etc. would falsely report "no S3 credentials configured" right after a successful `s3 configure`. `s3 show` applies the same fallback inline.

### Error translation
Every SDK response error is funneled through `translateError` in `errors.go`, which maps smithy codes (`NoSuchBucket`, `NoSuchKey`, `BucketAlreadyExists`, `AccessDenied`, auth errors, etc.) to project-wide `cmdutil.AgentError` kinds. Smithy imports are isolated to `errors.go`; command files never import `smithy-go` directly.

### Test client injection
Package-level `clientBuilder` in `helper.go` is swapped in tests via the `withFakeClient(API)` helper defined in `ls_test.go`. Subcommand tests use fake `API` implementations that embed the `API` interface and override only the methods under test. The same swap pattern applies to:
- `presignerBuilder` -- swapped in `presign_test.go`
- `transporterBuilder` -- swapped in `cp_test.go`, `mv_test.go`, `sync_test.go`

### Destructive-action confirmation
- `rb`, `rm`: require `prompter.Confirm()` unless `--yes`. In agent mode without `--yes`, return `cmdutil.NewConfirmationRequiredError`.
- `mv`, `cp`, `sync`: NO prompt (matches `aws s3`; the user committed by typing the verb).
- `sync --delete`: also no prompt (AWS convention -- `--delete` is opt-in already).

### Batching and pagination
- `rb --force` and `rm --recursive` use `DeleteObjects` in batches of `maxDeleteBatch = 1000` (defined in `rb.go`).
- List pagination uses the `ContinuationToken` loop. Guard against buggy/mirrored servers returning `IsTruncated=true` with an empty `NextContinuationToken`: break out of the loop and mark the result as truncated rather than spinning forever.

### Filter semantics (`--include` / `--exclude`)
Used by `cp`, `mv`, `rm`, `sync`. Shared helper `matchFilters` lives in `rm.go`:
- `filepath.Match` against the RELATIVE path (not basename, not absolute).
- `*` does NOT cross `/`. `*.txt` matches `a.txt` but not `sub/b.txt`.
- Malformed patterns are treated as non-matches rather than crashing the command.
- Exclude filters take precedence over includes when both match.

### Path traversal protection
`safeJoin(root, rel string) (string, error)` in `transfer.go` resolves both sides to absolute paths and rejects any `rel` that would escape `root`. Used by every download path (`sync` s3->local, `cp --recursive` s3->local) so that adversarial bucket keys containing `../` cannot overwrite files outside the destination.

### Output gating
Every per-file human line (`uploaded`, `downloaded`, `copied`, `moved`, `deleted`) is gated on `!isStructured(f.OutputFormat())`. Structured formats (`json`, `yaml`) emit a single complete payload at the end via `cmdutil.WriteStructured`. Never mix progress lines with JSON output.

### Content-type inference (uploads)
`inferContentType(path, override string)` in `transfer.go`:
1. If `override != ""` -> use it
2. Else `mime.TypeByExtension(filepath.Ext(path))`
3. Else `application/octet-stream`

### S3->S3 copy encoding
`CopySource` header must use per-component `url.PathEscape`:
```
url.PathEscape(bucket) + "/" + url.PathEscape(key)
```
Do NOT escape the whole `bucket/key` as a single string -- S3 rejects a pre-escaped `/` between them.

## Gotchas & Edge Cases

- `aws-sdk-go-v2/feature/s3/manager` is deprecated upstream in favour of `feature/s3/transfermanager`. `//nolint:staticcheck` suppresses the warning in `transfer.go`. Swap to `transfermanager` when it ships a tagged release.
- Integration test (`tests/integration/s3_test.go`) is gated by BOTH a build tag (`integration`) AND env var (`VERDA_S3_INTEGRATION=1`). Default `make test` never runs it.
- `NewClient` assumes a non-nil `*options.S3Credentials`. `buildClientDefault` always passes a fresh `&options.S3Credentials{}` on load failure, to honour that contract rather than panic.
- `verda s3 show` with no S3 credentials still exits 0 (prints `s3_configured: false`). This is intentional -- it is a status command, not a validation command.
- `uri.go` `Parse` accepts `s3://bucket` (empty key) as well as `s3://bucket/key/with/slashes`. Callers must decide whether an empty key is valid for their verb.
- `presign` writes the URL to stdout and the expiration hint to stderr so the output is safe to pipe (`verda s3 presign ... | pbcopy`).
- `sync --exact-timestamps` flips the mtime comparison from "newer source" to "different source"; otherwise near-identical mtimes can cause re-uploads.

## Relationships

- `cmdutil` (`internal/verda-cli/cmd/util`) -- Factory, IOStreams, `DebugJSON`, `WriteStructured`, `AgentError` helpers, `LongDesc`, `Examples`
- `options` -- `S3Credentials`, `LoadS3CredentialsForProfile`, `DefaultCredentialsFilePath`, `EnsureVerdaDir`
- `verdagostack/pkg/tui/wizard` -- only imported by `configure.go` for the credential-setup wizard
- AWS SDK v2 -- `aws`, `aws/signer/v4`, `config`, `credentials`, `feature/s3/manager`, `service/s3`, `service/s3/types`, `smithy-go`
- `charm.land/lipgloss/v2` -- destructive-action warning styles in `rb.go`, `rm.go`
