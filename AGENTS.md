# AI Agent Contract

Read `CLAUDE.md` first. This file defines how you execute, not what the project is.

Applies to every AI agent working in this repo. Primary targets are Claude Code (reads `CLAUDE.md`) and OpenAI Codex (reads this file). `CLAUDE.md` (project knowledge) + this file (execution contract) + `.golangci.yaml` (enforced style) form the complete brief. A `.cursor/rules/main.mdc` pointer exists for Cursor users but is not actively maintained.

## Mandatory Read-First, Plan-First Workflow

Do NOT write code until you have completed all steps below. No exceptions.

**Step 1 — Read** (always, every task):
1. `CLAUDE.md` (root) — architecture, conventions, pricing rules, per-command doc index
2. `cmd/<domain>/CLAUDE.md` in the target command directory — domain knowledge, gotchas, edge cases
3. `cmd/<domain>/README.md` in the target command directory — usage examples, flags, architecture
4. `.ai/skills/new-command.md` if adding or modifying a command

Per-command docs exist in: `auth`, `vm`, `template`, `volume`, `sshkey`, `startupscript`, `update`, `settings`. For commands without docs, read the source files directly.

**Step 2 — Verify** (always):
5. Run `make test` to confirm the repo is green before you start

**Step 3 — Plan** (required for non-trivial changes):
6. State what you will change and why before writing code
7. For risky areas (see table below): write a plan, get approval, then code
8. If superpowers skills are available: use `brainstorming` before creative work, `writing-plans` before multi-step tasks, `test-driven-development` before implementation

Skipping these steps leads to pattern violations, broken dual-mode, and pricing bugs.

## Execution Rules

- **Follow existing patterns** — find the nearest similar command, match its structure
- **Preserve dual mode** — every command must work interactive AND non-interactive. Never build one without the other
- **Never modify `verdagostack`** directly — describe needed changes for the maintainer
- **Commit only when asked** — don't auto-commit

## Risky Areas — Slow Down

| Area | Risk | What To Do |
|------|------|------------|
| `cmd/util/pricing.go` | Wrong math = wrong bills | Verify formula, test with real numbers |
| `options/credentials.go` | Break auth = break everything | Test all profiles, expired tokens |
| Agent mode (`--agent`) | JSON contract change = break downstream | Check structured error format |
| Wizard steps | Step ordering, cache invalidation | Map dependencies before coding |
| `verdagostack` types | Shared across repos | Don't modify, describe changes needed |

## Done Checklist

- [ ] `make build` passes
- [ ] `make lint` passes with zero issues (do not rely on pre-commit to surface these)
- [ ] `make test` passes (runs lint + unit tests)
- [ ] `--help` renders correctly for changed commands
- [ ] Interactive and non-interactive modes both work
- [ ] No leftover debug code, TODOs, or commented-out blocks

If `make lint` reports issues, fix them *before* announcing completion. See `CLAUDE.md` § "Go House Style" for the patterns that prevent the common hits (http.NoBody, American spelling, reused constants, rangeValCopy, nilerr annotations, etc.).
