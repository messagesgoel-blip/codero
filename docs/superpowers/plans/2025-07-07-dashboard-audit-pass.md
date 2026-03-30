# Dashboard Audit Pass — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Clean up dead code, fix _initialized guards, normalize API patterns, improve code quality, rename CSS tokens for Numera branding, add operator action stubs, and add a Scorecard tab across the Codero dashboard frontend + Go backend.

**Architecture:** Vanilla JS ES6 module SPA with hash-based router, pub/sub store, and Go `net/http` backend. All static files live in `internal/dashboard/static/`. The Go handlers register on a single `http.ServeMux`. Changes span 7 groups, each committed independently.

**Tech Stack:** Vanilla JavaScript (ES6 modules, no build), Go 1.25, HTML/CSS (no preprocessor)

---

## File Map

| File | Group(s) | Changes |
|------|----------|---------|
| `internal/dashboard/static/js/store.js` | 1, 7 | Remove `findings`, add `trackingConfig`, `scorecard` |
| `internal/dashboard/static/js/main.js` | 1, 5, 7 | Remove archives route registration, wire btn-chat-toggle, swap logo in applyTheme, register scorecard route |
| `internal/dashboard/static/js/router.js` | — | Already correct (archives redirect exists) |
| `internal/dashboard/static/js/api.js` | 3, 6, 7 | loadNodeRepos writes to store, add operator action POST helpers, add loadScorecard |
| `internal/dashboard/static/js/renderers/pipeline.js` | 2 | Add `_initialized` guard, fix buildPrTable filter |
| `internal/dashboard/static/js/renderers/archives.js` | 2 | Add activeTab guard to renderArchives |
| `internal/dashboard/static/js/renderers/gate.js` | 2, 4 | Add `_initialized` guard, replace cloneNode toggle with event delegation |
| `internal/dashboard/static/js/renderers/agents.js` | 4 | Replace emoji pressure indicators with CSS dot spans |
| `internal/dashboard/static/js/renderers/repos.js` | 3 | Remove duplicate store.set in refreshRepos |
| `internal/dashboard/static/js/renderers/sessions.js` | 6 | Add action buttons to expand content, wire click handlers via delegation |
| `internal/dashboard/static/js/components.js` | 4 | Make skeleton() use deterministic widths |
| `internal/dashboard/static/styles.css` | 4, 5, 6 | CSS dot classes for pressure, rename --primary→--accent + --accent→--accent-warm, Inter font, add .btn-sm / .session-actions |
| `internal/dashboard/static/index.html` | 1, 5, 7 | Fix btn-chat-toggle, add logo classes, add scorecard nav + page div |
| `internal/dashboard/static/numera-logo-light.svg` | 5 | New: light-mode logo variant |
| `internal/dashboard/static/js/renderers/scorecard.js` | 7 | New: scorecard page renderer |
| `internal/dashboard/dashboard_api_v1_handlers.go` | 6, 7 | Add action sub-routes to handleAssignmentDetail, add handleScorecard |
| `internal/dashboard/handlers.go` | 7 | Register scorecard route |

---

## Group 1: Dead Code Cleanup + Store Fixes

### Task 1.1: Remove `findings` from store, add `trackingConfig`

**Files:**
- Modify: `internal/dashboard/static/js/store.js:14`

- [ ] **Step 1: Edit store.js — remove `findings: []`, add `trackingConfig: null`**

In `store.js`, replace:

```js
    findings: [],
```

with nothing (delete the line). Then after the `nodeRepos: null,` line, add:

```js
    trackingConfig: null,
```

The resulting `_state` block should have these keys (in order):
`overview, sessions, assignments, pipeline, queue, repos, events, archives, health, settings, gateConfig, gateChecks, compliance, agents, nodeRepos, trackingConfig, blockReasons, gateHealth, chat, ui`.

- [ ] **Step 2: Verify no remaining references to store key `findings`**

Run:
```bash
cd internal/dashboard/static/js
grep -rn "store\.\(set\|select\|subscribe\).*findings" .
```
Expected: no output (gate.js uses local `findings` variable, not the store key).

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/static/js/store.js
git commit -m "chore(dashboard): remove dead findings key, add trackingConfig to store

Group 1: store.findings was never written to by any API call. trackingConfig
was used by agents.js and api.js but missing from initial state.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 1.2: Remove archives route registration from main.js

**Files:**
- Modify: `internal/dashboard/static/js/main.js:14,27`

- [ ] **Step 1: Remove archives import and route registration**

In `main.js`, delete this import line:
```js
import { initArchives, renderArchives, refreshArchives } from './renderers/archives.js';
```

And delete this route registration line:
```js
registerRoute('archives', { init: initArchives, render: renderArchives, refresh: refreshArchives });
```

The router already handles `#archives` → redirect to `#sessions` in `handleHash()`, so the route registration is dead code (router.js intercepts the hash before it reaches `routes.get(hash)`).

- [ ] **Step 2: Verify archives.js is now unused**

```bash
grep -rn "archives" internal/dashboard/static/js/main.js
```
Expected: no output.

```bash
grep -rn "import.*archives" internal/dashboard/static/js/
```
Expected: no output.

