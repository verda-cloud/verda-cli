---
name: update-command-knowledge
description: Auto-detect changed subcommand dirs from staged git changes and regenerate their README.md + CLAUDE.md knowledge docs
---

# Update Command Knowledge

This skill generates and updates per-subcommand knowledge docs (README.md + CLAUDE.md) in each `internal/verda-cli/cmd/*/` directory. It can be invoked headlessly via `claude -p "/update-command-knowledge"` or interactively via `/update-command-knowledge`.

## Step 1: Determine which command directories to process

If the argument contains `--all`, process ALL command directories:
- auth, vm, sshkey, startupscript, volume, settings, version, update

Otherwise, detect changed dirs from staged files:

1. Run `git diff --cached --name-only` using the Bash tool
2. From the output, extract unique directory names matching the pattern `internal/verda-cli/cmd/<dir>/` where `<dir>` is not `util`
3. Exclude any matches that are just files in `internal/verda-cli/cmd/` itself (like `cmd.go`, `helper.go`) — only include actual subdirectories

If no directories are found, print exactly:
> No command directories with staged changes. Nothing to update.

Then stop. Do not generate any files.

## Step 2: For each affected directory, read and analyze the source

For each directory `internal/verda-cli/cmd/<domain>/`:

1. Use the Glob tool to find all `*.go` files in the directory
2. Use the Read tool to read every `.go` file found
3. Analyze the code to extract:
   - **Parent command**: The `Use`, `Aliases`, and `Short` fields from the parent cobra command (usually in `<domain>.go`)
   - **Subcommands**: Each subcommand's `Use`, `Short`, `Long`, `Example`, and flags (from `cmd.Flags()` calls)
   - **API calls**: SDK client methods used (e.g., `client.VirtualMachines.List`, `client.SSHKeys.Create`)
   - **Business logic**: Pricing calculations, status mappings, validation rules, formatting
   - **Wizard flows**: If a `wizard.go` exists, document the steps, `clientFunc`, `apiCache` usage
   - **Interactive vs non-interactive**: Which flags allow non-interactive mode, what gets prompted when missing

Do NOT guess or infer — extract all information directly from the source code.

## Step 3: Generate README.md

For each directory, generate `internal/verda-cli/cmd/<domain>/README.md` using the Write tool.

If a README.md already exists in the directory, read it first with the Read tool before generating — you will need to preserve manually-added sections.

Follow this template:

```markdown
# verda <domain> — <Short description from parent command>

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda <domain> <sub>` | Short desc | `--flag1`, `--flag2` |

## Usage Examples

### <Subcommand 1>
\`\`\`bash
# Interactive
verda <domain> <sub>

# Non-interactive
verda <domain> <sub> --flag value
\`\`\`

(Repeat for each subcommand. Use the `Example` field from cobra commands when available.)

## Interactive vs Non-Interactive

Describe which flags enable non-interactive mode, what happens when flags are missing (prompts).

## Architecture Notes

- List key files and what each does
- API endpoints/SDK methods used
- Business logic (pricing, status mappings, etc.)
- Wizard flows if applicable
```

If the old README.md had manually-added sections titled "Gotchas" or "Notes", append them at the end of the newly generated content.

## Step 4: Generate CLAUDE.md

For each directory, generate `internal/verda-cli/cmd/<domain>/CLAUDE.md` using the Write tool.

If a CLAUDE.md already exists in the directory, read it first with the Read tool before generating — you will need to preserve manually-added gotchas.

Follow this template:

```markdown
# <Domain> Command Knowledge

## Quick Reference
- Parent: `verda <domain>` (aliases: <aliases or "none">)
- Subcommands: <comma-separated list>
- Files: list each .go file and a brief description of its role

## Domain-Specific Logic
- Pricing calculations, status mappings, validation rules
- Any non-obvious business rules extracted from the code

## Gotchas & Edge Cases
- Things that aren't obvious from reading the code
- Common mistakes when modifying this command

## Relationships
- Dependencies on other packages (util, SDK types, wizard engine)
- Shared state (apiCache, clientFunc patterns)
```

If the old CLAUDE.md had manually-added items in the "Gotchas & Edge Cases" section, append them below any auto-detected gotchas (avoid exact duplicates).

## Step 5: Stage the updated docs

After all files are written, run a single `git add` command using the Bash tool to stage all the README.md and CLAUDE.md files that were created or updated. For example:

```bash
git add internal/verda-cli/cmd/vm/README.md internal/verda-cli/cmd/vm/CLAUDE.md internal/verda-cli/cmd/sshkey/README.md internal/verda-cli/cmd/sshkey/CLAUDE.md
```

## Important rules

- Use the Write tool to create/update doc files — do NOT use Bash (echo/cat) for file creation
- Use Bash only for `git diff --cached --name-only` and `git add`
- Read ALL .go files in a directory before generating docs — accuracy matters
- If a directory has no .go files or no parent command definition, skip it and note why
- Do not modify any .go source files — this skill only creates/updates README.md and CLAUDE.md
- Do not create docs for `internal/verda-cli/cmd/util/` — always skip this directory
