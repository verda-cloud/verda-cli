# Version Command Knowledge

## Quick Reference
- Parent: `verda version` (top-level command, no parent group)
- Subcommands: none
- Files:
  - `version.go` -- Single command that prints version JSON

## Domain-Specific Logic
- Delegates entirely to `version.Get().ToJSON()` from `verdagostack/pkg/version`
- Output is JSON-formatted version/build info
- No authentication or API calls needed
- Uses `Run` callback (not `RunE`) since it has no error path

## Gotchas & Edge Cases
- The `Factory` parameter is accepted by `NewCmdVersion` for interface consistency but is not used
- Version values are injected at compile time via ldflags -- the source code itself contains no version strings

## Relationships
- Depends on `github.com/verda-cloud/verdagostack/pkg/version` for version retrieval
- Depends on `cmdutil.Factory` (unused) and `cmdutil.IOStreams` for output
- Uses `cmdutil.LongDesc` for help text formatting
