# Template Command Knowledge

## Quick Reference
- Parent: `verda template` (alias: `tmpl`)
- Subcommands: `create`, `list` (alias `ls`), `show`, `delete` (alias `rm`)
- Files:
  - `template.go` -- Parent command, registers subcommands
  - `create.go` -- Create command, name validation, runs VM wizard in template mode
  - `list.go` -- List command with `--type` filter and structured output
  - `show.go` -- Show command with field display and structured output
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
| `kind` | string | `"GPU"` or `"CPU"` |
| `instance_type` | string | e.g. `"1V100.6V"` |
| `location` | string | e.g. `"FIN-01"` |
| `image` | string | OS image slug |
| `os_volume_size` | int | GiB |
| `storage` | []StorageSpec | Each has `type` and `size` |
| `storage_skip` | bool | Skip storage step in wizard |
| `ssh_keys` | []string | Key **names** (not IDs) |
| `startup_script` | string | Script **name** (not ID) |
| `startup_script_skip` | bool | Skip startup script step in wizard |
| `hostname_pattern` | string | Pattern with `{random}` and `{location}` placeholders |

### Name Validation and Auto-Reformatting
- Valid names match `^[a-z0-9][a-z0-9-]*$`
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
- `{location}` -> lowercased location code (e.g. `"FIN-01"` -> `"fin-01"`)
- Only expanded when `hostname_pattern` is set AND `opts.Hostname` is empty (no `--hostname` flag)

### SSH Keys and Startup Scripts
- Stored in template by **name**, not ID
- Resolved to IDs at `vm create --from` time via `resolveSSHKeyNames()` and `resolveStartupScriptName()`
- On API error or name not found, produces a warning and the wizard prompts later
- Names are stored in `opts.sshKeyNames` / `opts.startupScriptName` for template-saving round-trip

## Gotchas & Edge Cases

- **Import cycle**: `cmd/template/` cannot import `cmd/vm/` for the Template type (circular dependency). Shared types live in `internal/verda-cli/template/`, re-exported by `cmd/template/types.go` via type aliases and `var` bindings.
- **`billingTypeSet` / `locationSet` flags**: Needed because `IsSet` in the wizard can't distinguish `"on-demand"` (falsy `IsSpot=false`) from "unset". When a template sets billing type or location, these booleans are set to `true` so the wizard skips those steps.
- **`NoOptDefVal` on `--from` flag**: Set to `" "` (space) so `--from` without a value is recognized as "flag changed but empty". When the user writes `verda vm create --from gpu-training`, cobra parses `gpu-training` as a positional arg; `RunE` recombines it into `opts.From`.
- **Startup script "None (skip)" label**: The wizard presents "None (skip)" as a selectable option. Previously, this label text was captured as the script name. Fixed by checking `Value != ""` before storing the name.
- **`ensurePricingCache`**: The confirm-deploy step calls this to fetch instance type and volume type pricing when the cache is empty. This happens when a template pre-filled earlier steps (skipping the steps that normally populate the cache).
- **Only first storage entry applied**: `applyTemplate()` only reads `tmpl.Storage[0]` because the wizard's convenience fields (`StorageSize`/`StorageType`) support a single additional volume.
- **AutoDescription**: `Template.AutoDescription()` joins non-empty `InstanceType`, `Image`, and `Location` with `", "` for the list view.
- **Directory permissions**: Template directories created with `0700`, files with `0644`.
- **Non-existent directory**: `List()` and `ListAll()` return `nil, nil` (not an error) when the templates directory doesn't exist yet.

## Relationships

- **`internal/verda-cli/template/`** -- Shared types (`Template`, `StorageSpec`, `Entry`) and I/O functions (`Save`, `Load`, `LoadFromPath`, `Resolve`, `List`, `ListAll`, `Delete`, `ValidateName`, `ExpandHostnamePattern`). Breaks the import cycle between `cmd/template/` and `cmd/vm/`.
- **`cmd/vm/wizard.go`** -- `WizardMode` (Deploy vs Template), `RunTemplateWizard()` (runs wizard without hostname/description/confirm steps), `TemplateResult` struct, `ensurePricingCache()`
- **`cmd/vm/template_apply.go`** -- `loadTemplateRef()`, `applyTemplate()`, `resolveTemplateNames()`, `resolveSSHKeyNames()`, `resolveStartupScriptName()`, `printTemplateSummary()`, `pickTemplate()`
- **`cmd/vm/create.go`** -- `--from` flag definition, `resolveCreateInputs()` orchestrates template loading + wizard invocation, `createOptions` struct with template-related internal fields (`billingTypeSet`, `locationSet`, `storageSkip`, `startupScriptSkip`, `sshKeyNames`, `startupScriptName`)
- **`cmdutil`** -- `Factory`, `IOStreams`, `LongDesc`, `Examples`, `DefaultSubCommandRun`, `WriteStructured`
- **`clioptions`** -- `VerdaDir()` for resolving `~/.verda/` base path
- **`petname`** -- `github.com/dustinkirkland/golang-petname` for `{random}` hostname expansion
