# Dashboard Refresh Design Spec

**Date**: 2026-03-22
**Scope**: Visual refresh + new Agents tab for Codero embedded dashboard
**Working tree**: `/srv/storage/repo/codero/.worktrees/COD-071-v3-closeout`

---

## 1. Summary

Refresh Codero's built-in dashboard to match the design language of the build-it-right reference implementation. Add a new top-level "Agents" tab that consolidates all agent-related surfaces. No new backend endpoints are needed; all required API surfaces already exist in COD-071.

## 2. Constraints

- **Codero is source of truth.** No build-it-right mock data models imported.
- **No new backend endpoints.** `/assignments`, `/agent-events`, `/compliance`, `/active-sessions`, `/health` all exist.
- **No Task Layer fields.** Do not block on `assignment_version`, `task_feedback_cache`, or `codero_github_links`.
- **Embedded static assets.** No frontend build pipeline. Pure HTML/CSS/JS served via Go `embed.FS`.
- **No `orch-*` commands.**

## 3. File Structure

Split the current 1175-line `index.html` monolith into three files under `internal/dashboard/static/`:

| File | Purpose |
|------|---------|
| `index.html` | HTML shell: sidebar nav, page containers, semantic structure |
| `styles.css` | Full CSS design system: tokens, components, layouts, themes |
| `app.js` | All JavaScript: routing, API layer, state management, tab renderers |

The existing `static_embed.go` already embeds `static/*`, so no Go changes needed for serving.

**Asset references must be relative** (`./styles.css`, `./app.js`). Codero serves the dashboard under a configurable base path (not always `/dashboard/`), as implemented in `internal/daemon/observability.go`. Absolute paths like `/dashboard/styles.css` would break under non-default base paths.

## 4. Visual System (from build-it-right)

### 4.1 Color Tokens (HSL, CSS custom properties)

**Dark theme (default):**
- `--bg-base`: 220 10% 13% (page background)
- `--surface-1`: 220 12% 8% (card/panel fill — darkest)
- `--surface-2`: 220 10% 16% (hover/elevation)
- `--surface-3`: 220 8% 20% (highest contrast)
- `--text`: 210 15% 85%
- `--text-muted`: 210 8% 48%
- `--border`: 220 8% 18%
- `--border-subtle`: 220 8% 18% / 50% opacity
- `--primary`: 120 45% 55% (green accent)
- `--ring`: matches primary

**Status colors (each with 12% bg / 25% border / 100% text variants):**
- `--status-active`: 120 45% 55% (green)
- `--status-blocked`: 38 85% 55% (amber)
- `--status-completed`: 210 55% 55% (blue)
- `--status-waiting`: 48 80% 55% (yellow)
- `--status-cancelled`: 0 0% 42% (gray)
- `--status-lost`: 0 65% 52% (red)

**Rule enforcement:**
- `--rule-hard`: 0 65% 52% (red)
- `--rule-soft`: 38 85% 55% (amber)
- `--rule-pass`: 120 45% 55% (green)
- `--rule-fail`: 0 65% 52% (red)
- `--rule-pending`: 48 80% 55% (yellow)

**Light theme:** Inverted luminance values, same hues.

### 4.2 Typography

- **UI text**: Source Sans 3 (fallback: system-ui, sans-serif)
- **Data/mono**: IBM Plex Mono
- **Labels**: text-xs, uppercase, tracking-wider, muted color
- **Headings**: text-lg, semibold, tight tracking
- **Timestamps**: 10px mono, muted

### 4.3 Component Patterns

**Status chips:** Inline-flex, rounded-full, px-2.5 py-0.5, mono text-xs. Include animated pulse dot (2s ease-in-out) for active states. Background at 12% opacity, border at 25%.

**Metric cards:** rounded-lg border, surface-1 fill, p-4. Value in text-2xl bold mono. Label below in text-xs uppercase muted. Staggered reveal animation (0.6s cubic-bezier, 60ms delay per card).

