# VM Command Knowledge

## Quick Reference
- Parent: `verda vm` (aliases: `instance`, `instances`)
- Subcommands: `create`, `list` (alias `ls`), `describe`, `availability`, `action`, plus shortcuts (`start`, `shutdown`, `hibernate`, `delete`)
- Files:
  - `vm.go` -- Parent command, registers subcommands and shortcuts
  - `create.go` -- Create command, flags, `createOptions` struct, request building, validation
  - `wizard.go` -- 13 wizard step definitions, `WizardMode`, `RunTemplateWizard`, step Defaults
  - `wizard_cache.go` -- `apiCache`, `fetchLocations`, `loadAllLocations`/`loadAvailableLocations`, `ensurePricingCache`, pricing helpers, instance type utils
  - `wizard_subflows.go` -- SSH key, startup script, storage interactive sub-flows
  - `wizard_summary.go` -- `renderDeploymentSummary` (accepts `io.Writer`)
  - `template_apply.go` -- Template loading, applying, name resolution with warnings
  - `list.go` -- List command with interactive selector, parallel volume fetching
  - `describe.go` -- Describe command with optional picker
  - `availability.go` -- Availability + pricing table
  - `action.go` -- Action command, data-driven action definitions, delete sub-flow
  - `batch.go` -- Batch operations (`--all`, `--hostname` glob)
  - `shortcuts.go` -- Generated shortcut commands from `shortcutDef` structs
  - `instances.go` -- Shared `fetchInstances` + `filterByStatus` helpers
  - `status_view.go` -- Instance card rendering, status colors

## Domain-Specific Logic

### Pricing (IMPORTANT)
- `price_per_hour` from API is the **TOTAL** instance price, not per-GPU (verified live on staging 2026-08-09, see `temp/docs/c1-ondemand-instance.json`)
- Per-unit display price = `totalPrice / instanceUnits(t)` where units = GPU count or vCPU count — division only, display only
- Spot pricing uses `SpotPrice` field instead of `PricePerHour`
- Volume hourly price: `cmdutil.VolumeHourlyPrice(monthlyPerGB, sizeGB)` (canonical helper, do not re-implement)
- `cmdutil.HoursInMonth` = 730 (365*24/12), matching web frontend constant `hoursInMonth`

### Contract Normalization
- `normalizeContract()` accepts many aliases: `pay_as_go`, `pay-as-you-go`, `payg`, `spot`, `long_term`, etc.
- Normalizes to constants: `contractPayAsYouGo`, `contractSpot`, `contractLongTerm`
- Long-term duration strings (`1 month`, `3 months`, etc.) are explicitly **rejected**
- When `IsSpot=true` and contract is empty, `request()` auto-sets `Contract=contractSpot`

### Kind / Instance Type Matching
- GPU types: any instance type NOT prefixed with `CPU.`
- CPU types: instance type prefixed with `CPU.`
- `validateKind()` cross-checks `--kind` against `--instance-type`
- `matchesKind()` used in wizard and template edit to filter instance type choices

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

## createOptions Mutation Lifecycle

Fields are populated in a defined sequence — each stage reads fields set by prior stages:

1. **Flag parsing (cobra)** — Sets all public fields from CLI flags. LocationCode defaults to FIN-01.
2. **Template application (applyTemplate)** — Fills fields the user did not pass explicitly (`cmd.Flags().Changed` is the authority: flags beat template values, e.g. `--from t --location FIN-03` keeps FIN-03). Sets `billingTypeSet`, `locationSet`, `storageSkip`, `startupScriptSkip` coordination flags (only for template-sourced values). Expands HostnamePattern (kept in `opts.hostnamePattern` so the wizard location step can re-expand `{location}` against the effective location).
3. **Name resolution (resolveTemplateNames)** — Resolves `sshKeyNames` → `SSHKeyIDs`, `startupScriptName` → `StartupScriptID` via API. On failure, prints warnings and leaves IDs empty for wizard.
4. **Wizard (buildCreateFlow steps)** — Fills remaining gaps interactively. Steps check `IsSet` to skip pre-filled values. Steps 8/9/10 manage state directly via Loader closures.
5. **Request building (request())** — Reads all fields to assemble `CreateInstanceRequest`. Auto-sets `Contract=contractSpot` when IsSpot && Contract is empty.

## Wizard Flow (13 steps)

```
billing-type -> contract -> kind -> instance-type -> location ->
image -> os-volume-size -> storage -> ssh-keys ->
startup-script -> hostname -> description -> confirm-deploy
```

- Steps with `DependsOn` re-run their Loader when dependencies change
- `contract` step: `ShouldSkip` returns true for spot billing
- `instance-type` step: accepts `WizardMode`. Deploy mode filters by real-time availability; template mode shows all instance types from the instance-types API (no availability filtering)
- `location` step: accepts `WizardMode`. Deploy mode shows only locations where the instance type is available and returns a clear error when none are; template mode shows all locations with a "None (decide at deploy time)" choice whose value is the `locationDecideLater` sentinel — an empty value would trip the engine's Default substitution and silently persist FIN-01 (review H5). The Setter also re-expands a template `hostnamePattern`'s `{location}` against the picked location so deploy hostnames use the effective location.
- `location` step: `IsSet` treats default `FIN-01` as unset (so wizard prompts)
- `location` step: `Required` is dynamic — true in deploy mode, false in template mode
- `storage`, `ssh-keys`, `startup-script` steps: manage values directly in Loader (Setter/Resetter are no-ops), include inline sub-flows for creating new resources via API
- `confirm-deploy` step: renders deployment summary with full cost breakdown via `renderDeploymentSummary(w, opts, cache)`
- Steps have `Default` functions that return current `opts` values for pre-selection (used when `--from` pre-fills values)

