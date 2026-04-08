# Update Command Knowledge

## Quick Reference
- Parent: `verda update` (no aliases)
- Subcommands: none (single command with `--list` and `--target` flags)
- Files:
  - `update.go` -- All logic: command def, GitHub API, archive extraction, binary replacement

## Domain-Specific Logic

### Version Resolution
- Current version from `version.Get().GitVersion` (from `verdagostack/pkg/version`)
- Auto-prepends `v` prefix if missing from `--target` flag
- Skips update if target == current

### Asset Matching
- Asset name: `verda_{versionWithoutV}_{runtime.GOOS}_{runtime.GOARCH}.{ext}`
- ext = `tar.gz` (non-Windows) or `zip` (Windows)
- Binary name in archive: `verda` or `verda.exe`

### Binary Replacement
- Atomic rename strategy: temp file in same dir -> rename
- Windows special case: `os.Remove(dst)` before rename
- Symlink-aware: uses `filepath.EvalSymlinks` on `os.Executable()`

## Gotchas & Edge Cases
- No authentication for GitHub API calls -- will hit rate limits (60 req/hr for unauthenticated)
- `httpTimeout` is 60s, separate from the Factory `Options().Timeout` -- download timeout is hardcoded
- The spinner uses `ctx` from the command, not a timeout context -- no separate timeout for download
- `--list` fetches up to 20 releases (`per_page=20`)
- If no asset matches the current OS/arch, returns a clear error with the expected asset name
- `runUpdate` uses `f.Debug()` for debug output but `runList` does not (list is simpler)

## Relationships
- Imports `cmdutil` (`internal/verda-cli/cmd/util`) for Factory, IOStreams, DebugJSON, LongDesc, Examples
- Imports `version` from `verdagostack/pkg/version` for current version info
- Does NOT use the Verda API client -- only GitHub API via raw HTTP
- No dependency on the Verda SDK (`verdacloud-sdk-go`) at all
- Uses standard library only for HTTP, archive handling, and file operations
