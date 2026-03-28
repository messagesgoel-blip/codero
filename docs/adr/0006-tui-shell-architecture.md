# ADR-0006: TUI Shell Architecture

**Status:** Superseded — TUI removed in favor of web dashboard (2026-03-28)
**Date:** 2026-03-20
**Context:** UI-001 TUI Visual Design Refresh

## Context

Codero already has the right product surfaces:
- review assistant shell
- findings and routing pane
- pipeline pane
- event stream and architecture pane

The next implementation slice is a shell-level refactor, not more isolated pane polish.
The goal is to make the TUI read as a coherent operator console while preserving the
existing Codero-specific workflows.

## Decision

Codero will evolve the TUI in phases:
- compact top operator strip
- shared pane and metric primitives
- mode-aware bottom action bar
- stronger active-pane and selected-row styling
- centered help/detail overlays where appropriate

The existing shell and pane structure stay in place until the refactor is stable.

## Reference Borrow Matrix

### Tier 1 - Primary implementation references

- `leg100/pug`
  - pane manager architecture, focusable split panes, numeric pane focus, resize controls
  - use for Codero findings explorer/detail, persistent event/log pane, and resizable multi-pane shell
- `charmbracelet/soft-serve`
  - shared UI core, footer/help contract, status bar contract, tabs, selector/detail split
  - use for shared pane primitives, page-level help model, and review/findings/activity tab patterns
- `GustavoCaso/docker-dash`
  - top navigation tabs, switchable right-side panels, contextual bottom help/status bar, alert banners
  - use for top-level Findings/Pipeline/Agents/Logs navigation and right-pane detail switching
- `cloudposse/atmos`
  - 3-column operator flow, focused-column border treatment, filtering, overview/detail toggle
  - use for findings/actions shell and a stronger mode-aware footer

### Tier 2 - Strong pane and workflow references

- `gitsocial-org/gitsocial`
  - host/router architecture, shared TUI core, review-specific list/detail/diff/feedback flows
- `dhth/prs`
  - PR list + timeline + detail split, help view, status styling, markdown rendering
- `BalanceBalls/nekot`
  - compact status strip, prompt/input discipline, searchable side lists, copy workflow
- `dhth/cueitup`
  - active-pane highlighting, footer mode chips, separate help view, focused inspector flow
- `kontrolplane/kue`
  - table-driven overview pages, filter bar, centered help/confirm overlays
- `dukaev/superdive`
  - tree/detail coupling, async panel loading, viewport-heavy detail surfaces
- `Rshep3087/lunchtui`
  - metric hero cards, responsive overview sections, help-toggle footer, mixed list/table/detail flows
- `charmbracelet/glow`
  - markdown pager ergonomics, polished help/status bars, adaptive rich-text rendering

### Tier 3 - Focused utility references

- `purpleclay/dns53`
  - page-owned help bindings, filtered list component, simple header/page/footer framing
- `ariasmn/ugm`
  - compact tab-to-switch list/detail flow, searchable list + viewport pairing
- `dhth/outtasync`
  - compact inline status badges, footer counts, quick filtered-view toggles
- `caarlos0/fork-cleaner`
  - bulk-select list workflow, dense summary rows, extra-help keys
- `stefanlogue/meteor`
  - focused form flow and config-driven task scaffolding
- `a3chron/gith`
  - polished quick-action flows, theme/accent discipline, step-based shortcut paths
- `chhetripradeep/chtop`
  - compact telemetry walls and lightweight refresh surfaces
- `khaapamyaki/at_cli`
  - tiny focused input/result flows

### Inspiration only - do not port code

- `STVR393/helius-personal-finance-tracker`
- `kyren223/eko`
- `bensadeh/circumflex`
- `termkit/gama`
- `rusinikita/trainer`

## Codero Adoption Plan

### Shell/navigation pass

- base the shell rewrite on `pug + soft-serve + docker-dash + atmos`
- goals: compact top operator strip, shared pane primitives, stronger focus treatment, pane resize/focus controls, mode-aware footer

### Findings/detail pass

- base the findings workflow on `cueitup + superdive + prs + glow`
- goals: clearer list/detail split, richer markdown detail rendering, timeline/detail transitions, dense but readable selection state

### Review assistant pass

- base the assistant on `nekot + atmos + glow`
- goals: better prompt bar, clearer current-context strip, improved markdown/table output, copy-friendly responses

### Event/log pass

- base the activity pane on `superdive + prs` and earlier `campfire` notes
- goals: stronger event timeline, severity/status cues, readable detail drilldown

### Utilities pass

- use `dns53`, `outtasync`, and `fork-cleaner` for filtered lists, compact counters, and bulk-action patterns
- use `chtop` only if a small telemetry strip or sparkline-style health pane adds real operator value

## Design Constraints

- Keep Codero review- and operator-first; do not drift into a chat-first shell
- Reuse ideas, not upstream taxonomy or branding
- Favor permissive-license repos for implementation patterns; keep copyleft repos as inspiration only
- Preserve the good Codero-specific surfaces already in place:
  - review assistant shell
  - findings and routing pane
  - pipeline pane
  - event stream and architecture pane

## Maintenance Cadence

- Review this ADR whenever UI-001 adds or removes a major shell surface.
- Revalidate the reference list before starting a new UI refactor wave.
- Keep Codero-specific wording in the backlog; keep implementation detail here.