Note: `archives.js` file itself stays — it's still imported by router redirect target (sessions page shows a History tab that uses archives data). Actually, no — the History tab in sessions.js loads archives independently. archives.js is truly unreferenced. We leave the file for now (removing it is safe but out of scope for "narrow diff").

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/static/js/main.js
git commit -m "chore(dashboard): remove dead archives route registration

Group 1: router.js already redirects #archives → #sessions before it reaches
the route table. The registered archives route was unreachable dead code.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 1.3: Wire btn-chat-toggle in header

**Files:**
- Modify: `internal/dashboard/static/js/main.js` (after the floating chat section, around line 101)

- [ ] **Step 1: Add click listener for btn-chat-toggle**

After the floating chat button block (after line 101), add:

```js
// --- Header chat toggle ---
const btnChatToggle = $('btn-chat-toggle');
if (btnChatToggle) {
  btnChatToggle.addEventListener('click', () => {
    if (chatFloatingPanel) {
      const visible = chatFloatingPanel.style.display !== 'none';
      chatFloatingPanel.style.display = visible ? 'none' : 'flex';
      if (!visible) renderFloatingChat(chatFloatingPanel);
    }
  });
}
```

This mirrors the existing `chatFloatingBtn` handler but is wired to the header button (`btn-chat-toggle` in index.html line 61).

- [ ] **Step 2: Verify btn-chat-toggle exists in HTML**

```bash
grep 'btn-chat-toggle' internal/dashboard/static/index.html
```
Expected: `<button class="header-btn" id="btn-chat-toggle"...`

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/static/js/main.js
git commit -m "fix(dashboard): wire btn-chat-toggle to floating chat panel

Group 1: the header chat button had no click handler. Mirror the existing
chat-floating-btn listener to toggle the floating panel.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Group 2: `_initialized` Guards + Pipeline PR Table Fix

### Task 2.1: Add `_initialized` guard to pipeline.js

**Files:**
- Modify: `internal/dashboard/static/js/renderers/pipeline.js:17-20`

- [ ] **Step 1: Add guard variable and early return**

Add `let _initialized = false;` before `initPipeline` (after line 13), then wrap the function body:

Replace:
```js
export function initPipeline() {
  store.subscribe('pipeline', () => renderPipeline());
  store.subscribe('assignments', () => renderPipeline());
}
```

With:
```js
let _initialized = false;

export function initPipeline() {
  if (_initialized) return;
  _initialized = true;
  store.subscribe('pipeline', () => renderPipeline());
  store.subscribe('assignments', () => renderPipeline());
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/dashboard/static/js/renderers/pipeline.js
git commit -m "fix(dashboard): add _initialized guard to initPipeline

Group 2: prevents duplicate store subscriptions if initPipeline is called
multiple times during router navigation.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 2.2: Add `_initialized` guard to gate.js

**Files:**
- Modify: `internal/dashboard/static/js/renderers/gate.js:21-26`

- [ ] **Step 1: Add guard variable and early return**

Add `let _initialized = false;` before `initGate` (after `activeSeverityFilter` declaration at line 10), then wrap:

Replace:
```js
export function initGate() {
  store.subscribe('gateChecks', () => renderGate());
  store.subscribe('blockReasons', () => renderGate());
  store.subscribe('gateConfig', () => renderGate());
  store.subscribe('compliance', () => renderGate());
}
```

With:
```js
let _initialized = false;

