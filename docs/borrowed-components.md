# Borrowed Components Registry

Status: active
Owner: sanjay
Updated: 2026-03-30

## Purpose

This document is the canonical shortlist of upstream projects Codero may borrow
from while keeping the roadmap unchanged.

It does not mean code has already been copied. It means the source has been
reviewed and classified for safe future intake.

## Legend

- `approved-copy`: permissive source; small code intake allowed with attribution
- `approved-reference`: safe as a design or behavior reference; default to
  reimplementation
- `reference-only`: useful architecture or UX reference; do not copy code
- `avoid`: do not use as an implementation source

## Registry

| Source | License | Status | Approved Codero Domains | What To Borrow | What Not To Borrow |
|---|---|---|---|---|---|
| [Ataraxy-Labs/opensessions](https://github.com/Ataraxy-Labs/opensessions) | MIT | `approved-copy` | session status UX, session detail fields, lightweight notify/progress/log semantics | session list/detail field model, progress payload ideas, log and notify API shape, operator-facing tail/status concepts | Bun server as source of truth, tmux sidebar UI, upstream branding, localhost daemon dependency |
| [coplane/par](https://github.com/coplane/par) | MIT | `approved-copy` | worktree naming, session naming, global inventory, cleanup ergonomics | deterministic session naming, label-first inventory, global context listing, cleanup conventions | control-center UI as Codero UI, Python-specific CLI structure, upstream taxonomy |
| [actions/scaleset](https://github.com/actions/scaleset) | MIT | `approved-copy` | listener/session loops, poll-and-ack message handling, narrow worker/control split | listener package patterns, message polling, ack semantics, just-in-time worker session ideas | GitHub-specific API assumptions outside the relevant client patterns |
| [wehnsdaefflae/terminal-control-mcp](https://github.com/wehnsdaefflae/terminal-control-mcp) | MIT | `approved-copy` | session capture, output modes, history isolation, timeout and cleanup behavior | `screen/history/tail/since_input` content modes, output capture semantics, isolated history patterns, cleanup timeouts | browser terminal as primary UI, MCP server as Codero's operator surface |
| [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad) | AGPL-3.0 | `reference-only` | detached session lifecycle, one-agent-one-worktree ergonomics, attach/resume semantics | behavioral model for `tmux` session lifecycle, worktree isolation, resume/kill flow | AGPL code, TUI, menu structure, direct product UX |

## Preferred Copy Targets

The following are good candidates for small, attributable code or logic intake
from permissive sources:

- deterministic session naming utilities
- bounded output-tail helpers
- session inventory data structures
- cleanup and timeout helpers
- poll-and-ack loops for message or event handling
- lightweight status/progress payload schemas

## Reference-Only Targets

The following should remain architecture references unless Codero's licensing
position changes:

- detached multi-agent terminal managers under AGPL
- end-to-end terminal UIs that would become a second Codero surface
- repos that collapse control plane, runtime, and operator UX into one product

## No-Go Patterns

Even from permissive sources, do not import:

- upstream branding, command names, or product taxonomy
- browser terminals as the default operator path
- full sidecar daemons when Codero already has the equivalent control-plane role
- framework glue that obscures Codero's deterministic review and merge rules

## Related Docs

- `docs/module-intake-policy.md`
- `docs/module-intake-registry.md`
- `docs/roadmap-intake-map.md`
