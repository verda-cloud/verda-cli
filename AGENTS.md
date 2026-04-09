# AI Agent Contract

Read `CLAUDE.md` first. This file defines how you execute, not what the project is.

## Startup Sequence

Before writing any code:

1. Read `CLAUDE.md` (root) — architecture, conventions, pricing rules
2. Read `CLAUDE.md` in the target command directory — domain-specific gotchas
3. Read `.ai/skills/new-command.md` if adding or modifying a command
4. Run `make test` to confirm the repo is green before you start

## Execution Rules

- **Plan before coding** when touching: pricing, auth, wizard flows, agent-mode, or shared utilities
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
- [ ] `make test` passes
- [ ] `--help` renders correctly for changed commands
- [ ] Interactive and non-interactive modes both work
- [ ] No leftover debug code, TODOs, or commented-out blocks
