# SSH Key Command Knowledge

## Quick Reference
- Parent: `verda ssh-key`
- Subcommands: `list` (alias `ls`), `add`, `delete` (alias `rm`)
- Files:
  - `sshkey.go` -- Parent command constructor, registers subcommands
  - `list.go` -- List all SSH keys in tabular format
  - `add.go` -- Add a new SSH key (interactive or flag-driven)
  - `delete.go` -- Delete an SSH key with interactive selection and confirmation

## Domain-Specific Logic
- `list` displays columns: NAME (20-char), ID (36-char UUID), FINGERPRINT
- `list` marshals `cmdutil.SSHKeyView`, never the raw SDK struct. `/v1/ssh-keys`
  omits `created_at` and sends `fingerprint: null`; marshaling the SDK type turns
  those into `0001-01-01T00:00:00Z` and `""`. A zero timestamp is not harmless —
  an age-based reaper (`older than 2h ⇒ delete`) reads it as ancient and deletes
  keys belonging to running jobs. Absent is safe, wrong is dangerous.
- The same view type backs the MCP `list_ssh_keys` / `add_ssh_key` tools, so the
  CLI and agent JSON contracts cannot drift.
- `add` uses `verda.CreateSSHKeyRequest{Name, PublicKey}` and calls `client.SSHKeys.AddSSHKey()`
- `delete` interactive mode fetches all keys via `GetAllSSHKeys()` to build the selection list, appends a "Cancel" entry at the end
- Deletion always requires confirmation via `prompter.Confirm()`, even when `--id` is provided directly

## Gotchas & Edge Cases
- Table output renders an absent fingerprint as `-` (`cmdutil.TextColumn`), never
  a blank column the eye slides over.
- View tags carry both `json:` and `yaml:` — the encoder is `go.yaml.in/yaml/v3`,
  which ignores json tags and would emit `createdat`.
- Backend gap, still open: `/v1/ssh-keys` does not return `created_at`. The CLI
  fix hides the damage; it does not supply the data. Remove this note when the
  API populates the field.
- In `add`, if the prompter returns an error (e.g., user presses Ctrl+C), `runAdd` returns `nil` (not the error) -- this is intentional to avoid printing error messages on user cancellation
- Same cancellation pattern in `delete` -- prompter errors return `nil`
- `delete` interactive mode uses two separate timeout contexts: one for listing keys, another for the delete call
- When no keys exist, both `list` and `delete` print "No SSH keys found." and return `nil` (not an error)

## Relationships
- Depends on `cmdutil.Factory` for VerdaClient, Prompter, Status, Debug, Options
- Depends on `cmdutil.IOStreams` for output routing
- SDK dependency: `github.com/verda-cloud/verdacloud-sdk-go/pkg/verda` (only in `add.go` for `CreateSSHKeyRequest`)
- Uses `cmdutil.LongDesc`, `cmdutil.Examples`, `cmdutil.DebugJSON`, `cmdutil.DefaultSubCommandRun`
