# Warpgate

Warpgate (named after the Starcraft Protoss building) is a lightweight app deployment and orchestration toolset written in Go. It replaces our k3s + Flux setup with something simpler built on Docker Compose.

## Stack

- **Orchestration**: Docker Compose + daemon for change detection
- **Persistent Storage**: Named volumes; SQLite apps replicate via Litestream
- **Cluster Networking**: Tailscale mesh
- **Reverse Proxy**: Traefik with automatic HTTPS (Let's Encrypt)
- **Secrets**: [SecretSauce](https://github.com/pangobit/secretsauce)
- **DNS**: Cloudflare
- **Access**: Tailscale SSH (admin to cluster, CI to cluster, node to node)

## Getting Started

### Build

```bash
go build ./cmd/warpgate
go build ./cmd/warpd
```

### Initialize a Project

```bash
warpgate init my-project
```

Creates a `warpgate.yml` config file. Edit it with your node details, apps, and networking.

### Commands

```bash
warpgate status                         # Show cluster and app status
warpgate deploy <app> [version]         # Deploy an app
warpgate logs <app>                     # Stream app logs
warpgate rollback <app>                 # Rollback to previous version
warpgate exec <app> <command>           # Run command in container
warpgate generate [node-id]             # Generate Docker Compose files (all nodes if omitted)
warpgate bootstrap <node-id>            # Bootstrap a node via SSH
warpgate bootstrap --host 10.0.0.5      # Ad-hoc bootstrap by IP
warpgate bootstrap <node-id> --dry-run  # Preview bootstrap script
```

### Daemon

```bash
warpd server                    # Start control plane
WARPGATE_MODE=agent warpd       # Start as agent only
```

### Environment-Specific Configs

```bash
warpgate -c warpgate.dev.yml status
warpgate -c warpgate.prod.yml deploy my-app
```

Environment variable expansion is supported in configs:

```yaml
registry:
  username: ${REGISTRY_USERNAME}
  password: ${REGISTRY_TOKEN}

apps:
  - name: my-app
    version: ${MYAPP_VERSION:-latest}
```

## Networking Model

Warpgate generates **one Docker Compose file per node** containing all apps targeted at that node.

- **Same-node**: Services resolve each other by service name via Docker DNS (e.g. `auth:8085`). No configuration needed.
- **Cross-node**: Services communicate via their Traefik domains (e.g. `https://auth.brighter.io`). Traefik provides load balancing for multi-node services.

## Configuration

See `examples/cluster-config/warpgate.yml` for a full example and `examples/environments/` for dev/prod variants.

All string values support `${VAR}` and `${VAR:-default}` environment variable expansion.

### Top-Level Fields

| Field | Required | Description |
|-------|----------|-------------|
| `version` | No | Config version. Defaults to `"1"` |
| `project` | Yes | Project name, used as Docker Compose project prefix |
| `nodes` | Yes | List of cluster nodes (at least one) |
| `networking` | No | Tailscale, DNS, and Traefik settings |
| `apps` | No | List of applications to deploy |
| `registry` | No | Docker registry credentials |
| `secrets` | No | Secrets provider configuration |
| `go_proxy` | No | Private Go module proxy URL (on tailnet). Used by bootstrap to install SecretSauce |

### `nodes[]`

| Field | Required | Description |
|-------|----------|-------------|
| `id` | Yes | Unique node identifier (e.g. `node-1`) |
| `host` | Yes | IP address or hostname |
| `tailscale_ip` | No | Tailscale mesh IP |
| `roles` | No | List of `control-plane` and/or `worker`. Defaults to both if omitted |
| `labels` | No | Key-value labels (e.g. `region: us-east`) |

### `networking`

| Field | Required | Description |
|-------|----------|-------------|
| `tailnet` | No | Tailscale tailnet name (e.g. `your-tailnet.ts.net`) |
| `dns.provider` | No | DNS provider (`cloudflare`) |
| `dns.zone` | No | DNS zone (e.g. `example.com`) |
| `dns.api_token` | No | DNS provider API token |
| `traefik.entry_points` | No | Traefik entrypoints, typically `[web, websecure]` |
| `traefik.acme.enabled` | No | Enable automatic HTTPS via Let's Encrypt |
| `traefik.acme.email` | No | ACME registration email |
| `traefik.acme.provider` | No | ACME provider: `letsencrypt` or `zerossl` |
| `traefik.acme.staging` | No | Use staging certs (avoids rate limits during testing) |

### `registry`

| Field | Required | Description |
|-------|----------|-------------|
| `server` | No | Registry hostname (e.g. `ghcr.io`) |
| `username` | No | Registry username |
| `password` | No | Registry password/token |

### `secrets`

| Field | Required | Description |
|-------|----------|-------------|
| `provider` | No | Secrets backend: `secretsauce`, `env`, or `file` |
| `config` | No | Provider-specific key-value config (e.g. `endpoint`, `token`) |

### `apps[]`

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Application name |
| `image` | Yes | Docker image (e.g. `ghcr.io/org/app`) |
| `version` | No | Image tag. Defaults to `latest` |
| `replicas` | No | Per-node replica count |
| `targets` | No | Node IDs to deploy to, or `[all]`. Defaults to all nodes |
| `domains` | No | Domain names for Traefik routing (auto-generates router labels) |
| `ports` | No | Port mappings (see below) |
| `env` | No | Environment variables as key-value pairs |
| `secrets` | No | List of secret names to inject from the secrets provider |
| `resources` | No | CPU/memory requests and limits (see below) |
| `volumes` | No | Named volume mounts (see below) |
| `health_check` | No | Health check configuration (see below) |
| `sidecars` | No | Sidecar containers that run alongside the app (see below) |
| `init` | No | Init containers that run before the app starts (see below) |
| `compose_file` | No | Path to a custom Docker Compose file override |

### `apps[].ports[]`

| Field | Required | Description |
|-------|----------|-------------|
| `container` | Yes | Container port |
| `host` | No | Explicit host port. If omitted, only the container port is exposed |
| `protocol` | No | `tcp` (default) or `udp` |

### `apps[].resources`

| Field | Required | Description |
|-------|----------|-------------|
| `cpus` | No | CPU reservation (e.g. `"0.5"`) |
| `memory` | No | Memory reservation (e.g. `"512M"`) |
| `cpu_limit` | No | CPU limit (e.g. `"1.0"`) |
| `memory_limit` | No | Memory limit (e.g. `"1G"`) |

### `apps[].volumes[]`

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Named volume identifier |
| `path` | Yes | Mount path inside the container |
| `size` | No | Size hint |
| `backup` | No | Include in backups |

### `apps[].health_check`

Either `path` or `command` should be set, not both.

| Field | Required | Description |
|-------|----------|-------------|
| `path` | No | HTTP health check path (e.g. `/health`) |
| `port` | No | Port for HTTP health check |
| `command` | No | Shell command health check (alternative to HTTP) |
| `interval` | No | Check interval (e.g. `"10s"`) |
| `timeout` | No | Check timeout (e.g. `"5s"`) |
| `retries` | No | Failure count before unhealthy |

### `apps[].sidecars[]`

Sidecar containers run alongside the main app. In the generated compose file, each sidecar becomes a service with `depends_on` the main app (`condition: service_started`) and `restart: unless-stopped`.

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Sidecar name (compose service becomes `{app}-{name}`) |
| `image` | Yes | Docker image |
| `command` | No | Override container command |
| `volumes` | No | Volume mounts in `name:/path` format |
| `env` | No | Environment variables |

### `apps[].init[]`

Init containers run before the main app starts. In the generated compose file, the main app `depends_on` each init container with `condition: service_completed_successfully`. Init containers use `restart: "no"`.

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Init container name (compose service becomes `{app}-{name}`) |
| `image` | Yes | Docker image |
| `command` | No | Command to run |
| `volumes` | No | Volume mounts in `name:/path` format |
| `env` | No | Environment variables |

**Example (auth with litestream):**

```yaml
apps:
  - name: auth
    image: ghcr.io/pangobit/auth:v3.1.0
    volumes:
      - name: auth-data
        path: /data
    sidecars:
      - name: litestream
        image: litestream/litestream:0.5.6
        volumes: [auth-data:/data]
        env:
          LITESTREAM_URL: ${LITESTREAM_URL}
    init:
      - name: litestream-restore
        image: litestream/litestream:0.5.6
        command: "litestream restore /data/auth.db"
        volumes: [auth-data:/data]
        env:
          LITESTREAM_URL: ${LITESTREAM_URL}
```

## Project Structure

```
warpgate/
├── cmd/
│   ├── warpgate/       # CLI tool
│   └── warpd/          # Daemon (server + agent)
├── pkg/
│   ├── cli/            # Cobra commands
│   ├── compose/        # Docker Compose generation
│   ├── config/         # warpgate.yml types and loading
│   ├── daemon/         # Daemon implementation
│   └── bootstrap/      # Node bootstrap via SSH (OS detection, install scripts)
└── examples/
    ├── cluster-config/ # Full cluster config example
    └── environments/   # Dev/prod config examples
```

## Bootstrap

Bootstrap installs dependencies on target nodes via SSH:
- Go, Docker, Docker Compose plugin
- SecretSauce (via private Go proxy on tailnet, if `go_proxy` is configured)
- `warpgate` system user with docker group access
- SSH keys for node-to-node access

**Prerequisites**: Tailscale installed, SSH server running, passwordless sudo.

**Supported OS**: Ubuntu 18.04+, Debian 10+, CentOS 7+, Rocky Linux 8+, AlmaLinux 8+, Fedora 33+, Amazon Linux.

## Status

WIP. See roadmap:

- [ ] Core deployment orchestration
- [ ] Rolling update strategy
- [ ] TUI dashboard (Charmbracelet)
- [ ] Web UI
- [ ] CI webhook API
- [ ] SecretSauce integration
- [ ] File watcher (GitOps)
- [ ] Backup/restore commands

## Credits

Inspired by [Kamal](https://kamal-deploy.org/). Built with [Charmbracelet](https://charm.sh/).
