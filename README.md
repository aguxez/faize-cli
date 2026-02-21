# Faize

Faize is a CLI that creates isolated, reproducible virtual machines for running AI coding agents. It uses Apple's Virtualization.framework on macOS to spin up lightweight VMs with an ephemeral overlay filesystem, VirtioFS mounts, and a network allowlist.

## Features

- **Isolated VMs** — Each session runs in its own lightweight virtual machine with configurable CPU, memory, and timeout limits
- **Ephemeral overlay filesystem** — Read-only rootfs with a writable overlay that resets between sessions
- **Network allowlists** — Outbound network access controlled via domain-based presets or custom rules
- **Secure file mounting** — Mount project directories into the VM with read-only or read-write access; sensitive paths (SSH keys, cloud credentials, keychains) are blocked by default
- **Git context detection** — Automatically mounts the `.git` directory from the repository root so the VM has access to git history
- **Clipboard bridge** — Syncs the host clipboard into the VM on Ctrl+V for text and image paste support
- **Session management** — List, stop, and clean up VM sessions from the CLI

## Setup

### Requirements

- macOS with Virtualization.framework support
- Go 1.24+
- Docker (for building VM images — kernel and rootfs)

### Quick Setup

```bash
# Full build: CLI + kernel + claude-rootfs
make all

# CLI only (artifacts auto-provision on first `faize start`)
make build

# Install to $GOPATH/bin
make install
```

### Individual Artifact Targets

```bash
make kernel         # Build just the kernel
make claude-rootfs  # Build just the Claude rootfs (requires Docker)
make rootfs         # Build the base rootfs (requires Docker)
make artifacts      # Build kernel + claude-rootfs
```

### How Auto-Provisioning Works

If artifacts are missing when you run `faize start`, the CLI will:

1. **Kernel** — download from GitHub releases, or build from source via Docker
2. **Claude rootfs** — build locally via Docker (not published to releases)

If Docker is unavailable, the CLI will suggest running `make artifacts` to pre-build.

### Artifact Storage

All artifacts live in `~/.faize/artifacts/`:

| Artifact | File | Description |
|----------|------|-------------|
| Kernel | `vmlinux` | ARM64 Linux kernel with virtio support |
| Claude rootfs | `claude-rootfs.img` | Alpine with dev tools and Claude CLI (1024MB) |

To remove all artifacts and force a rebuild:

```bash
faize prune --artifacts
```

<details>
<summary>Manual build scripts (advanced)</summary>

```bash
# Build kernel with specific version and output
./scripts/build-kernel.sh 6.6.10 /tmp/kernel-build ~/.faize/artifacts/vmlinux

# Build Claude rootfs with extra packages
EXTRA_DEPS="ripgrep python3-dev" ./scripts/build-claude-rootfs.sh

# Rebuild rootfs with deps from config
faize claude rebuild
```

</details>

## Quick Start

```bash
# Start a session for the current directory
faize start

# Start with a specific project
faize start --project ~/code/myapp

# List running sessions
faize ps

# Kill a session
faize kill <session-id>
```

## Commands

### `faize start [flags]`

Start a new VM session. Automatically mounts `~/.claude` (read-only), `~/.faize/toolchain` (read-write), and sets up the project at `/workspace`.

| Flag | Short | Description |
|------|-------|-------------|
| `--project` | `-p` | Project directory to mount (default: current directory) |
| `--mount` | `-m` | Additional mount paths (repeatable) |
| `--timeout` | `-t` | Session timeout, e.g. `2h` (default: from config) |
| `--persist-credentials` | | Persist Claude credentials across sessions |
| `--no-git-context` | | Disable automatic `.git` directory mounting |
| `--config` | | Config file path (default: `~/.faize/config.yaml`) |
| `--debug` | | Enable debug logging |

### `faize ps`

List running VM sessions.

### `faize kill [--force]`

Remove session metadata. With `--force`, also stops running sessions.

### `faize prune [--all] [--artifacts]`

Clean up stopped sessions. `--all` removes all sessions; `--artifacts` also removes downloaded kernel and rootfs images.

### `faize claude rebuild`

Rebuild the rootfs image with extra dependencies from config. After updating `claude.extra_deps` in the config, run this command then start a new session.

## Network Policies

Network access is controlled via domain allowlists configured in `~/.faize/config.yaml`:

| Preset | Domains |
|--------|---------|
| `npm` | registry.npmjs.org, npmjs.com |
| `pypi` | pypi.org, files.pythonhosted.org |
| `github` | github.com, api.github.com, raw.githubusercontent.com |
| `anthropic` | api.anthropic.com, anthropic.com |
| `openai` | api.openai.com, openai.com |
| `bun` | bun.sh, registry.npmjs.org |

Special values: `all` (unrestricted) and `none` (no network access).

## Configuration

Faize reads from `~/.faize/config.yaml`:

```yaml
resources:
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
  git_context: true
  extra_deps:
    - python3
    - ripgrep
```

## Security

Certain paths are always blocked from being mounted, regardless of configuration:

- `~/.ssh`, `~/.aws`, `~/.config/gcloud`, `~/.gnupg`, `~/.password-store`, `~/.docker/config.json`
- Browser keystores and keychains (`~/Library/Keychains` on macOS, `~/.local/share/keyrings` on Linux)
- Package manager credentials (`~/.netrc`, `~/.npmrc`, `~/.pypirc`)
- Cloud and infrastructure configs (`~/.kube`, `~/.azure`, `~/.config/gh`)

These hardcoded blocked paths cannot be overridden by user configuration.

## Project Structure

```
internal/
  cmd/          CLI commands (Cobra)
  config/       Configuration loading and defaults
  vm/           VM lifecycle, console, clipboard bridge (Virtualization.framework on macOS)
  session/      Session persistence (~/.faize/sessions/)
  mount/        Mount parsing, validation, and blocked-path enforcement
  network/      Network allowlist and domain presets
  git/          Git repository root detection
  guest/        Guest init script generation
  artifacts/    Kernel and rootfs download/build management
scripts/
  build-rootfs.sh          Alpine-based rootfs builder
  build-claude-rootfs.sh   Claude-specific rootfs with Bun and Claude CLI
  build-kernel.sh          Linux kernel builder with virtio support
```

## Development

```bash
make build       # Build and sign the CLI
make all         # Build CLI + kernel + claude-rootfs
make artifacts   # Build kernel + claude-rootfs only
make test        # Run tests
make lint        # Run linter
make fmt         # Format code
make vet         # Run go vet
make dev         # Build and show help
make clean       # Clean build artifacts
```
