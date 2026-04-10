# Template Command Knowledge

## Quick Reference
- Parent: `verda template` (alias: `tmpl`)
- Subcommands: `create`, `edit`, `list` (alias `ls`), `show`, `delete` (alias `rm`)
- Files:
  - `template.go` -- Parent command, registers subcommands
  - `create.go` -- Create command, name validation, runs VM wizard in template mode
  - `edit.go` -- Edit command, field menu with API-backed editors, `matchKind`
  - `list.go` -- List command with `--type` filter and structured output
  - `show.go` -- Show command with full field display, `pickTemplateEntry`, `parseRef`
  - `delete.go` -- Delete command with confirmation prompt
  - `types.go` -- Re-exports types/functions from shared `internal/verda-cli/template/`

## Domain-Specific Logic

### Template YAML Format
All fields except `resource` are optional. Stored at `~/.verda/templates/<resource>/<name>.yaml`.

| Field | Type | Description |
|-------|------|-------------|
| `resource` | string | Resource type, currently only `"vm"` |
| `billing_type` | string | `"on-demand"` or `"spot"` |
| `contract` | string | `"PAY_AS_YOU_GO"`, `"SPOT"`, `"LONG_TERM"` |
| `kind` | string | `"GPU"` or `"CPU"` (lowercase in template, case-insensitive in matching) |
| `instance_type` | string | e.g. `"1V100.6V"` |
| `location` | string | Optional. e.g. `"FIN-01"`. If omitted, wizard prompts at deploy time |
| `image` | string | OS image **name** (resolved to ID at deploy time) |
| `os_volume_size` | int | GiB |
| `storage` | []StorageSpec | Each has `type` and `size` |
| `storage_skip` | bool | Skip storage step in wizard |
| `ssh_keys` | []string | Key **names** (not IDs) |
| `startup_script` | string | Script **name** (not ID) |
| `startup_script_skip` | bool | Skip startup script step in wizard |
| `hostname_pattern` | string | Pattern with `{random}` and `{location}` placeholders |

### Name Validation and Auto-Reformatting
- Valid names match `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$` (no trailing hyphens)
- `normalizeName()` auto-formats: lowercase, replace spaces/underscores with hyphens, strip invalid chars, collapse consecutive hyphens, trim leading/trailing hyphens
- Create command re-prompts on invalid names and on name collisions with existing templates

### Template Resolution (--from flag)
- If ref contains `"/"` or ends with `".yaml"` -> treated as a file path
- Otherwise -> resolved as `~/.verda/templates/vm/<ref>.yaml`
- Empty ref (bare `--from`) -> interactive picker via `pickTemplate()`
- Resolution logic lives in `internal/verda-cli/template/template.go` `Resolve()`

### Skip Flags
- `storage_skip: true` -> wizard skips additional storage step entirely
- `startup_script_skip: true` -> wizard skips startup script step entirely
- Captured when user selects "None (skip)" during template creation wizard
- Maps to `opts.storageSkip` and `opts.startupScriptSkip` in `createOptions`

### Hostname Pattern Expansion
- `{random}` -> 3 petname words joined by hyphens (via `github.com/dustinkirkland/golang-petname`)
- Each `{random}` occurrence generates different words (uses `strings.Replace` with count=1 in a loop)
- `{location}` -> lowercased location code (e.g. `"FIN-01"` -> `"fin-01"`)
- Only expanded when `hostname_pattern` is set AND `opts.Hostname` is empty (no `--hostname` flag)

### SSH Keys and Startup Scripts
- Stored in template by **name**, not ID
- Resolved to IDs at `vm create --from` time via `resolveSSHKeyNames()` and `resolveStartupScriptName()`
- On API error or name not found, produces a warning to `ioStreams.ErrOut` (no longer silently swallowed)
- Names are stored in `opts.sshKeyNames` / `opts.startupScriptName` for template-saving round-trip

## Edit Command

The `template edit` command uses a field menu approach (not the full wizard):

1. Load existing template, display "Editing template: resource/name"
2. Show menu of all editable fields with current values
3. User picks a field to change
4. Run appropriate editor for that field:
   - **Simple fields** (billing type, kind, OS volume size, hostname pattern): inline prompts
   - **API-backed fields** (instance type, location, image, SSH keys, startup script): fetch choices from API with spinner, show select/multi-select
5. Return to menu — repeat until "Save & exit"
6. Save template to disk (atomic write)

### Edit field editors
- **Billing Type**: static select (on-demand / spot). Clears contract when switching to spot.
- **Kind**: static select (gpu / cpu)
- **Instance Type**: API call to `InstanceTypes.Get` (not availability), filtered by current kind, shows price
- **Location**: API call to `Locations.Get`, includes "None (decide at deploy time)" to clear location
- **Image**: API call to `Images.Get`, excludes cluster images
- **OS Volume Size**: text input with current value as default
- **SSH Keys**: API call to `SSHKeys.GetAllSSHKeys`, multi-select with current keys pre-selected
- **Startup Script**: API call to `StartupScripts.GetAllStartupScripts`, includes "None (clear)" option
- **Hostname Pattern**: text input with placeholder hint `{random}-{location}`