export function initGate() {
  if (_initialized) return;
  _initialized = true;
  store.subscribe('gateChecks', () => renderGate());
  store.subscribe('blockReasons', () => renderGate());
  store.subscribe('gateConfig', () => renderGate());
  store.subscribe('compliance', () => renderGate());
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/dashboard/static/js/renderers/gate.js
git commit -m "fix(dashboard): add _initialized guard to initGate

Group 2: same pattern as other renderers — prevents duplicate subscriptions.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 2.3: Fix buildPrTable to show all assignments

**Files:**
- Modify: `internal/dashboard/static/js/renderers/pipeline.js:99`

- [ ] **Step 1: Change the filter to include all assignments**

Replace:
```js
  const withPr = assignments.filter(a => a.prNumber || a.substatus);
```

With:
```js
  const withPr = assignments.filter(a => a.prNumber || a.substatus || a.state);
```

This ensures assignments that have a `state` (like `active`, `blocked`) but no PR number or substatus yet still appear in the pipeline table — they're visible in the kanban but hidden in the table, which is confusing.

- [ ] **Step 2: Commit**

```bash
git add internal/dashboard/static/js/renderers/pipeline.js
git commit -m "fix(dashboard): show stateful assignments in PR table

Group 2: assignments with a state (active, blocked) but no prNumber/substatus
were hidden from the table while visible in the kanban.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 2.4: Add activeTab guard to renderArchives

**Files:**
- Modify: `internal/dashboard/static/js/renderers/archives.js:20-21`

- [ ] **Step 1: Add early return if archives page is not active**

Replace:
```js
export function renderArchives() {
  const container = $('page-archives');
  if (!container) return;
```

With:
```js
export function renderArchives() {
  const container = $('page-archives');
  if (!container) return;
  if (store.state.ui.activeTab !== 'archives') return;
```

Since the route is no longer registered (Task 1.2), activeTab will never be 'archives', so this guard prevents the renderer from doing DOM work on the hidden page if any store subscriber still fires. (Belt-and-suspenders safety.)

- [ ] **Step 2: Commit**

```bash
git add internal/dashboard/static/js/renderers/archives.js
git commit -m "fix(dashboard): guard renderArchives against inactive tab

Group 2: belt-and-suspenders — archives route is dead but store subscribers
could still trigger a render cycle on the hidden page.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Group 3: API Normalization — loadNodeRepos

### Task 3.1: Make loadNodeRepos write to store

**Files:**
- Modify: `internal/dashboard/static/js/api.js:101-103`

- [ ] **Step 1: Update loadNodeRepos to write result to store**

Replace:
```js
export async function loadNodeRepos() {
  return apiFetch('node-repos');
}
```

With:
```js
export async function loadNodeRepos() {
  const data = await apiFetch('node-repos');
  store.set({ nodeRepos: data });
  return data;
}
```

This matches the pattern of every other `load*` function (e.g., `loadSessions`, `loadAssignments`, `loadGateChecks`) which all write to the store.

- [ ] **Step 2: Remove duplicate store.set from repos.js refreshRepos**

In `repos.js`, replace:
```js
export async function refreshRepos() {
  try {
    const data = await loadNodeRepos();
    store.set({ nodeRepos: data });
  } catch (err) {
    store.set({ nodeRepos: { error: err.message } });
  }
}
```

With:
```js
export async function refreshRepos() {
  try {
    await loadNodeRepos();
  } catch (err) {
    store.set({ nodeRepos: { error: err.message } });
  }
}
```

The `store.set({ nodeRepos: data })` line in `refreshRepos` was the only call site for `loadNodeRepos`. Now `loadNodeRepos` handles writing to the store internally (matching all other load functions), and the error fallback stays in `refreshRepos`.

- [ ] **Step 3: Verify no other callers of loadNodeRepos**

```bash
grep -rn 'loadNodeRepos' internal/dashboard/static/js/
```
Expected: `api.js` (definition + store.set) and `repos.js` (import + call).

- [ ] **Step 4: Commit**

```bash
git add internal/dashboard/static/js/api.js internal/dashboard/static/js/renderers/repos.js
git commit -m "refactor(dashboard): loadNodeRepos writes to store like all other loaders

Group 3: normalize the API pattern — loadNodeRepos was the only loader that
returned data without writing to the store. Remove duplicate store.set from
refreshRepos.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Group 4: Code Quality — Gate Delegation, Agents CSS Dots, Skeleton Widths

### Task 4.1: Replace gate toggle cloneNode with event delegation

**Files:**
- Modify: `internal/dashboard/static/js/renderers/gate.js:145-166` (delete attachToggleListeners)
- Modify: `internal/dashboard/static/js/renderers/gate.js:21-26` (add delegation in initGate)

- [ ] **Step 1: Add event delegation to initGate**

After the existing subscriptions in `initGate` and after `_initialized = true;`, add:

```js
  // Event delegation for gate config toggles — survives re-renders
  const container = $('page-gate');
  if (container) {
    container.addEventListener('change', async (e) => {
      if (!e.target.matches('.gate-toggles-card .toggle-input')) return;
      const varName = e.target.id.replace('gate-toggle-', '');
      const newValue = e.target.checked;
      try {
        await apiPut('settings/gate-config/' + encodeURIComponent(varName), { value: newValue });
        await loadGateConfig();
      } catch (err) {
        e.target.checked = !newValue;
        toast('Failed to update gate config: ' + err.message, 'error');
      }
    });
  }
```

- [ ] **Step 2: Remove attachToggleListeners function and its calls**

Delete the entire `attachToggleListeners` function (lines 145-166).

Then search for calls to it:
```bash
grep -n 'attachToggleListeners' internal/dashboard/static/js/renderers/gate.js
```

Remove any call(s) found — likely in `renderGate()`. For example, if there's a line like:
```js
  attachToggleListeners(gateConfig);
```
Delete it.

- [ ] **Step 3: Verify apiPut is imported**

```bash
grep "apiPut" internal/dashboard/static/js/renderers/gate.js | head -3
```
Expected: import line already includes `apiPut` (line 4).

- [ ] **Step 4: Also ensure loadGateConfig is imported**

```bash
grep "loadGateConfig" internal/dashboard/static/js/renderers/gate.js | head -3
```
Expected: import line already includes `loadGateConfig` (line 4).

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/static/js/renderers/gate.js
git commit -m "refactor(dashboard): gate toggle uses event delegation instead of cloneNode

Group 4: cloneNode to remove old listeners was fragile and lost accessibility
attributes. Event delegation on the page container survives re-renders natively.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 4.2: Replace emoji pressure indicators with CSS dot spans

**Files:**
- Modify: `internal/dashboard/static/js/renderers/agents.js:140-144`
- Modify: `internal/dashboard/static/styles.css` (add dot classes)

- [ ] **Step 1: Add CSS classes for pressure dots**

At the end of `styles.css` (before the closing comment or at the very end), add:

```css
/* ========== PRESSURE DOTS ========== */
.pressure-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 4px; vertical-align: middle; }
.pressure-dot.critical { background: var(--destructive); }
.pressure-dot.warning { background: var(--warning); }
```

- [ ] **Step 2: Replace emoji logic in _buildAgentExpandContent**

In `agents.js`, replace:
```js
  const pressureIcon = agent.activePressure === 'critical' ? '🔴'
    : agent.activePressure === 'warning' ? '🟡' : '';
  const pressureLabel = pressureIcon
    ? `${pressureIcon} ${esc(agent.activePressure)}`
    : '<span style="color:var(--fg-muted)">normal</span>';
```

With:
```js
  const pressureLabel = agent.activePressure === 'critical'
    ? `<span class="pressure-dot critical"></span>${esc(agent.activePressure)}`
    : agent.activePressure === 'warning'
      ? `<span class="pressure-dot warning"></span>${esc(agent.activePressure)}`
      : '<span style="color:var(--fg-muted)">normal</span>';
```

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/static/js/renderers/agents.js internal/dashboard/static/styles.css
git commit -m "fix(dashboard): replace emoji pressure indicators with CSS dots

Group 4: emoji render inconsistently across OS/browser. Use styled spans with
--destructive and --warning tokens for reliable, theme-aware rendering.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 4.3: Make skeleton() use deterministic widths

**Files:**
- Modify: `internal/dashboard/static/js/components.js:142-149`

- [ ] **Step 1: Replace Math.random with deterministic pattern**

Replace:
```js
export function skeleton(lines = 3) {
  let out = '<div class="skeleton-container">';
  for (let i = 0; i < lines; i++) {
    const w = 40 + Math.random() * 50;
    out += `<div class="skeleton-line" style="width:${w}%"></div>`;
  }
  return out + '</div>';
}
```

With:
```js
export function skeleton(lines = 3) {
  const widths = [85, 65, 45, 75, 55, 90, 50, 70, 60, 80];
  let out = '<div class="skeleton-container">';
  for (let i = 0; i < lines; i++) {
    const w = widths[i % widths.length];
    out += `<div class="skeleton-line" style="width:${w}%"></div>`;
  }
  return out + '</div>';
}
```

The deterministic widths prevent layout shift between renders and make snapshots/tests stable.

- [ ] **Step 2: Commit**

```bash
git add internal/dashboard/static/js/components.js
git commit -m "fix(dashboard): skeleton() uses deterministic widths

Group 4: Math.random caused layout shift between renders and made testing
non-deterministic. Use a fixed width array for stable skeleton loading.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Group 5: Numera Branding — CSS Token Rename + Inter Font + Light Logo

### Task 5.1: Rename CSS tokens `--primary` → `--accent`, `--accent` → `--accent-warm`

This is a two-phase rename to avoid collisions.

**Files:**
- Modify: `internal/dashboard/static/styles.css`

- [ ] **Step 1: Phase A — rename `--accent` → `--accent-warm` in both declarations and usages**

In the `:root` dark theme block, change:
```css
  --accent: #ffaa00;
```
to:
```css
  --accent-warm: #ffaa00;
```

In the `:root.light` block, change:
```css
  --accent: #e07b00;
```
to:
```css
  --accent-warm: #e07b00;
```

Then find and replace ALL `var(--accent)` → `var(--accent-warm)` in `styles.css`.

There is exactly 1 usage in styles.css (line 472):
```css
  background: var(--accent); color: var(--bg-base); border-color: var(--accent); font-weight: 600;
```
→
```css
  background: var(--accent-warm); color: var(--bg-base); border-color: var(--accent-warm); font-weight: 600;
```

- [ ] **Step 2: Phase B — rename `--primary` → `--accent` in both declarations and usages**

In `:root` dark theme block, change:
```css
  --primary: #00ff88;
  --primary-fg: #0a0a0f;
  --primary-glow: rgba(0, 255, 136, 0.25);
```
to:
```css
  --accent: #00ff88;
  --accent-fg: #0a0a0f;
  --accent-glow: rgba(0, 255, 136, 0.25);
```

In `:root.light` block, change:
```css
  --primary: #00a85a;
  --primary-fg: #f8f9fb;
  --primary-glow: rgba(0, 168, 90, 0.15);
```
to:
```css
  --accent: #00a85a;
  --accent-fg: #f8f9fb;
  --accent-glow: rgba(0, 168, 90, 0.15);
```

Then find and replace ALL in `styles.css`:
- `var(--primary)` → `var(--accent)` (14 occurrences)
- `var(--primary-fg)` → `var(--accent-fg)` (3 occurrences)
- `var(--primary-glow)` → `var(--accent-glow)` (2 occurrences)
- `var(--shadow-glow)` stays unchanged (it references `--primary-glow` in its declaration which we already renamed)

The `--shadow-glow` declaration on line 59 changes from:
```css
  --shadow-glow: 0 0 20px -4px var(--primary-glow);
```
to:
```css
  --shadow-glow: 0 0 20px -4px var(--accent-glow);
```

- [ ] **Step 3: Rename var(--accent) in JS files**

In these JS files, replace `var(--accent)` → `var(--accent-warm)`:

**`sessions.js`** — 5 occurrences:
- Line 226: `color:var(--accent)` → `color:var(--accent-warm)`
- Line 271: `color:var(--accent)` → `color:var(--accent-warm)`
- Line 287: `color:var(--accent)` → `color:var(--accent-warm)`
- Line 288: `{ color: 'var(--accent)' }` → `{ color: 'var(--accent-warm)' }`
- Line 121: `metricCard(String(sessions.length), 'Sessions', 'var(--accent)')` → `metricCard(String(sessions.length), 'Sessions', 'var(--accent-warm)')`

**`overview.js`** — 1 occurrence:
- Line 99: `metricCard(String(ov.runsToday), 'Runs Today', 'var(--accent)')` → `metricCard(String(ov.runsToday), 'Runs Today', 'var(--accent-warm)')`

**`components.js`** — 1 occurrence:
- Line 185: `const color = opts.color || 'var(--accent)';` → `const color = opts.color || 'var(--accent-warm)';`

Run this to find all occurrences:
```bash
grep -rn "var(--accent)" internal/dashboard/static/js/
```

After replacement, confirm zero remaining `var(--accent)` in JS (they should all be `var(--accent-warm)` now; no JS file uses the green `--accent`/old `--primary`).

- [ ] **Step 4: Verify no broken references**

```bash
grep -rn 'var(--primary' internal/dashboard/static/
```
Expected: no output (all renamed to --accent).

```bash
grep -rn 'var(--accent)' internal/dashboard/static/styles.css
```
Expected: multiple hits (the new green accent token).

```bash
grep -rn 'var(--accent-warm)' internal/dashboard/static/
```
Expected: styles.css (1) + sessions.js (5) + overview.js (1) + components.js (1).

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/static/styles.css internal/dashboard/static/js/renderers/sessions.js internal/dashboard/static/js/renderers/overview.js internal/dashboard/static/js/components.js
git commit -m "refactor(dashboard): rename CSS tokens — primary→accent, accent→accent-warm

Group 5: align with Numera design system naming. Green (#00ff88) is the accent
color; amber (#ffaa00) is accent-warm. Two-phase rename to avoid collision.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 5.2: Switch font from Space Grotesk to Inter

**Files:**
- Modify: `internal/dashboard/static/styles.css:2,44`

- [ ] **Step 1: Update Google Fonts import**

Replace:
```css
@import url('https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap');
```

With:
```css
@import url('https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap');
```

- [ ] **Step 2: Update the font-sans token**

Replace:
```css
  --font-sans: 'Space Grotesk', system-ui, -apple-system, sans-serif;
```

With:
```css
  --font-sans: 'Inter', system-ui, -apple-system, sans-serif;
```

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/static/styles.css
git commit -m "style(dashboard): switch body font to Inter

Group 5: Inter is the Numera brand font. Replace Space Grotesk in both the
Google Fonts import and the --font-sans token.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 5.3: Add light-mode logo SVG + swap in applyTheme

**Files:**
- Create: `internal/dashboard/static/numera-logo-light.svg`
- Modify: `internal/dashboard/static/js/main.js:32-36` (applyTheme function)

- [ ] **Step 1: Create numera-logo-light.svg**

```svg
<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 40 40" fill="none">
  <!-- Numera 3x3 grid icon — light mode uses brand green #00a85a with diagonal fade BL→TR -->
  <rect x="1.5" y="1.5" width="9" height="9" rx="2.5" fill="#00a85a" opacity="0.55"/>
  <rect x="14" y="1.5" width="9" height="9" rx="2.5" fill="#00a85a" opacity="0.35"/>
  <rect x="26.5" y="1.5" width="9" height="9" rx="2.5" fill="#00a85a" opacity="0.18"/>
  <rect x="1.5" y="14" width="9" height="9" rx="2.5" fill="#00a85a" opacity="0.70"/>
  <rect x="14" y="14" width="9" height="9" rx="2.5" fill="#00a85a" opacity="0.55"/>
  <rect x="26.5" y="14" width="9" height="9" rx="2.5" fill="#00a85a" opacity="0.30"/>
  <rect x="1.5" y="26.5" width="9" height="9" rx="2.5" fill="#00a85a" opacity="1.0"/>
  <rect x="14" y="26.5" width="9" height="9" rx="2.5" fill="#00a85a" opacity="0.75"/>
  <rect x="26.5" y="26.5" width="9" height="9" rx="2.5" fill="#00a85a" opacity="0.50"/>
</svg>
```

This is the same layout as the dark-mode logo but uses `#00a85a` (the light-mode accent green) instead of `#00ff88`.

- [ ] **Step 2: Add logo swap to applyTheme in main.js**

Replace:
```js
function applyTheme(theme) {
  document.documentElement.classList.toggle('light', theme === 'light');
  document.documentElement.classList.toggle('dark', theme !== 'light');
  localStorage.setItem('codero-theme', theme);
  store.set({ ui: { theme } });
}
```

With:
```js
function applyTheme(theme) {
  document.documentElement.classList.toggle('light', theme === 'light');
  document.documentElement.classList.toggle('dark', theme !== 'light');
  localStorage.setItem('codero-theme', theme);
  store.set({ ui: { theme } });
  // Swap sidebar logo for light/dark mode
  const logoImg = document.querySelector('.sidebar-logo-icon');
  if (logoImg) {
    logoImg.src = theme === 'light' ? './numera-logo-light.svg' : './numera-logo.svg';
  }
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/static/numera-logo-light.svg internal/dashboard/static/js/main.js
git commit -m "feat(dashboard): add light-mode logo, swap on theme toggle

Group 5: the dark logo uses #00ff88 which washes out on light backgrounds.
Light variant uses #00a85a matching :root.light --accent.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Group 6: Operator Actions Panel

### Task 6.1: Add operator action API functions

**Files:**
- Modify: `internal/dashboard/static/js/api.js` (add at end, before any trailing export)

- [ ] **Step 1: Add POST helper functions for session actions**

At the end of `api.js`, add:

```js
// ---- Operator Actions ----

export function sessionAction(assignmentId, action) {
  return apiPost(`assignments/${encodeURIComponent(assignmentId)}/${encodeURIComponent(action)}`, {});
}
```

This single function covers all operator actions (pause, resume, abandon, close, replay, release, release-slot). The Go handler will validate the action name.

- [ ] **Step 2: Verify apiPost is defined**

```bash
grep -n 'export.*function apiPost' internal/dashboard/static/js/api.js
```
Expected: definition exists.

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/static/js/api.js
git commit -m "feat(dashboard): add sessionAction API helper for operator actions

Group 6: generic POST helper that calls /assignments/{id}/{action}. The Go
handler validates the action name via a switch statement.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 6.2: Add Go handler sub-routes for assignment actions

**Files:**
- Modify: `internal/dashboard/dashboard_api_v1_handlers.go:141-175`

- [ ] **Step 1: Convert handleAssignmentDetail to a sub-router**

Replace the entire `handleAssignmentDetail` function with:

```go
func (h *Handler) handleAssignmentDetail(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)

	// Strip prefix and split: "/{id}" or "/{id}/{action}"
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/assignments/")
	parts := strings.SplitN(path, "/", 2)
	assignmentID := parts[0]
	if assignmentID == "" {
		writeError(w, http.StatusBadRequest, "assignment_id required", "missing_id")
		return
	}

	// Sub-action route
	if len(parts) == 2 && parts[1] != "" {
		h.handleAssignmentAction(w, r, assignmentID, parts[1])
		return
	}

	// Default: GET detail
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	assignment, err := queryAssignmentByID(r.Context(), h.db, assignmentID)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "assignment not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "assignment query failed", "db_error")
		return
	}

	checks, err := queryRuleChecksByAssignment(r.Context(), h.db, assignmentID)
	if err != nil {
		checks = []AssignmentRuleCheckRow{}
	}

	writeJSON(w, http.StatusOK, AssignmentDetailResponse{
		AssignmentSummary: *assignment,
		RuleChecks:        checks,
		SchemaVersion:     SchemaVersionV1,
		GeneratedAt:       time.Now().UTC(),
	})
}
```

- [ ] **Step 2: Add handleAssignmentAction stub**

Below `handleAssignmentDetail`, add:

```go
// AssignmentActionResponse is returned for POST /assignments/{id}/{action}.
type AssignmentActionResponse struct {
	AssignmentID  string    `json:"assignment_id"`
	Action        string    `json:"action"`
	Status        string    `json:"status"`
	Message       string    `json:"message,omitempty"`
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
}

var validAssignmentActions = map[string]bool{
	"pause": true, "resume": true, "abandon": true, "close": true,
	"replay": true, "release": true, "release-slot": true,
}

func (h *Handler) handleAssignmentAction(w http.ResponseWriter, r *http.Request, assignmentID, action string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required", "")
		return
	}
	if !validAssignmentActions[action] {
		writeError(w, http.StatusNotFound, "unknown action: "+action, "not_found")
		return
	}

	// Verify assignment exists
	_, err := queryAssignmentByID(r.Context(), h.db, assignmentID)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "assignment not found", "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "assignment query failed", "db_error")
		return
	}

	// TODO: wire to coordinator FSM when ready
	writeJSON(w, http.StatusOK, AssignmentActionResponse{
		AssignmentID:  assignmentID,
		Action:        action,
		Status:        "accepted",
		Message:       action + " action accepted (stub — coordinator not yet wired)",
		SchemaVersion: SchemaVersionV1,
		GeneratedAt:   time.Now().UTC(),
	})
}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd /srv/storage/repo/codero/.worktrees/main
go build -buildvcs=false ./internal/dashboard/...
```
Expected: clean build.

- [ ] **Step 4: Format and commit**

```bash
gofmt -w internal/dashboard/dashboard_api_v1_handlers.go
git add internal/dashboard/dashboard_api_v1_handlers.go
git commit -m "feat(dashboard): add assignment action sub-routes (stub)

