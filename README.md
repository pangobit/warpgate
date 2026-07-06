# Warpgate

Warpgate is a Go deployment tool for Docker Compose applications on Linux hosts over SSH. It is aimed at small self-managed clusters where you want a repo with `cluster.yml`, per-app `app.yml`, and user-written `compose.yml` files instead of a larger orchestration stack.

Warpgate 3 runs as a daemon on a server. The daemon watches your desired-state repository, commits semantic-version image bumps itself, and deploys only when a human operator says so from a TUI served over SSH. Git holds all desired state; the daemon is the only release actor; the operator is the only deploy actor.

It currently provides:

- Repo scaffolding with `warpgate init`
- Node bootstrap over SSH or Tailscale SSH
- A daemon (`warpgate serve`) that polls the desired-state repo and GHCR
- Automatic version-bump commits for services tracking a semver constraint
- An operator TUI served over SSH: deploy, rollback, pending updates, audit
- Atomic whole-stack deploys with automatic revert to the last healthy baseline
- Blue/green or recreate deployment strategies per app
- A lean HTTP API for CI (`POST /refresh`, `GET /status`)
- Shadow deployments for pre-release testing on the internal network

## Requirements

Target nodes:

- Linux
- Tailscale installed if you want to use Tailscale SSH
- Passwordless `sudo`
- Network access to pull container images

Daemon host:

- Linux host reachable over your tailnet
- Tailscale SSH access to the target nodes
- A GitHub App installed for the desired-state repository

The GitHub App needs **Contents: read and write** (config sync and bump commits). Generate a private key and note the App ID and installation ID.

**Private GHCR images need a separate registry token.** GHCR does not accept GitHub App installation tokens at all — the App's "Packages" permission exists in the UI but has no effect on the container registry (a years-old GitHub limitation, confirmed by GitHub support). Outside Actions, GHCR accepts classic personal access tokens. Create a classic PAT with **only** the `read:packages` scope (no repo scope — it cannot touch git) and set it as `WARPGATE_REGISTRY_TOKEN` on the daemon. Without it, the daemon authenticates anonymously and only public images can be watched; private images show `403 Forbidden` under Pending updates in the TUI.

Supported bootstrap targets in the code today include Ubuntu, Debian, CentOS, Rocky Linux, AlmaLinux, Fedora, and Amazon Linux.

## Quick Start

Install or build the CLI:

```bash
go install github.com/pangobit/warpgate/cmd/warpgate@latest
```

Create a repo:

```bash
mkdir my-infra
cd my-infra
warpgate init myapp
```

That creates:

```text
cluster.yml
apps/
  example-app/
    app.yml
    compose.yml
```

Edit `cluster.yml` with your node details (see below), push the repo to GitHub, and bootstrap each node:

```bash
warpgate bootstrap node-1 --tailscale-ssh
```

Then start the daemon on its host:

```bash
export WARPGATE_REPO=acme/my-infra
export WARPGATE_GH_APP_ID=123456
export WARPGATE_GH_INSTALLATION_ID=7891011
export WARPGATE_GH_PRIVATE_KEY_FILE=/etc/warpgate/github-app.pem
export WARPGATE_SSH_ADDR=100.64.0.20:7422
export WARPGATE_HTTP_ADDR=100.64.0.20:7411
warpgate serve --user root
```

And operate it from anywhere on the tailnet:

```bash
ssh -p 7422 100.64.0.20
```

The TUI shows the synced config commit, pending image updates, stack state, and the audit log. Press `d` to deploy the stack, `r` to roll back to the last healthy baseline.

## The Warpgate 3 Flow

1. You push code; CI builds and pushes `ghcr.io/acme/api:1.2.8`.
2. The daemon sees the new tag matches `image_semver: "~1.2"`, resolves its digest, and commits the pin to `app.yml` (`warpgate: release api api=1.2.8`).
3. The TUI shows the bump as a pending release.
4. You press `d`. The daemon deploys every app at its latest release, health-checks the stack, and advances the last-healthy baseline — or reverts everything to it on failure.

Humans commit config changes (ports, env, services). The daemon commits version bumps. Nobody commits generated deploy files from a workstation, and the operator never runs a git command to release.

Config-only commits made by humans also become deployable releases automatically when the daemon syncs them.

## Repo Layout

Warpgate expects an infra repo like this:

```text
my-infra/
├── cluster.yml
└── apps/
    ├── api/
    │   ├── app.yml
    │   └── compose.yml
    └── web/
        ├── app.yml
        └── compose.yml
```

