# Warpgate

Warpgate is a Go deployment tool for Docker Compose applications on Linux hosts over SSH. It is aimed at small self-managed clusters where you want a repo with `cluster.yml`, per-app `app.yml`, and user-written `compose.yml` files instead of a larger orchestration stack. The CLI still exists, but the Warpgate 2 workflow centers on a local browser UI for day-to-day release, deploy, status, and log operations.

It currently provides:

- Repo scaffolding with `warpgate init`
- Node bootstrap over SSH or Tailscale SSH
- App discovery from `apps/*/app.yml`
- Rolling deploys to one or more nodes
- Blue/green or recreate deployment strategies
- Rollback, status, logs, app removal, deploy lock management, and node cleanup
- Shadow deployments for pre-release testing on the internal network
- A local browser UI with GitHub App device authorization, repository sync, release editing, deploy actions, status, and logs

## Requirements

Target nodes:

- Linux
- Tailscale installed if you want to use `--tailscale-ssh`
- Passwordless `sudo`
- Network access to pull container images

Local machine:

- Go 1.24+
- SSH client
- A browser for the local UI
- A GitHub App with device flow enabled if you want the UI to read or write GitHub-backed config repositories

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

Or start the local UI:

```bash
warpgate ui --user root
```

The UI opens on loopback, uses Tailscale SSH for runtime operations by default, and lets you attach a GitHub-backed config repository from Settings.

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
compose_ref: main
targets: [node-1]
strategy: blue-green

release:
  services:
    api:
      image: ghcr.io/acme/api
      image_tag: v1.2.3
      image_digest: sha256:...
      secrets_prefix: api/prod
      port: 8080
      expose:
        public:
          domains: [api.example.com]
      environment:
        LOG_LEVEL: info
        APP_ENV: production

    admin:
      image: ghcr.io/acme/api-admin
      image_tag: v1.2.3
      image_digest: sha256:...
      secrets_prefix: api-admin/prod
      port: 8081
      expose:
        private:
          port: 8081

persistent_volumes:
  - compose_name: api-data
    name: warpgate-api-data
```

Release-owned deploy inputs must be declared under `release.services`. The compose file remains the topology source; Warpgate only overlays service images and generated env files for declared release services.

Create and deploy a release:

```bash
warpgate release api
warpgate deploy api --release latest --tailscale-ssh
```

Fields:

- `kind` is optional. If set, it must be `warpgate/app`.
- `release.services.<name>.image` is required for each first-class release service.
- `release.services.<name>.image_tag` defaults to `latest` if omitted.
- `release.services.<name>.image_digest` pins that service image immutably when set.
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
compose_ref: main
source:
  repo: github.com/acme/deploy-definitions
  compose_path: services/worker/compose.yml
release:
  services:
    worker:
      image: ghcr.io/acme/worker
      image_tag: v2.0.0
```

When the local UI is connected to GitHub, source compose files are read through the GitHub API using the authorized GitHub App user token. That allows private source repositories when the GitHub App installation has access to the source repo.

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
- Warpgate writes a `docker-compose.override.yml` that injects each release service's image reference, per-service `env_file` reference, and `extra_hosts` entries for internal hostnames.
- If `persistent_volumes` is set, Warpgate also injects top-level `volumes:` name overrides so named volumes stay stable across blue/green slot changes.
- If any release service has `environment` or `secrets_prefix`, Warpgate writes `.env.<service>` files for containers and a merged temporary `.env` for Compose interpolation.

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
strategy: recreate
release:
  services:
    web:
      image: ghcr.io/acme/web
      port: 8080
```

Named volume example:

```yaml
strategy: recreate
release:
  services:
    worker:
      image: ghcr.io/acme/worker

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

1. Warpgate fetches keys under each release service's `secrets_prefix`.
2. Warpgate merges those keys with that service's `environment`.
3. Secrets override `environment` values on name collisions.
4. The merged values are written to temporary `.env.<service>` files on the target node.
5. A merged `.env` is also written for Docker Compose interpolation in labels and other topology fields.

Example:

```yaml
release:
  services:
    api:
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

## Local UI

Start the local browser UI:

```bash
warpgate ui
```

Useful flags:

```bash
warpgate ui --user root
warpgate ui --addr 127.0.0.1:8080
warpgate ui --open=false
warpgate ui --db-path ./warpgate.db
warpgate ui --github-client-id Iv1.example
```

Defaults:

- The server binds to a loopback address and opens the browser automatically.
- The local UI database is stored under the user's config directory unless `--db-path` is set.
- Runtime deploy, status, and log operations use Tailscale SSH by default.
- The GitHub App client ID can be passed with `--github-client-id`, read from `WARPGATE_GITHUB_CLIENT_ID`, or entered in Settings when connecting GitHub.

Initial setup:

1. Create a GitHub App, enable device flow, and install it for the config repository and any private source compose repositories.
2. Give the app repository contents access. Warpgate reads config, writes release metadata, and commits updated `app.yml` release inputs.
3. Run `warpgate ui`.
4. In Settings, enter the repository owner, name, branch, and optional path such as `prod`.
5. In Settings, enter the GitHub App client ID and complete the device authorization flow.

The GitHub App client ID is not a secret. When entered through the UI, Warpgate stores it in an `HttpOnly`, `SameSite=Strict` browser session cookie so the next local request can reuse it without another CLI flag. GitHub user authorization tokens are stored in the local UI database.

UI pages:

- Dashboard shows repository sync, image sync, recent releases, and deployments.
- Apps lists discovered apps and opens per-app release details.
- App edit screens can update every release service in an `app.yml` and commit a release.
- Release pages deploy a selected release and disable the deploy button while the request is in flight.
- Status shows cluster nodes and runtime app/container state.
- Logs fetch recent container logs from a selected node and display structured JSON logs in readable rows.
- Settings manages the config repository and GitHub App authorization.

## Commands

Common commands:

```bash
warpgate init myapp

warpgate ui
warpgate ui --user root
warpgate ui --addr 127.0.0.1:8080 --open=false

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
- Remote compose sources are read from GitHub through the authorized GitHub App user token in the local UI path.
- The local browser UI is started with `warpgate ui`; Warpgate does not ship a separate daemon binary.

If you are evaluating behavior that depends on generated Traefik labels or more advanced orchestration, verify it against the current code before relying on it in production.

## Development

Build and test:

```bash
go build ./cmd/warpgate
go test ./...
go vet ./...
go fmt ./...
```
