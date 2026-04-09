# AI Skills for Verda Cloud

Verda CLI includes AI skills — markdown files that teach AI coding agents how to manage your cloud infrastructure through natural language. Install the skills and your agent gains structured knowledge of Verda Cloud workflows without you explaining them each time.

## Install

```bash
verda skills install
```

This auto-detects your AI agent (Claude Code, Cursor, etc.) and installs skills to the right location.

To reinstall or update after a CLI upgrade:

```bash
verda skills install --force
```

## What's Included

| Skill | Purpose |
|-------|---------|
| **verda-cloud** | Decision engine — teaches agents HOW to reason about tasks: classify requests, follow the deploy dependency chain, handle errors, stay safe |
| **verda-reference** | Command reference — teaches agents WHAT to run: all commands, flags, parameter sources, output fields |

## Example Prompts

### Overview — what's going on

```
Show me my Verda Cloud status
Give me an overview of my Verda Cloud resources
What's my Verda Cloud dashboard look like?
```

### Explore — check what's available

```
What GPU instances are available in Verda Cloud right now?
Show me the cheapest Verda Cloud GPU with at least 80GB VRAM
What CPU options does Verda Cloud have?
How much am I spending on my Verda Cloud VMs?
What's my Verda Cloud account balance?
```

### Deploy — create a VM

```
Deploy a Verda Cloud GPU VM for training with at least 80GB VRAM
I need a cheap spot GPU on Verda Cloud for testing
Spin up a Verda Cloud CPU instance for a small web server
Deploy a Verda Cloud A100 in FIN-01 with my SSH key
Create a Verda Cloud VM from my gpu-training template
```

### Deploy with more context

```
I'm fine-tuning a 13B model — what Verda Cloud GPU do I need and can you set it up?
I need a Verda Cloud VM with 200GB storage for a large dataset, NVMe preferred
Deploy a spot H100 on Verda Cloud for Jupyter notebooks
```

### Templates — save and reuse configurations

```
Show me my Verda Cloud templates
Deploy a Verda Cloud VM from my gpu-training template
What Verda Cloud templates do I have?
```

To create or edit templates interactively, run these in your terminal:

```bash
verda template create        # Save a new template via wizard
verda template edit my-tmpl  # Edit an existing template
```

### Manage — control existing VMs

```
List my running Verda Cloud VMs
Shut down my Verda Cloud training VM
Start my Verda Cloud gpu-runner instance back up
Hibernate my Verda Cloud dev box
Delete the Verda Cloud instance I'm not using anymore
```

### Cost management

```
How much am I spending per hour on Verda Cloud right now?
What would a Verda Cloud H100 cost me per hour?
Show me my Verda Cloud balance and how long it will last
Which of my Verda Cloud VMs is the most expensive?
```

### SSH keys and startup scripts

```
Show me my Verda Cloud SSH keys
List my Verda Cloud startup scripts
Which Verda Cloud SSH key is named "meng"?
```

### Volumes and storage

```
List my Verda Cloud volumes
Show me detached Verda Cloud volumes I could reuse
```

### Status checks

```
What's the status of my Verda Cloud training VM?
Is my Verda Cloud instance running?
Show me details of my Verda Cloud gpu-runner
```

Note: The agent can check VM status and configuration, but cannot access system logs or cloud-side diagnostics. For deeper troubleshooting, check the [Verda Cloud dashboard](https://verda.com) or contact support.

## How It Works

When you ask your AI agent something related to Verda Cloud, the agent automatically loads the skills and follows the workflows defined in them. The skills teach the agent to:

1. **Use the right commands** — maps natural language ("my keys", "GPU types") to the correct CLI syntax (`ssh-key list`, `instance-types --gpu`)
2. **Follow the dependency chain** — when deploying, checks billing → compute → instance type → availability → images → SSH keys → cost → confirm → create
3. **Stay safe** — always checks cost before creating, confirms before deleting, never guesses image slugs
4. **Handle errors** — parses structured `--agent` mode errors and recovers automatically
5. **Be efficient** — runs independent commands in parallel, caches results, skips unnecessary steps

## Supported Agents

| Agent | Skill location after install |
|-------|------------------------------|
| Claude Code | `~/.claude/skills/` |
| Cursor | `.cursor/rules/` (project-level) |

## Tips

- **Be specific when you can** — "Deploy 1A100.22V in FIN-03" is faster than "I need a GPU" because the agent skips discovery steps
- **Mention your template** — "Deploy from my gpu-training template" skips the entire configuration flow
- **Ask about costs first** — "How much would an H100 cost?" before "Deploy an H100" saves surprises
- **Use natural language** — say "my keys" not `ssh-key list`, the agent translates for you
