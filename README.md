# Warpgate

Deploy Docker Compose apps to Linux hosts over SSH/Tailscale. Desired state lives in git; a daemon watches the repo and commits image bumps; a human operator deploys from a TUI.

## Architecture

Three roles:

| Role | Host | Responsibility |
| --- | --- | --- |
| **Infra repo** | GitHub | `cluster.yml`, per-app `app.yml`, user-written `compose.yml` |
| **Daemon** | One Linux server on your tailnet | Sync config, watch GHCR, commit semver bumps, run deploys when told |
| **Target nodes** | App hosts on your tailnet | Docker, Traefik, SecretSauce; apps under `/opt/warpgate/apps/` |

Rules:

- Git holds desired state. The daemon is the only release actor (bump commits). The operator is the only deploy actor (TUI).
- The daemon polls GitHub via API (no local checkout). Deploys reach nodes over SSH/Tailscale SSH.
- Stack deploys are atomic: all apps at their latest release, in name order. Success advances a last-healthy baseline; failure reverts touched apps to it.
- One binary: `warpgate serve` is the daemon. Workstation commands (`init`, `bootstrap`, `shadow`, etc.) use the same CLI.

Typical flow:

1. CI pushes `ghcr.io/acme/api:1.2.8`.
2. Daemon matches `image_semver`, pins tag+digest, commits to `app.yml`.
3. TUI shows a pending release.
4. Operator presses `d`; daemon deploys the stack and health-checks it.

## Format

Repo layout:

```text
my-infra/
├── cluster.yml
└── apps/
    └── api/
        ├── app.yml
        └── compose.yml
```

App names come from directory names, not YAML. See [`examples/infra-repo`](examples/infra-repo).

### `cluster.yml`

Cluster inventory and shared settings: nodes, networking/Traefik/ACME, registry, optional SecretSauce URL, optional `go_proxy` for bootstrap.

```yaml
version: "2"
project: myapp

nodes:
  - id: node-1
    host: 203.0.113.10
    private_ip: 100.64.0.10

networking:
  private_network: my-tailnet.ts.net
  dns:
    provider: cloudflare
    zone: example.com
    api_token: ${CF_DNS_API_TOKEN}
  traefik:
    entry_points: [web, websecure]
    acme:
      enabled: true
      email: admin@example.com
      provider: letsencrypt
      challenge: dns

registry:
  server: ghcr.io

secrets:
  server: http://100.64.0.10:8090
```

### `app.yml`

Deploy metadata. Service images, env, routing, and strategy live under `release.services`. Warpgate overlays images and generated env files onto your compose topology — it does not generate the base compose file.

```yaml
kind: warpgate/app
targets: [node-1]
strategy: blue-green

release:
  services:
    api:
      image: ghcr.io/acme/api
      image_semver: "~1.2"      # daemon-owned pin when set
      secrets_prefix: api/prod
      port: 8080
      expose:
        public:
          domains: [api.example.com]
      environment:
        LOG_LEVEL: info
```

- `image_semver`: `*`, exact `1.2.3`, `~1.2`, or `^1`. Prereleases excluded unless named.
- `strategy`: `blue-green` (default) or `recreate` (host port bindings, single-writer volumes).
- `source` + `compose_ref`: fetch compose from another GitHub repo instead of local `compose.yml`.
- `persistent_volumes`: stable Docker volume names across blue/green slots.

### `compose.yml`

User-written. Define services, health checks, and topology. Health checks gate deploy success; without one, `docker compose up -d` succeeding is enough.

At deploy time Warpgate uploads compose (local or fetched), writes `docker-compose.override.yml` (image, env files, Traefik labels, volume name overrides), and runs compose on the node.

## Target nodes

Prepare nodes from a workstation with `cluster.yml` and SSH/Tailscale access:

```bash
warpgate bootstrap node-1 --tailscale-ssh
warpgate bootstrap --host 203.0.113.10 --tailscale-ssh
```

**Prerequisites:** Linux (Ubuntu, Debian, CentOS, Rocky, AlmaLinux, Fedora, Amazon Linux), passwordless `sudo`, image registry access. Tailscale + Tailscale SSH if using `--tailscale-ssh`.

**Bootstrap installs:** Docker + Compose plugin, Go, SecretSauce (when `go_proxy` set), `warpgate` user, Traefik, internal proxy, SecretSauce systemd service, `/opt/warpgate/apps/` and `/opt/warpgate/traefik/`.

**Not installed on nodes:** the `warpgate` binary (daemon runs elsewhere).

