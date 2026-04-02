# Verda CLI

Command-line interface for [Verda Cloud](https://verda.com) — manage VMs, volumes, SSH keys, startup scripts, and more from your terminal.

## Install

### Quick install (macOS / Linux)

```bash
curl -sSL https://raw.githubusercontent.com/verda-cloud/verda-cli/main/scripts/install.sh | sh
```

Install to a custom directory:

```bash
VERDA_INSTALL_DIR=~/.local/bin curl -sSL https://raw.githubusercontent.com/verda-cloud/verda-cli/main/scripts/install.sh | sh
```

Install a specific version:

```bash
VERDA_VERSION=v1.0.0 curl -sSL https://raw.githubusercontent.com/verda-cloud/verda-cli/main/scripts/install.sh | sh
```

### Manual download

Download the binary for your platform from [GitHub Releases](https://github.com/verda-cloud/verda-cli/releases):


| Platform              | File                                |
| --------------------- | ----------------------------------- |
| macOS (Apple Silicon) | `verda_VERSION_darwin_arm64.tar.gz` |
| macOS (Intel)         | `verda_VERSION_darwin_amd64.tar.gz` |
| Linux (x86_64)        | `verda_VERSION_linux_amd64.tar.gz`  |
| Linux (ARM64)         | `verda_VERSION_linux_arm64.tar.gz`  |
| Windows (x86_64)      | `verda_VERSION_windows_amd64.zip`   |
| Windows (ARM64)       | `verda_VERSION_windows_arm64.zip`   |


Extract and move to your PATH:

```bash
tar xzf verda_*.tar.gz
sudo mv verda /usr/local/bin/
```

### Go install (for Go developers)

```bash
go install github.com/verda-cloud/verda-cli/cmd/verda@latest
```

### Verify installation

```bash
verda version
```

### Update to latest version

```bash
verda update
```

Or install a specific version:

```bash
verda update --version v1.0.0
```

## Getting Started

### 1. Configure credentials

```bash
verda auth login
```

This starts an interactive wizard to save your API credentials to `~/.verda/credentials`.

### 2. List your VMs

```bash
verda vm list
```

### 3. Create a VM

![verda vm create](docs/images/vm-create-demo.gif)

```bash
# Interactive wizard
verda vm create

# Non-interactive
verda vm create \
  --kind gpu \
  --instance-type 1V100.6V \
  --location FIN-01 \
  --os ubuntu-24.04-cuda-12.8-open-docker \
  --os-volume-size 100 \
  --hostname gpu-runner
```

## Commands

```
Auth Commands:
  auth              Manage shared credentials and profiles

VM Commands:
  vm                Manage VM instances
  ssh               SSH into a running VM instance

Resource Commands:
  ssh-key           Manage SSH keys
  startup-script    Manage startup scripts
  volume            Manage volumes

Other Commands:
  completion        Generate shell completion scripts
  settings          Manage CLI settings
  update            Update Verda CLI to latest or specific version
  version           Print version information
```

### VM


| Command              | Description                                |
| -------------------- | ------------------------------------------ |
| `verda vm create`    | Create a VM (interactive wizard or flags)  |
| `verda vm list`      | List and inspect VM instances              |
| `verda vm describe`  | Show detailed info about a single VM       |
| `verda vm action`    | Start, shutdown, hibernate, or delete a VM |


### SSH

```bash
# Connect by hostname
verda ssh gpu-runner

# Connect with options
verda ssh gpu-runner --user ubuntu --key ~/.ssh/id_ed25519

# Port forwarding and other ssh args
verda ssh gpu-runner -- -L 8080:localhost:8080
```

### Volume


| Command                 | Description                                  |
| ----------------------- | -------------------------------------------- |
| `verda volume create`   | Create a block storage volume                |
| `verda volume list`     | List volumes                                 |
| `verda volume describe` | Show detailed info about a single volume     |
| `verda volume action`   | Detach, rename, resize, clone, or delete     |
| `verda volume trash`    | List deleted volumes (restorable within 96h) |


### SSH Keys & Startup Scripts


| Command                                    | Description            |
| ------------------------------------------ | ---------------------- |
| `verda ssh-key list / add / delete`        | Manage SSH keys        |
| `verda startup-script list / add / delete` | Manage startup scripts |


### Settings


| Command                         | Description                    |
| ------------------------------- | ------------------------------ |
| `verda settings theme`          | View or change the color theme |
| `verda settings theme --select` | Interactive theme picker       |


Available themes: `default`, `dracula`, `catppuccin`, `catppuccin-latte`, `nord`, `tokyonight`, `github-light`, `solarized-light`

### Update


| Command                         | Description                                       |
| ------------------------------- | ------------------------------------------------- |
| `verda update`                  | Update to the latest version                      |
| `verda update --version v1.0.0` | Install a specific version (upgrade or downgrade) |
| `verda update --list`           | List available versions                           |


### Auth


| Command                  | Description                               |
| ------------------------ | ----------------------------------------- |
| `verda auth login`       | Save API credentials (interactive wizard) |
| `verda auth show`        | Show active profile and credentials path  |
| `verda auth use PROFILE` | Switch active auth profile                |


### Shell Completion

```bash
# Bash
source <(verda completion bash)

# Zsh (add to ~/.zshrc or run once)
verda completion zsh > "${fpath[1]}/_verda"

# Fish
verda completion fish | source
```

## Global Flags


| Flag              | Description                                           |
| ----------------- | ----------------------------------------------------- |
| `--output, -o`    | Output format: `table`, `json`, `yaml` (default: table) |
| `--debug`         | Enable debug output (API request/response details)    |
| `--timeout`       | HTTP request timeout (default: 30s)                   |
| `--base-url`      | Override API base URL                                 |
| `--config`        | Path to config file (default: `~/.verda/config.yaml`) |

### Structured Output

All list and describe commands support `--output json` and `--output yaml` for scripting:

```bash
# Pipe to jq
verda vm list -o json | jq '.[].hostname'

# YAML output
verda volume describe vol-123 -o yaml

# Use in CI/CD scripts
INSTANCE_ID=$(verda vm list -o json | jq -r '.[0].id')
```

### Wait for Operations

Async commands support `--wait` to poll until completion:

```bash
verda vm create --hostname gpu-runner --wait --wait-timeout 10m
verda vm action --id abc-123 --wait
verda volume create --name data --size 500 --wait
```


## Configuration

Credentials are stored in `~/.verda/credentials` (AWS-style INI format):

```ini
[default]
verda_base_url      = https://api.verda.com/v1
verda_client_id     = your-client-id
verda_client_secret = your-client-secret
```

Settings (theme, etc.) are stored in `~/.verda/config.yaml`.

Override the config directory with `VERDA_HOME` environment variable.

## Contributing

See [CLAUDE.md](CLAUDE.md) and [AGENTS.md](AGENTS.md) for development setup and coding conventions.

## License

See [LICENSE](LICENSE) for details.
