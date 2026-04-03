# Actor Boundaries and Hop Model

**Version:** 1.0
**Task:** BND-001
**Last Updated:** 2026-04-01
**Status:** canonical for Codero repo

## Purpose

This document freezes actor authority and hop boundaries across agent, OpenClaw,
Codero, and GitHub. It is the single authoritative reference for ownership
models within the Codero repo.

All other repo-local docs must align with this document. When ownership
questions arise, this document takes precedence over older inline statements in
other contracts or roadmaps.

**Canonical spec authority:** This document derives from and must not contradict
`/srv/storage/local/codero/specication_033126/codero-agent-task-execution-spec.md`.

## Canonical Hop Model

Every task execution follows this message path:

```text
agent -> openclaw -> codero -> github -> codero -> openclaw -> agent
```text
agent -> openclaw -> codero -> github -> codero -> openclaw -> agent
```

2. Verify no doc claims OpenClaw owns durable state or merge authority.

3. Verify no doc claims agent owns commit, push, or GitHub mutation.

4. Confirm the six-hop model is consistent across:
   - `docs/contracts/actor-boundaries.md` (this document)
   - `docs/runtime/openclaw-privilege-profile.md`
   - `docs/contracts/agent-handling-contract.md`

## References

- Canonical spec: `/srv/storage/local/codero/specication_033126/codero-agent-task-execution-spec.md`
- Roadmap: `docs/roadmaps/dogfood-execution-roadmap.md`
- BND-001 task definition: freeze actor authority and hop boundaries

## Change Log

| Date | Version | Change |
|------|---------|--------|
| 2026-04-01 | 1.0 | Initial freeze (BND-001) |
