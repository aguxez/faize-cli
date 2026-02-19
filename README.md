# Faize

Faize is a CLI that creates isolated, reproducible virtual machines for running AI agents with controlled resource allocation, network restrictions, and secure file mounting. It uses Apple's Virtualization.framework on macOS to spin up lightweight VMs with an ephemeral overlay filesystem, VirtioFS mounts, and a network allowlist.

## Requirements

- macOS with Virtualization.framework support
- Go 1.24+
- Docker (for building rootfs images)

## Installation

```bash
# Build and code-sign (macOS)
make build

# Or build without signing
make build-unsigned

# Install to $GOPATH/bin (or ~/go/bin)
make install
```

## Quick Start

```bash
# Launch a sandboxed VM for the current directory
faize

# Launch with a specific project and extra mounts
faize --project ~/code/myapp --mount ~/.npmrc

# Start a Claude Code session
faize claude start --project ~/code/myapp

# List running sessions
faize ps

# Stop a session
faize stop <session-id>
```

## Commands

### `faize [flags]`

Launch a sandboxed VM session.

| Flag | Short | Description |
|------|-------|-------------|
| `--project` | `-p` | Project directory to mount (default: current directory) |
| `--mount` | `-m` | Additional mount paths (repeatable) |
| `--network` | `-n` | Network access policies (e.g. `npm`, `pypi`, `github`, `all`, `none`) |
| `--cpus` | | Number of CPUs (default: 2) |
| `--memory` | | Memory limit, e.g. `4GB` (default: 4GB) |
| `--timeout` | `-t` | Session timeout, e.g. `2h` (default: 2h) |
| `--minimal-test` | | Test mode: 1 CPU, 512MB RAM, no mounts, no network |
| `--config` | | Config file path (default: `~/.faize/config.yaml`) |
| `--debug` | | Enable debug logging |

### `faize claude start [flags]`

Start a Claude Code session in an isolated VM. Automatically mounts `~/.claude` (read-only), `~/.faize/toolchain` (read-write), and sets up the project at `/workspace`.

| Flag | Short | Description |
|------|-------|-------------|
| `--project` | `-p` | Project directory (default: current directory) |
| `--mount` | `-m` | Additional mount paths (repeatable) |
| `--cpus` | | Number of CPUs |
| `--memory` | | Memory limit |
| `--timeout` | `-t` | Session timeout |
| `--persist-credentials` | | Persist Claude credentials across sessions |
| `--debug` | | Enable debug logging |

### `faize claude attach <session-id>`

Attach to a running Claude Code session.

### `faize claude rebuild`

Rebuild the rootfs image with extra dependencies from config.

### `faize ps`

List running VM sessions.

### `faize stop <session-id>`

Stop a running session.

### `faize kill [--force]`

Remove session metadata. With `--force`, also stops running sessions.

### `faize prune [--all] [--artifacts]`

Clean up stopped sessions. `--all` removes all sessions; `--artifacts` also removes downloaded kernel and rootfs images.

## Network Policies

Network access is controlled via domain allowlists. Use preset names or literal domains:

| Preset | Domains |
|--------|---------|
| `npm` | registry.npmjs.org, npmjs.com |
| `pypi` | pypi.org, files.pythonhosted.org |
| `github` | github.com, api.github.com, raw.githubusercontent.com |
| `anthropic` | api.anthropic.com, anthropic.com |
| `openai` | api.openai.com, openai.com |
| `bun` | bun.sh, registry.npmjs.org |

Special values: `all` (unrestricted) and `none` (no network access).

```bash
faize --network npm --network github
faize --network all
faize --network none
```

## Configuration

Faize reads from `~/.faize/config.yaml`:

```yaml
defaults:
  cpus: 2
  memory: 4GB
  timeout: 2h

networks:
  - npm
  - pypi
  - github
  - anthropic

blocked_paths:
  - ~/.ssh
  - ~/.aws

claude:
  persist_credentials: false
  extra_deps:
    - python3
    - ripgrep
```

### Security

Certain paths are always blocked from being mounted, regardless of configuration:

- `~/.ssh`, `~/.aws`, `~/.config/gcloud`, `~/.gnupg`, `~/.password-store`, `~/.docker/config.json`
- Browser keystores, package manager credentials, kubeconfig, and other secret stores

## Project Structure

```
internal/
  cmd/          CLI commands (Cobra)
  config/       Configuration loading and defaults
  vm/           VM manager (Virtualization.framework on macOS, stub elsewhere)
  session/      Session persistence (~/.faize/sessions/)
  mount/        Mount parsing, validation, and blocked-path enforcement
  network/      Network allowlist and domain presets
  guest/        Guest init script generation
  artifacts/    Kernel and rootfs download/build management
scripts/
  build-rootfs.sh          Alpine-based rootfs builder
  build-claude-rootfs.sh   Claude-specific rootfs with Bun and Claude CLI
  build-kernel.sh          Linux kernel builder with virtio support
```

## Development

```bash
make build       # Build and sign
make test        # Run tests
make lint        # Run linter
make fmt         # Format code
make vet         # Run go vet
make dev         # Build and show help
make clean       # Clean build artifacts
```

## License

See [LICENSE](LICENSE) for details.
