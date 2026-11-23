# verda inference -- Verda Inference (LLM API)

Alias: `verda inf`. Everything is scoped to the project of the active credentials.

## Commands

| Command | Description | Key Flags |
|---------|-------------|-----------|
| `verda inference models` / `models list` | Model catalog | |
| `verda inference models get [<code>]` | One model + the project's rate limits for it | |
| `verda inference subscriptions` / `list` | Project subscriptions (table shows tenant status) | |
| `verda inference subscriptions status` | Active / suspended | |
| `verda inference subscriptions enable [<model>]` | Subscribe | `--tier` (default `standard`) |
| `verda inference subscriptions disable [<model>]` | Unsubscribe | `--tier`, `--yes` |
| `verda inference keys` / `keys list` | API keys (masked) | |
| `verda inference keys create` | New `dcs_` key, printed once | `--label` |
| `verda inference keys delete [<id>]` | Delete a key | `--yes` |
| `verda inference usage` | Tokens per day and model | `--from`, `--to`, `--key` |
| `verda inference limits` | Project quota tier and rate limits | `--all-tiers` |
| `verda inference setup [agent...]` | Connect local coding agents | `--model` (repeatable), `--yes`, `--dry-run`, `--rotate`, `--inference-url` |
| `verda inference remove [agent...]` | Undo setup for agents, delete their keys | `--yes`, `--dry-run` |

## Usage Examples

```bash
brew install verda-cloud/tap/verda-cli && verda auth login && verda inf setup

verda inf setup opencode kimi --model <code> --dry-run
verda inf setup opencode kimi --model <code> --yes
verda inf setup opencode --rotate --yes
verda inf remove kimi --yes

verda inf keys create --label my-app        # plaintext on stdout, notes on stderr
verda --agent inf keys create --label ci -o json   # plaintext in "key"
```

## What setup writes

| Agent | File | Entries | Key |
|-------|------|---------|-----|
| `opencode` | `$XDG_CONFIG_HOME/opencode/opencode.jsonc` if present, else `opencode.json` (default `~/.config`) | `providers.verda` (opencode ≥ 2) or `provider.verda` (v1) | `~/.verda/agents/opencode.key`, referenced as `{file:…}` |
| `kimi` | `$KIMI_CODE_HOME/config.toml`, `$KIMI_SHARE_DIR/config.toml`, `~/.kimi-code/config.toml` when that dir exists, else `~/.kimi/config.toml` | `[providers.verda]` (`openai_legacy`), `[models."verda/<code>"]` | inline `api_key` |
| `codex` (hidden, `--experimental-agents`) | `$CODEX_HOME/config.toml` + `verda.config.toml` profile | `[model_providers.verda]` (`wire_api = "responses"`) | `~/.verda/agents/codex.key`, read by the `codex-verda` alias |
| `claude-code` (hidden) | `~/.verda/agents/claude-code.json` (`--settings` file) | `env` block | inline `ANTHROPIC_AUTH_TOKEN` |

Aliases (codex, claude-code) go in one marked block in `~/.zshrc` (`$ZDOTDIR`)
or, for bash, `~/.bashrc` (macOS without `~/.bashrc`: `~/.bash_profile`);
other shells get the lines printed.

State: `~/.verda/inference-agents.yaml` (0600) — per profile and agent: key id,
label, models, base URL, files touched (with backup path).

## Endpoint override

`--inference-url` > `VERDA_INFERENCE_BASE_URL` / `inference.base-url` in
`~/.verda/config.yaml` > `verda_inference_base_url` in the credentials profile >
`https://inference.verda.com`. A trailing `/v1` is stripped; agents add their own paths.

## Architecture Notes

- `inference.go` -- group, `callAPI` (timeout + spinner), `writeStructured` (YAML via JSON round-trip), base URL.
- `models.go`, `subscriptions.go`, `keys.go`, `usage.go` (usage + limits) -- API commands.
- `setup.go` / `setup_output.go` / `remove.go` -- orchestration: resolve → plan (read-only) → confirm → apply.
- `agents.go` -- agent registry, `fileChange`, `applyChanges` (backup once, atomic writes).
- `opencode.go`, `kimi.go`, `experimental.go` -- per-agent writers.
- `jsonc.go` (hujson AST edits), `tomledit.go` (text-level TOML table cut/insert), `rcblock.go`, `state.go`, `fsutil.go`, `env.go` (`hostEnv`, injectable).
