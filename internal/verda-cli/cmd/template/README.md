# verda template -- Manage reusable resource templates

Save, list, show, and delete reusable resource configuration templates. Templates pre-fill the `vm create` wizard so you don't repeat the same settings.

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda template create [name]` | Interactive wizard to save a template | _(none)_ |
| `verda template list` | List all saved templates | `--type` |
| `verda template show <resource/name>` | Display template details | `-o json` |
| `verda template delete <resource/name>` | Delete a template (with confirmation) | _(none)_ |

Aliases: `verda tmpl`, `verda tmpl ls` (list), `verda tmpl rm` (delete)

## Usage Examples

### Create

```bash
# Interactive (prompts for name and runs VM wizard)
verda template create

# Create a template with a specific name
verda template create gpu-training

# Short alias
verda tmpl create my-template
```

The create command runs the VM wizard in **template mode** -- the same 10 configuration steps (billing type through startup script) but without hostname, description, or confirm-deploy. The resulting settings are saved to disk.

### List

```bash
# List all templates
verda template list

# List only VM templates
verda template list --type vm

# Short alias
verda tmpl ls
```

Output shows `NAME` (as `resource/name`) and an auto-generated `DESCRIPTION` built from instance type, image, and location.

### Show

```bash
# Show a VM template
verda template show vm/gpu-training

# Output as JSON
verda template show vm/gpu-training -o json
```

### Delete

```bash
# Delete a VM template (prompts for confirmation)
verda template delete vm/gpu-training

# Short alias
verda tmpl rm vm/gpu-training
```

## Template Storage

- Files stored at `~/.verda/templates/<resource>/<name>.yaml`
- Organized by resource type subdirectory (currently only `vm/`)
- Names must be lowercase alphanumeric with hyphens (regex: `^[a-z0-9][a-z0-9-]*$`)
- Auto-reformats invalid names: spaces and underscores become hyphens, uppercase becomes lowercase, other invalid characters are stripped, consecutive hyphens are collapsed

## Template YAML Format

A complete example showing all supported fields:

```yaml
resource: vm
billing_type: on-demand          # on-demand or spot
contract: PAY_AS_YOU_GO
kind: GPU                        # GPU or CPU
instance_type: 1V100.6V
location: FIN-01
image: ubuntu-24.04-cuda-12.8
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
```

### Flow

1. Template values pre-fill the wizard's `createOptions`
2. A summary of template values is printed to stderr
3. SSH keys and startup scripts are resolved by name to ID via the API; unresolved names produce warnings
4. Only unfilled steps are prompted (hostname, description, confirm-deploy are always prompted; other steps only if the template didn't fill them)
5. The confirm-deploy step fetches pricing for the deployment summary (via `ensurePricingCache` if earlier pricing steps were skipped)

### Template Resolution

The `--from` flag uses `NoOptDefVal` so it can be used in three ways:

- `--from gpu-training` -- resolves as a template name in `~/.verda/templates/vm/`
- `--from ./path/to/template.yaml` -- treated as a file path (contains `/` or ends with `.yaml`)
- `--from` (no value) -- shows an interactive picker of saved VM templates

When `--from` consumes no value, the template name may appear as a positional arg (e.g., `verda vm create --from gpu-training`). The `RunE` handler recombines it.

## Hostname Pattern

The `hostname_pattern` field supports two placeholders:

- `{random}` -- replaced with 3 random petname words joined by hyphens (e.g., `cold-cable-smiles`)
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
- **create.go** -- `template create` command; prompts for resource type and name, runs VM wizard in template mode, saves result
- **list.go** -- `template list` command; lists entries with auto-description, supports `--type` filter and structured output
- **show.go** -- `template show` command; displays template fields, supports `-o json` structured output
- **delete.go** -- `template delete` command; loads template to verify existence, confirms, then deletes
- **types.go** -- Re-exports types and functions from `internal/verda-cli/template/` to avoid import cycles

### Shared Package

- **`internal/verda-cli/template/template.go`** -- Core types (`Template`, `StorageSpec`, `Entry`), I/O functions (`Save`, `Load`, `LoadFromPath`, `Resolve`, `List`, `ListAll`, `Delete`), name validation, hostname pattern expansion

### Integration with `vm create`

- **`cmd/vm/create.go`** -- Defines `--from` flag, calls `resolveCreateInputs`
- **`cmd/vm/template_apply.go`** -- `loadTemplateRef`, `applyTemplate`, `resolveTemplateNames`, `printTemplateSummary`
- **`cmd/vm/wizard.go`** -- `WizardMode`, `RunTemplateWizard`, `TemplateResult`, `ensurePricingCache`
