# Contributing to MusterFlow

Thanks for your interest in contributing! MusterFlow turns OpenAPI specs into typed CLIs, MCP endpoints, and Starlark workflows.

## Development Setup

```bash
git clone https://github.com/totalwindupflightsystems/musterflow.git
cd musterflow
go build -o musterflow ./cmd/musterflow/
```

Requirements: Go 1.26.5+, GitHub personal access token (for private muster engine repo access).

## Build & Test

```bash
go build ./...
go vet ./...
go test -short -count=1 ./...
```

## Project Structure

See [AGENTS.md](./AGENTS.md) for the complete package map and agent workflow documentation.

## Pull Requests

1. Fork the repo and create a branch from `master`
2. Ensure `go build ./...`, `go vet ./...`, and `go test -short -count=1 ./...` all pass
3. Add tests for new functionality
4. Update specs in `specs/` if you change public APIs or CLI commands
5. Commit messages follow conventional commits (`feat:`, `fix:`, `refactor:`, etc.)

## Code Style

Standard Go conventions. Run `gofmt -w .` before committing. The project uses GitReins guards for pre-commit validation.

## License

MIT — see [LICENSE](./LICENSE).
