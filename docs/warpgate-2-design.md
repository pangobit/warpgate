# Warpgate 2.0 Design

## Status

Draft for review.

## Summary

Warpgate 2.0 makes the browser UI the primary local operator experience. The user runs `warpgate ui`, Warpgate starts a loopback-only web server, opens the browser, persists local operational state in Turso, authenticates to GitHub with GitHub App device flow, and can initiate releases by committing changes to Warpgate YAML.

The existing CLI remains useful for bootstrap, diagnostics, scripting, and direct deploys. The UI replaces day-to-day interactive CLI workflows without requiring users to operate a separate Warpgate service.

## Goals

- Run a local browser UI from `warpgate ui`.
- Bind the UI to loopback by default.
- Persist local UI state in Turso using the `turso.tech/database/tursogo` driver, not libsql/sqlite.
- Authenticate GitHub API access with GitHub App device flow instead of PATs or token env vars.
- Configure a GitHub infrastructure repository that Warpgate can read from and write to.
- Attach to an existing bootstrapped Warpgate infrastructure repository.
- Let operators initiate releases from the UI by tweaking deploy data in forms; Warpgate writes the resulting app YAML and commits it to GitHub.
- Provide manual "check now" actions for GitHub/config refresh and image refresh.
- Build a server-rendered web UI using the local Warpgate design system in `docs/design_system.md`.
- Reuse the existing deployment engine while the local UI is introduced.

## Non-Goals

- Building a full node-agent protocol in the first 2.0 slice.
- Replacing SSH or Tailscale SSH deployment with a new transport immediately.
- Supporting Git providers other than GitHub.
- Requiring Warpgate 2.0 to create a new infra repo before it can operate.
- Making webhooks the primary synchronization mechanism.
- Editing secrets through the Warpgate UI.
- Generating user `compose.yml` files.
- Removing existing CLI commands.
- Requiring a long-running Warpgate service for the first local UI slice.
- Reading from or writing to image source repositories. Image repos stay owned by their existing CI/CD pipelines.

## Existing Baseline

Warpgate currently has:

- `cmd/warpgate` for the CLI.
- `cmd/warpd` and `pkg/daemon` as earlier daemon scaffolding that is no longer the primary 2.0 product direction.
- `pkg/config` for `cluster.yml` and `apps/*/app.yml` loading.
- `pkg/deploy` for SSH/Tailscale SSH deployment orchestration.
- `pkg/release` for file-backed release manifests.
- `pkg/compose` for generated compose overrides.

The existing config model is Git-friendly and should remain the desired-state source:

- `cluster.yml` defines cluster-wide settings.
- `apps/<name>/app.yml` defines release metadata.
- `apps/<name>/compose.yml` is user-authored or fetched from a configured source.
- App names come from directory names.

## Referenced Patterns

Probe provides the identity display pattern:

- Define a small identity domain package with `User`, `Identifier`, `StaticIdentifier`, `WithUser`, and `UserFrom`.
- Display the resolved identity in the navigation shell.
- In local UI mode, unauthenticated GitHub state is allowed and displayed as `unknown`.

The referenced projects provided useful patterns during planning. Warpgate's local docs now capture the UI specifics needed for implementation:

- Use embedded Turso through `turso.tech/database/tursogo`.
- Use server-rendered `templ` templates, HTMX, embedded static assets, and vanilla CSS tokens.
- Keep HTTP handlers thin and route through an application service.

## Architecture

Warpgate 2.0 should keep a clean separation between desired state, operational state, and execution.

Desired state lives in GitHub:

- `cluster.yml`
- `apps/*/app.yml`
- optional local `apps/*/compose.yml`
- release-owned image tags, digests, environment, routing metadata, target nodes, and strategy

Operational state lives in Turso:

- configured GitHub repositories
- latest observed repo commits
- discovered apps
- release records
- deployment attempts
- deployment results
- image poll cursors
- config poll cursors
- audit events
- local UI startup metadata

Execution happens through the existing deployer:

- The local UI checks or fetches the configured repo state.
- It creates a release record from a committed config revision.
- It invokes the existing deploy path with the selected app and release inputs.
- It records success, failure, timestamps, actor identity, and deploy output summary.

## Package Layout

Warpgate 2.0 UI code should keep the current hexagonal architecture under `warpd/` while it is being renamed or moved. The existing `pkg/...` packages remain available to the CLI and can be adapted through connectors where the UI needs them.