Group 6: /assignments/{id}/{action} accepts POST for pause, resume, abandon,
close, replay, release, release-slot. Returns accepted status — coordinator
FSM wiring is a follow-up.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 6.3: Add action buttons to session expand panel

**Files:**
- Modify: `internal/dashboard/static/js/renderers/sessions.js` (`_buildExpandContent` + `_bindExpandToggles`)
- Modify: `internal/dashboard/static/styles.css` (add .session-actions, .btn-sm styles)

- [ ] **Step 1: Add button CSS**

At the end of `styles.css`, add:

```css
/* ========== OPERATOR ACTIONS ========== */
.session-actions { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; padding-top: 8px; border-top: 1px solid var(--border-subtle); }
.btn-sm { padding: 3px 10px; font-size: 0.72rem; border-radius: var(--radius-sm); border: 1px solid var(--border); background: var(--bg-surface-2); color: var(--fg-secondary); cursor: pointer; transition: background var(--duration-fast); }
.btn-sm:hover { background: var(--bg-surface-3); color: var(--fg-primary); }
.btn-sm.destructive { color: var(--destructive); border-color: var(--destructive); }
.btn-sm.destructive:hover { background: var(--destructive-bg); }
```

- [ ] **Step 2: Add action buttons to _buildExpandContent**

