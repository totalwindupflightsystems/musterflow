# Changelog

All notable changes to MusterFlow will be documented in this file.

## [Unreleased]

### Added
- OpenAPI spec → CLI subcommand generation (17 subcommands)
- HTTP dashboard with API management UI (:19876)
- MCP endpoint for AI agent integration
- Starlark workflow engine (DSL spec, Phase 2 implementation pending)
- WASM transform support (Phase 2 implementation pending)
- OAuth2, API key, bearer, and mTLS authentication
- Community catalog client
- Shell completion (bash, zsh, fish)
- Parquet output format (parquet-go v0.30.1)
- JSONL import/export for API registry
- Workflow engine HTTP builtins: `http_get`, `http_post`, `http_request` (DF-030). Flows can call real APIs: each returns a `{status, body, headers}` dict, non-2xx statuses are data rather than errors, requests time out after 10s and response bodies are capped at 1 MiB

### Fixed
- DuckDB lock conflict between CLI and dashboard (TASK-026)
- Dashboard no longer discloses the server's absolute flows directory in flow-not-found / unreadable errors (`POST /api/flows/<name>/run`, `POST /hooks/<name>`): missing and permission-denied flows now return stable, path-free messages while keeping the existing `{"error": ...}` body and 404 status
- CLI-dashboard routing for all 17 subcommands
- MCP routing and completion prompt blocking
- `start` now matches the documented port auto-discovery (GAP-018): a foreign process on the configured port is survived by serving on the next free port in `[port, port+9]` and printing which one it chose; a port held by another MusterFlow dashboard is refused instead of silently starting a second dashboard on another port

### Changed
- Upgraded kin-openapi to v0.142, cobra to v1.10.2, x/term to v0.45