Proposed layout:

```text
cmd/warpgate/
warpd/
  api/
    http/
    web/
      templates/
      assets/
  connectors/
    deploy/
    github/
    registry/
    tailscale/
    turso/
  usecase/
  internal/
    audit/
    configrepo/
    deployment/
    identity/
    imagewatch/
    release/
```

Responsibilities:

- `cmd/warpgate`: executable entrypoint for the CLI and local UI command.
- `warpd/api`: inbound adapters and routing. HTTP handlers bind requests, call use cases, and render responses.
- `warpd/api/web`: server-rendered UI handlers, templates, and embedded assets.
- `warpd/connectors`: outbound adapters for persistence, GitHub, registries, authorization, and the existing deploy engine.
- `warpd/usecase`: application orchestration and port interfaces.
- `warpd/internal/{domain}`: domain models, validation, constants, and pure behavior grouped by bounded domain.

Dependency direction:

- `api` depends on `usecase` and domain view models.
- `usecase` depends on domain packages and outbound port interfaces.
- `connectors` depend inward on use-case ports and domain models.
- `internal/{domain}` packages do not depend on `api`, `connectors`, or `usecase`.
- Existing `pkg/...` deployment/config/release code is treated as core capability and is wrapped by `warpd/connectors/deploy` rather than called from HTTP handlers.

## Local UI Runtime

`warpgate ui` starts the UI process.

Runtime behavior:

- Bind to `127.0.0.1:0` by default.
- Print the resolved local URL.
- Open the browser by default.
- Use a local Turso database path.
- Persist GitHub App user access tokens in the local Turso database.
- Refresh expiring GitHub App user tokens when GitHub returns a refresh token.

Minimal command configuration:

```text
warpgate ui \
  --addr 127.0.0.1:0 \
  --db-path ~/.config/warpgate/warpgate.db \
  --github-client-id <client-id>
```

Persisted UI-configured settings include:

- GitHub owner, repo, branch, and optional repository subpath.
- Deploy SSH mode, SSH user, and key path if needed.
- Attached repository records.
- Any non-secret UI preferences.

GitHub App user access tokens are persisted in Turso so the local UI can reconnect after restart. Users can clear the persisted token with Disconnect.

## Identity and GitHub Authorization

The local UI does not use GitHub to protect localhost. GitHub App authorization grants Warpgate access to GitHub APIs. When no GitHub session exists, the navigation identity is `unknown` and repository operations that need GitHub fail with a connect-GitHub error.

Identity model:

```go
type User struct {
    Email string
    DisplayName string
    Capabilities []string
}
```

Authorization:

- Local UI mode accepts loopback browser requests.
- GitHub connected state becomes the actor for commits and audits.
- Unknown GitHub state is allowed for read-only local UI screens.

GitHub App authorization:

- Use GitHub App device flow.
- Use a GitHub App client ID so permissions are app-scoped and repo-scoped.
- Require the app to be installed for the configured owner/repository.
- Require repository contents read access before Warpgate can attach or sync a repository.
- Avoid PATs and token environment variables for local UI flows.
- Store GitHub App user access tokens in local Turso and clear them when the user disconnects GitHub.
- Refresh expiring user access tokens when GitHub returns a refresh token.

## Persistence

Warpgate should use embedded Turso:

- Driver: `turso.tech/database/tursogo`
- Migrations: Goose embedded SQL
- Queries: sqlc
- Max open connections: `1`

The store should expose domain-level methods, not raw sqlc types. The coding agent should choose the concrete tables, indexes, and query shapes during implementation after discovering the final use-case boundaries.

Ideal conceptual data shape:

- Repository settings and attach state.
- Repo sync cursors, including last observed commit, last check time, and last error.
- Discovered apps, including source path, current config commit, raw YAML, and parsed deploy metadata.
- Release records, including immutable manifest inputs, config commit, actor, status, and creation time.
- Deployment attempts, including release, app, targets, actor, status, timing, and error summary.
- Image watch cursors, including app service, image, mutable tag, last observed digest, last check time, and last error.
- Audit events for operator actions and automated sync observations.

The implementation should optimize for clear ownership and query simplicity rather than preserving this list as a literal schema.

Status values should be constants in the relevant `warpd/internal/{domain}` package.

Release statuses:

- `draft`
- `ready`
- `deploying`
- `deployed`
- `failed`

Deployment statuses:

- `queued`
- `running`
- `succeeded`
- `failed`