In `sessions.js`, find `_buildExpandContent`. After the `out += detailGrid(items);` line inside the `for (const a of assigns)` loop (around line 256), add:

```js
    // Operator action buttons
    const actions = ['pause', 'resume', 'abandon', 'close', 'replay', 'release', 'release-slot'];
    const destructiveActions = new Set(['abandon', 'close']);
    out += '<div class="session-actions">';
    for (const act of actions) {
      const cls = destructiveActions.has(act) ? 'btn-sm destructive' : 'btn-sm';
      out += `<button class="${cls}" data-action="${esc(act)}" data-assignment-id="${esc(a.id)}">${esc(act)}</button>`;
    }
    out += '</div>';
```

- [ ] **Step 3: Add action button delegation to _bindExpandToggles**

In `_bindExpandToggles`, inside the existing event delegation on the table (inside the `table.addEventListener('click', (e) => {` handler), add a new check at the top (before the `tr.expandable` check):

```js
      // Handle operator action button clicks
      const actionBtn = e.target.closest('.btn-sm[data-action]');
      if (actionBtn) {
        e.stopPropagation(); // Don't toggle the expand row
        const action = actionBtn.dataset.action;
        const assignmentId = actionBtn.dataset.assignmentId;
        if (action && assignmentId) {
          actionBtn.disabled = true;
          actionBtn.textContent = action + '…';
          sessionAction(assignmentId, action)
            .then(res => {
              toast(`${action}: ${res.message || 'done'}`, 'success');
              refreshSessions();
            })
            .catch(err => {
              toast(`${action} failed: ${err.message}`, 'error');
            })
            .finally(() => {
              actionBtn.disabled = false;
              actionBtn.textContent = action;
            });
        }
        return;
      }
```