- `cluster.yml` defines cluster-wide settings and node inventory.
- `apps/<name>/app.yml` defines deployment metadata.
- `apps/<name>/compose.yml` is your Docker Compose file.
- App names come from directory names, not from YAML.

The repository under [`examples/infra-repo`](examples/infra-repo) shows a working example layout.

## `cluster.yml`

Minimal fields:

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

go_proxy: http://100.64.0.10:3000
```

Notes:

- `project` is required.
- `nodes` must contain at least one node with `id` and `host`.
- `private_ip` is used for internal routing and internal proxy binding.
- `registry.username` and `registry.password` are supported, but can also be stored in SecretSauce during bootstrap.
- `secrets.server` is optional. Without it, secret fetching is skipped.
- `go_proxy` is optional and is used during bootstrap when installing SecretSauce.
- `networking.dns.api_token` should be injected via environment expansion rather than committed directly.
- `networking.traefik.acme.challenge` defaults to `tls`; use `dns` for private services that still need public CA certificates.

## `app.yml`

Example:

```yaml
kind: warpgate/app
compose_ref: master
targets: [node-1]
strategy: blue-green

release:
  services:
    api:
      image: ghcr.io/acme/api
      image_semver: "~1.2"
      image_tag: 1.2.7
      image_digest: sha256:...
      secrets_prefix: api/prod
      port: 8080
      expose:
        public:
          domains: [api.example.com]
      environment:
        LOG_LEVEL: info
        APP_ENV: production

persistent_volumes:
  - compose_name: api-data
    name: warpgate-api-data
```

Release-owned deploy inputs must be declared under `release.services`. The compose file remains the topology source; Warpgate only overlays service images and generated env files for declared release services.

Fields:

- `kind` is optional. If set, it must be `warpgate/app`.
- `release.services.<name>.image` is required for each first-class release service.
- `release.services.<name>.image_semver` opts the service into daemon version tracking. Supported constraints: `*` (any stable version), `1.2.3` (exact), `~1.2` (same major.minor), `^1` (same major). Prereleases are excluded unless the constraint names one. Floating tags such as `1` or `1.2` are never selected.
- `release.services.<name>.image_tag` and `image_digest` are daemon-owned once `image_semver` is set: the daemon pins both in a bump commit. Without `image_semver`, you manage them by hand.
- `compose_ref` identifies the remote compose source revision when `source` is set.
- `targets` defaults to all nodes if omitted.
- `release.services.<name>.secrets_prefix` tells Warpgate which SecretSauce keys to fetch for that service.
- `release.services.<name>.port` is the container port used for service-level routing metadata.
- `strategy` may be `blue-green` or `recreate`.
- `persistent_volumes` remaps compose volume keys to stable Docker volume names managed by Warpgate.
- `release.services.<name>.environment` adds non-secret key/value pairs to that service's generated env file during deploy.
- `source` can be used instead of a local `compose.yml` to fetch a compose file from GitHub.

`source` example:

```yaml
compose_ref: master
source:
  repo: github.com/acme/deploy-definitions
  compose_path: services/worker/compose.yml
release:
  services:
    worker:
      image: ghcr.io/acme/worker
      image_semver: "^2"
