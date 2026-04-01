# OpenClaw Update Policy for Codero

Status: shadow-mode / Codero-local  
Task: TOOL-004 (contained)  
Date: 2026-04-01

## Purpose

This document defines the OpenClaw update cadence and change-control checks for
Codero. It specifies how often OpenClaw should be reviewed, what triggers
immediate review, and what validation must occur before and after changes.

**Shadow mode means:**

- This document describes the intended update policy for Codero.
- It does not enforce this policy globally.
- It does not claim that other repos follow this cadence.
- It does not trigger any version changes now.
- It does not modify shared OpenClaw defaults.
- Enforcement work belongs to later tasks.

The purpose is to replace ad hoc upgrades with a predictable and auditable
version-management process.

## Baseline Concept

The **Codero OpenClaw baseline** is the approved combination of:

- OpenClaw version (pinned)
- Plugin allowlist (litellm, acpx)
- Privilege profile (adapter-only, no forbidden credentials)
- Config shape (loopback gateway, token auth, isolated workspace)
- PTY bridge path (`/srv/storage/shared/tools/bin/agent-tmux-bridge`)

Changes to any of these components require review according to this policy.

## Current Baseline

| Component | Current value | Source |
|-----------|---------------|--------|
| OpenClaw version | `2026.3.28` | `openclaw.json` meta.lastTouchedVersion |
| Plugins | `litellm`, `acpx` | `openclaw.json` plugins.entries |
| Gateway bind | `loopback` | `openclaw.json` gateway.bind |
| Gateway auth | `token` | `openclaw.json` gateway.auth.mode |
| Workspace | `$HOME/.openclaw-codero-smoke/workspace` | `openclaw.json` agents.defaults.workspace |
| PTY bridge | `/srv/storage/shared/tools/bin/agent-tmux-bridge` | Shared tooling baseline |

## Update Cadence

### Scheduled Review

**Monthly scheduled baseline review.** On the first week of each month:

1. Check for new OpenClaw releases
2. Review changelog for breaking changes
3. Evaluate whether update is needed
4. If updating, follow the change-control checklist
5. Record review outcome even if no change is made

### Immediate Review Triggers

Update review must happen immediately (out-of-band) for:

| Trigger | Reason |
|---------|--------|
| Security vulnerability in OpenClaw | Protect runtime integrity |
| Security vulnerability in enabled plugin | Protect runtime integrity |
| Correctness bug affecting Codero runtime path | Restore functionality |
| Breaking change in LiteLLM compatibility | Model routing depends on it |
| Breaking change in PTY bridge compatibility | Session delivery depends on it |
| Plugin API change affecting acpx | Codero integration depends on it |

Immediate reviews use the same checklist but skip the scheduled waiting period.

### What Does Not Require Immediate Review

| Situation | Action |
|-----------|--------|
| New OpenClaw release with no Codero-relevant changes | Note in next monthly review |
| New bundled plugin we do not use | No action needed |
| Upstream cosmetic/UX changes | Note in next monthly review |
| Deprecation warning for unused feature | Note for awareness |

## Change-Control Checklist

Before adopting any OpenClaw version, config, or plugin change, complete this
checklist:

### Pre-Change Review

| Check | Description | Status |
|-------|-------------|--------|
| Version pin identified | New version number documented | ☐ |
| Changelog reviewed | Breaking changes, deprecations, security fixes noted | ☐ |
| Plugin allowlist unchanged | Only `litellm` and `acpx` enabled | ☐ |
| Privilege profile unchanged | No new forbidden credentials required | ☐ |
| Config shape unchanged | Gateway, auth, workspace consistent with baseline | ☐ |
| PTY bridge compatible | Shared bridge still works with new version | ☐ |
| LiteLLM compatible | Model routing still works | ☐ |
| Codero API compatible | Session/heartbeat/submit calls still work | ☐ |
| No auto-update enabled | Floating updates disabled | ☐ |

### Post-Change Certification

After applying a change, rerun these validations:

| Validation | Script | Required |
|------------|--------|----------|
| Tooling baseline | `scripts/validate-tooling-baseline.sh` | Yes |
| Privilege profile | `scripts/validate-openclaw-privileges.sh` | Yes |
| Plugin allowlist | `scripts/validate-openclaw-plugins.sh` | Yes |
| PTY bridge smoke | Manual: bridge `deliver` to test session | Yes |
| Codero heartbeat | Manual: session register + heartbeat cycle | Yes |
| LiteLLM model call | Manual: model request through provider | Yes |

