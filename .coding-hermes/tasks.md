# MusterFlow — Task Board (Model-Router Matrix)

> **Core purpose:** Turn any OpenAPI spec into a CLI, MCP tool, and workflow engine.
> **Language:** Go 1.26.5 | **Repo:** github.com/totalwindupflightsystems/musterflow
> **Foreman:** deepseek-v4-pro @ deepseek-foreman | **Worker:** GLM-5.2 via ollama-cloud
> **DuckBrain:** 6 entries under /projects/musterflow/
> **Status:** ALL PHASES COMPLETE. Idle tick 13/7+. NEVER-DONE audit #13 complete — 0 blocking gaps. Cooldown: 43200s (STABLE — 12th re-fix held, no reversion after daemon restart).
> **Last tick:** 2026-07-23 20:28 UTC
> **Cooldown reversions:** 11 (resolved — 12th re-fix at 43200s held through this tick). Cooldown stable — no further escalation needed.
> **Host resource exhaustion:** Resolved (tick #7) — build/vet/tests all pass. fork/mem/threads normal.

---

## Task Matrix

| ID | Task | Priority | Complexity | Deps | Tags | Model | Reasoning | Fallback |
|----|------|----------|------------|------|------|-------|-----------|----------|
| NEVER-DONE | 11-point audit sweep | Medium | 2 ± 1 | none | +++terminal, +++file-editing, +documentation | deepseek-v4-pro | Medium | GLM-5.2 |

## Completed (all tasks done)

All 50+ tasks across historical TASK-001–TASK-030, FIX-031–FIX-033, DOC-034–DOC-040, SEC-041–042, DEPS-043–045, PERF-046, SPEC-047, DUCKBRAIN-048, CI-049–050, QUALITY-051, FIX-052 complete. Summary by phase:

| Phase | Purpose | Key outcomes |
|-------|---------|--------------|
| Core fixes | CLI-dashboard routing, MCP routing, completion prompt blocking | 17 CLI subcommands all route correctly |
| Docs | README corrections, LICENSE, AGENTS.md, CONTRIBUTING.md | All standard repo files present |
| Deps | kin-openapi v0.142, cobra v1.10.2, x/term v0.45, mapstructure v2.3 | 0 outdated direct deps |
| CI | ci.yml + docker.yml, git remote, muster engine relative paths | CI: 10/10 test packages green |
| Perf | 7 benchmarks across 7 packages | Baseline established |
| Quality | root.go 1408→695 lines, golangci-lint, code splitting | 0 TODOs/FIXMEs/HACKs |
| Misc | SEC-041 go1.26.5 upgrade, DuckBrain seeding, specs/ created | 0 vulns, 0 stubs |

## Assumptions

- Project is stable and complete — idle ticks find zero actionable gaps
- 2 pre-existing CI failures (golangci-lint Go 1.24 vs go.mod 1.26.5, Docker DuckDB CGO cross-compile) are non-actionable infrastructure issues
- NEVER-DONE audit runs every foreman tick; if it finds gaps, tasks get added to matrix above

## Routing Notes

- NEVER-DONE audit: foreman runs directly (deepseek-v4-pro) — needs full context, terminal, file search, memory access
- Worker model (GLM-5.2) for any new Go implementation tasks that emerge

## Execution Order

1. NEVER-DONE (runs every tick, creates new tasks if gaps found)

## Escalation Conditions

- Audit finds spec drift → escalate to foreman + create SPEC task
- Audit finds test gap → escalate to worker (GLM-5.2 via ollama-cloud)
- Audit finds new dependency vuln → escalate CRITICAL if stdlib, MEDIUM if transitive
- Idle counter reaches 7 → escalate to Bane (project genuinely complete, consider archiving)
- Cooldown reversion (daemon restart resetting 43200→1800s) → escalate to Bane (TOML config fix needed)