```

Source compose files are read through the GitHub API using the daemon's GitHub App installation token. Private source repositories work when the installation has access to them.

Current validation rules to keep in mind:

- `expose.public` requires `port` and at least one domain.
- `expose.private` requires a port.
- `expose.internal` requires `port` and a hostname.
- `image_semver` must parse as a supported constraint.

## `compose.yml`

Warpgate does not generate your base compose file. You write and maintain it.

Example:

```yaml
services:
  api:
    image: ghcr.io/acme/api
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3
```

Deploy-time behavior:

- If `app.yml` has a local `compose.yml`, Warpgate uploads it to the target node.
- If `app.yml` uses `source`, Warpgate fetches the compose file from GitHub instead.
- Warpgate writes a `docker-compose.override.yml` that injects each release service's image reference, per-service `env_file` reference, and `extra_hosts` entries for internal hostnames.
- If `persistent_volumes` is set, Warpgate also injects top-level `volumes:` name overrides so named volumes stay stable across blue/green slot changes.
- If any release service has `environment` or `secrets_prefix`, Warpgate writes `.env.<service>` files for containers and a merged temporary `.env` for Compose interpolation.

Health checks matter:

- If the compose file defines a container health check, deploy waits for it before considering the rollout successful.
- Without a health check, deploy proceeds as soon as `docker compose up -d` succeeds.
- Stack deploys use these health checks to decide whether to advance the last-healthy baseline or revert.

## Deployment Strategies

Warpgate supports two strategies:

- `blue-green`
- `recreate`

`blue-green` is the default. Warpgate starts a new compose project, waits for health, and then tears down the previous slot.

`recreate` stops the old slot before starting the new one. Use it when your compose file binds host ports and both slots cannot run at the same time.

For stateful apps that use SQLite or another single-writer local store, pair `recreate` with `persistent_volumes` so both slots resolve to the same Docker volume name without running concurrently.

If you keep host `ports:` mappings in your compose file, `recreate` is usually the safer choice.

## Stack Deploys and Rollback

The daemon deploys the stack as one operation: every app at its newest committed release, in app-name order. The whole-stack result decides what happens next:

- All apps healthy: the **last-healthy baseline** advances to this release set.
- Any app fails: every app this attempt touched is redeployed at the baseline. The baseline does not move.
- A revert itself fails: the attempt is flagged `revert-failed` in the TUI and audit log and the stack waits for an operator.
- First-ever deploy with no baseline: the attempt halts as `failed`; there is nothing safe to revert to.

`r` in the TUI redeploys the entire baseline on demand.

## The Daemon

`warpgate serve` is configured by environment:

| Variable | Meaning | Default |
| --- | --- | --- |
| `WARPGATE_REPO` | Desired-state repository, `owner/repo` | required on first run |
| `WARPGATE_REPO_BRANCH` | Branch to watch and write bumps to | `master` |
| `WARPGATE_REPO_PATH` | Optional repository subdirectory | empty |
| `WARPGATE_GH_APP_ID` | GitHub App ID | required |
| `WARPGATE_GH_INSTALLATION_ID` | GitHub App installation ID | required |
| `WARPGATE_GH_PRIVATE_KEY_FILE` | Path to the App private key PEM (file only; PEM content in env is not supported) | required |
| `WARPGATE_SSH_ADDR` | Operator TUI SSH listen address | `127.0.0.1:7422` |
| `WARPGATE_HTTP_ADDR` | CI API listen address | `127.0.0.1:7411` |
| `WARPGATE_REGISTRY_TOKEN` | Classic PAT with `read:packages` for GHCR reads (App tokens are not accepted by GHCR) | optional; without it only public images are watchable |
| `WARPGATE_HOST_KEY` | Daemon SSH host key path | generated under the config dir |
| `WARPGATE_DB_PATH` | Daemon database path | under the config dir |

Flags: `--tailscale-ssh` (default true), `--ssh-key`, `--user` control how the daemon reaches target nodes.

For production, run the daemon under systemd — see [`examples/systemd/warpgate.service`](examples/systemd/warpgate.service). It loads the App key via `LoadCredential` (the key never enters the process environment), reads secrets from a root-owned `EnvironmentFile` (`/etc/warpgate/env`, mode `0400`), runs as the unprivileged `warpgate` user with a private state directory, and applies standard service sandboxing. Keep both secret files `root:root 0400`.

The daemon fails at startup on missing or invalid GitHub App credentials, and refuses to run without a repository (env or previously attached).

**Access control is the network.** Bind both listen addresses to a tailnet IP and use Tailscale ACLs to decide who can reach the TUI and the CI API. The daemon trusts connections it receives; do not bind either address to a public interface.

Polling: config and image intervals come from stored poller settings (defaults: config every minute, images every 5 minutes). CI can nudge an immediate poll:

```bash
curl -X POST http://100.64.0.20:7411/refresh
curl http://100.64.0.20:7411/status
```

`/status` returns the synced commit, pending updates, and stack state as JSON.

### Operator TUI

```bash
ssh -p 7422 <daemon-tailnet-addr>
```

Keys: `d` deploy stack (with confirmation), `r` rollback to baseline (with confirmation), `s` schedule an immediate daemon poll, `a` audit log, `u` reload the view, `q` quit. Quitting is disabled while a deploy or rollback is running.

To explore the TUI locally without daemon credentials, SSH, GitHub, GHCR, or a
real stack, run:

```bash
warpgate preview
warpgate preview --scenario failure
warpgate preview --scenario empty
```

Preview mode uses in-memory fixture data and a fake deployer, so deploy and
rollback confirmations are safe to exercise.

## Shadow Deployments

A shadow deployment runs a version of an app alongside the live deployment on the same node(s). The shadow is not wired to the public Traefik proxy, so it is only reachable over the internal (Tailscale) network. This lets you test a release candidate before promoting it to live.

```bash
warpgate shadow deploy api v2.0.0 --tailscale-ssh
warpgate shadow status api --tailscale-ssh
warpgate shadow promote api --tailscale-ssh
warpgate shadow remove api --tailscale-ssh
```

The shadow is accessible at `shadow-<hostname>` if the app has an `expose.internal.hostname` configured. Promote runs a standard blue/green deploy of the shadow version, then tears down the shadow containers.

Notes:

- A shadow requires a live deployment to already exist.
- Only one shadow per app can exist at a time.
- The shadow uses the same compose file, secrets, and environment as the live deployment.
- The shadow runs as a separate Docker Compose project (`<app>-shadow`) alongside the live blue/green project.
- Shadow commands run from a workstation with a local repo checkout (`cluster.yml` present).

## Bootstrap

`warpgate bootstrap` installs the software Warpgate expects on a node and sets up `/opt/warpgate`.

The bootstrap flow includes:

- OS detection
- `warpgate` system user creation
- Go installation
- Docker and the Docker Compose plugin
- SecretSauce installation
- SSH key setup
- Public Traefik setup
- Internal proxy setup
- SecretSauce server setup
- Traefik DNS challenge credential setup when `challenge: dns`
- Registry credential storage

Examples:

```bash
warpgate bootstrap node-1 --tailscale-ssh
warpgate bootstrap --host 203.0.113.10 --tailscale-ssh
warpgate bootstrap node-1 --dry-run
SS_MASTER_PASSWORD=secret warpgate bootstrap node-1 --tailscale-ssh
CF_DNS_API_TOKEN=token warpgate bootstrap node-1 --tailscale-ssh
```

After bootstrap, application data lives under `/opt/warpgate/apps/<app>/`, and Traefik-related files live under `/opt/warpgate/traefik/`.

## Secrets

Warpgate integrates with SecretSauce when `cluster.yml` includes a `secrets.server`.

Deploy behavior:

1. Warpgate fetches keys under each release service's `secrets_prefix`.
2. Warpgate merges those keys with that service's `environment`.
3. Secrets override `environment` values on name collisions.
4. The merged values are written to temporary `.env.<service>` files on the target node.
5. A merged `.env` is also written for Docker Compose interpolation in labels and other topology fields.

Registry credentials can also be read from SecretSauce if they were stored during bootstrap.

## Commands

```bash
warpgate init myapp