## GitHub Integration

The GitHub connector should start with the Contents API and Git Data API as needed.

Required operations:

- Get branch head SHA.
- Read a file at a ref.
- List app config files under `apps/*/app.yml` relative to the configured repository subpath.
- Write one file with optimistic concurrency.
- Create a commit on the configured branch.

The primary onboarding path should support an existing bootstrapped infra repo. The local UI is configured with owner, repo, branch, and optional subpath, then validates that the selected repo root already contains a Warpgate layout:

- `cluster.yml`
- `apps/`
- zero or more `apps/<name>/app.yml`
- optional `apps/<name>/compose.yml`

If validation succeeds, Warpgate imports the repo state into Turso as observed desired state. It should not rewrite or normalize the repo during initial attach. Repo creation and scaffolding can remain a CLI concern.

The UI release initiation flow should be commit-first:

1. User tweaks deploy data in the UI, such as service image tags, digests, environment values, routing data, targets, or strategy.
2. Warpgate loads the current `apps/<name>/app.yml`.
3. Warpgate applies the requested deploy-data changes to the YAML structure.
4. Warpgate validates the resulting YAML as a Warpgate app config.
5. Warpgate checks the latest known blob SHA or commit SHA.
6. Warpgate commits the modified `apps/<name>/app.yml` to GitHub.
7. Warpgate syncs the resulting commit into Turso.
8. Warpgate creates a release record from that committed config.
9. User can deploy the release.

Commit messages should be deterministic and reviewable:

```text
warpgate: release <app> <service>=<tag-or-digest>
```

If the GitHub commit fails because the branch moved, the UI should show a conflict and require a refresh before retrying.

## Config Refresh

The config refresh action checks the configured GitHub repository branch.

Behavior:

- Read branch head SHA.
- If unchanged, record `last_checked_at`.
- If changed, read `cluster.yml` and `apps/*/app.yml` under the configured repository subpath.
- Validate discovered config.
- Upsert app rows with the new commit SHA and YAML.
- Record an audit or sync event.
- Preserve historical releases and deployments.

Config refresh should not auto-deploy config changes in the first slice. It should surface that a config change exists and whether a release can be created.

The UI must expose a manual "check now" or "update now" action for config synchronization. That action should run the config sync use case and return fresh status to the UI.

## Image Watching

Image watching should track release service images from app configs.

Initial behavior:

- For each `release.services.<name>` with an image and mutable tag, poll the registry for the current digest.
- Compare the digest with `image_watch_cursors.last_digest`.
- Record changes.
- Mark apps as having an available image update.

Digest-pinned services should not be treated as mutable update candidates.

The first registry connector should support GHCR because current examples use `ghcr.io`. The interface should allow adding Docker Hub or other registries later.

Warpgate does not touch image source repositories. Those repositories are managed by their own CI/CD pipelines. Warpgate only observes published registry metadata and writes deploy intent to the configured infrastructure repo.

Image changes should not deploy automatically in the first slice.

The UI must expose a manual "check now" action for image metadata. That action should refresh registry metadata for configured watches and update persisted image watch state.

## Release Lifecycle

A release is a durable record derived from committed desired state.

Release lifecycle:

```text
config edit -> GitHub commit -> repo sync -> release ready -> deploy running -> deployed or failed
```

Invariants:

- A UI-created release must reference a GitHub commit SHA.
- A release manifest must be reproducible from the stored YAML and compose content/source reference.
- Deployments must reference a release row.
- Failed deployments remain visible and cannot overwrite prior successful history.

Warpgate may continue storing file-backed release manifests if needed to reuse `pkg/deploy` initially, but Turso is the source for the web UI's operational history.

## Deployment Execution

The first implementation should wrap the existing `pkg/deploy.Deployer`.

The application service should define a narrow deployment port:

```go
type Deployer interface {
    DeployRelease(ctx context.Context, input DeployReleaseInput) (DeployResult, error)
}
```

An adapter can translate that into the current deployer. This keeps the service testable and prevents web handlers from depending on SSH details.

Deployment records should capture:

- app
- release
- actor
- target nodes
- start time
- finish time
- status
- error message

Detailed streaming logs can be added later. The first slice can record a summary and rely on existing CLI log commands for deep inspection.

## Web UI

The UI should follow the local Warpgate design system in [`docs/design_system.md`](design_system.md). That document is the source of truth for CSS tokens, layout, components, template shape, and Warpgate-specific UI surfaces.

