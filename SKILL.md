---
name: musterflow
description: "Turn any OpenAPI spec into a CLI, MCP tool, and workflow"
metadata:
  author: Bane
  version: "0.1.0"
  language: go
  coding-hermes: true
  foreman: musterflow-foreman
---

# MusterFlow

Turn any OpenAPI spec into a CLI, MCP tool, and workflow. Connect an API spec, get a full CLI with subcommands for every endpoint, serve MCP tools over HTTP, and build Starlark workflows.

## Quick Start

```bash
# Build
go build ./...

# Test
go test ./... -count=1

# Lint
go vet ./...
```

## Commands

- `musterflow connect <spec-url>` — Connect an API from its OpenAPI spec
- `musterflow <api> <operation>` — Execute API operations via CLI
- `musterflow start` — Start the dashboard and MCP server
- `musterflow catalog search <query>` — Search the community catalog
- `musterflow flow create <name>` — Create Starlark workflows
- `musterflow auth add <api-id>` — Add auth credentials per API

## Agent Context

This project is managed by the coding-hermes autonomous pipeline.

- **Foreman:** musterflow-foreman (cron-driven autonomous coding)
- **Quality gates:** GitReins Tier 1 (secrets, lint, build, test) + Tier 2 (LLM evaluation)
- **Agent skills:** coding-hermes, coding-hermes-cron, hilo-usage, gitreins
- **Task board:** `.coding-hermes/tasks.md`
- **25 features complete** — see `.coding-hermes/tasks.md` for full list

## Architecture

```
cmd/musterflow/main.go        → Entrypoint
internal/app/                 → API connection registry
internal/auth/                → Credential management
internal/catalog/             → Community catalog
internal/cli/                 → CLI command tree
internal/config/              → YAML config
internal/dashboard/           → Web dashboard
internal/mcp/                 → MCP server
internal/wasm/                → WASM transforms
internal/workflow/            → Starlark workflows
internal/completion/          → Shell completion
```