## Show Command

Displays all template fields, including those previously hidden:
- Fields with empty values show `-`
- `storage_skip: true` shows `None (skipped)`
- `startup_script_skip: true` shows `None (skipped)`
- `hostname_pattern` always displayed
- Wider label column (`%-18s`) for alignment

## Interactive Picker

`pickTemplateEntry` (in `show.go`) is shared by show, delete, and edit:
- Calls `ListAll(baseDir)` to get templates across all resource types
- Shows select prompt with `resource/name` + description
- Returns `nil` on user cancel (Ctrl+C)
- Returns error if no templates found

## Gotchas & Edge Cases

- **Import cycle**: `cmd/template/` cannot import `cmd/vm/` for the Template type (circular dependency). Shared types live in `internal/verda-cli/template/`, re-exported by `cmd/template/types.go` via type aliases and `var` bindings.
- **`billingTypeSet` / `locationSet` flags**: Needed because `IsSet` in the wizard can't distinguish `"on-demand"` (falsy `IsSpot=false`) from "unset". When a template sets billing type or location, these booleans are set to `true` so the wizard skips those steps.
- **Template without location triggers wizard**: When `--from` is used and the template has no location (`!opts.locationSet`), `resolveCreateInputs` triggers the wizard so the user is prompted for location instead of silently defaulting to FIN-01.
- **Template instance type/location use different APIs**: Template wizard (create) uses instance-types API and locations API directly. Deploy wizard uses availability API to filter. Template edit also uses instance-types and locations APIs directly.
- **Template error message**: `Resolve()` now shows `template name is required — template "X" not found` with guidance to run `verda template list` or use `--from` interactively.
- **`NoOptDefVal` on `--from` flag**: Set to `" "` (space) so `--from` without a value is recognized as "flag changed but empty". When the user writes `verda vm create --from gpu-training`, cobra parses `gpu-training` as a positional arg; `RunE` recombines it into `opts.From`.
- **Startup script "None (skip)" label**: The wizard presents "None (skip)" as a selectable option. Previously, this label text was captured as the script name. Fixed by checking `Value != ""` before storing the name.
- **`ensurePricingCache`**: The confirm-deploy step calls this (with parent context, not `context.Background()`) to fetch pricing when the cache is empty from template pre-fill.
- **Only first storage entry applied**: `applyTemplate()` only reads `tmpl.Storage[0]` because the wizard's convenience fields (`StorageSize`/`StorageType`) support a single additional volume.
- **AutoDescription**: `Template.AutoDescription()` joins non-empty `InstanceType`, `Image`, and `Location` with `", "` for the list view.
- **Directory permissions**: Template directories created with `0700`, files with `0644`.
- **Non-existent directory**: `List()` and `ListAll()` return `nil, nil` (not an error) when the templates directory doesn't exist yet.
- **Atomic file writes**: `Save` writes to `.tmp` file then renames to prevent corruption on crash.
- **Entry JSON tags**: `Entry` struct has `json`/`yaml` tags for consistent lowercase keys in `-o json` output.
- **Edit matchKind**: `edit.go` has its own `matchKind` function (separate from `wizard_cache.go`'s `matchesKind`) for filtering instance types by kind in the edit field editor.

## Relationships

- **`internal/verda-cli/template/`** -- Shared types (`Template`, `StorageSpec`, `Entry`) and I/O functions (`Save`, `Load`, `LoadFromPath`, `Resolve`, `List`, `ListAll`, `Delete`, `ValidateName`, `ExpandHostnamePattern`). Breaks the import cycle between `cmd/template/` and `cmd/vm/`.
- **`cmd/vm/wizard.go`** -- `WizardMode` (Deploy vs Template), `RunTemplateWizard()`, `TemplateResult` struct
- **`cmd/vm/wizard_cache.go`** -- `ensurePricingCache()` (accepts parent context)
- **`cmd/vm/template_apply.go`** -- `loadTemplateRef()`, `applyTemplate()`, `resolveTemplateNames()` (with warnings), `resolveSSHKeyNames()`, `resolveStartupScriptName()`, `printTemplateSummary()`, `pickTemplate()`
- **`cmd/vm/create.go`** -- `--from` flag definition, `resolveCreateInputs()` orchestrates template loading + wizard invocation, `createOptions` struct with template-related internal fields
- **`cmdutil`** -- `Factory`, `IOStreams`, `WithSpinner`, `TemplatesBaseDir`, `LongDesc`, `Examples`, `DefaultSubCommandRun`, `WriteStructured`
- **`petname`** -- `github.com/dustinkirkland/golang-petname` for `{random}` hostname expansion