- [ ] **Step 4: Add sessionAction import to sessions.js**

At the top of `sessions.js`, add `sessionAction` to the api.js import:

Find the import line for api.js (should be something like):
```js
import { loadSessions, loadArchives } from '../api.js';
```

Add `sessionAction` to it:
```js
import { loadSessions, loadArchives, sessionAction } from '../api.js';
```

Also ensure `refreshSessions` is available. Since it's defined in the same file, it's already in scope.

- [ ] **Step 5: Add toast import if not present**

Check:
```bash
grep "toast" internal/dashboard/static/js/renderers/sessions.js | head -3
```

If `toast` isn't imported from components.js, add it to the import line.

- [ ] **Step 6: Commit**

```bash
git add internal/dashboard/static/js/renderers/sessions.js internal/dashboard/static/styles.css
git commit -m "feat(dashboard): operator action buttons in session expand panel

Group 6: each assignment row shows pause/resume/abandon/close/replay/release/
release-slot buttons. Event delegation on the table handles clicks, calls the
new sessionAction API helper, and refreshes on success.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Group 7: Scorecard Tab

### Task 7.1: Add scorecard store key

**Files:**
- Modify: `internal/dashboard/static/js/store.js`

- [ ] **Step 1: Add scorecard key to initial state**

After the `trackingConfig: null,` line (added in Task 1.1), add:

```js
    scorecard: null,
