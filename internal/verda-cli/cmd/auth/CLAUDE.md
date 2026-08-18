# Auth Command Knowledge

## Quick Reference
- Parent: `verda auth`
- Subcommands: `login`, `use PROFILE`, `show`
- Files:
  - `auth.go` -- Parent command; registers subcommands
  - `login.go` -- Save credentials to INI file; interactive or flag-driven
  - `wizard.go` -- 4-step wizard flow definition (profile, base-url, client-id, client-secret)
  - `use.go` -- Switch active profile by writing `~/.verda/config.yaml`
  - `show.go` -- Display resolved auth state (no secrets printed)
  - `path.go` -- Helpers: `resolveCredentialsFile`, `defaultConfigFilePath`
  - `auth_test.go` -- Tests for `writeActiveProfile`, `resolveCredentialsFile`
  - `wizard_test.go` -- Wizard flow tests with mock prompter
  - `login_test.go` -- Flag-driven write path: new/named profile, merge, re-auth overwrite, 0600, flag-over-env

## Domain-Specific Logic
- Credentials file resolution order: explicit flag > `VERDA_SHARED_CREDENTIALS_FILE` env var > `options.DefaultCredentialsFilePath()`
- Config file is always `~/.verda/config.yaml`; active profile stored at YAML path `auth.profile`
- Credentials file uses AWS-style INI format with `verda_`-prefixed keys
- File permissions set to `0600` on non-Windows platforms after writing credentials
- `show` never prints actual secrets -- only booleans for whether they are loaded
- `use` validates the target profile exists in the credentials file before switching

## Gotchas & Edge Cases
- The wizard is triggered only when `--client-id` OR `--client-secret` is empty (whitespace-trimmed). If both are provided via flags, the wizard is skipped entirely.
- After the wizard runs, there is a second validation gate -- if client-id or client-secret is still empty (e.g., user cancelled the wizard), the command returns a usage error.
- `wizard.PasswordPrompt` is used for client-secret (masked input), while other fields use `wizard.TextInputPrompt`.
- The `selectThemeWizard` pattern of returning `nil` on wizard error (user cancel) is NOT used here -- login returns the wizard error directly.
- `writeActiveProfile` in `use.go` merges into existing config YAML rather than overwriting the whole file.
- `login` creates the `~/.verda/` directory via `options.EnsureVerdaDir()` before saving.
- **Never run `auth login` against the real config dir.** Set `VERDA_HOME` to a temp dir for
  any manual or pty-driven run (`make run.sandbox ARGS="auth login"`). Re-running login
  replaces an existing profile with no warning -- intentional, it is the re-auth path --
  and a client secret cannot be read back from the API, so a clobber is unrecoverable.
  `VERDA_SHARED_CREDENTIALS_FILE` alone is insufficient: `EnsureVerdaDir()` resolves
  through `VerdaDir()` and would still mkdir the real `~/.verda`. Tests must set
  `VERDA_HOME` for the same reason.
- `login_test.go` covers only the flag-driven path. Supplying both `--client-id` and
  `--client-secret` is what skips the wizard, so the post-wizard validation gate is
  unreachable from a test -- the engine is constructed inline in `RunE`, and a wizard in a
  test would start a real `tea.Program` against the developer's stdin. Covering that gate
  means injecting the engine.

## Relationships
- `cmdutil.Factory` / `cmdutil.IOStreams` -- standard dependency injection
- `options` package -- `VerdaDir()`, `DefaultCredentialsFilePath()`, `LoadSharedCredentialsForProfile()`, `EnsureVerdaDir()`, `WriteSecureFile()`
- `pkg/tui/wizard` -- wizard engine and step definitions
- `pkg/tui/bubbletea` -- `HintStyle()` for wizard hint bar
- `gopkg.in/ini.v1` -- INI file read/write for credentials
- `go.yaml.in/yaml/v3` -- YAML read/write for config
