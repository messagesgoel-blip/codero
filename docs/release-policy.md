# Release Policy

## Cadence

- Trunk remains releasable at all times.
- Releases are tagged from `main`.

## Promotion Rules

- All required CI checks green.
- No unresolved blocking issues.
- Changelog notes include behavior changes and rollback notes.

## Rollback

- Every release PR must document rollback steps.
- Emergency rollback is previous stable tag deployment.
