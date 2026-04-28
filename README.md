# Warpgate

Warpgate is a Go CLI for deploying Docker Compose applications to Linux hosts over SSH. It is aimed at small self-managed clusters where you want a repo with `cluster.yml`, per-app `app.yml`, and user-written `compose.yml` files instead of a larger orchestration stack.

It currently provides:

- Repo scaffolding with `warpgate init`
- Node bootstrap over SSH or Tailscale SSH
- App discovery from `apps/*/app.yml`
- Rolling deploys to one or more nodes
- Blue/green or recreate deployment strategies
- Rollback, status, logs, app removal, deploy lock management, and node cleanup
- Shadow deployments for pre-release testing on the internal network

## Requirements

Target nodes:

- Linux
- Tailscale installed if you want to use `--tailscale-ssh`
- Passwordless `sudo`
- Network access to pull container images

Local machine:

- Go 1.24+
- SSH client

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

Edit `cluster.yml` with your node details:

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

Bootstrap the node:

```bash
warpgate bootstrap node-1 --tailscale-ssh
```

Deploy the example app:

```bash
warpgate deploy example-app --tailscale-ssh
```

Check status and logs:

```bash
warpgate status
warpgate status example-app --tailscale-ssh
warpgate logs --node node-1 --app example-app --tailscale-ssh
```

Roll back if needed:

```bash
warpgate rollback example-app --tailscale-ssh
```

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

The repository under [`examples/infra-repo`](/home/ray/projects/warpgate/examples/infra-repo) shows a working example layout.

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
image: ghcr.io/acme/api
image_tag: v1.2.3
image_digest: sha256:...
compose_ref: main
targets: [node-1]
secrets_prefix: api/prod
port: 8080
strategy: blue-green

expose:
  public:
    domains: [api.example.com]
  private:
    port: 8080
  internal:
    hostname: api.internal

environment:
  LOG_LEVEL: info
  APP_ENV: production

persistent_volumes:
  - compose_name: api-data
    name: warpgate-api-data

sidecars:
  admin:
    port: 8081
    expose:
      private:
        port: 8081
```

`version` is still accepted as a compatibility alias for older app configs. New configs should use `image_tag` for the build tag, `image_digest` when the image is pinned immutably, and `compose_ref` for remote compose sources.

Create and deploy a release:

```bash
warpgate release api
warpgate deploy api --release latest --tailscale-ssh
```

Fields:

- `kind` is optional. If set, it must be `warpgate/app`.
- `image` is required.
- `image_tag` defaults to `latest` if omitted.
- `image_digest` pins the image immutably when set.
- `compose_ref` identifies the remote compose source revision when `source` is set.
- `targets` defaults to all nodes if omitted.
- `secrets_prefix` tells Warpgate which SecretSauce keys to fetch.
- `port` is the container port used for app-level routing metadata.
- `strategy` may be `blue-green` or `recreate`.
- `persistent_volumes` remaps compose volume keys to stable Docker volume names managed by Warpgate.
- `environment` adds non-secret key/value pairs to the generated `.env` file during deploy.
- `source` can be used instead of a local `compose.yml` to fetch a compose file from GitHub.

`source` example:

```yaml
image: ghcr.io/acme/worker
image_tag: v2.0.0
compose_ref: main
source:
  repo: github.com/acme/deploy-definitions
  compose_path: services/worker/compose.yml
```

Current validation rules to keep in mind:

- `expose.public` requires `port` and at least one domain.
- `expose.private` requires a port.
- `expose.internal` requires `port` and a hostname.

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
- Warpgate writes a `docker-compose.override.yml` that injects the release image reference, an `env_file` reference for release env, and `extra_hosts` entries for internal hostnames.
- If `persistent_volumes` is set, Warpgate also injects top-level `volumes:` name overrides so named volumes stay stable across blue/green slot changes.
- If `environment` or `secrets_prefix` is set, Warpgate writes a temporary `.env` file, references it from the generated override, and passes `--env-file .env` to `docker compose`.

Health checks matter:

- If the compose file defines a container health check, deploy waits for it before considering the rollout successful.
- Without a health check, deploy proceeds as soon as `docker compose up -d` succeeds.

## Deployment Strategies

Warpgate supports two strategies:

- `blue-green`
- `recreate`

`blue-green` is the default. Warpgate starts a new compose project, waits for health, and then tears down the previous slot.

`recreate` stops the old slot before starting the new one. Use it when your compose file binds host ports and both slots cannot run at the same time.

For stateful apps that use SQLite or another single-writer local store, pair `recreate` with `persistent_volumes` so both slots resolve to the same Docker volume name without running concurrently.

Example:

```yaml
image: ghcr.io/acme/web
port: 8080
strategy: recreate
```

Named volume example:

```yaml
image: ghcr.io/acme/worker
strategy: recreate

