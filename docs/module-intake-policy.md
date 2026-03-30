# Module Intake Policy

Status: active
Owner: sanjay
Updated: 2026-03-30

## Purpose

This document defines how Codero adopts ideas, code, tests, and operational
patterns from upstream projects without changing the roadmap or drifting into
wholesale product import.

It turns the existing rule in `README.md` into an execution policy:

- roadmap first
- contracts first
- smallest useful intake
- license-safe reuse only
- attribution and rollback always

## Non-Goals

This policy does not:

- change the canonical roadmap in `docs/roadmap.md`
- authorize bulk copying from an upstream repo
- authorize copying code from incompatible licenses
- replace the per-module intake record in `docs/module-intake-registry.md`

## Core Rules

1. Every intake must map to an existing roadmap item, contract gap, or proving-gap.
2. Codero borrows implementation patterns, not upstream branding, taxonomy, or UI identity.
3. Prefer the smallest slice that closes the roadmap gap.
4. Prefer permissive-license sources for direct code intake.
5. Keep copyleft sources as architecture and behavior references unless Codero's
   repository license is deliberately made compatible.
6. No copied code lands without contract linkage, tests, rollback notes, and
   attribution metadata.

## Intake Modes

### 1. Direct Code Intake

Allowed when all of the following are true:

- the source license is permissive
- the copied slice is narrow and identifiable
- Codero gains more from reuse than reimplementation
- the copied code can be covered by Codero tests

Default permissive list:

- MIT
- Apache-2.0
- BSD-2-Clause
- BSD-3-Clause
- ISC
- 0BSD

### 2. Behavior Intake

Use the upstream repo as a behavioral reference and reimplement the feature in
Codero's own architecture.

This is the default for:

- agent/session managers with strong product opinions
- repos whose architecture is useful but whose code would distort Codero
- repos with copyleft licenses

### 3. Reference-Only Intake

Use the source for research, screenshots, UX examples, naming patterns, or
operational guidance only. No code is copied.

## License Policy

### Safe For Small Code Intake

- MIT
- Apache-2.0
- BSD family
- ISC

### Case-By-Case Only

- MPL-2.0
- LGPL
- EPL

These require explicit review of file-boundary or linking implications before
copying code. Until that review exists, treat them as reference-only.

### Reference-Only By Default

- GPL
- AGPL
- SSPL
- BSL

For Codero's current trajectory, these are architecture references, not code
sources.

## Required Intake Record

Every intake must record:

- roadmap target
- source repo and URL
- source license
- source commit, tag, or review date
- intake mode: direct code, behavior, or reference-only
- files touched in Codero
- contract or spec link
- tests added or updated
- rollback path
- attribution note

## Intake Workflow

1. Identify the roadmap item or contract gap being closed.
2. Choose one to three upstream candidates.
3. Classify each candidate by license and intake mode.
4. Record the chosen source in `docs/borrowed-components.md`.
5. Record the specific intake in `docs/module-intake-registry.md` if code or a
   normative module is being adopted.
6. Write or update the relevant contract before landing the implementation.
7. Import or reimplement the smallest useful slice.
8. Add parity, unit, contract, or integration coverage for the adopted behavior.
9. Document rollback notes.
10. Preserve attribution in docs and release notices.

## Things We Do Not Copy

- full upstream terminal UIs
- upstream product branding or terminology when Codero already has a clearer term
- end-to-end frameworks when only one subsystem is needed
- code that would force Codero into an incompatible open-source license
- code that bypasses Codero's contracts, tests, or deterministic state rules

## Acceptance Checklist

An intake is ready only when:

- the roadmap target is named
- the source is listed in `docs/borrowed-components.md`
- license classification is explicit
- direct-copy versus reimplementation decision is explicit
- contracts are updated
- validation is in place
- rollback notes exist
- attribution text exists

## Open Source Readiness

Codero is intended to become an open source project. Before the first public
release, the repository should add at minimum:

- a root `LICENSE`
- a root `NOTICE` or `THIRD_PARTY_NOTICES`
- source-attribution entries for copied code

Until that is done, direct code intake should remain conservative and limited to
clearly permissive upstream sources.

## Related Docs

- `docs/roadmap.md`
- `docs/module-intake-registry.md`
- `docs/borrowed-components.md`
- `docs/roadmap-intake-map.md`
- `docs/adr/0006-tui-shell-architecture.md`