All automated validators must pass. Manual checks must be recorded.

### Certification Matrix

The PTY smoke matrix from `/srv/storage/shared/memory/OPENCLAW-PTY-NOTES.md`
must be rerun after any meaningful OpenClaw change:

| Family | Required smoke | Token reply |
|--------|----------------|-------------|
| codex | deliver + interrupt | `GENERIC_INTERRUPT_OK` |
| claude | deliver + interrupt | `CLAUDE_INTERRUPT_LIVE_OK` |
| gemini | deliver + interrupt | `GEMINI_INTERRUPT_LIVE_OK` |
| copilot | deliver + interrupt | `COPILOT_INTERRUPT_LIVE_OK` |
| opencode | deliver + interrupt | `OPENCODE_INTERRUPT_LIVE_OK` |

A change is not certified until at least one family from this matrix passes
the live smoke test.

## What Counts as a Meaningful Change

| Change type | Meaningful? | Requires full certification |
|-------------|-------------|----------------------------|
| OpenClaw version bump | Yes | Yes |
| Plugin enable/disable | Yes | Yes |
| Plugin config change | Yes | Yes (if affects Codero path) |
| Gateway bind change | Yes | Yes |
| Auth mode change | Yes | Yes |
| Workspace path change | Possibly | Review case-by-case |
| Model provider change | Yes | Yes (LiteLLM routing) |
| Cosmetic config comment | No | No |

## Recording Review Outcomes

After each scheduled or immediate review, record:

| Field | Description |
|-------|-------------|
| Review date | When the review occurred |
| Review type | Scheduled / Immediate |
| Trigger | What prompted the review |
| Previous baseline | Version, plugins, config shape |
| New baseline | Version, plugins, config shape (if changed) |
| Changes made | List of specific changes, or "none" |
| Checklist outcome | All checks passed / which failed |
| Certification outcome | All validations passed / which failed |
| Reviewer | Who performed the review |
| Notes | Any observations, risks, or follow-ups |

This record should be kept in the Codero repo or shared memory for audit.

## No Auto-Updates

**Production-like runtime paths must not auto-update OpenClaw.**

The config should not include:

- Auto-update enabled flags
- Floating version pins
- Unreviewed upstream sync

If OpenClaw has auto-update features, they must be disabled for Codero
baselines.

Current config has no auto-update mechanism visible. Verify this remains true
after any version change.

## What Is Not Yet Enforced

This document describes the intended update policy. The following are not yet
enforced:

| Not enforced | Belongs to |
|--------------|------------|
| Automated version pin check | Future automation |
| Automated certification gate | CERT-001 |
| Automated changelog parsing | Future automation |
| Global cadence enforcement | TOOL-005+ |
| Cross-repo version sync | Future coordination |

## Validation

To validate update policy readiness:

```bash
# 1. Verify current version is documented
jq '.meta.lastTouchedVersion' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}"

# 2. Verify no auto-update config present
! grep -i 'auto.update\|autoUpdate' "${OPENCLAW_CONFIG_PATH:-$HOME/.openclaw-codero-smoke/openclaw.json}" && echo "PASS: no auto-update"

# 3. Run all baseline validators
scripts/validate-tooling-baseline.sh
scripts/validate-openclaw-privileges.sh
scripts/validate-openclaw-plugins.sh

# 4. Verify PTY bridge is executable
test -x /srv/storage/shared/tools/bin/agent-tmux-bridge && echo "PASS: PTY bridge OK"
```

All validation checks are read-only. They do not modify any state.

## Update Workflow Summary

```
┌─────────────────────────────────────────────────────────────┐
│  1. Trigger: Monthly schedule or immediate security/bug    │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  2. Pre-change review: checklist, changelog, compatibility │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  3. Apply change in test/smoke config first                │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  4. Post-change certification: validators + PTY smoke      │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  5. Record outcome: baseline, changes, certification       │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  6. Promote to production baseline if all checks pass      │
└─────────────────────────────────────────────────────────────┘
```

## Related Documents

- Tooling baseline: `docs/runtime/codero-tooling-baseline.md`
- Privilege profile: `docs/runtime/openclaw-privilege-profile.md`
- Plugin policy: `docs/runtime/openclaw-plugin-policy.md`
- PTY notes: `/srv/storage/shared/memory/OPENCLAW-PTY-NOTES.md`
- Policy doc: `/srv/storage/local/codero/specication_033126/codero-shared-tooling-and-openclaw-policy.md`
