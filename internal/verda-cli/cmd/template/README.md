# verda template -- Manage reusable resource templates

Save, list, show, edit, and delete reusable resource configuration templates. Templates pre-fill the `vm create` wizard so you don't repeat the same settings.

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda template create [name]` | Interactive wizard to save a template | _(none)_ |
| `verda template edit [resource/name]` | Edit specific fields of a template | _(none)_ |
| `verda template list` | List all saved templates | `--type` |
| `verda template show [resource/name]` | Display template details | `-o json` |
| `verda template delete [resource/name]` | Delete a template (with confirmation) | _(none)_ |

Aliases: `verda tmpl`, `verda tmpl ls` (list), `verda tmpl rm` (delete)

All commands with `[resource/name]` show an interactive picker when the argument is omitted.

## Usage Examples

### Create

```bash
# Interactive (prompts for name and runs VM wizard)
verda template create

# Create a template with a specific name
verda template create gpu-training
```

The create command runs the VM wizard in **template mode** -- the same 10 configuration steps (billing type through startup script) but without hostname, description, or confirm-deploy. In template mode, instance types come from the instance-types API (not filtered by availability) and location is optional ("None — decide at deploy time"). The resulting settings are saved to disk.

### Edit

```bash
# Interactive picker, then field menu
verda template edit

# Edit a specific template
verda template edit vm/gpu-training
```

Shows a menu of all template fields with their current values. Pick a field to change, edit it with the appropriate prompt (static choices for simple fields, API-backed selection for instance type/location/image/SSH keys/startup script). Location includes a "None (decide at deploy time)" option to clear the value. Repeat until "Save & exit".

### List

```bash
# List all templates
verda template list

# List only VM templates
verda template list --type vm

# JSON output
verda template list -o json
```

Output shows `NAME` (as `resource/name`) and an auto-generated `DESCRIPTION` built from instance type, image, and location.

### Show

```bash
# Interactive picker
verda template show

# Show a VM template
verda template show vm/gpu-training

# Output as JSON
verda template show vm/gpu-training -o json
```

Displays all template fields including hostname pattern, storage skip, and startup script skip status. Unset fields show `-`, explicitly skipped fields show `None (skipped)`.

### Delete

```bash
# Interactive picker with confirmation
verda template delete

# Delete a specific template
verda template delete vm/gpu-training
```

## Template Storage

- Files stored at `~/.verda/templates/<resource>/<name>.yaml`
- Base directory resolved via `cmdutil.TemplatesBaseDir()`
- Organized by resource type subdirectory (currently only `vm/`)
- Names must be lowercase alphanumeric with hyphens, no trailing hyphens (regex: `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
- Auto-reformats invalid names: spaces and underscores become hyphens, uppercase becomes lowercase, other invalid characters are stripped, consecutive hyphens are collapsed
- Atomic writes: saves to `.tmp` file then renames to prevent corruption

## Template YAML Format

A complete example showing all supported fields:

```yaml
resource: vm
billing_type: on-demand          # on-demand or spot
contract: PAY_AS_YOU_GO
kind: GPU                        # GPU or CPU
instance_type: 1V100.6V
location: FIN-01                  # optional — omit to prompt at deploy time
image: ubuntu-24.04-cuda-12.8     # stored by name, resolved to ID at deploy time
os_volume_size: 200              # GiB
storage:
  - type: NVMe
    size: 500
storage_skip: true               # explicitly skip additional storage
ssh_keys:
  - milek                        # by name, resolved to ID at create time
startup_script: setup-training   # by name, resolved to ID at create time
startup_script_skip: true        # explicitly skip startup script
hostname_pattern: "gpu-{random}-{location}"  # auto-generate hostnames
```

All fields except `resource` are optional; omitted fields are left for the wizard to prompt.

## Using Templates with `vm create`

```bash
verda vm create --from gpu-training              # load by name
verda vm create --from ./my-template.yaml        # load from file path
verda vm create --from                           # pick from list (interactive)
verda vm create --from gpu-training --hostname my-vm --description "test"

# Override template values with flags
verda vm create --from gpu-training --location FIN-03
verda vm create --from gpu-training --hostname my-vm --os-volume-size 200
```

