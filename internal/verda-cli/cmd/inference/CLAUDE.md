# Inference Command Knowledge

## Quick Reference
- Parent: `verda inference` (alias `inf`); see README.md for the command table.
- SDK: `client.Inference` (verdacloud-sdk-go). Every route except `ListTiers` returns 404 while inference is not enabled for the project.
- `models get` / `subscriptions enable|disable` take the model **code**; requests to the API use `endpoint_model`.

## Setup / remove invariants
- **Add, never replace.** Only `verda`-named entries are written: `provider(s).verda` (opencode), `[providers.verda]` + `[models."verda/*"]` (kimi), `[model_providers.verda]` (codex). Never touch `model`/`default_model` or other providers.
- **Byte preservation.** JSONC edits go through the hujson AST (`jsonc.go`), never `Patch`/`Format` (they reflow the file). TOML edits are text-level (`replaceTOMLTables`) and validated with go-toml before and after; an unparsable file is refused, never rewritten. Tests assert setup→remove restores the original bytes.
- **Backups**: a pre-existing file gets `<file>.verda-bak` the first time verda changes it (`backupOnce` never overwrites). Writes are atomic (temp + rename) and follow symlinks.
- **Keys**: one key per agent per machine, label `verda-cli <agent> (<hostname>)` (≤128 runes). Re-run reuses it when the API still lists the id and the plaintext is readable locally; otherwise a new key is created and the old id deleted after the configs are rewritten. A key created in a run whose file writes fail is deleted again.
- **Plaintext key** never goes to stdout/JSON from setup, never to `DebugJSON`; only `keys create` prints it (once). The SDK type redacts under `fmt` but not `encoding/json`.
- **Profiles**: agent configs are machine-global, so an agent belongs to one profile; setup/remove for another profile is refused with the command to run.
- **Experimental agents** (`codex`, `claude-code`) need `/v1/responses` / `/v1/messages`, served only by the Global Router; hidden behind `--experimental-agents`. `remove` accepts them without the flag.
- **Never point tests at a real home.** All paths come from `hostEnv`; tests use `newTestHost(t)` + the in-memory `fakeAPI`.

## Gotchas
- `--base-url` is the global API flag; the inference endpoint flag is `--inference-url`.
- Non-interactive setup/remove need `--yes` (or `--dry-run`); setup without `--model` reuses the models recorded in state.
- Kimi requires `max_context_size`; when the API has no `context_length`, 131072 is used and a warning is printed.
- opencode version: `opencode --version` major ≥ 2 → `providers` (v2) format, else v1; unknown version → v2 only if the file already has `providers`. Switching formats removes our entry from the other key.
