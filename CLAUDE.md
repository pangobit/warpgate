# Warpgate - Claude Code Guidelines

## Build & Test

```bash
go build ./cmd/warpgate        # Build CLI
go test ./...                  # Run all tests
go test -run ^TestMyFunc$      # Run a single test
go vet ./...                   # Vet
```

## Code Style

- Use `go fmt .` for formatting
- Prefer `strings.Builder` or concatenation over `fmt.Sprintf` for simple formatting
- Use standard Go error handling patterns — don't discard errors
- Avoid third-party packages unless necessary

## Comments

- Provide at least one package-level comment
- Add comments to exported functions and all struct fields in godoc-friendly format
- Do not use inline comments to explain code — code should be self-explanatory
- Comments should describe *what* the type or function is, not *why* it was structured a certain way

## Testing

- Use table-driven tests for multiple cases
- Focus on testing actual business logic, not mocked behavior
- Test negative cases (nil values, invalid inputs, error conditions)
- Avoid tautological tests (setting a field then asserting its value)

## Architecture

- `warpgate` lives in `cmd/warpgate/` and uses `pkg/cli/`
- The daemon is started with `warpgate serve` and composed in `warpd/serve.go`
- `warpd/usecase/` orchestrates sync, image watching, bump commits, and stack deploys against ports in `warpd/usecase/ports.go`
- Connectors under `warpd/connectors/`: GitHub App auth + repo API (`github`), GHCR (`registry`), deploy engine adapter (`deploy`), Turso store (`turso`)
- Operator TUI is served over SSH from `warpd/api/ssh/` (wish + bubbletea); the CI HTTP API is `warpd/api/ci/`
- Semver constraint matching in `pkg/semver/`; config loading and app discovery in `pkg/config/`; compose override generation in `pkg/compose/`
- Deploy orchestration in `pkg/deploy/`, SSH client in `pkg/ssh/`, node bootstrap in `pkg/bootstrap/`
- Warpgate ships one CLI binary; the daemon is `warpgate serve`, never a separate binary
- The daemon is the only release actor (bump commits, synced-config releases); the operator is the only deploy actor (TUI). Do not add unattended deploy paths
