# Verda CLI

Command-line interface for [Verda Cloud](https://verda.com) — manage VMs, volumes, SSH keys, startup scripts, and more from your terminal.

![verda vm create](docs/images/vm-create-demo.gif)

Both interactive and non-interactive modes — use the wizard for quick tasks, flags for scripts and automation.

## Install

### Quick install (macOS / Linux)

```bash
curl -sSL https://raw.githubusercontent.com/verda-cloud/verda-cli/main/scripts/install.sh | sh
```

Install to a custom directory:

```bash
VERDA_INSTALL_DIR=~/.local/bin curl -sSL https://raw.githubusercontent.com/verda-cloud/verda-cli/main/scripts/install.sh | sh
```

### Homebrew (macOS / Linux)

```bash
brew install verda-cloud/tap/verda
```

### Scoop (Windows)

```powershell
scoop bucket add verda https://github.com/verda-cloud/homebrew-tap
scoop install verda
```

### Linux packages (deb / rpm / apk)

Download `.deb`, `.rpm`, or `.apk` packages from [GitHub Releases](https://github.com/verda-cloud/verda-cli/releases):

```bash
# Debian / Ubuntu
sudo dpkg -i verda_VERSION_linux_amd64.deb

# RHEL / Fedora
sudo rpm -i verda_VERSION_linux_amd64.rpm

# Alpine
sudo apk add --allow-untrusted verda_VERSION_linux_amd64.apk
```

### Manual download

Download the binary for your platform from [GitHub Releases](https://github.com/verda-cloud/verda-cli/releases):

| Platform | File |
|----------|------|
| macOS (Apple Silicon) | `verda_VERSION_darwin_arm64.tar.gz` |
| macOS (Intel) | `verda_VERSION_darwin_amd64.tar.gz` |
| Linux (x86_64) | `verda_VERSION_linux_amd64.tar.gz` |
| Linux (ARM64) | `verda_VERSION_linux_arm64.tar.gz` |
| Windows (x86_64) | `verda_VERSION_windows_amd64.zip` |
| Windows (ARM64) | `verda_VERSION_windows_arm64.zip` |

```bash
tar xzf verda_*.tar.gz
sudo mv verda /usr/local/bin/
```

### Go install

```bash
go install github.com/verda-cloud/verda-cli/cmd/verda@latest
```

### Verify & update

```bash
verda version            # verify installation
verda update             # update to latest
verda update --version v1.0.0  # specific version
```

## Getting Started

### 1. Configure credentials

```bash
verda auth login
```

### 2. Explore available resources

```bash
verda locations                        # datacenter locations
verda instance-types --gpu             # GPU instance types with pricing
verda availability --location FIN-01   # what's in stock
```

### 3. Deploy a VM

```bash
# Interactive wizard
verda vm create

# Non-interactive
verda vm create \
  --kind gpu \
  --instance-type 1V100.6V \
  --location FIN-01 \
  --os ubuntu-24.04-cuda-13.0-open-docker \
  --os-volume-size 100 \
  --hostname gpu-runner
```

### 4. Connect

```bash
verda ssh gpu-runner
```

## AI Agent Integration (MCP) — Beta

The Verda CLI includes a built-in [MCP](https://modelcontextprotocol.io/) server that lets AI agents (Claude Code, Cursor, etc.) manage your infrastructure through natural language. This feature is in **beta** — feedback welcome.

### Setup

```json
{
  "mcpServers": {
    "verda": {
      "command": "verda",
      "args": ["mcp", "serve"]
    }
  }
}
```

Add this to your agent's MCP config:

| Agent | Config file |
|-------|------------|
| Claude Code | `.mcp.json` in project root |
| Cursor | `~/.cursor/mcp.json` |

### Usage

Once configured, just talk to your agent:

```
"What GPU types are available right now?"
"How much does an 8x H100 cost per hour?"
"I need a cheap GPU for testing — what's the best option?"
"Deploy a V100 GPU VM with 100GB OS volume"
"Show my running VMs and what they're costing me"
"Shut down my training VM"
```

Credentials are shared with the CLI — run `verda auth login` first.

### Agent Mode

For scripts and agents that use the CLI directly (without MCP):

```bash
verda --agent vm list                  # JSON output, no prompts
verda --agent vm create ...            # structured errors for missing flags
verda --agent vm action --id X --action shutdown --yes
```

See [Agent Error Format](docs/agent-errors.md) for the structured error specification.

## Configuration

Credentials are stored in `~/.verda/credentials` (INI format):

```ini
[default]
verda_base_url      = https://api.verda.com/v1
verda_client_id     = your-client-id
verda_client_secret = your-client-secret
```

Multiple profiles are supported — switch with `verda auth use PROFILE`.

## Documentation

| Doc | Description |
|-----|-------------|
| [Command Reference](docs/commands.md) | Full list of commands, flags, and examples |
| [Agent Error Format](docs/agent-errors.md) | Structured error specification for `--agent` mode |

## Contributing

See [CLAUDE.md](CLAUDE.md) and [AGENTS.md](AGENTS.md) for development setup and coding conventions.

## License

See [LICENSE](LICENSE) for details.
