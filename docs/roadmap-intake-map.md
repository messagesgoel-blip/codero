# Roadmap Intake Map

Status: active
Owner: sanjay
Updated: 2026-03-30

## Purpose

This document maps the current roadmap to upstream implementation candidates.
It does not change the roadmap. It answers a narrower question:

"If we need to fulfill a roadmap item, which upstream sources are safe to copy
from, which are reference-only, and which paths should we avoid?"

## Legend

- `safe copy`: permissive source, small-slice intake allowed
- `reference only`: reimplement behavior, do not copy code
- `no-go`: do not use to fulfill this roadmap area

## Phase 1 Map

| Roadmap Area | Internal Codero Target | Safe Copy | Reference Only | No-Go |
|---|---|---|---|---|
| P1-A Dashboard API gaps | `internal/dashboard`, `docs/dashboard-architecture.md` | `opensessions` for session detail and progress/log field shapes; `par` for global inventory and naming cues | `claude-squad` for session/worktree workflows | copying any upstream TUI pane layout or branding into the dashboard |
| P1-B Agent discovery and setup | `cmd/codero/agent_*`, `docs/agent-setup.md` | `par` for naming and cleanup rules; `opensessions` for programmatic metadata concepts | Claude Code hooks docs, Aider watch-file docs, `claude-squad` lifecycle behavior | upstream wrappers that make Codero depend on another product's control loop |
| P1-C proving period evidence | scorecard, runbooks, delivery accounting | `actions/scaleset` for poll-and-ack event accounting patterns | upstream observability guidance | importing an external dashboard or metrics product as the system of record |
| P1-D recovery drills | daemon restart, Redis restart, stale session cleanup | `actions/scaleset` for listener/session retry and ack loops; `terminal-control-mcp` for timeout and cleanup behavior | `claude-squad` lifecycle recovery behavior | Kubernetes-only controller assumptions that do not fit Codero's local runtime |
| P1-E pre-commit enforcement | `scripts/review`, hooks, `commit-gate` path | keep Codero-native; no external code required by default | upstream CodeRabbit and agent hook docs only | generic hook frameworks that weaken deterministic gate ordering |

## Capability Map

### Feedback Delivery To Running Agents

Internal target:

- `internal/delivery_pipeline`
- `internal/webhook/feedback_push.go`
- `internal/feedback`
- `cmd/codero/agent_launch.go`

Safe copy:

- `terminal-control-mcp` for output retrieval modes, bounded capture, history isolation, timeout cleanup
- `opensessions` for lightweight notify, status, and log payload ideas

Reference only:

- Claude Code hooks documentation for resume-time feedback injection
- Aider watch-file documentation for file-triggered workflows

No-go:

- live stdin injection as the primary feedback mechanism
- browser terminals as the default operator interface

### Session Naming, Inventory, And Reattach

Internal target:

- `internal/tmux`
- session APIs
- dashboard Sessions and Agents tabs

Safe copy:

- `par` for deterministic naming and global inventory conventions
- `opensessions` for session detail fields and operator-facing breadcrumbs

Reference only:

- `claude-squad` for attach/resume/kill workflow

No-go:

- importing a second terminal UI as Codero's canonical operator surface

### Listener And Message Loops

Internal target:

- delivery events
- feedback availability
- any future mailbox ack loop

Safe copy:

- `actions/scaleset` for poll -> message -> ack -> continue loop shape

Reference only:

- GitHub ARC docs for the conceptual listener/controller split

No-go:

- importing GitHub-specific controller code paths that do not map to Codero's domain

### Operator Dashboard Without Terminal Access

Internal target:

- `/dashboard`
- session list/detail/tail
- delivery state and feedback availability

Safe copy:

- `opensessions` for what metadata operators need to see
- `par` for all-context inventory patterns
- `terminal-control-mcp` for read-only capture semantics

Reference only:

- `claude-squad` as an example of useful operator actions around detached sessions

No-go:

- copying an upstream TUI whole and embedding it behind the dashboard

## Recommended Execution Order

1. Use `docs/roadmap.md` to choose the target.
2. Check `docs/borrowed-components.md` for license-safe sources.
3. Use this map to choose the right upstream repo for that roadmap slice.
4. Record the intake in `docs/module-intake-registry.md` when a real module is adopted.
5. Land code only after the contract and tests are in place.

## Related Docs

- `docs/roadmap.md`
- `docs/module-intake-policy.md`
- `docs/borrowed-components.md`
- `docs/module-intake-registry.md`