```

- [ ] **Step 2: Commit**

```bash
git add internal/dashboard/static/js/store.js
git commit -m "feat(dashboard): add scorecard key to store

Group 7: initial null state for the new Scorecard tab data.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 7.2: Add loadScorecard API function

**Files:**
- Modify: `internal/dashboard/static/js/api.js`

- [ ] **Step 1: Add loadScorecard at end of api.js**

```js
export async function loadScorecard() {
  const data = await apiFetch('scorecard');
  store.set({ scorecard: data });
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/dashboard/static/js/api.js
git commit -m "feat(dashboard): add loadScorecard API function

Group 7: fetches /api/v1/dashboard/scorecard and writes to store.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 7.3: Create scorecard.js renderer

**Files:**
- Create: `internal/dashboard/static/js/renderers/scorecard.js`

- [ ] **Step 1: Create the renderer**

```js
// scorecard.js — Scorecard page renderer.
// Shows aggregated quality, velocity, and compliance metrics.

import store from '../store.js';
import { loadScorecard } from '../api.js';
import { esc, $, setHtml } from '../utils.js';
import { glassCard, metricCard, skeleton, toast } from '../components.js';

let _initialized = false;

export function initScorecard() {
  if (_initialized) return;
  _initialized = true;
  store.subscribe('scorecard', () => renderScorecard());
}

export async function refreshScorecard() {
  try {
    await loadScorecard();
  } catch (err) {
    toast('Failed to load scorecard: ' + err.message, 'error');
  }
}

