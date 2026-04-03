# verda vm -- Manage virtual machines

Parent command for creating and managing Verda Cloud VM instances (GPU and CPU).

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda vm create` | Create a VM instance | `--kind`, `--instance-type`, `--os`, `--hostname`, `--location`, `--is-spot`, `--contract`, `--ssh-key`, `--os-volume-size`, `--storage-size`, `--volume`, `--existing-volume`, `--startup-script`, `--coupon`, `--description`, `--pricing` |
| `verda vm list` | List VM instances | `--status` |
| `verda vm action` | Perform actions on a VM instance | `--id` |

Aliases: `verda instance`, `verda instances`

## Usage Examples

### Create

```bash
# Interactive (wizard launches when required fields are missing)
verda vm create

# Non-interactive GPU instance
verda vm create \
  --kind gpu \
  --instance-type 1V100.6V \
  --location FIN-01 \
  --os ubuntu-24.04-cuda-12.8-open-docker \
  --os-volume-size 100 \
  --hostname gpu-runner \
  --description "GPU runner for batch jobs" \
  --ssh-key ssh_key_123

# Non-interactive CPU spot instance with additional storage
verda vm create \
  --kind cpu \
  --instance-type CPU.4V.16G \
  --location FIN-03 \
  --os ubuntu-24.04 \
  --os-volume-size 55 \
  --hostname training-node \
  --is-spot \
  --storage-size 500

# Attach an existing volume and a new inline volume
verda vm create \
  --instance-type 1V100.6V \
  --os ubuntu-24.04-cuda-12.8-open-docker \
  --hostname my-vm \
  --existing-volume vol_abc123 \
  --volume data:500:NVMe:FIN-03:move_to_trash --is-spot
```

### List

```bash
# Interactive list with instance selector
verda vm list

# Filter by status
verda vm list --status running
```

### Action

```bash
# Interactive: select instance then action
verda vm action

# Specify instance ID directly
verda vm action --id abc-123
```

## Interactive vs Non-Interactive

### Create

The wizard launches automatically when any of `--instance-type`, `--os`, or `--hostname` are missing. The wizard walks through 13 steps:

1. Billing type (On-Demand or Spot)
2. Contract period (skipped for Spot)
3. Compute type (GPU or CPU)
4. Instance type (filtered by kind and availability)
5. Datacenter location (filtered by instance type availability)
6. OS image (cluster images excluded)
7. OS volume size (default: 50 GiB)
8. Storage -- add new volumes, attach existing detached volumes, or skip
9. SSH keys -- select existing or add new inline
10. Startup script -- select existing, add new (from file or paste), or skip
11. Hostname (auto-generated default based on location)
12. Description (defaults to hostname)
13. Deployment summary with cost breakdown and confirmation

When all three required flags are provided, the wizard is skipped entirely.

### List

Always interactive. Displays instances in a selectable list. Selecting an instance fetches fresh details (including volumes) and renders a styled card. Loop continues until "Exit" is chosen.

### Action

Interactive instance selector appears when `--id` is not provided. Available actions depend on instance status:

| Action | Available when |
|--------|---------------|
| Start | offline |
| Shutdown | running |
| Force shutdown | running |
| Hibernate | running |
| Delete instance | always |

Destructive actions (Shutdown, Force shutdown, Delete) show confirmation prompts with warnings. Delete has a sub-flow for selecting which attached volumes to also delete.

## Architecture Notes

### Files

- **vm.go** -- Parent command definition, registers subcommands
- **create.go** -- `vm create` command, flag definitions, request building, contract normalization, volume spec parsing, kind validation
- **wizard.go** -- 13-step interactive wizard using the wizard engine; includes `apiCache` for deduplicating API calls, `clientFunc` lazy client pattern, pricing calculations, and inline sub-flows for adding SSH keys and startup scripts
- **list.go** -- `vm list` command with interactive instance selector and detail card viewer
- **action.go** -- `vm action` command with status-filtered action menu, confirmation flows, delete sub-flow with volume selection
- **status_view.go** -- Animated polling status view, instance card renderer, status-to-color mappings, terminal status detection

### API Endpoints / SDK Methods

- `client.Instances.Create` -- Create instance
- `client.Instances.Get` -- List instances (with optional status filter)
- `client.Instances.GetByID` -- Get instance details
- `client.Instances.Start` / `Shutdown` / `ForceShutdown` / `Hibernate` -- Instance lifecycle
- `client.Instances.Delete` -- Delete instance with optional volume IDs
- `client.InstanceAvailability.GetAllAvailabilities` -- Location/instance type availability
- `client.InstanceTypes.Get` -- Instance type catalog with pricing
- `client.Images.Get` -- OS image catalog
- `client.Locations.Get` -- Datacenter locations
- `client.Volumes.GetVolume` / `ListVolumes` -- Volume details
- `client.VolumeTypes.GetAllVolumeTypes` -- Volume type pricing
- `client.SSHKeys.GetAllSSHKeys` / `AddSSHKey` -- SSH key management
- `client.StartupScripts.GetAllStartupScripts` / `AddStartupScript` -- Startup script management
- `client.LongTerm.GetInstancePeriods` -- Long-term contract periods

### Business Logic

- **Pricing**: `price_per_hour` from the API is the TOTAL instance price, not per-GPU. Per-unit price is derived by dividing by GPU count (or vCPU count for CPU instances).
- **Volume pricing**: Hourly = `ceil(monthlyPerGB * sizeGB / 730 * 10000) / 10000` (730 = hours in a month, matching the web frontend).
- **Spot policies**: OS volumes and storage volumes support `keep_detached`, `move_to_trash`, or `delete_permanently` on spot discontinue. Spot policies are rejected when `--is-spot` is false.
- **Contract normalization**: Accepts many variants (pay_as_go, pay-as-you-go, payg, etc.) and normalizes to API values. Long-term duration strings (1 month, 3 months, etc.) are rejected because the POST API does not accept them.
- **Kind validation**: `--kind cpu` requires instance type starting with `CPU.`; `--kind gpu` rejects `CPU.`-prefixed types.
- **Volume specs**: `--volume` flag uses `name:size:type[:location[:on-spot-discontinue]]` format.
- **Status polling**: After create or action, polls every 5s (up to 5 min timeout) with animated spinner showing elapsed time. Terminal statuses: running, offline, error, discontinued, not_found, no_capacity.
