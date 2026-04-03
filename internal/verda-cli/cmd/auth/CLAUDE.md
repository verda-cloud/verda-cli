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

## Relationships
- `cmdutil.Factory` / `cmdutil.IOStreams` -- standard dependency injection
- `options` package -- `VerdaDir()`, `DefaultCredentialsFilePath()`, `LoadSharedCredentialsForProfile()`, `EnsureVerdaDir()`, `WriteSecureFile()`
- `verdagostack/pkg/tui/wizard` -- wizard engine and step definitions
- `verdagostack/pkg/tui/bubbletea` -- `HintStyle()` for wizard hint bar
- `gopkg.in/ini.v1` -- INI file read/write for credentials
- `go.yaml.in/yaml/v3` -- YAML read/write for config
