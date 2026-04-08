# verda auth -- Manage shared credentials and profiles

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda auth login` | Save API credentials for a profile | `--profile`, `--base-url`, `--client-id`, `--client-secret`, `--credentials-file` |
| `verda auth use PROFILE` | Switch the active auth profile | `--credentials-file` |
| `verda auth show` | Show the resolved auth profile | (none) |

## Usage Examples

### Login
```bash
# Interactive wizard (prompts for all fields)
verda auth login

# Non-interactive with flags
verda auth login \
  --client-id your-client-id \
  --client-secret your-client-secret

# Staging profile with custom API endpoint
verda auth login \
  --profile staging \
  --base-url https://staging-api.verda.com/v1 \
  --client-id staging-id \
  --client-secret staging-secret

# Custom credentials file
verda auth login \
  --credentials-file /path/to/credentials \
  --client-id your-id \
  --client-secret your-secret
```

### Use
```bash
# Switch to a named profile
verda auth use staging
```

### Show
```bash
# Display the currently active profile and credential status
verda auth show
```

## Interactive vs Non-Interactive

The `login` command enters interactive wizard mode when `--client-id` or `--client-secret` are missing. The wizard prompts for all four fields in order: profile, base-url, client-id, client-secret. Fields already set via flags are skipped by the wizard (the `IsSet` callback controls this).

`use` and `show` are always non-interactive.

## Credential Resolution Order

Authentication credentials are resolved from multiple sources. The table below
shows the order of precedence (highest first):

| Priority | Source | Example |
|----------|--------|---------|
| 1 | CLI flags | `--auth.client-id=xxx --auth.client-secret=yyy` |
| 2 | Config file (viper) | `auth.client-id` in `~/.verda/config.yaml` |
| 3 | Environment variables | `VERDA_CLIENT_ID`, `VERDA_CLIENT_SECRET` |
| 4 | Credentials file profile | `[default]` section in `~/.verda/credentials` |

### Supported Environment Variables

| Variable | Description |
|----------|-------------|
| `VERDA_CLIENT_ID` | API client ID |
| `VERDA_CLIENT_SECRET` | API client secret |
| `VERDA_PROFILE` | Credentials profile name |
| `VERDA_SHARED_CREDENTIALS_FILE` | Path to credentials file |
| `VERDA_AGENT` | Enable agent mode (`1` or `true`) |
| `VERDA_HOME` | Base directory for config (default `~/.verda`) |

### Explicit Profile Override

When `--auth.profile` is passed explicitly, the credentials file values for that
profile override env vars and config file values — but CLI flags still win.

For example:

```bash
# Env var is set
export VERDA_CLIENT_ID=env-id

# Explicit profile overrides the env var
verda compute list --auth.profile=staging
# → uses client ID from [staging] in ~/.verda/credentials, not env-id

# But a CLI flag always wins
verda compute list --auth.profile=staging --auth.client-id=flag-id
# → uses flag-id
```

## Architecture Notes

- **auth.go** -- Parent command registration; adds `login`, `use`, `show` subcommands.
- **login.go** -- Saves credentials to an AWS-style INI file (`~/.verda/credentials`). Creates or updates a named profile section. Sets file permissions to `0600` on Unix.
- **wizard.go** -- Defines the 4-step interactive wizard flow for `login` (profile -> base-url -> client-id -> client-secret). Uses `wizard.PasswordPrompt` for the client-secret step. Includes validation (non-empty, URL prefix check).
- **use.go** -- Switches the active profile by writing to `~/.verda/config.yaml` under `auth.profile`. Validates the profile exists in the credentials file before switching.
- **show.go** -- Reads resolved options from `Factory.Options()` and prints profile name, credentials file path, base URL, and whether client ID/secret are loaded (boolean, not the actual values).
- **path.go** -- Helper functions for resolving the credentials file path (flag > `VERDA_SHARED_CREDENTIALS_FILE` env var > default) and the config file path (`~/.verda/config.yaml`).
- **auth_test.go** -- Tests for `writeActiveProfile` and `resolveCredentialsFile`.
- **wizard_test.go** -- Tests the wizard flow with mock prompter, covering both full interactive and partial preset scenarios.

### Credentials Storage

- Format: AWS-style INI via `gopkg.in/ini.v1`
- Default path: `~/.verda/credentials`
- Keys per profile section: `verda_base_url`, `verda_client_id`, `verda_client_secret`

### Config Storage

- Format: YAML via `go.yaml.in/yaml/v3`
- Path: `~/.verda/config.yaml`
- Structure: `auth.profile` key holds the active profile name
