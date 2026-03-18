# Release Policy

## Cadence

- Trunk remains releasable at all times.
- Releases are tagged from `main`.

## Promotion Rules

- All required CI checks green.
- No unresolved blocking issues.
- Changelog notes include behavior changes and rollback notes.

## Build Requirements

- Release binaries **must** be built with version stamping:
  ```bash
  make build VERSION=vX.Y.Z
  # or equivalently:
  go build -trimpath -ldflags "-X main.version=vX.Y.Z" -o codero ./cmd/codero
  ```
- Builds without `VERSION` produce a `dev` binary — **never promote `dev` builds as releases**.
- Verify stamping before promotion: `./codero version` must return `vX.Y.Z` exactly.

## Rollback

- Every release PR must document rollback steps.
- Emergency rollback is previous stable tag deployment.
