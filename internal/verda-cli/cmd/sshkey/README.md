# verda ssh-key -- Manage SSH keys

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda ssh-key list` | List all SSH keys (Name, ID, Fingerprint) | _(none)_ |
| `verda ssh-key add` | Add an SSH key to your account | `--name`, `--public-key` |
| `verda ssh-key delete` | Delete an SSH key from your account | `--id` |

## Usage Examples

### List
```bash
verda ssh-key list
verda ssh-key ls
```

### Add
```bash
# Interactive
verda ssh-key add

# Non-interactive
verda ssh-key add --name my-key --public-key "ssh-ed25519 AAAA..."
```

### Delete
```bash
# Interactive (select from list, then confirm)
verda ssh-key delete

# Non-interactive
verda ssh-key delete --id abc-123
```

## Interactive vs Non-Interactive

| Command | Non-interactive flags | Prompted when missing |
|---------|----------------------|----------------------|
| `add` | `--name`, `--public-key` | Name via text input, public key via text input |
| `delete` | `--id` | Fetches all keys, presents select list, then confirms |
| `list` | _(always non-interactive)_ | N/A |

All destructive actions (`delete`) require confirmation even in non-interactive mode.

## Architecture Notes

- **sshkey.go** -- Parent command registration; wires `list`, `add`, `delete` subcommands.
- **list.go** -- Calls `client.SSHKeys.GetAllSSHKeys()`, prints tabular output (Name, ID, Fingerprint).
- **add.go** -- Collects name and public key (flags or prompts), calls `client.SSHKeys.AddSSHKey()` with `CreateSSHKeyRequest`.
- **delete.go** -- In interactive mode, fetches all keys to build a select list with a "Cancel" option. Calls `client.SSHKeys.DeleteSSHKey()` after confirmation.
- All API calls use `context.WithTimeout` and show a spinner via `f.Status()`.
- Debug output via `cmdutil.DebugJSON` on `ioStreams.ErrOut`.
- SDK types from `github.com/verda-cloud/verdacloud-sdk-go/pkg/verda`.
