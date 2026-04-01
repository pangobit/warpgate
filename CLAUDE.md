# Warpgate - Claude Code Guidelines

## Build & Test

```bash
go build ./cmd/warpgate        # Build CLI
go build ./cmd/warpd           # Build daemon
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

- `warpgate` (CLI) lives in `cmd/warpgate/` and uses `pkg/cli/`
- `warpd` (daemon) lives in `cmd/warpd/` and uses `pkg/daemon/`
- Config loading and app discovery in `pkg/config/`, compose override generation in `pkg/compose/`
- Deploy orchestration in `pkg/deploy/`, SSH client in `pkg/ssh/`, node bootstrap in `pkg/bootstrap/`
- The CLI and daemon are separate binaries — don't add daemon commands to the CLI
