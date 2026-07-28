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

### Fixed
- DuckDB lock conflict between CLI and dashboard (TASK-026)
- CLI-dashboard routing for all 17 subcommands
- MCP routing and completion prompt blocking

### Changed
- Upgraded kin-openapi to v0.142, cobra to v1.10.2, x/term to v0.45
