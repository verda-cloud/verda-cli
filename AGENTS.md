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
- **Senior comment style** — default to no comments. Add one only when the WHY is non-obvious (invariant, workaround, gotcha, evolution point). One line, identifier-first. Never narrate WHAT — well-named identifiers do that. Delete comments when the reason expires. See CLAUDE.md "Comment style — write like a senior"
- **Preserve dual mode** — every command must work interactive AND non-interactive. Never build one without the other
- **Interactive hint bar** — every direct `prompter.Select(...)` outside the wizard engine must pass `tui.WithShowHints(true)` (and the equivalent option on `MultiSelect`) so the prompt renders its key hints below the choices. Wizard steps are exempt — the composite already renders the hint bar
- **Ctrl+C exits immediately, no confirmation** — use `cmdutil.IsPromptCancel(err)` to detect either Esc or Ctrl+C and return cleanly. When a flow needs different behavior per key (e.g. a "Back to list / Exit" gate where Esc means back), split with `IsPromptInterrupt(err)` (Ctrl+C) and `IsPromptBack(err)` (Esc). Never show an "Exit?" confirmation dialog — Unix users expect Ctrl+C to be terminal
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
- [ ] Interactive Selects pass `tui.WithShowHints(true)` so the hint bar renders
- [ ] No leftover debug code, TODOs, or commented-out blocks

If `make lint` reports issues, fix them *before* announcing completion. See `CLAUDE.md` § "Go House Style" for the patterns that prevent the common hits (http.NoBody, American spelling, reused constants, rangeValCopy, nilerr annotations, etc.).

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **verda-cli** (7546 symbols, 19146 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> If any GitNexus tool warns the index is stale, run `npx gitnexus analyze` in terminal first.

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `gitnexus_impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `gitnexus_detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `gitnexus_query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `gitnexus_context({name: "symbolName"})`.

## Never Do

- NEVER edit a function, class, or method without first running `gitnexus_impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `gitnexus_rename` which understands the call graph.
- NEVER commit changes without running `gitnexus_detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/verda-cli/context` | Codebase overview, check index freshness |
| `gitnexus://repo/verda-cli/clusters` | All functional areas |
| `gitnexus://repo/verda-cli/processes` | All execution flows |
| `gitnexus://repo/verda-cli/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
