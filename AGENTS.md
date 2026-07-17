# MusterFlow — Agent Guide

## Project Overview

MusterFlow turns any OpenAPI spec into a CLI, an MCP tool, and a workflow engine. Connect an OpenAPI spec URL → get instant CLI subcommands for every endpoint, an HTTP MCP server for AI agents, and a Starlark workflow engine for automation.

- **Language:** Go 1.26.5
- **Module:** `github.com/totalwindupflightsystems/musterflow`
- **Engine dependency:** `github.com/wojons/muster` (via `replace` directive to `/home/kara/muster`)
- **CLI binary:** `cmd/musterflow/main.go`

## Build & Test

```bash
# Build
go build -o musterflow ./cmd/musterflow/

# Vet
go vet ./...

# Test (short)
go test -short -count=1 ./...

# Full test suite
go test -count=1 ./...
```

## Package Structure

| Package | Purpose |
|---------|---------|
| `cmd/musterflow` | CLI entry point |
| `internal/cli` | Cobra command definitions, routing |
| `internal/dashboard` | HTTP dashboard + API server on :9876 |
| `internal/mcp` | MCP server |
| `internal/app` | API connection, refresh, state |
| `internal/auth` | OAuth2, credential management |
| `internal/catalog` | Community catalog client |
| `internal/completion` | Shell completion |
| `internal/config` | Configuration management |
| `internal/wasm` | WASM transforms |
| `internal/workflow` | Starlark workflow engine |

## Key Patterns

- **CLI-Dashboard routing:** When dashboard is running, CLI commands route through dashboard HTTP API (not direct DB access). Use `internal/cli/root.go` pattern.
- **Test fixtures:** Tests use real HTTP servers with `httptest.NewServer`. Auth tests use local OAuth2 test servers.
- **Replace directive:** `go.mod` has `replace github.com/wojons/muster => /home/kara/muster` for local engine development.

## Foreman

This project is maintained by the `musterflow-foreman` cron (coding-hermes fleet). Foreman: `deepseek-v4-pro` on `deepseek-foreman` (PAYG). Workers: GLM-5.2 via ollama-cloud for Go tasks.
