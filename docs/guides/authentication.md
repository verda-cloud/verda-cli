# Authentication

How the Verda CLI finds API credentials. For the `verda auth` commands themselves (`login`, `show`, `use`), see the
[auth reference](../reference/auth.md).

## Credential Resolution Order

Credentials are resolved from multiple sources in order of precedence:

| Priority | Source | Example |
|----------|--------|---------|
| 1 | CLI flags (highest) | `--auth.client-id=xxx` |
| 2 | Config file | `auth.client-id` in `~/.verda/config.yaml` |
| 3 | Environment variables | `VERDA_CLIENT_ID`, `VERDA_CLIENT_SECRET` |
| 4 | Credentials file | `[default]` in `~/.verda/credentials` |

> **Note:** `--auth.profile` / `VERDA_PROFILE` select WHICH credentials file
> profile fills missing values; inline sources (flags, config, env — including
> the `VERDA_AUTH_*` spellings) always win over stored profile values.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `VERDA_CLIENT_ID` | API client ID |
| `VERDA_CLIENT_SECRET` | API client secret |
| `VERDA_PROFILE` | Credentials profile name |
| `VERDA_SHARED_CREDENTIALS_FILE` | Path to credentials file |
| `VERDA_AGENT` | Enable agent mode (`1` or `true`) |
| `VERDA_HOME` | Base directory for config (default `~/.verda`) |

```bash
# Use env vars for CI/CD pipelines
export VERDA_CLIENT_ID=your-client-id
export VERDA_CLIENT_SECRET=your-client-secret
verda vm list
```
