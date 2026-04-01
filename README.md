# Warpgate

Warpgate is a lightweight app deployment tool written in Go. It replaces our k3s + Flux setup with something simpler: Docker Compose on bare metal, orchestrated over Tailscale SSH.

## Stack

- **Runtime**: Docker Compose (user-written, not generated)
- **Reverse Proxy**: Traefik with automatic HTTPS via Let's Encrypt
- **Networking**: Tailscale mesh (node-to-node, admin-to-node, CI-to-node)
- **Secrets**: [SecretSauce](https://github.com/pangobit/secretsauce) — injects secrets as env vars at runtime
- **DNS**: Cloudflare
- **Storage**: Named Docker volumes; SQLite apps replicate via Litestream

## How It Works

Warpgate is an orchestrator, not a config generator. You write standard Docker Compose files for your apps. Warpgate handles:

1. **Bootstrap** — SSH to a node, install Docker, Traefik, SecretSauce
2. **Deploy** — Upload your compose file, inject Traefik labels via a thin override, pull and start containers with secrets
3. **Rollback** — Re-deploy the previous version

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

go_proxy: http://100.x.x.x:3000  # Private Go proxy for SecretSauce install
```

**`apps/<name>/app.yml`** is a small deployment descriptor — the app doesn't need to know about your cluster:

```yaml
image: ghcr.io/org/auth
version: v3.1.0
targets: [node-1]           # Which nodes to deploy to (omit for all)
domains: [auth.example.com] # Traefik routing (omit if no ingress needed)
secrets_prefix: auth/prod   # SecretSauce prefix (omit if no secrets)
port: 8085                  # Container port for Traefik load balancer
```

**`apps/<name>/compose.yml`** is a standard Docker Compose file you write and maintain. Warpgate doesn't generate or modify it — it uploads it as-is. Secrets referenced as `${VAR}` are injected by SecretSauce at runtime:

```yaml
services:
  auth:
    image: ghcr.io/org/auth
    restart: unless-stopped
    ports: ["8085:8085"]
    environment:
      DB_PATH: "/data/auth.db"
      SESSION_AUTH_KEY: ${SESSION_AUTH_KEY}  # Injected by SecretSauce
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
warpgate bootstrap node-1 --tailscale-ssh   # Install Docker, Traefik, SecretSauce on a node
warpgate bootstrap --host 100.x.x.x --tailscale-ssh  # Ad-hoc bootstrap by IP

# Deploy
warpgate deploy auth                        # Deploy app at version from app.yml
warpgate deploy auth v3.2.0                 # Deploy specific version
warpgate deploy auth --dry-run              # Preview what would happen
warpgate rollback auth                      # Re-deploy previous version

# Inspect
warpgate status                             # Show cluster, nodes, and all apps
warpgate logs auth                          # Stream app logs (WIP)
warpgate exec auth -- sh                    # Exec into container (WIP)
```

### Deploy Flow

`warpgate deploy auth v3.2.0` does the following on each target node:

1. Uploads `apps/auth/compose.yml` to `/opt/warpgate/apps/auth/compose.yml`
2. Generates a thin `docker-compose.override.yml` with Traefik labels and the image tag
3. Runs `docker compose pull`
4. Runs `secretsauce run auth/prod -- docker compose -f compose.yml -f docker-compose.override.yml up -d`
5. Saves deploy state (`state.json`) for rollback

The generated override is the **only** thing Warpgate creates — it looks like:

```yaml
services:
  auth:
    image: ghcr.io/org/auth:v3.2.0
    labels:
      traefik.enable: "true"
      traefik.http.routers.auth.rule: "Host(`auth.example.com`)"
      traefik.http.routers.auth.entrypoints: "web,websecure"
      traefik.http.routers.auth.tls.certresolver: "letsencrypt"
      traefik.http.services.auth.loadbalancer.server.port: "8085"
    networks: [warpgate]
networks:
  warpgate:
    external: true
```

## Bootstrap

Bootstrap installs dependencies on target nodes via Tailscale SSH:

- Docker and Docker Compose plugin
- Go (for SecretSauce installation)
- [SecretSauce](https://github.com/pangobit/secretsauce) via private Go proxy on tailnet
- Traefik reverse proxy (as a Docker Compose service on the `warpgate` network)
- `warpgate` system user with docker group access
- SSH keys for node-to-node access

**Prerequisites**: Tailscale installed with SSH enabled, passwordless sudo.

**Supported OS**: Ubuntu 18.04+, Debian 10+, CentOS 7+, Rocky Linux 8+, AlmaLinux 8+, Fedora 33+, Amazon Linux.

### Remote Node Layout

After bootstrap and deploys, each node has:

```
/opt/warpgate/
├── traefik/
│   └── compose.yml              # Traefik service (started at bootstrap)
├── apps/
│   ├── auth/
│   │   ├── compose.yml              # Uploaded from infra repo
│   │   ├── docker-compose.override.yml  # Generated by warpgate
│   │   └── state.json               # Deploy state (version, previous version)
│   └── ...
```

## Networking Model

- **Same-node**: Services resolve each other by service name via Docker DNS (e.g. `auth:8085`)
- **Cross-node**: Services communicate via Traefik domains (e.g. `https://auth.example.com`)
- **Traefik** runs per-node, discovers containers via Docker labels on the shared `warpgate` network
- All nodes and some services are on the Tailscale tailnet

## Project Structure

```
warpgate/
├── cmd/
│   ├── warpgate/       # CLI binary
│   └── warpd/          # Daemon binary (future)
├── pkg/
│   ├── cli/            # Cobra commands
│   ├── config/         # Config types, loading, app discovery
│   ├── compose/        # Compose override generator (Traefik labels)
│   ├── deploy/         # Deploy orchestration, state management
│   ├── ssh/            # SSH client (key-based and Tailscale)
│   ├── bootstrap/      # Node provisioning (OS detection, install scripts)
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

- [ ] Rolling update strategy (blue/green via Traefik)
- [ ] Image watcher / CI push trigger
- [ ] Log streaming and exec commands
- [ ] TUI/Web dashboard
- [ ] Backup/restore for volumes

## Credits

Inspired by [Kamal](https://kamal-deploy.org/) by 37signals.
