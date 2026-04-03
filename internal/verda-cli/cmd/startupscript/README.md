# verda startup-script -- Manage startup scripts

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda startup-script list` | List all startup scripts (Name, ID, Created) | _(none)_ |
| `verda startup-script add` | Add a startup script | `--name`, `--file`, `--script` |
| `verda startup-script delete` | Delete a startup script | `--id` |

## Usage Examples

### List
```bash
verda startup-script list
verda startup-script ls
```

### Add
```bash
# Interactive
verda startup-script add

# From file
verda startup-script add --name setup --file ./init.sh

# Inline script
verda startup-script add --name setup --script "#!/bin/bash\napt update"
```

### Delete
```bash
# Interactive (select from list, then confirm)
verda startup-script delete

# Non-interactive
verda startup-script delete --id abc-123
```

## Interactive vs Non-Interactive

| Command | Non-interactive flags | Prompted when missing |
|---------|----------------------|----------------------|
| `add` | `--name`, `--file` or `--script` | Name via text input; script source via select ("Load from file" / "Paste content") |
| `delete` | `--id` | Fetches all scripts, presents select list, then confirms |
| `list` | _(always non-interactive)_ | N/A |

All destructive actions (`delete`) require confirmation even in non-interactive mode.

## Architecture Notes

- **startupscript.go** -- Parent command registration; wires `list`, `add`, `delete` subcommands.
- **list.go** -- Calls `client.StartupScripts.GetAllStartupScripts()`, prints tabular output (Name, ID, CreatedAt formatted as `2006-01-02 15:04`).
- **add.go** -- Three script input modes: `--file` (reads from disk via `os.ReadFile`), `--script` (inline string), or interactive (select file path or use TUI editor with `.sh` extension and bash template default). Calls `client.StartupScripts.AddStartupScript()` with `CreateStartupScriptRequest`.
- **delete.go** -- In interactive mode, fetches all scripts to build a select list with a "Cancel" option. Calls `client.StartupScripts.DeleteStartupScript()` after confirmation.
- All API calls use `context.WithTimeout` and show a spinner via `f.Status()`.
- Debug output via `cmdutil.DebugJSON` on `ioStreams.ErrOut`.
