# CATALOG-003 — tolerance path for cosmetic spec defects

Status: implemented on `wt/CATALOG-003`, evidence produced 2026-11-20.
Kanban card `t_cafe0b6d`, goal `t_c54add61`, board rows `CATALOG-002` / `CATALOG-003`.
Pass-1 premise: `docs/catalog/candidates-pass1.md`, section "High-reach specs blocked by `connect` today".

## What landed

A new `internal/specfix` package applies a bounded, opt-in repair pass to the
raw spec bytes *before* the engine parses them. `musterflow connect --tolerate`
enables it; without the flag behaviour is byte-identical to today (strict).

Every repair is reported as a `Warning{Code, Path, Detail}` on
`app.ConnectResult.Tolerated` and printed by the CLI, so a degraded connection
can never look like a clean one.

| code | rule | what it touches |
|---|---|---|
| `example-type-mismatch` | example whose JSON kind contradicts the sibling `type` | metadata only |
| `default-type-mismatch` | same, for `default` | metadata only |
| `required-bool` | schema-level `required` that is a boolean | structural impossibility |
| `array-missing-items` | `type: array` with no `items` | structural impossibility (empty schema inserted) |
| `dangling-local-ref` | local `$ref` whose target does not exist | schema position → empty schema; elsewhere reported, left alone |
| `non-string-text` | `description`/`title` that is not a string | metadata only |
| `null-type` | literal `"null"` type name | structural impossibility |
| `example-missing-required` | object example omitting a property its schema requires | metadata only |
| `external-ref` | non-`#` reference | **recorded only — never fetched or rewritten** |

Paths, operations, parameters, responses, properties and `required` semantics are
never removed.

## Pass-1 vs pass-2 outcome (all eight, real terminals)

Binaries: strict `sha256 29095aa9…` (HEAD d53faeb), tolerant `sha256 554db9be…`.
Scratch specs under `/tmp/mf-cat003-specs`, data dirs under `/tmp/mf-cat003-verify`;
canonical `~/.musterflow` and port 9876 untouched. Full transcripts:
`connect-sweep-pass2.log` in this directory.

| candidate | pass-1 strict | pass-2 `--tolerate` | endpoints | remaining blocker (honest) |
|---|---|---|---|---|
| **spotify** | `cannot unmarshal bool into field Schema.required of type []string` | **connects** (33 defects: 22 dangling-ref, 9 non-string-text, 1 required-bool, 1 external-ref) | 96 | — |
| **square** | `failed to resolve "AppFeeAllocation" … map key not found` | **connects** (37 defects: 5 dangling-ref, 32 non-string-text) | 332 | — |
| **digitalocean** | `cannot unmarshal object into field Tag.description of type string` | **connects** (918 defects: 916 external-ref, 2 non-string-text) | 916 | — |
| docker | `invalid example: value must be a string` (ClusterInfo) | still rejected after 14 repairs | — | `invalid example: Error at "/2377/tcp": Value is not nullable` — an example object carries nulls the converted schema does not allow; not repairable without evaluating examples against their schemas |
| gitlab | `schema type is 'array', schema 'items' must be non-null` | still rejected after 168 repairs | — | `invalid example: Error at "/0/type": property "type" is missing` — array-level example whose *items* schema declares `required`; the rule would need cross-node evaluation |
| weaviate | `invalid example: value must be an array` (AggregateRequest) | still rejected after 6 repairs | — | `unsupported 'type' value "null"` on `DBUserCredential` — the literal does not exist in the source document: it is produced by the Swagger 2.0 → 3.x conversion (from `x-nullable` on a typeless schema), so it cannot be repaired before the conversion |
| pagerduty | `bad data in "#/components/requestBodies/OrchestrationCacheVariableDataPutResponse" (expecting ref to response object)` | still rejected after 96 repairs | — | the component bucket holds a ref to the wrong object kind; repairing it would mean inventing a requestBody |
| algolia | `encountered disallowed external reference: "paths/objects/common/schemas.yml#/attributeToUpdate"` | still rejected after 63 repairs | — | the published entrypoint is not self-contained; the tolerance path deliberately does not fetch external documents |

Result: **3 of 8** connect with tolerance (acceptance criterion: at least 2),
strict behaviour unchanged for all eight.

## Gates actually run (this worktree, 2026-11-20)

```
gofmt -l internal/specfix/ internal/app/connect.go internal/cli/root.go   -> (empty)
go build -o /tmp/mf-cat003 ./cmd/musterflow/                              -> rc=0
go vet ./...                                                              -> rc=0
go test -short -count=1 ./internal/app/ ./internal/cli/                   -> ok (0.868s, 2.826s)
go test -count=1 ./internal/specfix/                                      -> ok
```

## Known follow-ups (not this row)

- `internal/app/refresh.go`, `internal/cli/api.go` and `internal/mcp/tools.go`
  parse specs without the tolerance option, so refreshing a tolerated API
  re-parses strictly. Wiring `Tolerate` through those call sites is a separate row.
- Dashboard-routed connects (`connectViaDashboard`) have no tolerance knob; the
  CLI refuses `--tolerate` in that mode rather than pretending to repair.
- The five still-rejected candidates need rules that evaluate examples against
  their schemas or handle the Swagger 2.0 conversion stage.
