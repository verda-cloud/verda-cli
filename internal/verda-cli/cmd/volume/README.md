# verda volume -- Manage volumes

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda volume list` | List all block storage volumes | `--status` |
| `verda volume create` | Create a new block storage volume | `--name`, `--size`, `--type`, `--location` |
| `verda volume action` | Perform actions on a volume (detach, rename, resize, clone, delete) | `--id` |
| `verda volume trash` | List deleted volumes in trash | (none) |

## Usage Examples

### list
```bash
# List all volumes
verda volume list
verda vol ls

# Filter by status
verda volume list --status attached
```

### create
```bash
# Interactive (prompts for type, name, size, location)
verda volume create

# Non-interactive
verda volume create --name my-vol --size 100 --type NVMe --location FIN-01
```

### action
```bash
# Interactive volume picker
verda volume action

# Skip picker with known ID
verda vol action --id abc-123
```

### trash
```bash
verda volume trash
verda vol trash
```

## Interactive vs Non-Interactive

### create
All four flags (`--name`, `--size`, `--type`, `--location`) can be provided for fully non-interactive mode. Any missing flag triggers an interactive prompt for that field. Type is prompted as a selection (NVMe / HDD with pricing), size defaults to 100 GiB, location is fetched from the API and offered as a selection.

### action
If `--id` is omitted, an interactive volume picker is shown. The action itself is always selected interactively. Destructive actions (detach, delete) require confirmation. Rename, resize, and clone prompt for additional input via a `Prepare` callback before execution.

### list / trash
These are display-only commands with no interactive prompts.

## Architecture Notes

- **volume.go** -- Parent command definition. Alias: `vol`. Registers `create`, `list`, `action`, `trash` subcommands.
- **list.go** -- Lists volumes in a table format (Name, ID, Size, Type, Status, Location). Optionally filters via `client.Volumes.ListVolumesByStatus()`.
- **create.go** -- Multi-step creation flow. Fetches volume types for pricing, locations for selection. Displays a pricing summary before confirmation. Uses `VolumeCreateRequest` with Name, Size, Type, LocationCode.
- **action.go** -- Action menu on a selected volume. Builds available actions dynamically (Detach only shown when attached). Uses a `volumeAction` struct with `Label`, `ConfirmMsg`, `WarningMsg`, `Prepare`, and `Execute` callbacks. Shared `selectVolume()` helper renders a filterable list.
- **trash.go** -- Lists deleted volumes with rich formatting (lipgloss styling). Shows recovery window: 96-hour expiry from `DeletedAt`. Permanently deleted volumes are flagged. Uses a pager (`status.Pager`) for scrollable output when the list is long.

### API Endpoints / SDK Methods

| Method | Used In |
|--------|---------|
| `client.Volumes.ListVolumes()` | list, action (volume picker) |
| `client.Volumes.ListVolumesByStatus()` | list (with `--status`) |
| `client.Volumes.GetVolume()` | action |
| `client.Volumes.DetachVolume()` | action (Detach) |
| `client.Volumes.RenameVolume()` | action (Rename) |
| `client.Volumes.ResizeVolume()` | action (Resize) |
| `client.Volumes.CloneVolume()` | action (Clone) |
| `client.Volumes.DeleteVolume()` | action (Delete) |
| `client.Volumes.GetVolumesInTrash()` | trash |
| `client.Volumes.CreateVolume()` | create |
| `client.VolumeTypes.GetAllVolumeTypes()` | create (pricing) |
| `client.Locations.Get()` | create (location picker) |

### Volume Pricing

- API provides `price_per_month_per_gb` on each `VolumeType`.
- Monthly cost = `price_per_month_per_gb * size`.
- Hourly cost = `ceil(price_per_month_per_gb * size / 730 * 10000) / 10000` (730 = 365*24/12 hours/month).
- Pricing is displayed in the create summary and in the volume type selector labels.

### Trash / Recovery Window

- Deleted volumes remain recoverable for 96 hours from `DeletedAt`.
- `formatDuration()` renders remaining time as `Xd Yh` or `Xh`.
- Permanently deleted volumes display a red warning marker.