Flags passed alongside `--from` override the template values.

### Flow

1. Template values pre-fill the wizard's `createOptions` (CLI flags take precedence — they are parsed first)
2. A summary of template values is printed to stderr
3. SSH keys and startup scripts are resolved by name to ID via the API; unresolved names produce warnings (no longer silently swallowed)
4. Only unfilled steps are prompted (hostname, description, confirm-deploy are always prompted; other steps only if the template didn't fill them)
5. The confirm-deploy step fetches pricing for the deployment summary (via `ensurePricingCache` if earlier pricing steps were skipped)

### Template Resolution

The `--from` flag uses `NoOptDefVal` so it can be used in three ways:

- `--from gpu-training` -- resolves as a template name in `~/.verda/templates/vm/`
- `--from ./path/to/template.yaml` -- treated as a file path (contains `/` or ends with `.yaml`)
- `--from` (no value) -- shows an interactive picker of saved VM templates

## Hostname Pattern

The `hostname_pattern` field supports two placeholders:

- `{random}` -- replaced with 3 random petname words joined by hyphens (e.g., `cold-cable-smiles`). Each `{random}` occurrence generates different words.
- `{location}` -- replaced with the lowercased location code (e.g., `fin-01`)

Example: `"gpu-{random}-{location}"` expands to something like `"gpu-cold-cable-smiles-fin-01"`.

The pattern is expanded only when `hostname_pattern` is set and no explicit `--hostname` flag is provided.

## Skip Flags

Templates can explicitly mark steps as skipped so the wizard does not re-ask:

- **`storage_skip: true`** -- skip the additional storage step entirely (do not prompt for NVMe/HDD volumes)
- **`startup_script_skip: true`** -- skip the startup script step entirely (do not prompt for a script)

These are captured when the user selects "None (skip)" during template creation and prevent the wizard from treating the empty value as "not yet filled."

## Architecture Notes

### Files

- **template.go** -- Parent command definition (`verda template`), registers subcommands
- **create.go** -- `template create` command; prompts for resource type and name, runs VM wizard in template mode, saves result. Contains `normalizeName`, `vmResultToTemplate`
- **edit.go** -- `template edit` command; field menu loop with API-backed editors for instance type, location, image, SSH keys, startup script. Contains `matchKind` for filtering
- **list.go** -- `template list` command; lists entries with auto-description, supports `--type` filter and structured output
- **show.go** -- `template show` command; displays all fields (including skip flags and hostname pattern), supports `-o json`. Contains shared `pickTemplateEntry` helper and `parseRef`
- **delete.go** -- `template delete` command; loads template to verify existence, confirms, then deletes
- **types.go** -- Re-exports types and functions from `internal/verda-cli/template/` to avoid import cycles

### Shared Package

- **`internal/verda-cli/template/template.go`** -- Core types (`Template`, `StorageSpec`, `Entry` with JSON tags), I/O functions (`Save` with atomic write, `Load`, `LoadFromPath`, `Resolve`, `List`, `ListAll`, `Delete`), name validation (`ValidateName`), hostname pattern expansion (`ExpandHostnamePattern`)

### Integration with `vm create`

- **`cmd/vm/create.go`** -- Defines `--from` flag, calls `resolveCreateInputs`
- **`cmd/vm/template_apply.go`** -- `loadTemplateRef`, `applyTemplate`, `resolveTemplateNames` (with warnings to `ioStreams.ErrOut`), `printTemplateSummary`
- **`cmd/vm/wizard.go`** -- `WizardMode`, `RunTemplateWizard`, `TemplateResult`, step Default functions
- **`cmd/vm/wizard_cache.go`** -- `ensurePricingCache` (accepts parent context)

### Shared Helpers

- **`cmdutil.TemplatesBaseDir`** (`cmd/util/paths.go`) -- Centralized `~/.verda/templates` path
- **`cmdutil.WithSpinner[T]`** (`cmd/util/spinner.go`) -- Used by edit command for API-backed field editors
- **`pickTemplateEntry`** (`show.go`) -- Shared interactive template picker, used by show, delete, and edit commands
