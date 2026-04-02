# Bot PTY Delivery Contract

**Version:** 1.0
**Last Updated:** 2026-04-02
**Status:** active

## Purpose

This contract defines how external bot shells deliver messages into a live
agent terminal session while Codero remains the source of truth for policy,
auth, review state, and GitHub mutation.

Codero provides a repo-local PTY delivery runtime for this contract backed by
shared tooling. The implementation uses a structured event envelope boundary
where Codero emits the intent and the transport layer (OpenClaw or a bridge)
owns the timing and PTY injection.

It formalizes the PTY-first runtime direction for human-attached sessions:

- OpenClaw or another bot shell is the communication layer
- Codero is the auth, policy, and delivery-state authority
- The live PTY or `tmux` session is the continuity surface

## Scope

This contract applies to any Codero-managed flow that injects messages into an
already-running agent session through a terminal adapter.

It covers:

- readiness checks before injection
- busy-session interruption rules
- accepted, processing, and final-answer detection
- family-specific submit retry behavior where the CLI needs it
- operator-visible continuity requirements

It does not redefine:

- Codero durable delivery state
- dashboard or API authorization
- GitHub write authority
- merge or gate policy

## Product Stance

External bot shells are transport and presentation surfaces only.

They may:

- query Codero state
- present summaries to users
- deliver text into a live PTY session
- expose slash commands that map onto Codero APIs

They may not:

- become a new source of truth for delivery state
- decide whether a branch is mergeable
- bypass Codero auth or policy checks
- mutate GitHub state outside Codero-owned contracts

## Target Operations

These operations describe the repo-local PTY delivery runtime.

Codero provides a high-level `Deliver` operation through its `ReplyToClient`
interface, which is backed by a shared PTY bridge. While `internal/tmux/tmux.go`
exposes low-level session primitives, the `delivery_pipeline` package uses
the higher-level bridge for busy detection and family-specific injection.

The PTY delivery flow is modeled as the following logical operations:

- `start-profile` — launch a managed session for an agent family
- `wait-ready` — confirm the session reached a family-specific ready state
- `deliver` — inject a message into a live session with busy handling and
  post-submit observation
- `capture` — read recent pane output for operator or adapter inspection
- `status` — report process and session metadata
- `stop` — terminate a managed session

Low-level raw text injection may exist as an implementation detail, but
`deliver` is the high-level operation for bot-to-session messaging in the
active Codero-owned runtime.

## Delivery State Model

The target `deliver` flow must distinguish the following observable states:

- `ready` — the session is at an input prompt or other family-specific idle view
- `accepted` — the delivered prompt has been acknowledged or queued by the CLI
- `processing` — the agent has visibly begun acting on the delivered prompt
- `final` — the agent has emitted an operator-visible answer and returned to an
  idle-ready view or equivalent settled state
- `blocked` — the session is in a family-specific unrecoverable state such as
  quota exhaustion or auth failure

### State Detection Rules

- State detection is per family. One global regex is not sufficient.
- Structural matchers are preferred over literal verb lists when a family’s
  on-screen wording is unstable.
- The adapter may extend its wait window after `accepted` or `processing`
  appears.
- PTY state detection is observational only. Codero durable state remains
  authoritative for pipeline and review truth.

## Busy-Session Interruption Contract

When the repo-local `deliver` operation sees that the target session appears
busy, the adapter may send a single `Esc` before injecting the new message.

When that happens:

- the original user message must still be preserved verbatim
- the injected text must be wrapped with a short interruption note
- exact-output instructions must not be weakened or rewritten
- the agent may decide whether to act on the new message immediately or after
  the interrupted work, but it must still receive the new request exactly

If the session is already idle, `deliver` must not send an interruption key.

## Family-Specific Detection Principles

These cues are the normative target for the repo-local `deliver`
implementation. The family-specific detection logic is currently provided by
the shared bridge tool (`agent-tmux-bridge`) used by the Codero PTY adapter.

### Codex

- Accepted cues include the queued-message banner after submit.
- Processing cues include the visible working banner.
- A second `Enter` is allowed if the delivered prompt is still visibly unsent
  and no accepted cue appeared.

### Claude

- Claude status wording is not treated as stable.
- Processing must be detected structurally from transient status lines rather
  than by hard-coding every verb.
- `esc to interrupt` remains a valid secondary busy cue.

### Gemini

- Processing is keyed off the active spinner line that includes
  `(esc to cancel, Ns)`.
- Final answer detection may use the current answer glyph plus the return to the
  idle composer.

### Copilot

- Accepted cues include the current pending marker.
- Processing cues include the current thinking banner.
- A second `Enter` is allowed if the prompt is still visibly unsent after the
  first submit and no accepted cue appeared.

### OpenCode

- Busy detection keys off the visible thinking section and the current
  `esc interrupt` footer.
- Completion must not depend on a single answer glyph; a rendered answer plus a
  settled ready footer is sufficient.

## External Validation Evidence

The following disposable live smokes were verified in the shared reference
helper before this contract was captured:

- Codex:
  - `CONDITIONAL_ESC_OK`
  - `GENERIC_INTERRUPT_OK`
- Claude:
  - `CLAUDE_INTERRUPT_LIVE_OK`
- Gemini:
  - `GEMINI_INTERRUPT_LIVE_OK`
- Copilot:
  - `COPILOT_INTERRUPT_LIVE_OK`
- OpenCode:
  - `OPENCODE_INTERRUPT_LIVE_OK`

These tokens are evidence that the shared interrupted-work `deliver` path can
reach a real final answer across all supported managed families.

## Implementation Notes

Codero implements the PTY delivery surface in `internal/event/reply_to.go`
by wrapping the shared reference helper. The repo-local tmux package
continues to offer raw session lifecycle and key-injection primitives, while
the higher-level delivery logic is delegated to the bridge.

Reference implementation and tests:

- `internal/event/reply_to.go`
- `/srv/storage/shared/tools/bin/agent-tmux-bridge`
- `/srv/storage/shared/tools/tests/agent-tmux-bridge.sh`

## Follow-Ups

- If callers need it, expose machine-readable delivery state alongside captured
  pane text.
- Decide whether low-level raw `send` should stay minimal or gain the same
  family-specific retry behavior that `deliver` already has.
