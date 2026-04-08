# verda update -- Update Verda CLI to the latest or a specific version

## Commands

This is a single command (no subcommands).

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda update` | Update CLI binary in-place from GitHub Releases | `--target`, `--list` |

## Usage Examples

```bash
# Update to latest version
verda update

# Install a specific version (upgrade or downgrade)
verda update --target v1.0.0

# List available versions (marks current with *)
verda update --list
```

## Interactive vs Non-Interactive

This command is entirely non-interactive. No prompts are used. Behavior is controlled by flags:
- No flags: fetches and installs the latest release.
- `--target <tag>`: installs the specified version. Accepts with or without `v` prefix.
- `--list`: prints up to 20 available versions and exits. The current version is marked with `*`.

If already at the target version, it prints "Already at vX.Y.Z" and exits.

## Architecture Notes

- **update.go** -- Single file containing all logic: command definition, GitHub API interaction, archive extraction, and binary replacement.

### Update Flow
1. Resolve target version (latest via API, or from `--target` flag)
2. Compare with current version from `version.Get().GitVersion`
3. Download platform-specific archive asset from GitHub Releases
4. Extract binary from tar.gz (Linux/macOS) or zip (Windows)
5. Atomic binary replacement: write to temp file, chmod 0755, rename over current executable

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

### Binary Replacement Strategy
- Resolves symlinks via `filepath.EvalSymlinks` to find the real executable path
- Writes new binary to a temp file in the same directory as the current executable
- Sets permissions to 0755
- Renames temp file over current executable (atomic on POSIX)
- On Windows: removes destination before rename (rename over existing file not supported)
