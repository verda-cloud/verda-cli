# Scripting and Agent Mode

How to drive the Verda CLI from scripts, CI pipelines and AI agents. Every flag mentioned here is listed under
[Global Flags](../commands.md#global-flags) or on the command's [reference page](../commands.md).

## Structured Output

All list and describe commands support `--output json` and `--output yaml` for scripting:

```bash
# Pipe to jq
verda vm list -o json | jq '.[].hostname'

# YAML output
verda volume describe vol-123 -o yaml

# Use in CI/CD scripts
INSTANCE_ID=$(verda vm list -o json | jq -r '.[0].id')
```

## Wait for Operations

Async commands support `--wait` to poll until completion:

```bash
verda vm create --hostname gpu-runner --wait --wait-timeout 10m
verda vm action --id abc-123 --wait
verda volume create --name data --size 500 --wait
```

## Agent Mode (Beta)

The `--agent` flag optimizes CLI behavior for AI agents and scripts:

```bash
verda --agent vm list                  # JSON output, no interactive picker
verda --agent vm create ...            # structured errors for missing flags
verda --agent vm action --id X --action shutdown --yes  # --yes is required for destructive actions
```

See [Agent Errors](../agent-errors.md) for the structured error format.
