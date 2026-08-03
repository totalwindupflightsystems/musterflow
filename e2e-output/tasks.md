# MusterFlow E2E Tasks — Tick 48 (2026-08-03)

| ID | Task | Pri | Cpx | Deps | Tags | Files |
|----|------|-----|-----|------|------|-------|
| *(none)* | No new gaps found — 26/26 battery checks PASS | — | — | — | — | — |

## Closed regressions re-verified this tick

| ID | Status | Verification |
|----|--------|--------------|
| BUG-001 | PASS | flow run byte-exact `42\ntrigger=none`, empty stderr with dashboard lock held |
| BUG-002 | PASS | flow create/list/run route via dashboard HTTP API |
| BUG-003 | PASS | flows stored under --data-dir/flows |
| BUG-004 | PASS | --data-dir honored end-to-end; ~/.musterflow untouched |
| E2E38-001 | PASS | --source round-trip byte-exact |
| E2E38-002 | PASS | --description persisted in sidecar |
| E2E38-003 | PASS | webhook_url absent on non-webhook flows, present on webhook flows |