## Shared Helpers

- **`cmdutil.WithSpinner[T]`** (`cmd/util/spinner.go`) -- Generic spinner wrapper used across all commands. Handles nil Status, spinner start errors gracefully.
- **`cmdutil.RunWithSpinner`** -- Convenience wrapper for error-only functions.
- **`fetchInstances`** (`instances.go`) -- Shared instance loading with spinner + status filtering. Used by `selectInstance`, `selectInstances`, `fetchBatchInstances`.
- **`filterByStatus`** (`instances.go`) -- Client-side status filtering for multiple statuses.
- **`cmdutil.TemplatesBaseDir`** (`cmd/util/paths.go`) -- Centralized `~/.verda/templates` path construction.

## Gotchas & Edge Cases

- **Wizard triggers when ANY of instance-type, os, or hostname is missing** -- not all three. Providing two of three still launches the wizard. Also triggers when `--from` was used but the template had no location (`templateWithoutLocation` check in `resolveCreateInputs`).
- **Location default quirk**: `LocationCode` defaults to `FIN-01` in createOptions, but the wizard's `IsSet` returns false for `FIN-01` specifically, so the wizard always prompts for location even when the default is in effect.
- **Flags override template values**: `applyTemplate` fills only flags the user did not pass — `cmd.Flags().Changed` is the authority (not field emptiness), and template-sourced values are the only ones that arm the `*Set` coordination flags.
- **Contract step offers only deployable contracts**: the Loader drops long-term periods whose codes `normalizeContract` would reject at request time (POST /v1/instances takes no durations). Non-fatal API errors: if fetching periods fails, the step falls back to offering only "Pay as you go".
- **apiCache invalidation**: Cache is invalidated when `isSpot` changes (user switches billing type), because availability differs between spot and on-demand.
- **Lazy client resolution**: `clientFunc` defers credential resolution until the first API-dependent wizard step fires. Early steps (billing-type, kind, text inputs) run without credentials.
- **Hidden flag aliases**: `--type`, `--image`, `--ssh-key-id`, `--startup-script-id`, `--spot` are hidden aliases for their primary flags.
- **Volume spec on-spot-discontinue**: Only valid when `--is-spot` is set, enforced both in `parseVolumeSpec` and `appendStorageVolume`.
- **Description defaults to hostname**: Both in `descriptionValue()` for non-interactive mode and in the wizard's `stepDescription` Default function.
- **pollInstanceStatus variadic target**: Accepts optional `expectStatus` -- action commands pass the expected status, create passes none (polls until any terminal status).
- **Delete does NOT poll** -- `action.Execute` is nil for delete, handled by `runDeleteFlow` which returns after the API call.
- **SSH key / startup script inline creation**: These wizard steps create resources via API during the wizard, not deferred to instance creation.
- **Cluster images filtered out**: `stepImage` skips images where `IsCluster` is true.
- **Agent-mode missing flags checked before template application**: `missingCreateFlags` runs before `resolveCreateInputs`, so `--from` alone cannot satisfy required flags in agent mode.
- **Agent mode never waits by default**: `--wait`'s default is locked in at flag registration, before `--agent` is parsed (the factory is built during command-tree construction), so `runCreate` and the `vm action` agent branch apply the override at runtime: they return after issuance with `status: "accepted"` unless `--wait` was passed explicitly, in which case they poll via `cmdutil.PollInstanceStatus` and report `completed` (a failed transition is an error). MCP `vm_action` shares this accepted/completed contract.
- **Delete volume semantics are explicit, never nil**: interactive single delete, agent single delete, and batch delete all pass an explicit `[]string{}` when no volumes should die — nil `volume_ids` invokes the API default of deleting the OS volume, contradicting the "unselected keeps billing" warning. Agent single delete mirrors batch exactly: `--yes` required, volumes only with `--with-volumes`, batch-shaped JSON output.
- **Template name resolution warnings**: `resolveSSHKeyNames` and `resolveStartupScriptName` now return warnings instead of silently swallowing errors.

## Relationships

- **wizard engine**: `pkg/tui/wizard` -- provides `Flow`, `Step`, `Store`, `Engine`, `Choice`, prompt types
- **tui package**: `pkg/tui` -- `Prompter`, `Status` interfaces, `WithDefault`, `WithConfirmDefault`, `WithEditorDefault`, `WithFileExt`, `WithMultiSelectDefaults` options
- **bubbletea package**: `pkg/tui/bubbletea` -- `HintStyle()` for wizard hints
- **SDK**: `verdacloud-sdk-go/pkg/verda` -- all API client types, constants (`LocationFIN01`, `VolumeTypeNVMe`, `VolumeTypeHDD`, `SpotDiscontinue*`, `Status*`)
- **cmdutil**: `cmd/util` -- `Factory`, `IOStreams`, `WithSpinner`, `RunWithSpinner`, `TemplatesBaseDir`, `DebugJSON`, `UsageErrorf`, `ValidateHostname`, `GenerateHostname`, `LongDesc`, `Examples`, `DefaultSubCommandRun`
- **Factory dependencies**: `f.VerdaClient()`, `f.Prompter()`, `f.Status()`, `f.Debug()`, `f.Options().Timeout`, `f.OutputFormat()`, `f.AgentMode()`
