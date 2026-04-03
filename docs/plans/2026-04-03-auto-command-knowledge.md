# Auto-Maintained Per-Subcommand Knowledge Docs

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Every subcommand directory gets a README.md (for humans) and CLAUDE.md (for AI), automatically kept in sync by a pre-commit hook that runs a smart Claude skill headlessly.

**Architecture:** A single skill (`update-command-knowledge`) handles everything — it inspects `git diff --cached` to find which `cmd/*/` dirs changed, reads the code, and regenerates the docs for only those dirs. A one-line pre-commit hook triggers it via `claude -p`. Initial generation covers all 8 existing domains.

**Tech Stack:** Claude Code CLI (headless `-p` mode), git pre-commit hook, `.ai/skills/` skill file

---

### Task 1: Define the README.md + CLAUDE.md templates

Establish what goes in each file so the skill has a clear target format.

**README.md** (human-first, GitHub-rendered):
```markdown
# verda <domain> — <short description>

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda <domain> <sub>` | ... | `--flag` |

## Usage Examples

...

## Interactive vs Non-Interactive

...

## Architecture Notes

- Key files and what they do
- API endpoints used
- Business logic / pricing rules
```

**CLAUDE.md** (AI-first, loaded by Claude Code automatically):
```markdown
# <domain> Command Knowledge

## Quick Reference
- Parent command: `verda <domain>` (aliases: ...)
- Subcommands: list, create, action, ...
- Key files: create.go, list.go, ...

## Domain-Specific Logic
- Pricing calculations, status mappings, etc.

## Gotchas & Edge Cases
- Things that aren't obvious from reading the code