The implementation should use:

- `templ` templates
- HTMX forms
- embedded CSS and JS assets
- token-based vanilla CSS
- minimal JavaScript for theme and keyboard affordances

Do not assume future coding sessions can access the Cobot repository. All design specifics needed to implement the Warpgate UI should live in `docs/design_system.md`.

Primary screens:

- Dashboard
  - project name
  - repo sync state
  - "check now" action for GitHub/config sync
  - app count
  - latest deployment status
- Apps
  - app list
  - target nodes
  - current release services
  - image update indicators
  - "check now" action for image metadata
- App detail
  - current YAML
  - release services
  - release history
  - deployment history
- Edit deploy data
  - structured controls for release service fields, targets, strategy, routing, and environment
  - generated YAML preview for `app.yml`
  - validation errors
  - commit preview
  - commit action
- Release detail
  - manifest inputs
  - GitHub commit SHA
  - deploy action
  - deploy status
- Settings
  - GitHub account connect/disconnect
  - GitHub repo display

Handlers should only bind requests, call use-case methods, and render templates. Business rules belong in `warpd/usecase` and pure domain packages under `warpd/internal`.

## API Surface

The UI can start with HTML routes only. JSON endpoints are optional unless needed by HTMX.

Proposed routes:

```text
GET  /
GET  /apps
GET  /apps/{app}
GET  /apps/{app}/edit
POST /apps/{app}/deploy-data
POST /apps/{app}/commit
GET  /releases/{releaseID}
POST /releases/{releaseID}/deploy
POST /sync/config/check-now
POST /sync/images/check-now
GET  /settings
POST /auth/github/start
POST /auth/github/complete
POST /auth/github/disconnect
GET  /assets/*
```

Routes use local identity middleware to attach the current GitHub actor or `unknown`.

Optional unauthenticated health endpoint:

```text
GET /healthz
```

## Configuration and Secrets

The local UI command config needs:

- Turso DB path.
- GitHub App client ID.
- Local listen address.

The UI-configured persisted settings include:

- GitHub owner, repo, branch, and optional repository subpath.
- Deploy SSH mode and user.

GitHub App user access tokens are stored in the local Turso database so the UI can restore GitHub access after restart. Disconnect removes the persisted token. A later hardening slice can move this secret to the OS keychain.

## Operational Model

The user starts Warpgate locally:

```text
warpgate ui
```

The UI binds to loopback and stores local state under the user config directory by default.

The local UI needs network access to:

- GitHub API.
- container registries.
- target nodes over SSH or Tailscale SSH.

## Observability

Initial observability:

- structured process logs
- persisted audit events
- persisted refresh errors
- deployment status records
- `/healthz`

Later observability:

- live deployment log streaming
- webhook event history

## Failure Handling

GitHub conflicts:

- Detect branch or file SHA mismatch.
- Return a conflict error.
- Ask the user to refresh before retrying.

Invalid YAML:

- Reject before commit.
- Show validation errors in the edit screen.

Refresh failures:

- Preserve previous good state.
- Record `last_error`.
- Retry when the user checks again.

Deployment failures:

- Mark deployment `failed`.
- Keep release available for retry.
- Do not delete or modify prior successful release records.

Turso open or migration failures:

- Fail UI startup.
- Log the migration error.

GitHub authorization failures:

- Keep the local UI usable.
- Show `unknown` identity.
- Block GitHub API operations until the user connects GitHub.

## Verification Plan

Required checks for implementation PRs:

```bash
go fmt ./...
go test ./...
go vet ./...
go tool templ generate
```

The exact generator commands may change after tools are added to `go.mod`.

Test coverage should include:

- Turso store methods.
- Local identity middleware.
- GitHub App device flow with fake GitHub endpoints.
- GitHub connector request construction.
- Config sync with fake GitHub.
- Release commit flow with fake GitHub.
- Deploy service with fake deployer.
- Handler tests for routing, binding, validation errors, and redirects.

## Review Questions

- Should Warpgate use a Pangobit-owned GitHub App client ID by default or require users to provide one?
- Should the UI commit directly to `main`, or create a branch/PR for release YAML edits?
- Should initial repo attach fail hard on invalid app configs, or import valid apps while surfacing invalid ones?
- Should image updates create release drafts automatically, or only show an update indicator?
- Should config repo changes create release drafts automatically, or require operator action?
- Should deployment logs stream in the first UI slice, or is persisted status enough?
