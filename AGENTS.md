# AI Agent Guidelines for Verda CLI

This document provides instructions for AI coding agents (Claude Code, Codex, etc.) working on this codebase.

## Before You Start

1. Read `CLAUDE.md` for project overview, build commands, and architecture
2. Read `.ai/skills/new-command.md` when creating or modifying CLI commands
3. Run `go build ./...` and `go test ./...` to verify your changes compile and pass

## Skills

Skills are structured guides in `.ai/skills/` that document patterns and checklists:

| Skill | When to Use |
|-------|-------------|
| [new-command.md](.ai/skills/new-command.md) | Adding a new subcommand or modifying an existing one |

## Key Rules

### Always

- Support `--debug` flag on every command that calls the API (use `cmdutil.DebugJSON`)
- Wrap API calls with spinner + timeout context
- Support both interactive (prompts) and non-interactive (flags) modes
- Add confirmation for destructive actions
- Register new commands in their parent command file and `cmd/cmd.go` if new domain
- Run `go build ./...` and `go test ./...` before finishing

### Never

- Skip the `--debug` output pattern
- Make API calls without a timeout context
- Write to `ioStreams.Out` for non-data output (use `ioStreams.ErrOut` for prompts/warnings/debug)
- Forget to handle user cancellation (Esc/Ctrl+C returns nil error)
- Use lipgloss v1 imports (`github.com/charmbracelet/lipgloss`) -- use `charm.land/lipgloss/v2`
- Use bubbletea v1 imports -- use `charm.land/bubbletea/v2`

### Dependencies

When modifying `verdagostack` (local replace):
- Bubble Tea v2 changed `tea.KeyMsg` to `tea.KeyPressMsg`
- `KeyPressMsg.String()` returns `"space"` not `" "` for space key
- `KeyPressMsg.String()` returns `"enter"`, `"esc"`, `"backspace"`, `"tab"` for special keys
- Wizard API uses `ViewDef` (not `RegionDef`), `NewProgressView` (not `NewProgressRegion`)

## Project Layout

```
cmd/verda/main.go                          # Entrypoint
internal/verda-cli/
  cmd/cmd.go                               # Root command, command groups
  cmd/util/                                # Factory, IOStreams, helpers, hostname
  cmd/vm/                                  # VM: create, list, action, wizard, status_view
  cmd/auth/                                # Auth: login, show, use, wizard
  cmd/sshkey/                              # SSH keys: list, add, delete
  cmd/startupscript/                       # Startup scripts: list, add, delete
  cmd/volume/                              # Volumes: list, create, action, trash
  options/                                 # Global options, credentials
```