**Data tables:** Full-width, text-sm. Headers: text-xs uppercase tracking-wider muted. Cells: px-4 py-3, mono text-xs, border-b subtle. Row hover: surface-2.

**Expandable rows:** Click to expand detail grid (2-4 columns). Expansion area: surface-2 fill, border-y subtle, px-8 py-3.

**Timeline events:** Vertical dot+line pattern. Dot color by event type. Connector line in border/50.

**Cards (compliance rules):** Flex header with enforcement badge (hard=red, soft=amber). Stats row with colored pass/fail/pending counts.

### 4.4 Layout

**Sidebar navigation** (replacing top tabs):
- Width: 14rem (224px), flex-shrink-0
- Background: slightly darker than main (220 12% 7%)
- Nav items: full-width, px-3 py-2, rounded-md, gap-2.5
- Active: accent background + accent foreground
- Inactive: muted foreground, hover accent/50
- Count badges: 10px mono, secondary bg, px-1.5 py-0.5 rounded

**Main content:** flex-1, overflow-y-auto, p-6, space-y-6.

**Responsive:** Sidebar collapses on < 768px (icon-only or hamburger).

### 4.5 States

**Loading:** Skeleton blocks with pulsing surface-2 animation. Per-section, not full-page.

**Empty:** Centered muted icon + text, per-section.

**Error:** Inline banner with muted-red background, retry button.

## 5. Tab Structure

| Tab | Existing? | Data Source |
|-----|-----------|-------------|
| Overview | Yes (was "Processes") | `/overview`, `/repos`, `/active-sessions`, `/gate-health` |
| **Agents** | **New** | `/active-sessions`, `/assignments`, `/compliance`, `/agent-events`, `/health` |
| Events | Yes (was "Event Logs") | `/activity` |
| Findings | Yes | `/gate-checks`, `/block-reasons` |
| Architecture | Yes | Static diagram (keep mostly intact) |
| Settings | Yes | `/settings` |

## 6. Agents Tab Design

### 6.1 Metric Strip (top)

Four metric cards in a row:
1. **Active Sessions** — count from `/active-sessions` → `active_count`
2. **Assignments** — count from `/assignments` → `count`
3. **Compliance Score** — derived: `(passing checks / total checks) * 100` from `/compliance`. Display "—" when there are zero checks (not 0%).
4. **Recent Events** — total count from `/agent-events` → `count`

### 6.2 Sessions Panel

Expandable table from `/active-sessions`:

| Column | Field | Notes |
|--------|-------|-------|
| Status dot | `activity_state` | Pulse animation if active |
| Agent | `agent_id` / `owner_agent` | With mode badge |
| Repo/Branch | `repo`, `branch` | Mono |
| Task | `task.id` + `task.title` | If present |
| Phase | `task.phase` | Status chip |
| Heartbeat | `last_heartbeat_at` | Relative time, warn if stale |
| Elapsed | `elapsed_sec` | Formatted |

Expand reveals: `session_id`, `worktree`, `pr_number`, `started_at`, `progress_at`.

### 6.3 Assignments Panel

Table from `/assignments`:

| Column | Field | Notes |
|--------|-------|-------|
| State chip | `state` | Color-coded (active/blocked/completed/cancelled/lost) |
| Assignment | `assignment_id` | Mono, truncated |
| Agent | `agent_id` | |
| Repo/Branch | `repo`, `branch` | Mono |
| Substatus | `substatus` | Chip if non-empty |
| Blocked Reason | `blocked_reason` | Red text if present |
| Started | `started_at` | Relative time |
| Ended | `ended_at` | Relative time or "—" |

Expand reveals: `session_id`, `task_id`, `worktree`, `end_reason`, `superseded_by`, `branch_state`, `pr_number`, `mode`.

### 6.4 Bottom Split

**Left: Compliance Rules** from `/compliance` → `rules[]`:
- Card per rule with enforcement badge (hard/soft)
- Stats: pass count, fail count, pending (derived from `checks[]` filtered by `rule_id`)
- `rule_version` shown
- Active violations highlighted (checks where `result="fail"` and `resolved_at` is null)

