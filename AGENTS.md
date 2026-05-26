# Warpgate Agent Instructions

## Project Overview

Warpgate is a lightweight Go-based deployment tool replacing our k3s + Flux setup. Stack:
- Docker Compose for app runtime (user-written, not generated)
- Traefik for routing/SSL (auto-configured via compose override labels)
- Tailscale for networking
- SecretSauce for secrets injection at deploy time
- Charmbracelet for TUI (planned)

## Build & Test

```bash
go build ./cmd/warpgate        # Build CLI
go test ./...                  # Run all tests
go test -run ^TestMyFunc$      # Run a single test
go vet ./...                   # Vet
```

## Architecture

- `cmd/warpgate/` - CLI binary, uses `pkg/cli/`
- `pkg/config/` - `cluster.yml` and `app.yml` types, loading, and app discovery from `apps/` directories
- `pkg/cli/` - Cobra commands for the CLI
- `pkg/compose/` - Compose override generation (Traefik labels + image tag only)
- `pkg/deploy/` - Deploy orchestration, rollback, and deploy state management
- `pkg/ssh/` - SSH client (key-based and Tailscale SSH modes)
- `pkg/bootstrap/` - Node provisioning via SSH (OS detection, install scripts, Traefik setup)
- `warpd/` - Local browser UI internals used by `warpgate ui`

Warpgate ships one CLI binary. The local browser UI is started with `warpgate ui`; do not add a separate daemon binary.

### Config Model
- `cluster.yml` at repo root defines nodes, networking, traefik, and registry
- Each app has its own directory under `apps/<name>/` with `app.yml` (deploy metadata) and `compose.yml` (user-written Docker Compose)
- Warpgate discovers apps by scanning `apps/*/app.yml`
- App name is derived from directory name, not from YAML

### Deploy Flow
- Upload `compose.yml` to remote node at `/opt/warpgate/apps/<name>/`
- Generate thin `docker-compose.override.yml` with Traefik labels and image tag
- Run `secretsauce run <prefix> -- docker compose -f compose.yml -f docker-compose.override.yml up -d`
- Save deploy state to `state.json` for rollback

### Networking Model
- Same-node services use Docker DNS (service name resolution)
- Cross-node services use Traefik domains (load balanced)
- Traefik runs per-node, discovers containers via Docker labels on the `warpgate` network

## Code Style

- Use `go fmt .` for formatting
- Prefer `strings.Builder` or concatenation over `fmt.Sprintf` for simple formatting
- Use standard Go error handling patterns — don't discard errors
- Avoid third-party packages unless necessary

## Comment Guidelines

- Provide at least one package-level comment
- Add comments to exported functions and all struct fields in godoc-friendly format
- Do not use inline comments to explain code — code should be self-explanatory
- Comments should describe *what* the type or function is, not *why*
- Keep comments self-contained and matter-of-fact

## Testing Guidelines

- Use table-driven tests for multiple test cases
- Focus on testing actual business logic, not mocked behavior
- Test negative cases (nil values, invalid inputs, error conditions)
- Avoid tautological tests (setting a field then asserting its value)
- Integration tests (HTTP, templating) are less valuable than unit tests of core logic

## Bootstrap Details

### Prerequisites on Target Nodes
- Tailscale installed and configured with SSH enabled
- User has passwordless sudo access

### What Bootstrap Installs
1. **Go** - Downloaded from official tarball
2. **Docker** - Via distro packages (apt/yum)
3. **Docker Compose** - As plugin (docker compose)
4. **SecretSauce** - Via private Go proxy on tailnet (if `go_proxy` configured)
5. **Traefik** - As Docker Compose service on the `warpgate` network
6. **warpgate user** - System user with sudo and docker group
7. **SSH keys** - Generates ed25519 key for warpgate user
8. **Directories** - `/opt/warpgate/apps/` and `/opt/warpgate/traefik/`

### Supported Operating Systems
- Ubuntu (18.04+), Debian (10+), CentOS (7+), Rocky Linux (8+)
- AlmaLinux (8+), Fedora (33+), Amazon Linux

### Bootstrap Files
- `pkg/bootstrap/os.go` - OS detection and identification
- `pkg/bootstrap/installer.go` - Installation script generation (including Traefik)
- `pkg/bootstrap/bootstrap.go` - Bootstrap orchestration
- `pkg/ssh/client.go` - SSH client used by both bootstrap and deploy
