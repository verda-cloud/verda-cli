# verda version -- Print version information

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda version` | Print build and version info as JSON | _(none)_ |

## Usage Examples

```bash
verda version
```

## Architecture Notes

- **version.go** -- Single file, single command. Calls `version.Get().ToJSON()` from the `verdagostack` library and prints the result to `ioStreams.Out`.
- No API calls, no authentication required, no flags.
- Uses `Run` (not `RunE`) since it cannot fail.
- Version info is provided by `github.com/verda-cloud/verdagostack/pkg/version` -- the actual version values are set at build time via ldflags (defined outside this package).