**Right: Agent Event Timeline** from `/agent-events` → `events[]`:
- Vertical timeline with colored dots by `event_type`
- Each entry shows: time, agent, event type, payload summary
- Scroll for older events

## 7. Existing Tab Updates

### Overview
- Visual refresh only (new tokens, metric cards, status chips)
- Keep: sparkline, blocked count, pass rate, avg gate time
- Keep: repo table with gate pills
- Add metric strip at top (reuse pattern from Agents)

### Events
- Visual refresh (new table styling, timeline pattern)
- Keep: severity filtering (ALL/CRITICAL/HIGH/MEDIUM/LOW)
- Apply expandable row pattern for event detail

### Findings
- Visual refresh (new card/table styling)
- Keep: card/table view toggle, resolution panel
- Apply rule enforcement badges to severity

### Architecture
- Minimal refresh: update colors/borders to match new tokens
- Keep diagram structure intact

### Settings
- Visual refresh (new card styling, toggle styling)
- Keep: integration cards, gate pipeline table, save behavior

## 8. JavaScript Architecture

```
app.js structure:
├── Constants (API base, poll interval, page sizes)
├── State management (activeTab, theme, section loading/error states)
├── API layer (apiFetch with error handling, per-section fetchers)
├── Theme management (dark/light, system preference)
├── Router (tab switching via hash-based navigation only, keyboard nav)
│   ├── Uses window.location.hash (#overview, #agents, etc.)
│   ├── No History API pushState — static file server has no SPA path rewrites
├── Shared renderers (statusChip, metricCard, dataTable, timeline, skeleton, empty, error)
├── Tab renderers
│   ├── renderOverview()
│   ├── renderAgents()
│   ├── renderEvents()
│   ├── renderFindings()
│   ├── renderArchitecture()
│   └── renderSettings()
├── Polling (10s interval, per-section refresh)
├── SSE (DEFERRED — not required for first pass; polling is sufficient)
└── Init (DOMContentLoaded → load theme, render shell, start polling)
```

## 9. Testing

- **Existing tests pass.** No backend changes means `dashboard_test.go` and `queries_internal_test.go` stay green.
- **Static embed coverage.** Extend tests to verify that `index.html`, `styles.css`, and `app.js` are all embedded and served correctly under the configured dashboard base path. This means:
  - `GET {basePath}/` returns HTML that references `./styles.css` and `./app.js`
  - `GET {basePath}/styles.css` returns CSS with `text/css` content type
  - `GET {basePath}/app.js` returns JS with `application/javascript` content type
  - All three assets are present in the `embed.FS`
- **Run**: `go test ./internal/dashboard ./internal/daemon` then `go test ./...`

## 10. Contract Doc

Update `/docs/contracts/dashboard-api-contract.md` only if the UI reveals a gap. Currently no changes expected since all endpoints exist.

## 11. Files Changed

| File | Change |
|------|--------|
| `internal/dashboard/static/index.html` | Rewrite: HTML shell only |
| `internal/dashboard/static/styles.css` | New: full CSS design system |
| `internal/dashboard/static/app.js` | New: all JavaScript |
| `internal/dashboard/dashboard_test.go` | Update: static serving assertions if needed |
| `docs/superpowers/specs/2026-03-22-dashboard-refresh-design.md` | This spec |

## 12. Deferred to Task Layer

- `assignment_version` field display
- `task_feedback_cache` integration
- `codero_github_links` in assignment detail
- Sparkline chart rendering (data exists, chart not drawn)
- Chat assistant wiring (endpoint exists, UI deferred)
- SSE live event stream (polling is sufficient for first pass; SSE can be wired in a follow-up)

## 13. Risk & Mitigation

| Risk | Mitigation |
|------|------------|
| Large single JS file | Structured with clear sections; can split later if needed |
| No frontend tests | Go embed test verifies files exist and serve; manual visual QA |
| CSS token drift from build-it-right | Codero tokens are independent; build-it-right is reference only |
| Responsive breakpoints | Test at 768px and 1024px; sidebar collapses gracefully |
