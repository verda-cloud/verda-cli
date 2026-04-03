# VM Command Knowledge

## Quick Reference
- Parent: `verda vm` (aliases: `instance`, `instances`)
- Subcommands: `create`, `list` (alias `ls`), `action`
- Files:
  - `vm.go` -- Parent command, registers subcommands
  - `create.go` -- Create command, flags, request building, validation helpers
  - `wizard.go` -- 13-step interactive wizard with apiCache and clientFunc
  - `list.go` -- List command with interactive instance selector
  - `action.go` -- Action command (start/shutdown/hibernate/delete)
  - `status_view.go` -- Animated polling, instance card rendering, status colors

## Domain-Specific Logic

### Pricing (IMPORTANT)
- `price_per_hour` from API is the **TOTAL** instance price, not per-GPU
- Per-unit price = `totalPrice / instanceUnits(t)` where units = GPU count or vCPU count
- Spot pricing uses `SpotPrice` field instead of `PricePerHour`
- Volume hourly price: `ceil(monthlyPerGB * sizeGB / 730 * 10000) / 10000`
- 730 = hours in month (365*24/12), matching web frontend constant `hoursInMonth`

### Contract Normalization
- `normalizeContract()` accepts many aliases: `pay_as_go`, `pay-as-you-go`, `payg`, `spot`, `long_term`, etc.
- Normalizes to uppercase API values: `PAY_AS_YOU_GO`, `SPOT`, `LONG_TERM`
- Long-term duration strings (`1 month`, `3 months`, etc.) are explicitly **rejected** -- the POST /v1/instances API does not accept them
- When `IsSpot=true` and contract is empty, `request()` auto-sets `Contract="SPOT"`

### Kind / Instance Type Matching
- GPU types: any instance type NOT prefixed with `CPU.`
- CPU types: instance type prefixed with `CPU.`
- `validateKind()` cross-checks `--kind` against `--instance-type`
- `matchesKind()` used in wizard to filter instance type choices

### Spot Policies
- Valid policies: `keep_detached`, `move_to_trash`, `delete_permanently` (from `verda.SpotDiscontinue*` constants)
- Apply to OS volumes (`--os-volume-on-spot-discontinue`) and storage (`--storage-on-spot-discontinue`)
- Rejected when `--is-spot` is false

### Volume Specs
- `--volume` flag format: `name:size:type[:location[:on-spot-discontinue]]`
- `--storage-size` / `--storage-name` / `--storage-type` are convenience flags that generate a VolumeCreateRequest appended via `appendStorageVolume()`
- Default storage type: `verda.VolumeTypeNVMe`
- Default OS volume name: `<hostname>-os`; default storage name: `<hostname>-storage`

### Status Mappings
- Terminal statuses (polling stops): `running`, `offline`, `error`, `discontinued`, `not_found`, `no_capacity`
- In-progress statuses: `new` -> "Creating instance...", `ordered` -> "Instance ordered...", `provisioning` -> "Provisioning instance...", `validating` -> "Validating instance...", `pending` -> "Waiting for capacity..."
- Status colors: green=running, yellow=provisioning/ordered/new/validating/pending, red=error/no_capacity, dim=offline/discontinued/deleting

### Action Availability
- Start: only from `offline`
- Shutdown / Force shutdown / Hibernate: only from `running`
- Delete: always available (no ValidFrom filter)
- Delete sub-flow: fetches attached volumes, lets user multi-select which to delete, warns about continued billing for undeleted volumes

## Wizard Flow (13 steps)

```
billing-type -> contract -> kind -> instance-type -> location ->
image -> os-volume-size -> storage -> ssh-keys ->
startup-script -> hostname -> description -> confirm-deploy
```

- Steps with `DependsOn` re-run their Loader when dependencies change
- `contract` step: `ShouldSkip` returns true for spot billing
- `location` step: `IsSet` treats default `FIN-01` as unset (so wizard prompts)
- `storage`, `ssh-keys`, `startup-script` steps: manage values directly in Loader (Setter/Resetter are no-ops), include inline sub-flows for creating new resources via API
- `confirm-deploy` step: renders deployment summary with full cost breakdown, then confirms

## Gotchas & Edge Cases

- **Wizard triggers when ANY of instance-type, os, or hostname is missing** -- not all three. Providing two of three still launches the wizard.
- **Location default quirk**: `LocationCode` defaults to `FIN-01` in createOptions, but the wizard's `IsSet` returns false for `FIN-01` specifically, so the wizard always prompts for location even when the default is in effect.
- **apiCache invalidation**: Cache is invalidated when `isSpot` changes (user switches billing type), because availability differs between spot and on-demand.
- **Lazy client resolution**: `clientFunc` defers credential resolution until the first API-dependent wizard step fires. Early steps (billing-type, kind, text inputs) run without credentials.
- **Hidden flag aliases**: `--type`, `--image`, `--ssh-key-id`, `--startup-script-id`, `--spot` are hidden aliases for their primary flags.
- **Volume spec on-spot-discontinue**: Only valid when `--is-spot` is set, enforced both in `parseVolumeSpec` and `appendStorageVolume`.
- **Description defaults to hostname**: Both in `descriptionValue()` for non-interactive mode and in the wizard's `stepDescription` Default function.
- **pollInstanceStatus variadic target**: Accepts optional `expectStatus` -- action commands pass the expected status, create passes none (polls until any terminal status).
- **Delete does NOT poll** -- `action.Execute` is nil for delete, handled by `runDeleteFlow` which returns after the API call.
- **SSH key / startup script inline creation**: These wizard steps create resources via API during the wizard, not deferred to instance creation.
- **Cluster images filtered out**: `stepImage` skips images where `IsCluster` is true.
- **Contract step non-fatal API errors**: If fetching long-term periods fails, the step gracefully falls back to offering only "Pay as you go".

## Relationships

- **wizard engine**: `verdagostack/pkg/tui/wizard` -- provides `Flow`, `Step`, `Store`, `Engine`, `Choice`, prompt types
- **tui package**: `verdagostack/pkg/tui` -- `Prompter`, `Status` interfaces, `WithDefault`, `WithConfirmDefault`, `WithEditorDefault`, `WithFileExt` options
- **bubbletea package**: `verdagostack/pkg/tui/bubbletea` -- `HintStyle()` for wizard hints
- **SDK**: `verdacloud-sdk-go/pkg/verda` -- all API client types, constants (`LocationFIN01`, `VolumeTypeNVMe`, `VolumeTypeHDD`, `SpotDiscontinue*`, `Status*`)
- **cmdutil**: `cmd/util` -- `Factory`, `IOStreams`, `DebugJSON`, `UsageErrorf`, `ValidateHostname`, `GenerateHostname`, `LongDesc`, `Examples`, `DefaultSubCommandRun`
- **Factory dependencies**: `f.VerdaClient()`, `f.Prompter()`, `f.Status()`, `f.Debug()`, `f.Options().Timeout`
