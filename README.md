# Warpgate

Warpgate is a lightweight app deployment tool written in Go. It replaces our k3s + Flux setup with something simpler: Docker Compose on bare metal, orchestrated over Tailscale SSH.

## Stack

- **Runtime**: Docker Compose (user-written, not generated)
- **Reverse Proxy**: Traefik with automatic HTTPS via Let's Encrypt
- **Networking**: Tailscale mesh (direct WireGuard peering between nodes)
- **Secrets**: [SecretSauce](https://github.com/pangobit/secretsauce) — secrets server on the tailnet, fetched at deploy time
- **DNS**: Cloudflare
- **Storage**: Named Docker volumes; SQLite apps replicate via Litestream
- **TUI**: [Charmbracelet](https://charm.sh/) v2 (bubbletea, bubbles, lipgloss)

## Getting Started

**Prerequisites**: A Linode (or any Linux VM) with [Tailscale](https://tailscale.com/) installed and SSH enabled.

```bash
# 1. Install warpgate
go install github.com/pangobit/warpgate/cmd/warpgate@latest

# 2. Scaffold a new project
mkdir my-infra && cd my-infra
warpgate init my-project

# 3. Edit cluster.yml with your node's Tailscale IP
#    Edit apps/example-app/app.yml with your image
#    Edit apps/example-app/compose.yml with your service config

# 4. Bootstrap the node (installs Docker, Traefik, etc.)
warpgate bootstrap node-1 --tailscale-ssh

# 5. Bootstrap the secrets server (auto-generates master password)
warpgate bootstrap node-1 --secrets-server --tailscale-ssh
#    → Save the displayed password, manage secrets at http://<node-ip>:8090

# 6. Deploy your app
warpgate deploy example-app

# 7. Deploy a new version
warpgate deploy example-app v2.0.0

# 8. Something wrong? Roll back
warpgate rollback example-app
```

That's it. Warpgate handles zero-downtime blue/green swaps, health check gating, and Traefik routing automatically.

## How It Works

Warpgate is an orchestrator, not a config generator. You write standard Docker Compose files for your apps. Warpgate handles:

1. **Bootstrap** — SSH to a node, install Docker, Traefik, optionally SecretSauce server (with TUI progress)
2. **Deploy** — Zero-downtime blue/green deploy with health check gating, secrets fetched from SecretSauce API
3. **Rollback** — Re-deploy the previous version
4. **Status** — Query live deployment state from target nodes
5. **Logs** — Fetch recent container logs from a node, with optional app filter and grep
6. **Remove** — Stop and clean up a single app from target nodes
7. **Cleanup** — Remove all Warpgate dependencies from a node

### Infra Repo Layout

Your deployment configs live in a shared infrastructure repo:

```
infrastructure/
├── cluster.yml                  # Nodes, networking, registry
├── apps/
│   ├── auth/
│   │   ├── app.yml              # Deploy metadata (image, version, targets, domains)
│   │   └── compose.yml          # Standard Docker Compose file
│   ├── api/
│   │   ├── app.yml
│   │   └── compose.yml
│   └── ...
```

**`cluster.yml`** defines your infrastructure:

```yaml
version: "2"
project: myapp

nodes:
  - id: node-1
    host: 10.0.0.1
    tailscale_ip: 100.x.x.x
  - id: node-2
    host: 10.0.0.2
    tailscale_ip: 100.x.x.y

networking:
  tailnet: my-tailnet.ts.net
  dns:
    provider: cloudflare
    zone: example.com
  traefik:
    entry_points: [web, websecure]
    acme:
      enabled: true
      email: admin@example.com
      provider: letsencrypt

registry:
  server: ghcr.io
  username: ${REGISTRY_USERNAME}
  password: ${REGISTRY_TOKEN}

secrets:
  server: http://100.x.x.x:8090  # SecretSauce server URL on tailnet

go_proxy: http://100.x.x.x:3000  # Private Go proxy for SecretSauce install
```

**`apps/<name>/app.yml`** is a small deployment descriptor — the app doesn't need to know about your cluster:

```yaml
image: ghcr.io/org/auth
version: v3.1.0
targets: [node-1, node-2]     # Which nodes to deploy to (omit for all)
domains: [auth.example.com]   # External Traefik routing (omit if no ingress)
internal: auth.internal        # Internal service-to-service hostname (omit if not needed)
secrets_prefix: auth/prod      # SecretSauce key prefix (omit if no secrets)
port: 8085                     # Container port for Traefik
```

**`apps/<name>/compose.yml`** is a standard Docker Compose file you write and maintain. Warpgate uploads it as-is. Secrets referenced as `${VAR}` are fetched from the SecretSauce server at deploy time and injected via a `.env` file:

```yaml
services:
  auth:
    image: ghcr.io/org/auth
    restart: unless-stopped
    ports: ["8085:8085"]
    environment:
      DB_PATH: "/data/auth.db"
      SESSION_AUTH_KEY: ${SESSION_AUTH_KEY}  # Fetched from SecretSauce server
    volumes: [auth-data:/data]
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8085"]
      interval: 10s

  litestream:
    image: litestream/litestream:0.5.6
    command: "replicate -config /etc/litestream.yml"
    volumes: [auth-data:/data]
    depends_on:
      auth: { condition: service_started }
    restart: unless-stopped

volumes:
  auth-data:
```

## Commands

```bash
# Setup
warpgate init my-project                    # Scaffold cluster.yml + apps/ structure
warpgate bootstrap node-1 --tailscale-ssh   # Install Docker, Traefik (TUI)
warpgate bootstrap node-1 --secrets-server --tailscale-ssh  # Also set up SecretSauce server
warpgate bootstrap --host 100.x.x.x --tailscale-ssh  # Ad-hoc bootstrap by IP
warpgate bootstrap node-1 --dry-run         # Preview bootstrap script

# Deploy
warpgate deploy auth                        # Deploy at version from app.yml
warpgate deploy auth v3.2.0                 # Deploy specific version
warpgate deploy auth --dry-run              # Preview what would happen
warpgate deploy auth --user root            # Specify SSH user
warpgate rollback auth                      # Re-deploy previous version

# Inspect
warpgate status                             # Show cluster, nodes, and all apps
warpgate status auth --tailscale-ssh        # Live status from target nodes
warpgate logs --node node-1 --tailscale-ssh # All container logs on a node
warpgate logs --node node-1 --app auth      # Filter to one app's containers
warpgate logs --node node-1 --grep "error"  # Server-side grep filter
warpgate exec auth -- sh                    # Exec into container (WIP)

# App removal
warpgate remove auth --tailscale-ssh        # Stop and remove app from nodes
warpgate remove auth --force                # Skip confirmation
warpgate remove auth --nodes node-1,node-2  # Target specific nodes

# Deploy locks
warpgate lock break auth --tailscale-ssh    # Remove a stale deploy lock

# Teardown
warpgate cleanup node-1 --tailscale-ssh     # Remove Warpgate from a node
warpgate cleanup node-1 --force             # Skip confirmation
warpgate cleanup node-1 --remove-go --remove-docker  # Also remove Go and Docker
```

## Zero-Downtime Deploys

Warpgate uses blue/green deploys via Docker Compose project naming. Each deploy:

1. Uploads compose + override to the node; if the app has a `secrets_prefix`, fetches secrets from the SecretSauce API and uploads a `.env` file
2. Starts the new version as a separate compose project (e.g., `auth-green`)
3. Both old and new containers run simultaneously with the same Traefik labels — Traefik load-balances between them
4. Polls `docker inspect` for the new container's health check status
5. Once healthy, stops the old compose project (e.g., `auth-blue`), removes `.env` file
6. Saves deploy state for rollback

**Multi-node rolling deploys**: nodes are updated sequentially. Node-1 must pass health checks before node-2 starts. If any node fails, the rollout stops — already-deployed nodes stay on the new version, remaining nodes stay on the old version.

**Health check gating**: if the compose file defines a `healthcheck`, warpgate waits up to 2 minutes for the container to report healthy. If unhealthy, the new version is torn down and the old version keeps running. Apps without a healthcheck deploy immediately.

## Internal Service Routing

When apps need to communicate across nodes (e.g., `brighter-platform` on node-1 talks to `auth` on node-2), Warpgate provides internal load-balanced routing via Traefik and Tailscale.

Add `internal` to an app's `app.yml`:

```yaml
# apps/auth/app.yml
internal: auth.internal
```

Other apps reference it by the internal hostname:

```yaml
# apps/brighter-platform/compose.yml
environment:
  AUTH_SERVICE_HOST: "auth.internal:8080"
```

**How it works**:

- Traefik runs an `internal` entrypoint on port 8080 alongside the public `web`/`websecure` entrypoints
- The compose override adds `extra_hosts` entries so containers can resolve internal hostnames to the Docker host (where Traefik listens)
- At deploy time, warpgate writes a Traefik dynamic config file to every node listing all Tailscale IPs running the service as backends
- Traefik's file provider auto-reloads — cross-node traffic flows over direct WireGuard tunnels (sub-millisecond latency in the same data center)

```
brighter-platform (node-1)
  → auth.internal:8080
  → local Traefik (internal entrypoint)
  → load-balances to:
      auth on node-1 (100.x.x.1:8085, local)
      auth on node-2 (100.x.x.2:8085, via Tailscale)
```

## Bootstrap

Bootstrap installs dependencies on target nodes via Tailscale SSH, with a step-by-step TUI showing progress:

```
  Connecting to 100.95.115.81...

  Bootstrapping test-node (100.95.115.81)

  ✓ Detecting OS (Ubuntu 22.04, amd64)
  ✓ Creating warpgate user
  ✓ Installing Go
  ⠋ Installing Docker
    Configuring docker group
    Installing SecretSauce
    Setting up SSH keys
    Setting up Warpgate + Traefik
```

**What gets installed**:
- Docker and Docker Compose plugin
- Go (for SecretSauce, if `go_proxy` configured)
- [SecretSauce](https://github.com/pangobit/secretsauce) binary (if `go_proxy` configured)
- Traefik reverse proxy with external (80/443) and internal (8080) entrypoints
- `warpgate` system user with docker group access
- SSH keys for node-to-node access

**With `--secrets-server`** (for the node that runs the SecretSauce server):
- SecretSauce configured as a systemd service with auto-unseal via master key file
- Vault automatically initialized with master password (`SS_MASTER_PASSWORD` env or auto-generated)
- Listens on port 8090 (HTTP) and 8091 (gRPC) with web UI enabled
- If password is auto-generated, it is displayed once after bootstrap — save it

**Prerequisites**: Tailscale installed with SSH enabled, passwordless sudo.

**Supported OS**: Ubuntu 18.04+, Debian 10+, CentOS 7+, Rocky Linux 8+, AlmaLinux 8+, Fedora 33+, Amazon Linux.

### Remote Node Layout

After bootstrap and deploys, each node has:

```
/opt/warpgate/
├── traefik/
│   ├── compose.yml              # Traefik service (started at bootstrap)
│   └── dynamic/                 # Auto-reloaded by Traefik file provider
│       ├── auth.yml             # Internal route: auth.internal → [node IPs]
│       └── api.yml              # Internal route: api.internal → [node IPs]
├── secretsauce/                 # Only on --secrets-server nodes
│   ├── vault.db                 # Encrypted secrets database
│   └── master.key               # Master key for auto-unseal (0600)
├── apps/
│   ├── auth/
│   │   ├── compose.yml              # Uploaded from infra repo
│   │   ├── docker-compose.override.yml  # Generated (Traefik labels + image tag)
│   │   └── state.json               # Deploy state (version, slot, previous version)
│   └── ...
```

## Secrets Management

Warpgate uses [SecretSauce](https://github.com/pangobit/secretsauce) as a secrets server running on the tailnet. Secrets are managed centrally and fetched at deploy time — no secrets database or encryption keys on target nodes.

### How it works

1. **SecretSauce server** runs on one node, auto-unsealed via a master key file
2. Secrets are managed via the SecretSauce web UI, CLI, or API
3. At deploy time, Warpgate fetches secrets matching the app's `secrets_prefix` from the API
4. Secrets are uploaded as a `.env` file to the node (0600 permissions)
5. `docker compose --env-file .env up -d` resolves `${VAR}` references in compose.yml
6. The `.env` file is deleted after the health check passes

### Setup

1. Add the server URL to `cluster.yml`:
   ```yaml
   secrets:
     server: http://100.x.x.x:8090
   ```

2. Bootstrap the secrets server node (vault is initialized automatically):
   ```bash
   warpgate bootstrap secrets-node --secrets-server --tailscale-ssh
   ```
   If `SS_MASTER_PASSWORD` is set, that password is used. Otherwise a strong password is auto-generated and displayed once — save it.

3. Open the SecretSauce web UI at `http://<node-ip>:8090`, log in, and add secrets.

4. Deploy as usual — Warpgate fetches secrets from the API and injects them automatically.

## Networking Model

- **External traffic**: Traefik routes public domains (`auth.example.com`) to local containers via Docker labels
- **Internal traffic**: Traefik routes internal hostnames (`auth.internal`) to containers across all nodes via Tailscale IPs
- **Same-node**: Routed via Docker network (no Tailscale overhead)
- **Cross-node**: Routed via Tailscale WireGuard tunnels (direct peer-to-peer, no relay)
- **Load balancing**: Traefik health-checks backends and distributes traffic across healthy instances

## Project Structure

```
warpgate/
├── cmd/
│   ├── warpgate/       # CLI binary
│   └── warpd/          # Daemon binary (future)
├── pkg/
│   ├── cli/            # Cobra commands
│   ├── config/         # Config types, loading, app discovery
│   ├── compose/        # Compose override generator (Traefik labels, internal routing)
│   ├── deploy/         # Blue/green deploy, health checks, internal route configs
│   ├── secrets/        # SecretSauce API client, .env file generation
│   ├── ssh/            # SSH client (key-based and Tailscale)
│   ├── bootstrap/      # Node provisioning (OS detection, install scripts, steps)
│   ├── cleanup/        # Node cleanup (reverse bootstrap)
│   ├── tui/            # Charmbracelet v2 step-runner TUI
│   └── daemon/         # Daemon (future)
└── examples/
    └── infra-repo/     # Example infrastructure repo layout
```

## Build

```bash
go build ./cmd/warpgate    # Build CLI
go test ./...              # Run tests
go vet ./...               # Vet
```

## Status

Core deployment flow is implemented. Remaining work:

- [x] Zero-downtime blue/green deploys
- [x] Health check gating
- [x] Rolling multi-node deploys
- [x] Internal service-to-service routing
- [x] TUI for bootstrap and cleanup
- [x] Deploy locking (prevents concurrent deploys)
- [x] Per-app removal
- [x] Live status queries
- [x] Rollback to previous version
- [ ] Image watcher / CI push trigger
- [x] Node-centric log inspection
- [ ] Exec into containers
- [ ] Web dashboard
- [ ] Backup/restore for volumes

## Credits

Inspired by [Kamal](https://kamal-deploy.org/) by 37signals.
