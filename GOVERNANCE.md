# MusterFlow Governance

## Project Decision Making

MusterFlow follows a BDFL (Benevolent Dictator for Life) model with consensus-seeking on major decisions.

**BDFL:** Alexis Okuwa (@wojons)

## Decision Process

1. **Small changes** (bug fixes, docs, minor features): Direct PR with review
2. **Medium changes** (new features, refactors): Open an issue for discussion before implementing
3. **Major changes** (architecture, breaking changes, new subsystems): RFC document with 1-week comment period

## Roles

| Role | Responsibilities |
|------|-----------------|
| **BDFL** | Final decision authority, project vision, release management |
| **Maintainers** | Code review, issue triage, merge PRs |
| **Contributors** | Submit PRs, file issues, participate in discussions |

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## Maintainer Guidelines

- PRs require at least one approving review before merge
- Breaking changes require a migration path documented in the PR
- All CI checks must pass before merge
- Releases follow [Semantic Versioning](https://semver.org/)

## Becoming a Maintainer

Contributors who demonstrate sustained, high-quality contributions and alignment with project values may be invited to become maintainers. Nominations are made by existing maintainers and approved by the BDFL.

## Conflict Resolution

1. Discuss the issue in the relevant GitHub issue or PR
2. If unresolved, escalate to maintainers for mediation
3. If still unresolved, the BDFL makes the final decision
