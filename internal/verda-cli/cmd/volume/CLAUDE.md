# Volume Command Knowledge

## Quick Reference
- Parent: `verda volume` (aliases: `vol`)
- Subcommands: `list` (alias `ls`), `create`, `action`, `trash`
- Files:
  - `volume.go` -- Parent command registration
  - `list.go` -- List volumes with optional status filter
  - `create.go` -- Interactive/flag-driven volume creation with pricing display
  - `action.go` -- Action menu (detach, rename, resize, clone, delete) with volume picker
  - `trash.go` -- List deleted volumes with 96h recovery window display

## Domain-Specific Logic

### Pricing Calculation
- `price_per_month_per_gb` comes from `VolumeType.Price.PricePerMonthPerGB`
- `hoursInMonth = 730` (365*24/12), matching the web frontend
- Hourly = `ceil(monthlyPerGB * size / 730 * 10000) / 10000`
- Monthly = `monthlyPerGB * size`
- Volume types are keyed by `verda.VolumeTypeNVMe` and `verda.VolumeTypeHDD`

### Trash Recovery
- Recovery window: 96 hours from `DeletedAt`
- `formatDuration()` renders as `Xd Yh` (>=24h) or `Xh` (<24h)
- `IsPermanentlyDeleted` flag distinguishes recoverable vs gone

### Action Availability
- Detach only available when `vol.InstanceID != nil && *vol.InstanceID != ""`
- Resize is grow-only: new size must be > current size
- Delete passes `false` for permanent delete flag (soft delete, goes to trash)
- Clone defaults name to `<vol.Name>-clone`

### Volume Status Values
- Used in list `--status` filter: `attached`, `detached`, `ordered` (from API)
- `IsOSVolume` overrides display status to "OS" (in select) or "Main OS" (in summary)

## Gotchas & Edge Cases
- `action.go` uses closures with captured variables (`newName`, `newSize`, `cloneName`) that are set in `Prepare` and read in `Execute` -- order matters
- `selectVolume()` returns `("", nil)` on cancel/Esc -- callers must check for empty string, not error
- `list.go` branches on `opts.Status != ""` to call different SDK methods (`ListVolumes` vs `ListVolumesByStatus`)
- `create.go` fetches volume types before any prompts to show pricing in the type selector
- `trash.go` uses `status.Pager()` for scrollable output; falls back to direct print if status is nil
- Spinner pattern: create a spinner, do work, stop spinner, then check error -- consistent across all commands

## Relationships
- Imports `cmdutil` (`internal/verda-cli/cmd/util`) for Factory, IOStreams, DebugJSON, LongDesc, Examples
- Imports `verda` SDK (`verdacloud-sdk-go/pkg/verda`) for API types and client
- Imports `tui` (`verdagostack/pkg/tui`) for Prompter interface and options (`WithDefault`, `WithConfirmDefault`, `WithPagerTitle`)
- Imports `lipgloss` v2 for styled terminal output (bold, dim, warning colors)
