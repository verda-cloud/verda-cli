# verda update -- Update Verda CLI to the latest or a specific version

## Commands

This is a single command (no subcommands).

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda update` | Update CLI binary in-place from GitHub Releases | `--target`, `--list`, `--verify`, `--skip-verify` |

## Usage Examples

```bash
# Update to latest version
verda update

# Install a specific version (upgrade or downgrade)
verda update --target v1.0.0

# List available versions (marks current with *)
verda update --list

# Verify the installed binary against the release's binary checksums
verda update --verify

# Bypass checksum verification (NOT recommended; escape hatch only)
verda update --skip-verify
```

## Interactive vs Non-Interactive

This command is entirely non-interactive. No prompts are used. Behavior is controlled by flags:
- No flags: fetches and installs the latest release.
- `--target <tag>`: installs the specified version. Accepts with or without `v` prefix.
- `--list`: prints up to 20 available versions and exits. The current version is marked with `*`.
- `--verify`: checks the installed binary against the release's binary checksums and exits.
- `--skip-verify`: skips the default archive checksum verification (see below).

If already at the target version, it prints "Already at vX.Y.Z" and exits.
In `-o json` / `--agent` mode the outcome is a structured `updateResult`
(`version`, `previousVersion`, `path`, `updated`, `checksumVerified`) instead
of the plain text lines.

## Integrity Verification

Verification is ON by default and fails closed: the downloaded archive is
checked against the release's `verda_<version>_SHA256SUMS` (published by
goreleaser) before the running binary is replaced. Any failure — hash
mismatch, or the checksum file being unreachable/unparseable — aborts the
update and leaves the existing binary untouched; the error names
`--skip-verify` as the escape hatch.

`curl | sh` installs via `scripts/install.sh` verify the same sums file
(`sha256sum -c` / `shasum -a 256 -c`). Escape hatch there:
`VERDA_INSTALL_SKIP_VERIFY=1`. The release trusts cosign signatures for
authenticity; CLI-side cosign verification is a documented next step (see
`CLAUDE.md` in this directory) and intentionally not implemented yet.

## Architecture Notes

- **update.go** -- Command definition, GitHub API interaction, archive download and verification, archive extraction, binary replacement.
- **verify.go** -- Checksum helpers (fetch, parse, hash, match) plus the `--verify` flow that checks an installed binary against the release's binary sums.

### Update Flow
1. Resolve target version (latest via API, or from `--target` flag)
2. Compare with current version from `version.Get().GitVersion`
3. Download platform-specific archive asset from GitHub Releases
4. Verify archive bytes against the release's `verda_<version>_SHA256SUMS` (skipped only with `--skip-verify`); abort without replacing on any failure
5. Extract binary from tar.gz (Linux/macOS) or zip (Windows)
6. Atomic binary replacement: write to temp file, chmod 0755, rename over current executable

### GitHub API
- Base URL: `https://api.github.com`
- Repo: `verda-cloud/verda-cli`
- Endpoints used:
  - `GET /repos/{repo}/releases/latest` -- resolve latest version
  - `GET /repos/{repo}/releases?per_page=20` -- list versions
  - `GET /repos/{repo}/releases/tags/{tag}` -- fetch specific release for asset URLs
- HTTP timeout: 60 seconds
- Accept header: `application/vnd.github+json`

### Asset Naming Convention
- Pattern: `verda_{version}_{os}_{arch}.{ext}`
- Version is without `v` prefix (e.g., `1.0.0`)
- Extension: `tar.gz` on Linux/macOS, `zip` on Windows
- Binary name inside archive: `verda` (or `verda.exe` on Windows)
- Checksum assets on every release: `verda_{version}_SHA256SUMS` (archive
  checksums; used by the update path and install.sh) and
  `verda_{version}_binary_SHA256SUMS` (unpacked-binary checksums; used by
  `--verify`), each with cosign `.sig`/`.pem` bundles (not yet verified CLI-side)

### Binary Replacement Strategy
- Resolves symlinks via `filepath.EvalSymlinks` to find the real executable path
- Writes new binary to a temp file in the same directory as the current executable
- Sets permissions to 0755
- Renames temp file over current executable (atomic on POSIX)
- On Windows: removes destination before rename (rename over existing file not supported)