Registry and ACME credentials can be supplied via env at bootstrap time (`REGISTRY_USERNAME`, `REGISTRY_TOKEN`, `CF_DNS_API_TOKEN`, `SS_MASTER_PASSWORD`). SecretSauce keys are fetched at deploy time using each service's `secrets_prefix`.

Remove with `warpgate cleanup [node-id] --tailscale-ssh` (optional `--remove-go`, `--remove-docker`).

## Daemon

Runs on a dedicated host. Install from a [GitHub Release](https://github.com/pangobit/warpgate/releases) (`warpgate-linux-amd64` / `warpgate-linux-arm64`) or `go install github.com/pangobit/warpgate/cmd/warpgate@latest`. Production layout: `/usr/local/bin/warpgate` + systemd — see [`examples/systemd/warpgate.service`](examples/systemd/warpgate.service).

```bash
export WARPGATE_REPO=acme/my-infra
export WARPGATE_GH_APP_ID=123456
export WARPGATE_GH_INSTALLATION_ID=7891011
export WARPGATE_GH_PRIVATE_KEY_FILE=/etc/warpgate/github-app.pem
export WARPGATE_REGISTRY_TOKEN=ghp_...          # classic PAT, read:packages only
export WARPGATE_SSH_ADDR=100.64.0.20:7422
export WARPGATE_HTTP_ADDR=100.64.0.20:7411
warpgate serve --user root
```

| Variable | Purpose | Default |
| --- | --- | --- |
| `WARPGATE_REPO` | Desired-state repo `owner/repo` | required on first run |
| `WARPGATE_REPO_BRANCH` | Branch to watch/write | `master` |
| `WARPGATE_GH_APP_ID` / `WARPGATE_GH_INSTALLATION_ID` / `WARPGATE_GH_PRIVATE_KEY_FILE` | GitHub App (Contents read+write) | required |
| `WARPGATE_REGISTRY_TOKEN` | GHCR reads (App tokens do not work on GHCR) | optional |
| `WARPGATE_SSH_ADDR` | Operator TUI | `127.0.0.1:7422` |
| `WARPGATE_HTTP_ADDR` | CI API | `127.0.0.1:7411` |
| `WARPGATE_DB_PATH` / `WARPGATE_HOST_KEY` | Daemon state | under config dir |

Bind listen addresses to a tailnet IP; access control is Tailscale ACLs, not app-level auth.

**Upgrade:** `sudo warpgate upgrade` (or `--version v1.2.3`, `--dry-run`). Verifies SHA-256 from release `checksums.txt`, replaces binary, restarts `warpgate.service`.

**CI nudge:** `POST /refresh`, `GET /status` on the HTTP addr. Defaults: config poll 1m, image poll 5m.

## CLI and TUI

Install CLI: `go install github.com/pangobit/warpgate/cmd/warpgate@latest`

```bash
warpgate init myapp                    # scaffold infra repo
warpgate version
warpgate upgrade                       # daemon host only
warpgate preview                       # local TUI with fixture data

warpgate bootstrap node-1 --tailscale-ssh
warpgate cleanup node-1 --tailscale-ssh

warpgate shadow deploy api v2.0.0 --tailscale-ssh   # internal-network test deploy
warpgate shadow promote api --tailscale-ssh
warpgate shadow remove api --tailscale-ssh
```

Deploy, rollback, status, and logs are **not** workstation commands — use the TUI:

```bash
ssh -p 7422 <daemon-tailnet-addr>
```

| Key | Action |
| --- | --- |
| `d` | Deploy stack (confirm) |
| `r` | Rollback to last-healthy baseline (confirm) |
| `s` | Poll now |
| `a` | Audit log |
| `u` | Refresh view |
| `q` | Quit |

To explore the TUI locally without daemon credentials, SSH, GitHub, GHCR, or a
real stack, run:

```bash
warpgate preview
warpgate preview --scenario failure
warpgate preview --scenario empty
```

Preview mode uses in-memory fixture data and a fake deployer, so deploy and
rollback confirmations are safe to exercise.

Shadow deploys run a candidate alongside live on the internal network (`shadow-<hostname>` when `expose.internal.hostname` is set). One shadow per app; requires an existing live deployment.

`warpgate <command> --help` for flags.

## Development

```bash
go build ./cmd/warpgate
go test ./...
go vet ./...
```

Local daemon smoke test (not production): `docker compose up --build` with `.env` setting `WARPGATE_REPO`, GitHub App creds, and `WARPGATE_GH_PRIVATE_KEY_FILE` (host path, bind-mounted). State persists in the `warpgate-state` volume.