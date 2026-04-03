# Startup Script Command Knowledge

## Quick Reference
- Parent: `verda startup-script`
- Subcommands: `list` (alias `ls`), `add`, `delete` (alias `rm`)
- Files:
  - `startupscript.go` -- Parent command constructor, registers subcommands
  - `list.go` -- List all startup scripts in tabular format
  - `add.go` -- Add a new startup script (interactive, from file, or inline)
  - `delete.go` -- Delete a startup script with interactive selection and confirmation

## Domain-Specific Logic
- `list` displays columns: NAME (20-char), ID (36-char UUID), CREATED (formatted `2006-01-02 15:04`)
- `add` has three input modes resolved in this priority: `--file` > `--script` > interactive prompt
- Interactive add offers two source options: "Load from file" (text input for path) or "Paste content" (TUI editor with `WithEditorDefault("#!/bin/bash\n\n# Your startup script here\n")` and `WithFileExt(".sh")`)
- `add` validates that final content is non-empty after trimming whitespace
- Uses `verda.CreateStartupScriptRequest{Name, Script}` and calls `client.StartupScripts.AddStartupScript()`
- `delete` selection labels show `Name  ID` (no fingerprint, unlike ssh-key which shows fingerprint too)

## Gotchas & Edge Cases
- In `add`, prompter errors return `nil` (not the error) -- intentional for Ctrl+C cancellation
- Same cancellation pattern in `delete`
- `add` imports `os` for `ReadFile` and `strings` for `TrimSpace` -- the only command in this package that reads files from disk
- `add` imports `github.com/verda-cloud/verdagostack/pkg/tui` for `tui.WithEditorDefault` and `tui.WithFileExt` editor options
- `delete` interactive mode uses two separate timeout contexts: one for listing, another for deleting
- When no scripts exist, both `list` and `delete` print a friendly message and return `nil`

## Relationships
- Depends on `cmdutil.Factory` for VerdaClient, Prompter, Status, Debug, Options
- Depends on `cmdutil.IOStreams` for output routing
- SDK dependency: `github.com/verda-cloud/verdacloud-sdk-go/pkg/verda` (in `add.go` for `CreateStartupScriptRequest`)
- TUI dependency: `github.com/verda-cloud/verdagostack/pkg/tui` (in `add.go` for editor options)
- Uses `cmdutil.LongDesc`, `cmdutil.Examples`, `cmdutil.DebugJSON`, `cmdutil.DefaultSubCommandRun`
