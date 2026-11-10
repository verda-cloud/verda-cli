# Authentication

How the Verda CLI finds API credentials. For the `verda auth` commands themselves (`login`, `show`, `use`), see the
[auth reference](../reference/auth.md).

## Credential Resolution Order

Flags, environment variables, `~/.verda/config.yaml` and the credentials file are checked in a fixed order for each
credential, the credentials file path and the profile. `verda auth --help` and the
[auth reference](../reference/auth.md) list that order for releases after v1.10.0; up to v1.10.0 they print a
shorter list that leaves out `config.yaml`.

## Environment Variables

Pairs such as `VERDA_AUTH_CLIENT_ID` and `VERDA_CLIENT_ID` set the same value at different precedence.

| Variable | Description |
|----------|-------------|
| `VERDA_CLIENT_ID` | API client ID |
| `VERDA_CLIENT_SECRET` | API client secret |
| `VERDA_AUTH_CLIENT_ID` | API client ID |
| `VERDA_AUTH_CLIENT_SECRET` | API client secret |
| `VERDA_AUTH_TOKEN` | Bearer token |
| `VERDA_PROFILE` | Credentials profile name |
| `VERDA_AUTH_PROFILE` | Credentials profile name |
| `VERDA_AUTH_CREDENTIALS_FILE` | Path to credentials file |
| `VERDA_SHARED_CREDENTIALS_FILE` | Path to credentials file |
| `VERDA_AGENT` | Enable agent mode (`1` or `true`) |
| `VERDA_HOME` | Base directory for config (default `~/.verda`) |

```bash
# Use env vars for CI/CD pipelines
export VERDA_CLIENT_ID=your-client-id
export VERDA_CLIENT_SECRET=your-client-secret
verda vm list
```