export function renderScorecard() {
  const container = $('page-scorecard');
  if (!container) return;

  const data = store.select('scorecard');
  if (!data) {
    setHtml(container, skeleton(6));
    return;
  }

  const metrics = [
    metricCard(String(data.gatePassRate ?? '—'), 'Gate Pass Rate', 'var(--accent)'),
    metricCard(String(data.avgCycleTime ?? '—'), 'Avg Cycle Time', 'var(--accent-warm)'),
    metricCard(String(data.mergeRate ?? '—'), 'Merge Rate', 'var(--accent)'),
    metricCard(String(data.complianceScore ?? '—'), 'Compliance', 'var(--accent)'),
  ];

  const metricsHtml = `<div class="metrics-grid">${metrics.join('')}</div>`;

  // Summary card
  const summary = data.summary
    ? `<div class="scorecard-summary">${esc(data.summary)}</div>`
    : '';

  setHtml(container, metricsHtml + glassCard('Scorecard', summary || '<p style="color:var(--fg-muted)">No scorecard data available.</p>'));
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/dashboard/static/js/renderers/scorecard.js
git commit -m "feat(dashboard): add scorecard renderer

Group 7: new page showing gate pass rate, cycle time, merge rate, and
compliance score. Follows the standard init/render/refresh pattern.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 7.4: Register scorecard route and nav item

**Files:**
- Modify: `internal/dashboard/static/js/main.js` (import + register)
- Modify: `internal/dashboard/static/index.html` (nav item + page div)

- [ ] **Step 1: Add import and route registration in main.js**

After the repos import line, add:
```js
import { initScorecard, renderScorecard, refreshScorecard } from './renderers/scorecard.js';
```

After the settings route registration, add:
```js
registerRoute('scorecard', { init: initScorecard, render: renderScorecard, refresh: refreshScorecard });
```

Also update the `titleMap` to include scorecard:
```js
const titleMap = {
  overview: 'Overview', sessions: 'Sessions', agents: 'Agents', pipeline: 'Delivery Pipeline',
  tasks: 'Tasks', repos: 'Node Repositories', gate: 'Gate Checks', chat: 'Chat Assistant',
  archives: 'Archives & Timing', settings: 'Settings', scorecard: 'Scorecard',
};
```

- [ ] **Step 2: Add nav item in index.html**

Before the settings nav item (line 49), add:
```html
      <a class="nav-item" data-tab="scorecard" href="#scorecard">
        <span class="nav-icon">&#9733;</span><span class="nav-label">Scorecard</span>
      </a>
```

- [ ] **Step 3: Add page div in index.html**

After the settings page div (line 76 `<div class="page" id="page-settings"></div>`), add:
```html
      <div class="page" id="page-scorecard"></div>
```

- [ ] **Step 4: Commit**

```bash
git add internal/dashboard/static/js/main.js internal/dashboard/static/index.html
git commit -m "feat(dashboard): register scorecard route and nav item

Group 7: wires the scorecard renderer into the router, adds sidebar nav link
with star icon, and creates the page container div.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 7.5: Add Go scorecard handler

**Files:**
- Modify: `internal/dashboard/dashboard_api_v1_handlers.go` (add handler)
- Modify: `internal/dashboard/handlers.go` (register route)

- [ ] **Step 1: Add ScorecardResponse type and handler**

In `dashboard_api_v1_handlers.go`, add near the other response types:

```go
// ScorecardResponse is the response for GET /api/v1/dashboard/scorecard.
type ScorecardResponse struct {
	GatePassRate    string    `json:"gatePassRate"`
	AvgCycleTime    string    `json:"avgCycleTime"`
	MergeRate       string    `json:"mergeRate"`
	ComplianceScore string    `json:"complianceScore"`
	Summary         string    `json:"summary"`
	SchemaVersion   string    `json:"schema_version"`
	GeneratedAt     time.Time `json:"generated_at"`
}

func (h *Handler) handleScorecard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	setCORSHeaders(w)

	// Stub response — real aggregation is a follow-up
	writeJSON(w, http.StatusOK, ScorecardResponse{
		GatePassRate:    "—",
		AvgCycleTime:    "—",
		MergeRate:       "—",
		ComplianceScore: "—",
		Summary:         "Scorecard data aggregation not yet implemented.",
		SchemaVersion:   SchemaVersionV1,
		GeneratedAt:     time.Now().UTC(),
	})
}
```

- [ ] **Step 2: Register the route in handlers.go**

In `handlers.go`, add after the agents route registration:
```go
	mux.HandleFunc("/api/v1/dashboard/scorecard", h.handleScorecard)
```

- [ ] **Step 3: Verify it compiles**

```bash
cd /srv/storage/repo/codero/.worktrees/main
go build -buildvcs=false ./internal/dashboard/...
```
Expected: clean build.

- [ ] **Step 4: Format and commit**

```bash
gofmt -w internal/dashboard/dashboard_api_v1_handlers.go internal/dashboard/handlers.go
git add internal/dashboard/dashboard_api_v1_handlers.go internal/dashboard/handlers.go
git commit -m "feat(dashboard): add scorecard API endpoint (stub)

Group 7: GET /api/v1/dashboard/scorecard returns placeholder metrics.
Real aggregation query is a follow-up task.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Final Validation

- [ ] **Step 1: Full Go build**

```bash
cd /srv/storage/repo/codero/.worktrees/main
go build -buildvcs=false ./...
```

- [ ] **Step 2: Go tests**

```bash
go test -buildvcs=false ./internal/dashboard/...
```

- [ ] **Step 3: Verify no var(--primary) remnants**

```bash
grep -rn 'var(--primary' internal/dashboard/static/
```
Expected: zero matches.

- [ ] **Step 4: Verify all new store keys are present**

```bash
grep -E 'trackingConfig|scorecard' internal/dashboard/static/js/store.js
```
Expected: both present.

- [ ] **Step 5: Review final diff**

```bash
git --no-pager diff main --stat
```
