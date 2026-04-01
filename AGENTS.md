# Warpgate Agent Instructions

## Project Overview

Warpgate is a lightweight Go-based deployment tool replacing our k3s + Flux setup. Stack:
- Docker Compose for orchestration
- Traefik for routing/SSL
- Tailscale for networking
- SecretSauce for secrets
- Charmbracelet for TUI (planned)

## Build & Test

```bash
go build ./cmd/warpgate        # Build CLI
go build ./cmd/warpd           # Build daemon
go test ./...                  # Run all tests
go test -run ^TestMyFunc$      # Run a single test
go vet ./...                   # Vet
```

## Architecture

- `cmd/warpgate/` - CLI binary, uses `pkg/cli/`
- `cmd/warpd/` - Daemon binary (server + agent modes), uses `pkg/daemon/`
- `pkg/config/` - `warpgate.yml` types and loading with env var expansion
- `pkg/cli/` - Cobra commands for the CLI
- `pkg/compose/` - Docker Compose file generation (one file per node, all apps, Traefik labels, sidecars, init containers)
- `pkg/daemon/` - Server/agent daemon implementation
- `pkg/bootstrap/` - Node setup via SSH (OS detection, install scripts)

The CLI and daemon are separate binaries. Daemon commands belong in `warpd`, not `warpgate`.

### Networking Model
- One compose file per node with all apps targeted at that node
- Same-node services use Docker DNS (service name resolution)
- Cross-node services use Traefik domains (load balanced)
- Sidecars use `depends_on` with `condition: service_started`
- Init containers use `depends_on` with `condition: service_completed_successfully`

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
- Tailscale installed and configured
- SSH server running
- User has passwordless sudo access

### What Bootstrap Installs
1. **Go** - Downloaded from official tarball
2. **Docker** - Via distro packages (apt/yum)
3. **Docker Compose** - As plugin (docker compose)
4. **SecretSauce** - Via private Go proxy on tailnet (if `go_proxy` configured)
5. **warpgate user** - System user with sudo and docker group
6. **SSH keys** - Generates ed25519 key for warpgate user

### Supported Operating Systems
- Ubuntu (18.04+), Debian (10+), CentOS (7+), Rocky Linux (8+)
- AlmaLinux (8+), Fedora (33+), Amazon Linux

### Bootstrap Files
- `pkg/bootstrap/os.go` - OS detection and identification
- `pkg/bootstrap/ssh.go` - SSH client for remote execution
- `pkg/bootstrap/installer.go` - Installation script generation
- `pkg/bootstrap/bootstrap.go` - Bootstrap orchestration
