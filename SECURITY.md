# Security Policy

## Supported Versions

MusterFlow is under active development. Only the latest commit on `master` is supported with security updates.

## Reporting a Vulnerability

If you discover a security vulnerability, please do NOT open a public issue.

Email: wojonstech@gmail.com

We will respond within 72 hours with acknowledgment and a timeline for resolution.

## Scope

- Sanitization of user-supplied OpenAPI specs
- OAuth2 credential handling
- MCP server exposure
- WASM sandbox isolation (Phase 2)
- Starlark execution sandboxing (Phase 2)

## Out of Scope

- Issues in third-party OpenAPI specifications
- Misconfiguration of user-deployed instances
- Denial of service via unbounded resource consumption (expected behavior for a CLI tool processing user-supplied specs)
