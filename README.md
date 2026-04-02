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

Resource Commands:
  ssh-key           Manage SSH keys
  startup-script    Manage startup scripts
  volume            Manage volumes

Other Commands:
  settings          Manage CLI settings
  update            Update Verda CLI to latest or specific version
  version           Print version information
```

### VM


| Command           | Description                                |
| ----------------- | ------------------------------------------ |
| `verda vm create` | Create a VM (interactive wizard or flags)  |
| `verda vm list`   | List and inspect VM instances              |
| `verda vm action` | Start, shutdown, hibernate, or delete a VM |


### Volume


| Command               | Description                                  |
| --------------------- | -------------------------------------------- |
| `verda volume create` | Create a block storage volume                |
| `verda volume list`   | List volumes                                 |
| `verda volume action` | Detach, rename, resize, clone, or delete     |
| `verda volume trash`  | List deleted volumes (restorable within 96h) |


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


## Global Flags


| Flag         | Description                                           |
| ------------ | ----------------------------------------------------- |
| `--debug`    | Enable debug output (API request/response details)    |
| `--timeout`  | HTTP request timeout (default: 30s)                   |
| `--base-url` | Override API base URL                                 |
| `--config`   | Path to config file (default: `~/.verda/config.yaml`) |


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
