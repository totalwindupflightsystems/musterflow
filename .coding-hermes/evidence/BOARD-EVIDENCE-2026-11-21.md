# BOARD-EVIDENCE-2026-11-21 — foreman-verified closure evidence for four stale-premise rows

Tick: musterflow-foreman-2026-11-21-01-05-08. Tree: master @ 4fef90f + this evidence commit.
Each finding below was re-verified live by the foreman at HEAD before closure.

## DF-MUSTERFLOW-1-R4 — README quickstart fallback
- Claim (row): "README quickstart dead for two consecutive runs; no documented fallback".
- Resolution: fallback IS documented at HEAD — commit c242efa added README lines 49-55
  ("petstore3 upstream is a third-party host that has had repeated 500 outages... e.g. the
  petstore v2 spec: musterflow connect https://petstore.swagger.io/v2/swagger.json").
- Live probe 2026-11-21 (binary built from master 4fef90f, scratch data dir /tmp/mf-r4-probe,
  canonical ~/.musterflow untouched): connect v2 spec -> "Connected: swagger-petstore,
  ID 3a0927ddd62cdfa9, Version 1.0.9, Endpoints: 20", exit code 0.
  Full transcript: .coding-hermes/evidence/DF-MUSTERFLOW-1-R4/probe-2026-11-21.txt

## GAP-017 — battery claims must be matched by committed per-window artifacts
- Resolution: the E2E-001 fixture now commits an artifact inside every battery window:
  - 8b3e23f "test(e2e): commit tick-229 battery artifact — 36/36 PASS at 08f5220"
  - d1a665e "e2e(musterflow): full battery 28/28 at tick 233 window opener — merged tree
    8508c2c, artifact committed (GAP-017 window evidence)" — artifact e2e-output/report-tick233.md
    (28 PASS / 0 FAIL, prior window opener tick 226, event 478).
- The claims-without-artifacts gap described by the row no longer exists at HEAD.

## QA-MUSTERFLOW-4 + QA-MUSTERFLOW-2-R13 — "board audit trail silent since tick 221"
- Resolution: events.jsonl is ACTIVE at HEAD — appended today (2026-11-21) with event ids
  555-561: COS task_created rows (CATALOG-005/006, DISP-001), this tick's audit event 558
  (CI-health triage), task_created INT-CI-203, and three task_dispatched events (559-561).
  The board is committed with every tick; the silent-trail premise is dead.

## QA-MUSTERFLOW-4-R5 — "DF-022 stuck, re-dispatch REJECTED twice, no fix landed"
- Resolution: DF-022 is deferred at HEAD with an explicit routing decision:
  "Engine-generator-routed: -n/--namespace + -w/--watch boilerplate is emitted by the
  wojons/muster generator; musterflow only registers generated leaves. Fix belongs in the
  engine repo." The stuck loop ends by routing, which is the recorded resolution.