persistent_volumes:
  - compose_name: worker-data
    name: warpgate-worker-data
```

If you keep host `ports:` mappings in your compose file, `recreate` is usually the safer choice.

## Shadow Deployments

A shadow deployment runs a version of an app alongside the live deployment on the same node(s). The shadow is not wired to the public Traefik proxy, so it is only reachable over the internal (Tailscale) network. This lets you test a release candidate before promoting it to live.

Deploy a shadow:

```bash
warpgate shadow deploy api v2.0.0 --tailscale-ssh
```

Check shadow status:

```bash
warpgate shadow status api --tailscale-ssh
warpgate shadow status --tailscale-ssh
```

The shadow is accessible at `shadow-<hostname>` if the app has an `expose.internal.hostname` configured. For example, if `api` has `hostname: api.internal`, the shadow is reachable at `shadow-api.internal` from any node on the Tailscale network.

When you are satisfied with the shadow, promote it to live:

```bash
warpgate shadow promote api --tailscale-ssh
```

Promote runs a standard blue/green deploy of the shadow version, then tears down the shadow containers. The live deployment is updated in place with zero downtime.

To discard a shadow without promoting:

```bash
warpgate shadow remove api --tailscale-ssh
```

Notes:

- A shadow requires a live deployment to already exist.
- Only one shadow per app can exist at a time.
- The shadow uses the same compose file, secrets, and environment as the live deployment.
- The shadow runs as a separate Docker Compose project (`<app>-shadow`) alongside the live blue/green project.

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

1. Warpgate fetches keys under `secrets_prefix`.
2. Warpgate merges those keys with `environment` from `app.yml`.
3. Secrets override `environment` values on name collisions.
4. The merged values are written to a temporary `.env` file on the target node.

Example:

```yaml
image: ghcr.io/acme/api
secrets_prefix: api/prod
environment:
  LOG_LEVEL: info
```

```yaml
services:
  api:
    image: ghcr.io/acme/api
    restart: unless-stopped
```

Registry credentials can also be read from SecretSauce if they were stored during bootstrap.

## Commands

Common commands:

```bash
warpgate init myapp

warpgate bootstrap node-1 --tailscale-ssh
warpgate bootstrap --host 203.0.113.10 --tailscale-ssh
warpgate bootstrap node-1 --dry-run

warpgate deploy api
warpgate deploy api v1.2.4
warpgate deploy --all
warpgate deploy api --dry-run

warpgate status
warpgate status api --tailscale-ssh
warpgate dashboard --tailscale-ssh

warpgate logs --node node-1 --tailscale-ssh
warpgate logs --node node-1 --app api --tail 50 --grep error --tailscale-ssh

warpgate rollback api --tailscale-ssh
warpgate remove api --tailscale-ssh
warpgate remove api --tailscale-ssh --force
warpgate remove --all --force

warpgate lock break api --tailscale-ssh

warpgate shadow deploy api v2.0.0 --tailscale-ssh
warpgate shadow status api --tailscale-ssh
warpgate shadow status --tailscale-ssh
warpgate shadow promote api --tailscale-ssh
warpgate shadow remove api --tailscale-ssh

warpgate cleanup node-1 --tailscale-ssh
warpgate cleanup --host 203.0.113.10 --tailscale-ssh --force
warpgate cleanup node-1 --tailscale-ssh --remove-go --remove-docker
```

Use `warpgate <command> --help` for full flag details.

## Current Behavior and Limits

This README is intentionally limited to what the current code does.

- App discovery is local and file-based. Warpgate scans `apps/*/app.yml`.
- The deploy override currently injects image tags and internal hostname `extra_hosts`.
- Remote compose sources currently support GitHub raw fetches.
- The daemon binary exists under [`cmd/warpd`](/home/ray/projects/warpgate/cmd/warpd/main.go), but the main workflow today is the `warpgate` CLI.

If you are evaluating behavior that depends on generated Traefik labels or more advanced orchestration, verify it against the current code before relying on it in production.

## Development

Build and test:

```bash
go build ./cmd/warpgate
go build ./cmd/warpd
go test ./...
go vet ./...
go fmt ./...
```