## Relationships
- Which other commands/packages this domain depends on
- Shared state (apiCache, wizard engine, etc.)
```

**Step 1: No file to create yet — this is the template definition the skill will use.**

---

### Task 2: Create the skill `.ai/skills/update-command-knowledge.md`

**Files:**
- Create: `.ai/skills/update-command-knowledge.md`

**Step 1: Write the skill file**

The skill must:
1. Run `git diff --cached --name-only` to find staged files
2. Extract unique subcommand dirs from paths matching `internal/verda-cli/cmd/*/`
3. Skip `util/` (shared package, not a command)
4. For each affected dir:
   a. Read all `.go` files in the dir
   b. Analyze: parent command, subcommands, flags, aliases, API calls, business logic
   c. Generate/update `README.md` using the human template
   d. Generate/update `CLAUDE.md` using the AI template
5. Stage the updated docs with `git add`
6. If no command dirs changed, exit with a short message — no work needed

Key design decisions:
- The skill runs headlessly via `claude -p "/update-command-knowledge"` — no interactive prompts
- Uses `--model sonnet` for cost efficiency (doc generation doesn't need opus)
- The skill itself contains the template structures so it's self-contained
- Handles first-time generation (no existing docs) and updates (preserve any manually-added gotchas sections)

```markdown
---
name: update-command-knowledge
description: Auto-detect changed subcommand dirs from staged git changes and regenerate their README.md + CLAUDE.md knowledge docs
---

(full content in implementation step)
```

**Step 2: Verify skill loads**

Run: `claude -p "/update-command-knowledge" --print --model sonnet --dangerously-skip-permissions --max-budget-usd 0.50`

Expected: Skill executes, detects no staged changes (clean tree), prints "No command directories with staged changes. Nothing to update."

---

### Task 3: Create the pre-commit hook script

**Files:**
- Create: `scripts/hooks/pre-commit-update-docs`

**Step 1: Write the hook script**

```bash
#!/usr/bin/env bash
set -euo pipefail

# Check if any files under internal/verda-cli/cmd/ (excluding util/) are staged
CHANGED_CMD_FILES=$(git diff --cached --name-only -- 'internal/verda-cli/cmd/' | grep -v 'cmd/util/' | grep -v 'cmd/cmd.go' | grep -v 'cmd/helper.go' || true)

if [ -z "$CHANGED_CMD_FILES" ]; then
  exit 0
fi

echo "📝 Updating command knowledge docs for changed subcommands..."

# Run Claude headlessly with the skill — sonnet for speed/cost
claude -p "/update-command-knowledge" \
  --model sonnet \
  --dangerously-skip-permissions \
  --max-budget-usd 0.50 \
  --print

# Stage any updated/created doc files
git diff --name-only -- 'internal/verda-cli/cmd/*/README.md' 'internal/verda-cli/cmd/*/CLAUDE.md' | xargs -r git add

echo "✅ Command knowledge docs updated and staged."
```

**Step 2: Make it executable**

Run: `chmod +x scripts/hooks/pre-commit-update-docs`

**Step 3: Commit**

```bash
git add scripts/hooks/pre-commit-update-docs
git commit -m "feat: add pre-commit hook script for auto-updating command knowledge docs"
```

---

### Task 4: Create hook installation mechanism

**Files:**
- Modify: `Makefile` (or create if none exists)

**Step 1: Add a Makefile target for hook installation**

```makefile
.PHONY: install-hooks
install-hooks:
	@echo "Installing git hooks..."
	@mkdir -p .git/hooks
	@cp scripts/hooks/pre-commit-update-docs .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "✅ Pre-commit hook installed."
```

If there's already a pre-commit hook, the script should be appended or chained rather than overwriting.

**Step 2: Commit**

```bash
git add Makefile
git commit -m "feat: add make install-hooks target for doc auto-update hook"
```

---

### Task 5: Initial generation — run skill for ALL subcommand dirs

This is the bootstrap — generate README.md + CLAUDE.md for all 8 command domains.

**Dirs to process:**
- `internal/verda-cli/cmd/auth/`
- `internal/verda-cli/cmd/vm/`
- `internal/verda-cli/cmd/sshkey/`
- `internal/verda-cli/cmd/startupscript/`
- `internal/verda-cli/cmd/volume/`
- `internal/verda-cli/cmd/settings/`
- `internal/verda-cli/cmd/version/`
- `internal/verda-cli/cmd/update/`

**Step 1: Run the skill with a force-all flag**

The skill should support a `--all` or "generate all" mode for initial bootstrap:

```bash
claude -p "/update-command-knowledge --all" \
  --model sonnet \
  --dangerously-skip-permissions \
  --max-budget-usd 2.00 \
  --print
```

**Step 2: Review generated docs**

Manually review a couple of the generated README.md and CLAUDE.md files for quality.

**Step 3: Commit**

```bash
git add internal/verda-cli/cmd/*/README.md internal/verda-cli/cmd/*/CLAUDE.md
git commit -m "docs: add initial README.md and CLAUDE.md for all subcommand directories"
```

---

### Task 6: Update root project docs to reference per-command knowledge

**Files:**
- Modify: `CLAUDE.md` (root)
- Modify: `AGENTS.md`
- Modify: `README.md` (root, optional)

**Step 1: Add to root CLAUDE.md**

Add a section:
```markdown
## Per-Command Knowledge

Each subcommand directory has its own docs:
- `README.md` — Human-readable: usage, examples, flags, architecture
- `CLAUDE.md` — AI context: gotchas, domain logic, relationships

These are auto-maintained by a pre-commit hook. See `.ai/skills/update-command-knowledge.md`.

When modifying a command, the hook will auto-update its docs on commit.
For manual update: `claude -p "/update-command-knowledge --all"`
```

**Step 2: Add to AGENTS.md**

Update the "Before You Start" section:
```markdown
3. Read the `CLAUDE.md` in the specific subcommand directory you're working on (e.g., `cmd/vm/CLAUDE.md`)
```

Update the skills table:
```markdown
| [update-command-knowledge.md](.ai/skills/update-command-knowledge.md) | Auto-updating per-command README.md and CLAUDE.md |
```

**Step 3: Commit**

```bash
git add CLAUDE.md AGENTS.md
git commit -m "docs: reference per-command knowledge docs in root project files"
```

---

### Task 7: Add SKIP escape hatch

**Files:**
- Modify: `scripts/hooks/pre-commit-update-docs`

**Step 1: Add skip environment variable**

At the top of the hook script, add:
```bash
if [ "${SKIP_DOC_UPDATE:-}" = "1" ]; then
  echo "⏭️  Skipping command knowledge doc update (SKIP_DOC_UPDATE=1)"
  exit 0
fi
```

Usage: `SKIP_DOC_UPDATE=1 git commit -m "quick fix"`

**Step 2: Commit**

```bash
git add scripts/hooks/pre-commit-update-docs
git commit -m "feat: add SKIP_DOC_UPDATE escape hatch for pre-commit hook"
```

---

## Execution Order

Tasks 1-3 are the core (skill + hook). Task 4 is installation. Task 5 is bootstrap. Tasks 6-7 are polish.

The critical path: **Task 2 → Task 3 → Task 5** (skill → hook → initial generation).

## Cost Estimate

- Initial generation (Task 5): ~8 sonnet calls, ~$1-2
- Per-commit updates: ~1 sonnet call for 1-2 dirs, ~$0.05-0.15
- Monthly cost at ~100 commits/month: ~$5-15