warpgate serve
warpgate serve --user root --ssh-key ~/.ssh/deploy

warpgate bootstrap node-1 --tailscale-ssh
warpgate bootstrap --host 203.0.113.10 --tailscale-ssh
warpgate bootstrap node-1 --dry-run

warpgate shadow deploy api v2.0.0 --tailscale-ssh
warpgate shadow status --tailscale-ssh
warpgate shadow promote api --tailscale-ssh
warpgate shadow remove api --tailscale-ssh

warpgate cleanup node-1 --tailscale-ssh
warpgate cleanup node-1 --tailscale-ssh --remove-go --remove-docker
```

Deploy, rollback, status, logs, and release operations live in the daemon TUI; the Warpgate 2 web UI and per-app workstation deploy commands were removed in Warpgate 3.

Use `warpgate <command> --help` for full flag details.

## Current Behavior and Limits

This README is intentionally limited to what the current code does.

- The daemon polls GitHub through the API; it keeps no local git checkout. Bump commits are atomic Git Data API commits.
- Only GHCR is supported for tag and digest reads.
- The daemon never deploys on its own. Bumps and synced config become pending releases; a human deploys them.
- Release manifests are committed to the repo under `apps/<app>/releases/` alongside bump commits, as in Warpgate 2.
- TUI and CI API trust the network layer; bind them to the tailnet only.
- Warpgate ships one CLI binary; the daemon is `warpgate serve`, not a separate binary.

## Development

Build and test:

```bash
go build ./cmd/warpgate
go test ./...
go vet ./...
go fmt ./...
```

### Local testing with Docker

The repo ships a `Dockerfile` and `docker-compose.yml` for running the daemon locally. They are for testing only, not production.

Create a `.env` next to `docker-compose.yml`:

```bash
WARPGATE_REPO=acme/my-infra
WARPGATE_GH_APP_ID=123456
WARPGATE_GH_INSTALLATION_ID=7891011
WARPGATE_GH_PRIVATE_KEY_FILE=/path/to/github-app.pem
```

`WARPGATE_GH_PRIVATE_KEY_FILE` here is the **host** path; compose bind-mounts it read-only and points the daemon at the in-container copy.

Then:

```bash
docker compose up --build
curl http://127.0.0.1:7411/status
ssh -p 7422 127.0.0.1
```

Daemon state persists in the `warpgate-state` volume. The container has no tailscaled, so it runs with `--tailscale-ssh=false`; exercising real node deploys from the container requires mounting an SSH key and adjusting the `command` to pass `--ssh-key`.
