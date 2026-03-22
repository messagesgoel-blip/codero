# Dashboard Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refresh Codero's embedded dashboard with build-it-right design language, add a new Agents tab wired to existing live endpoints, and split the monolithic index.html into three focused static assets.

**Architecture:** Single-page dashboard served via Go `embed.FS` under a configurable base path. Three static files (index.html, styles.css, app.js) with hash-based routing, 10s polling, and per-section loading states. No build pipeline; no SSE for first pass.

**Tech Stack:** Vanilla HTML/CSS/JS, Go embed, HSL color tokens, IBM Plex Mono + Source Sans 3

**Spec:** `docs/superpowers/specs/2026-03-22-dashboard-refresh-design.md`

**Working tree:** `/srv/storage/repo/codero/.worktrees/COD-071-v3-closeout`

**Base path for all file references:** `internal/dashboard/`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `static/index.html` | Rewrite | HTML shell: sidebar nav, page containers, semantic structure only |
| `static/styles.css` | Create | Full CSS design system: tokens, components, layouts, dark/light themes |
| `static/app.js` | Create | All JS: hash router, API layer, state, shared renderers, tab renderers, polling |
| `dashboard_test.go` | Modify | Update embed/static assertions for 3-file structure |
| `static_embed.go` | No change | Already embeds `static/*` directory |

---

## Task 1: Shell Split and Serving Parity

**Goal:** Split the 1925-line `static/index.html` into three files. After this task the dashboard serves and behaves identically to before — same tabs, same data, same look.

**Files:**
- Rewrite: `static/index.html` (currently lines 1–1925)
- Create: `static/styles.css`
- Create: `static/app.js`
- Modify: `dashboard_test.go` (lines 722–736: `TestStaticEmbedHasIndexHTML`, lines 1537–1567: `TestDashboardHTML_HasProcessesTab`)

### Steps

- [ ] **Step 1: Extract CSS into styles.css**

Copy lines 10–475 (contents of the `<style>` block, not the tags) from `static/index.html` into a new file `static/styles.css`. The CSS must be identical — no modifications yet.

- [ ] **Step 2: Extract JS into app.js**

Copy lines 1569–1922 (contents of the `<script>` block, not the tags) from `static/index.html` into a new file `static/app.js`. The JS must be identical — no modifications yet.

- [ ] **Step 3: Rewrite index.html as shell**

Replace the entire `static/index.html` with just the HTML structure. Key requirements:
- `<link rel="stylesheet" href="./styles.css">` in `<head>` (relative path)
- `<script src="./app.js"></script>` before `</body>` (relative path)
- Keep the full HTML body from lines 477–1567 unchanged
- Keep the `<head>` metadata (charset, viewport, font imports, title)
- Remove the `<style>...</style>` block entirely
- Remove the `<script>...</script>` block entirely

Verify relative paths: `./styles.css` and `./app.js`, NOT `/dashboard/styles.css`.

- [ ] **Step 4: Update embed test — TestStaticEmbedHasIndexHTML**

In `dashboard_test.go`, update `TestStaticEmbedHasIndexHTML` (line 722) to verify all three files are embedded:

```go
func TestStaticEmbedHasIndexHTML(t *testing.T) {
	subFS, err := fs.Sub(dashboard.Static, "static")
	if err != nil {
		t.Fatalf("sub FS: %v", err)
	}
	// Verify index.html is embedded and references the split assets.
	f, err := subFS.Open("index.html")
	if err != nil {
		t.Fatalf("index.html not embedded: %v", err)
	}
	data, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Contains(data, []byte("codero")) {
		t.Error("index.html does not contain expected content")
	}
	if !bytes.Contains(data, []byte(`href="./styles.css"`)) {
		t.Error("index.html does not reference ./styles.css")
	}
	if !bytes.Contains(data, []byte(`src="./app.js"`)) {
		t.Error("index.html does not reference ./app.js")
	}

	// Verify styles.css is embedded.
	sf, err := subFS.Open("styles.css")
	if err != nil {
		t.Fatalf("styles.css not embedded: %v", err)
	}
	sData, _ := io.ReadAll(sf)
	sf.Close()
	if len(sData) == 0 {
		t.Error("styles.css is empty")
	}

	// Verify app.js is embedded.
	jf, err := subFS.Open("app.js")
	if err != nil {
		t.Fatalf("app.js not embedded: %v", err)
	}
	jData, _ := io.ReadAll(jf)
	jf.Close()
	if len(jData) == 0 {
		t.Error("app.js is empty")
	}
}
```

- [ ] **Step 5: Update TestDashboardHTML_HasProcessesTab**

This test (line 1537) reads `index.html` and checks for specific content. Some checks reference JS code that now lives in `app.js`. Update the test:
- Checks for HTML content (`"Processes"`, `"Event Logs"`, `"Findings"`, `"System Health"`, `"Review Findings"`, `"agents active"`) stay against `index.html`
- Checks for JS content (`"active-sessions"`, `"apiFetch('/health')"`, `"fetch(API + '/chat'"`) must read from `app.js` instead
- Rename to `TestDashboardHTML_HasExpectedContent` to reflect broader scope

```go
func TestDashboardHTML_HasExpectedContent(t *testing.T) {
	subFS, err := fs.Sub(dashboard.Static, "static")
	if err != nil {
		t.Fatalf("sub FS: %v", err)
	}

	// Check HTML shell content.
	htmlF, err := subFS.Open("index.html")
	if err != nil {
		t.Fatalf("open index.html: %v", err)
	}
	htmlData, _ := io.ReadAll(htmlF)
	htmlF.Close()
	html := string(htmlData)

	for _, want := range []string{
		"Processes", "Event Logs", "Findings",
		"System Health", "Review Findings", "agents active",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing %q", want)
		}
	}

	// Check JS content.
	jsF, err := subFS.Open("app.js")
	if err != nil {
		t.Fatalf("open app.js: %v", err)
	}
	jsData, _ := io.ReadAll(jsF)
	jsF.Close()
	js := string(jsData)

	for _, want := range []string{
		"active-sessions", "apiFetch", "/health",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("app.js missing %q", want)
		}
	}
}
```

- [ ] **Step 6: Add HTTP-level serving test**

Add a test that verifies all three files are served with correct content types under the configured base path. This is required by the spec (Section 9).

```go
func TestStaticFilesServedWithContentTypes(t *testing.T) {
	subFS, err := fs.Sub(dashboard.Static, "static")
	if err != nil {
		t.Fatalf("sub FS: %v", err)
	}
	fileServer := http.FileServer(http.FS(subFS))
	mux := http.NewServeMux()
	basePath := "/dashboard"
	mux.Handle(basePath+"/", http.StripPrefix(basePath, fileServer))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tests := []struct {
		path        string
		wantType    string
		wantContain string
	}{
		{"/dashboard/index.html", "text/html", "codero"},
		{"/dashboard/styles.css", "text/css", "--bg-base"},
		{"/dashboard/app.js", "text/javascript", "apiFetch"},
	}
	for _, tt := range tests {
		resp, err := http.Get(srv.URL + tt.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tt.path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("GET %s: status %d", tt.path, resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, tt.wantType) {
			t.Errorf("GET %s: Content-Type %q, want %q", tt.path, ct, tt.wantType)
		}
		body, _ := io.ReadAll(resp.Body)
		if !bytes.Contains(body, []byte(tt.wantContain)) {
			t.Errorf("GET %s: body missing %q", tt.path, tt.wantContain)
		}
	}
}
```

Requires imports: `"net/http"`, `"net/http/httptest"`, `"strings"` (some may already be imported).

- [ ] **Step 7: Run tests**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
go test ./internal/dashboard/... -run "TestStaticEmbed|TestDashboardHTML|TestStaticFilesServed" -v
```

Expected: PASS — all embed assertions green, content-type assertions green, both HTML and JS content found.

- [ ] **Step 8: Verify serving parity**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
go test ./internal/dashboard/... ./internal/daemon/... -v
```

Expected: All existing tests pass with no changes to behavior.

- [ ] **Step 9: Commit**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
git add internal/dashboard/static/index.html internal/dashboard/static/styles.css internal/dashboard/static/app.js internal/dashboard/dashboard_test.go
git commit -m "refactor: split dashboard index.html into html/css/js with relative asset refs"
```

---

## Task 2: Shared Frontend Primitives

**Goal:** Replace the CSS with the new design system and build shared JS primitives (hash router, API wrapper, renderers for status chips, metric cards, expandable rows, timeline, loading/empty/error states). After this task the dashboard shell is restyled but tab content is still rendered by the old tab-specific code.

**Files:**
- Rewrite: `static/styles.css`
- Modify: `static/app.js` (add shared primitives at top, keep existing tab renderers at bottom)

### Steps

- [ ] **Step 0: Update Google Fonts link in index.html**

The new CSS references `Source Sans 3` and `IBM Plex Mono`. Update the `<head>` font imports in `static/index.html` now so fonts load correctly when the new CSS is applied. Change the existing Google Fonts `<link>` to:

```html
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600;700&family=Source+Sans+3:wght@400;500;600;700&display=swap" rel="stylesheet">
```

This prevents transient font breakage between this commit and Task 3.

- [ ] **Step 1: Write the new CSS design system in styles.css**

Replace `static/styles.css` entirely with the new design system. Must include:

**CSS custom properties (`:root` and `[data-theme]`):**
```css
:root {
  --font-sans: 'Source Sans 3', system-ui, sans-serif;
  --font-mono: 'IBM Plex Mono', monospace;
}
[data-theme="dark"] {
  --bg-base: hsl(220 10% 13%);
  --surface-1: hsl(220 12% 8%);
  --surface-2: hsl(220 10% 16%);
  --surface-3: hsl(220 8% 20%);
  --sidebar-bg: hsl(220 12% 7%);
  --text: hsl(210 15% 85%);
  --text-muted: hsl(210 8% 48%);
  --border: hsl(220 8% 18%);
  --border-subtle: hsl(220 8% 18% / 0.5);
  --primary: hsl(120 45% 55%);
  --ring: hsl(120 45% 55%);
  /* Status colors — full, bg (12%), border (25%) */
  --status-active: hsl(120 45% 55%);
  --status-active-bg: hsl(120 45% 55% / 0.12);
  --status-active-border: hsl(120 45% 55% / 0.25);
  --status-blocked: hsl(38 85% 55%);
  --status-blocked-bg: hsl(38 85% 55% / 0.12);
  --status-blocked-border: hsl(38 85% 55% / 0.25);
  --status-completed: hsl(210 55% 55%);
  --status-completed-bg: hsl(210 55% 55% / 0.12);
  --status-completed-border: hsl(210 55% 55% / 0.25);
  --status-waiting: hsl(48 80% 55%);
  --status-waiting-bg: hsl(48 80% 55% / 0.12);
  --status-waiting-border: hsl(48 80% 55% / 0.25);
  --status-cancelled: hsl(0 0% 42%);
  --status-cancelled-bg: hsl(0 0% 42% / 0.12);
  --status-cancelled-border: hsl(0 0% 42% / 0.25);
  --status-lost: hsl(0 65% 52%);
  --status-lost-bg: hsl(0 65% 52% / 0.12);
  --status-lost-border: hsl(0 65% 52% / 0.25);
  /* Rule enforcement */
  --rule-hard: hsl(0 65% 52%);
  --rule-soft: hsl(38 85% 55%);
  --rule-pass: hsl(120 45% 55%);
  --rule-fail: hsl(0 65% 52%);
  --rule-pending: hsl(48 80% 55%);
}
[data-theme="light"] {
  --bg-base: hsl(220 10% 96%);
  --surface-1: hsl(0 0% 100%);
  --surface-2: hsl(220 10% 94%);
  --surface-3: hsl(220 8% 90%);
  --sidebar-bg: hsl(220 12% 95%);
  --text: hsl(220 10% 15%);
  --text-muted: hsl(210 8% 45%);
  --border: hsl(220 8% 82%);
  --border-subtle: hsl(220 8% 82% / 0.5);
  --primary: hsl(120 45% 40%);
  --ring: hsl(120 45% 40%);
  /* Status colors keep same hue, adjusted lightness for light bg */
  --status-active: hsl(120 45% 38%);
  --status-active-bg: hsl(120 45% 55% / 0.10);
  --status-active-border: hsl(120 45% 55% / 0.20);
  --status-blocked: hsl(38 85% 42%);
  --status-blocked-bg: hsl(38 85% 55% / 0.10);
  --status-blocked-border: hsl(38 85% 55% / 0.20);
  --status-completed: hsl(210 55% 42%);
  --status-completed-bg: hsl(210 55% 55% / 0.10);
  --status-completed-border: hsl(210 55% 55% / 0.20);
  --status-waiting: hsl(48 80% 38%);
  --status-waiting-bg: hsl(48 80% 55% / 0.10);
  --status-waiting-border: hsl(48 80% 55% / 0.20);
  --status-cancelled: hsl(0 0% 55%);
  --status-cancelled-bg: hsl(0 0% 42% / 0.10);
  --status-cancelled-border: hsl(0 0% 42% / 0.20);
  --status-lost: hsl(0 65% 45%);
  --status-lost-bg: hsl(0 65% 52% / 0.10);
  --status-lost-border: hsl(0 65% 52% / 0.20);
  --rule-hard: hsl(0 65% 45%);
  --rule-soft: hsl(38 85% 42%);
  --rule-pass: hsl(120 45% 38%);
  --rule-fail: hsl(0 65% 45%);
  --rule-pending: hsl(48 80% 38%);
}
```

**Base styles:**
```css
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: var(--font-sans);
  background: var(--bg-base);
  color: var(--text);
  letter-spacing: -0.005em;
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
}
```

**Layout — sidebar + main:**
```css
.shell { display: flex; height: 100vh; overflow: hidden; }
.sidebar {
  width: 14rem; flex-shrink: 0;
  background: var(--sidebar-bg);
  border-right: 1px solid var(--border);
  display: flex; flex-direction: column;
  padding: 1rem 0.75rem;
}
.sidebar-logo { padding: 0.5rem 0.75rem; margin-bottom: 1.5rem; font-family: var(--font-mono); font-weight: 700; font-size: 0.875rem; color: var(--primary); }
.sidebar-nav { display: flex; flex-direction: column; gap: 0.125rem; }
.nav-item {
  display: flex; align-items: center; gap: 0.5rem;
  padding: 0.5rem 0.75rem; border-radius: 0.375rem;
  font-size: 0.8125rem; color: var(--text-muted);
  cursor: pointer; border: none; background: none;
  width: 100%; text-align: left;
  transition: background 150ms, color 150ms;
}
.nav-item:hover { background: var(--surface-2); color: var(--text); }
.nav-item.active { background: var(--surface-3); color: var(--text); font-weight: 600; }
.nav-badge {
  margin-left: auto; font-size: 10px; font-family: var(--font-mono);
  background: var(--surface-3); padding: 0.125rem 0.375rem; border-radius: 0.25rem;
}
.main-content { flex: 1; overflow-y: auto; padding: 1.5rem; }
.page { display: none; }
.page.active { display: block; }
.page-header { margin-bottom: 1.5rem; }
.page-header h2 { font-size: 1.125rem; font-weight: 600; letter-spacing: -0.01em; }
.page-header p { font-size: 0.75rem; color: var(--text-muted); margin-top: 0.25rem; }
```

**Component styles — status chips:**
```css
.status-chip {
  display: inline-flex; align-items: center; gap: 0.375rem;
  padding: 0.125rem 0.625rem; border-radius: 9999px;
  font-size: 0.6875rem; font-family: var(--font-mono);
  font-weight: 500; letter-spacing: 0.02em;
}
.status-chip.active { background: var(--status-active-bg); color: var(--status-active); border: 1px solid var(--status-active-border); }
.status-chip.blocked { background: var(--status-blocked-bg); color: var(--status-blocked); border: 1px solid var(--status-blocked-border); }
.status-chip.completed { background: var(--status-completed-bg); color: var(--status-completed); border: 1px solid var(--status-completed-border); }
.status-chip.waiting { background: var(--status-waiting-bg); color: var(--status-waiting); border: 1px solid var(--status-waiting-border); }
.status-chip.cancelled { background: var(--status-cancelled-bg); color: var(--status-cancelled); border: 1px solid var(--status-cancelled-border); }
.status-chip.lost { background: var(--status-lost-bg); color: var(--status-lost); border: 1px solid var(--status-lost-border); }
.pulse-dot { width: 0.5rem; height: 0.5rem; border-radius: 9999px; }
.pulse-dot.animate { animation: pulse 2s ease-in-out infinite; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
```

**Component styles — metric cards:**
```css
.metric-strip { display: grid; grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr)); gap: 0.75rem; margin-bottom: 1.5rem; }
.metric-card {
  background: var(--surface-1); border: 1px solid var(--border);
  border-radius: 0.375rem; padding: 1rem;
  animation: reveal 0.6s cubic-bezier(0.16, 1, 0.3, 1) both;
}
.metric-card:nth-child(2) { animation-delay: 60ms; }
.metric-card:nth-child(3) { animation-delay: 120ms; }
.metric-card:nth-child(4) { animation-delay: 180ms; }
.metric-value { font-size: 1.5rem; font-weight: 700; font-family: var(--font-mono); letter-spacing: -0.02em; }
.metric-label { font-size: 0.6875rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-muted); margin-top: 0.25rem; }
@keyframes reveal { from { opacity: 0; transform: translateY(12px); filter: blur(4px); } to { opacity: 1; transform: translateY(0); filter: blur(0); } }
```

**Component styles — data tables:**
```css
.data-table-wrap { background: var(--surface-1); border: 1px solid var(--border); border-radius: 0.375rem; overflow: hidden; }
.data-table { width: 100%; border-collapse: collapse; font-size: 0.8125rem; }
.data-table th {
  text-align: left; padding: 0.625rem 1rem;
  font-size: 0.6875rem; font-weight: 500; text-transform: uppercase;
  letter-spacing: 0.05em; color: var(--text-muted);
  border-bottom: 1px solid var(--border);
}
.data-table td { padding: 0.75rem 1rem; font-family: var(--font-mono); font-size: 0.75rem; border-bottom: 1px solid var(--border-subtle); }
.data-table tr:last-child td { border-bottom: none; }
.data-table tr:hover td { background: var(--surface-2); }
.data-table tr.expandable { cursor: pointer; }
.expand-row td { padding: 0; border-bottom: none; }
.expand-content { background: var(--surface-2); padding: 0.75rem 2rem; border-top: 1px solid var(--border-subtle); border-bottom: 1px solid var(--border-subtle); }
.detail-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 0.5rem; }
@media (min-width: 768px) { .detail-grid { grid-template-columns: repeat(4, 1fr); } }
.detail-item-label { font-size: 0.625rem; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-muted); }
.detail-item-value { font-size: 0.75rem; font-family: var(--font-mono); }
```

**Component styles — timeline:**
```css
.timeline { display: flex; flex-direction: column; }
.timeline-entry { display: flex; gap: 1rem; }
.timeline-track { display: flex; flex-direction: column; align-items: center; width: 1rem; flex-shrink: 0; }
.timeline-dot { width: 0.625rem; height: 0.625rem; border-radius: 9999px; margin-top: 0.375rem; flex-shrink: 0; }
.timeline-line { width: 1px; flex: 1; background: var(--border-subtle); }
.timeline-body { flex: 1; padding-bottom: 1rem; }
.timeline-time { font-size: 10px; font-family: var(--font-mono); color: var(--text-muted); }
.timeline-text { font-size: 0.75rem; margin-top: 0.125rem; }
.timeline-agent { font-size: 0.6875rem; color: var(--text-muted); font-family: var(--font-mono); }
```

**Component styles — enforcement badges:**
```css
.enforcement-badge {
  display: inline-block; padding: 0.125rem 0.375rem; border-radius: 0.25rem;
  font-size: 10px; font-family: var(--font-mono); font-weight: 500;
}
.enforcement-badge.hard { background: hsl(0 65% 52% / 0.15); color: var(--rule-hard); border: 1px solid hsl(0 65% 52% / 0.25); }
.enforcement-badge.soft { background: hsl(38 85% 55% / 0.15); color: var(--rule-soft); border: 1px solid hsl(38 85% 55% / 0.25); }
```

**Component styles — rule cards:**
```css
.rule-card {
  background: var(--surface-1); border: 1px solid var(--border);
  border-radius: 0.375rem; padding: 1rem;
}
.rule-card-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 0.5rem; margin-bottom: 0.75rem; }
.rule-card-name { font-size: 0.8125rem; font-weight: 600; }
.rule-card-desc { font-size: 0.75rem; color: var(--text-muted); margin-bottom: 0.5rem; }
.rule-card-stats { display: flex; gap: 1rem; font-size: 0.75rem; font-family: var(--font-mono); }
.rule-card-version { font-size: 10px; font-family: var(--font-mono); color: var(--text-muted); }
.rules-grid { display: grid; grid-template-columns: 1fr; gap: 0.75rem; }
@media (min-width: 768px) { .rules-grid { grid-template-columns: repeat(2, 1fr); } }
```

**Loading/empty/error states:**
```css
.skeleton { background: var(--surface-2); border-radius: 0.375rem; animation: skeleton-pulse 1.5s ease-in-out infinite; }
.skeleton-line { height: 0.75rem; margin-bottom: 0.5rem; }
.skeleton-card { height: 5rem; }
@keyframes skeleton-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.5; } }
.empty-state { text-align: center; padding: 3rem 1rem; color: var(--text-muted); }
.empty-state-icon { font-size: 2rem; margin-bottom: 0.75rem; opacity: 0.4; }
.empty-state-text { font-size: 0.8125rem; }
.error-banner {
  background: hsl(0 65% 52% / 0.1); border: 1px solid hsl(0 65% 52% / 0.2);
  border-radius: 0.375rem; padding: 0.75rem 1rem;
  display: flex; align-items: center; justify-content: space-between;
  font-size: 0.8125rem; color: var(--status-lost);
}
.error-banner button {
  background: var(--surface-3); border: 1px solid var(--border);
  border-radius: 0.25rem; padding: 0.25rem 0.75rem;
  font-size: 0.75rem; color: var(--text); cursor: pointer;
}
```

**Section container + panel header pattern:**
```css
.section { margin-bottom: 1.5rem; }
.section-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 0.75rem; }
.section-title { font-size: 0.8125rem; font-weight: 600; }
.section-subtitle { font-size: 0.6875rem; color: var(--text-muted); }
.split-panel { display: grid; grid-template-columns: 1fr; gap: 1.5rem; }
@media (min-width: 768px) { .split-panel { grid-template-columns: 1fr 1fr; } }
```

**Bottom bar (health) and theme toggle:**
```css
.health-bar {
  position: sticky; bottom: 0; left: 0; right: 0;
  background: var(--surface-1); border-top: 1px solid var(--border);
  padding: 0.375rem 1rem; display: flex; align-items: center; gap: 1rem;
  font-size: 0.6875rem; font-family: var(--font-mono); color: var(--text-muted);
}
.health-dot { width: 0.4375rem; height: 0.4375rem; border-radius: 9999px; }
.health-dot.ok { background: var(--status-active); }
.health-dot.stale { background: var(--status-waiting); }
.health-dot.down { background: var(--status-lost); }
.theme-toggle { display: flex; gap: 0.125rem; margin-left: auto; }
.theme-toggle button {
  background: none; border: 1px solid var(--border);
  border-radius: 0.25rem; padding: 0.125rem 0.5rem;
  font-size: 10px; font-family: var(--font-mono); color: var(--text-muted); cursor: pointer;
}
.theme-toggle button.active { background: var(--surface-3); color: var(--text); }
```

**Responsive sidebar collapse:**
```css
@media (max-width: 767px) {
  .sidebar { width: 3.5rem; padding: 0.75rem 0.5rem; }
  .nav-item span, .nav-badge, .sidebar-logo span { display: none; }
  .nav-item { justify-content: center; padding: 0.5rem; }
  .main-content { padding: 1rem; }
}
```

Also include scrollbar styling and focus/active ring styles for keyboard nav.

- [ ] **Step 2: Build shared JS primitives in app.js**

Replace the top section of `app.js` (before any existing tab-specific code) with the shared framework. Keep all existing tab rendering code for now — it will be replaced in later tasks.

**Constants and state:**
```javascript
'use strict';
var API = '/api/v1/dashboard';
var POLL = 10000;
var activeTab = 'overview';
var theme = 'dark';
var expandedRows = {};
var sectionState = {};  // { [sectionId]: { loading: bool, error: string|null, data: any } }
```

**Hash router:**
```javascript
var TABS = ['overview', 'agents', 'events', 'findings', 'architecture', 'settings'];

function initRouter() {
  window.addEventListener('hashchange', onHashChange);
  onHashChange();
}

function onHashChange() {
  var hash = window.location.hash.replace('#', '') || 'overview';
  if (TABS.indexOf(hash) === -1) hash = 'overview';
  switchTab(hash);
}

function switchTab(tab) {
  activeTab = tab;
  TABS.forEach(function(t) {
    var page = document.getElementById('page-' + t);
    var nav = document.getElementById('nav-' + t);
    if (page) page.classList.toggle('active', t === tab);
    if (nav) nav.classList.toggle('active', t === tab);
  });
  refreshActiveTab();
}

function navigateTo(tab) {
  window.location.hash = '#' + tab;
}
```

**API wrapper:**
```javascript
async function apiFetch(path, opts) {
  var res = await fetch(API + path, opts || {});
  if (!res.ok) {
    var msg = res.statusText;
    try { var j = await res.json(); msg = j.error || msg; } catch (_e) {}
    var err = new Error(msg);
    err.status = res.status;
    throw err;
  }
  return res.json();
}

async function fetchSection(sectionId, path) {
  sectionState[sectionId] = { loading: true, error: null, data: sectionState[sectionId] ? sectionState[sectionId].data : null };
  renderSectionState(sectionId);
  try {
    var data = await apiFetch(path);
    sectionState[sectionId] = { loading: false, error: null, data: data };
  } catch (e) {
    sectionState[sectionId] = { loading: false, error: e.message, data: sectionState[sectionId] ? sectionState[sectionId].data : null };
  }
  renderSectionState(sectionId);
  return sectionState[sectionId].data;
}
```

**DOM helpers:**
```javascript
function el(tag, attrs, children) {
  var e = document.createElement(tag);
  if (attrs) Object.keys(attrs).forEach(function(k) {
    if (k === 'className') e.className = attrs[k];
    else if (k === 'onclick') e.onclick = attrs[k];
    else if (k === 'innerHTML') e.innerHTML = attrs[k];
    else e.setAttribute(k, attrs[k]);
  });
  if (children) {
    if (typeof children === 'string') e.textContent = children;
    else if (Array.isArray(children)) children.forEach(function(c) { if (c) e.appendChild(c); });
    else e.appendChild(children);
  }
  return e;
}

function esc(s) { var d = document.createElement('div'); d.textContent = s || ''; return d.innerHTML; }

function clearEl(id) { var e = document.getElementById(id); if (e) e.innerHTML = ''; return e; }
```

**Shared renderers:**
```javascript
function statusChip(state) {
  var cls = mapStateToClass(state);
  var dot = state === 'active' ? '<span class="pulse-dot animate" style="background:var(--status-active)"></span>' : '';
  return '<span class="status-chip ' + cls + '">' + dot + esc(state) + '</span>';
}

function mapStateToClass(state) {
  var map = { active: 'active', waiting: 'waiting', blocked: 'blocked', completed: 'completed', cancelled: 'cancelled', lost: 'lost', ended: 'completed', pass: 'active', fail: 'lost' };
  return map[state] || 'cancelled';
}

function metricCard(value, label, colorVar) {
  var style = colorVar ? 'color:var(' + colorVar + ')' : '';
  return '<div class="metric-card"><div class="metric-value" style="' + style + '">' + esc(String(value)) + '</div><div class="metric-label">' + esc(label) + '</div></div>';
}

function skeletonCards(n) {
  var html = '<div class="metric-strip">';
  for (var i = 0; i < n; i++) html += '<div class="skeleton skeleton-card"></div>';
  return html + '</div>';
}

function skeletonTable(rows) {
  var html = '<div class="data-table-wrap"><div style="padding:1rem">';
  for (var i = 0; i < rows; i++) html += '<div class="skeleton skeleton-line" style="width:' + (60 + Math.random() * 30) + '%"></div>';
  return html + '</div></div>';
}

function emptyState(icon, text) {
  return '<div class="empty-state"><div class="empty-state-icon">' + icon + '</div><div class="empty-state-text">' + esc(text) + '</div></div>';
}

function errorBanner(msg, retryFn) {
  var id = 'err-' + Math.random().toString(36).slice(2, 8);
  setTimeout(function() {
    var btn = document.getElementById(id);
    if (btn) btn.onclick = retryFn;
  }, 0);
  return '<div class="error-banner"><span>' + esc(msg) + '</span><button id="' + id + '">Retry</button></div>';
}

function relativeTime(ts) {
  if (!ts) return '\u2014';
  var diff = (Date.now() - new Date(ts).getTime()) / 1000;
  if (diff < 60) return Math.floor(diff) + 's ago';
  if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
  if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
  return Math.floor(diff / 86400) + 'd ago';
}

function formatDuration(sec) {
  if (!sec && sec !== 0) return '\u2014';
  if (sec < 60) return sec + 's';
  if (sec < 3600) return Math.floor(sec / 60) + 'm ' + (sec % 60) + 's';
  return Math.floor(sec / 3600) + 'h ' + Math.floor((sec % 3600) / 60) + 'm';
}

function truncId(id) {
  if (!id) return '\u2014';
  return id.length > 12 ? id.slice(0, 12) + '\u2026' : id;
}
```

**Expandable row helper:**
```javascript
function toggleExpand(tableId, rowId) {
  var key = tableId + ':' + rowId;
  expandedRows[key] = !expandedRows[key];
  var expRow = document.getElementById('expand-' + key.replace(/[^a-z0-9]/gi, '-'));
  if (expRow) expRow.style.display = expandedRows[key] ? '' : 'none';
}

function isExpanded(tableId, rowId) {
  return !!expandedRows[tableId + ':' + rowId];
}

function expandChevron(tableId, rowId) {
  return isExpanded(tableId, rowId) ? '\u25BE' : '\u25B8';
}

function detailGrid(items) {
  // items: [{label, value}]
  var html = '<div class="detail-grid">';
  items.forEach(function(item) {
    html += '<div><div class="detail-item-label">' + esc(item.label) + '</div><div class="detail-item-value">' + esc(item.value || '\u2014') + '</div></div>';
  });
  return html + '</div>';
}
```

**Section state renderer (dispatches to tab renderers):**
```javascript
function renderSectionState(sectionId) {
  // Individual tab renderers call this pattern internally.
  // This is the glue: loading → skeleton, error → banner, data → render.
}
```

**Theme management:**
```javascript
function setTheme(t) {
  theme = t;
  if (t === 'system') {
    var prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    document.documentElement.setAttribute('data-theme', prefersDark ? 'dark' : 'light');
  } else {
    document.documentElement.setAttribute('data-theme', t);
  }
  document.querySelectorAll('.theme-toggle button').forEach(function(b) {
    b.classList.toggle('active', b.dataset.theme === t);
  });
  try { localStorage.setItem('codero-theme', t); } catch (_) {}
}

function initTheme() {
  var saved = null;
  try { saved = localStorage.getItem('codero-theme'); } catch (_) {}
  setTheme(saved || 'dark');
}
```

**Polling:**
```javascript
var pollTimer = null;

function startPolling() {
  if (pollTimer) clearTimeout(pollTimer);
  pollTimer = setTimeout(function() { refreshActiveTab().then(startPolling); }, POLL);
}

async function refreshActiveTab() {
  // Dispatches to the active tab's refresh function.
  var fn = tabRefreshers[activeTab];
  if (fn) await fn();
}

var tabRefreshers = {};
// Tab refreshers are registered by each tab renderer module.
```

**Init:**
```javascript
document.addEventListener('DOMContentLoaded', function() {
  initTheme();
  initRouter();
  startPolling();
});
```

- [ ] **Step 3: Run tests to verify no regressions**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
go test ./internal/dashboard/... -v
```

Expected: PASS. The CSS/JS content changed but embed tests check for `codero`, `./styles.css`, `./app.js`, `apiFetch`, `/health`, `active-sessions` — all still present.

- [ ] **Step 4: Commit**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
git add internal/dashboard/static/index.html internal/dashboard/static/styles.css internal/dashboard/static/app.js
git commit -m "feat: add CSS design system and shared JS primitives for dashboard refresh"
```

Note: `index.html` is staged because Step 0 updated the Google Fonts link.

---

## Task 3: Sidebar + Tab Skeleton

**Goal:** Replace the top-tab navigation with the left sidebar layout. Wire all 6 tabs (Overview, Agents, Events, Findings, Architecture, Settings). Existing tab content renders in the correct container.

**Files:**
- Modify: `static/index.html`
- Modify: `static/app.js` (wire sidebar nav items)

### Steps

- [ ] **Step 1: Rewrite index.html with sidebar layout**

Replace the full body content of `static/index.html`. The new structure:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Codero Dashboard</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600;700&family=Source+Sans+3:wght@400;500;600;700&display=swap" rel="stylesheet">
  <link rel="stylesheet" href="./styles.css">
</head>
<body>
  <div class="shell">
    <!-- Sidebar -->
    <aside class="sidebar">
      <div class="sidebar-logo"><span>codero</span></div>
      <nav class="sidebar-nav">
        <button class="nav-item active" id="nav-overview" onclick="navigateTo('overview')">
          <span>Overview</span>
        </button>
        <button class="nav-item" id="nav-agents" onclick="navigateTo('agents')">
          <span>Agents</span>
          <span class="nav-badge" id="badge-agents">—</span>
        </button>
        <button class="nav-item" id="nav-events" onclick="navigateTo('events')">
          <span>Events</span>
        </button>
        <button class="nav-item" id="nav-findings" onclick="navigateTo('findings')">
          <span>Findings</span>
        </button>
        <button class="nav-item" id="nav-architecture" onclick="navigateTo('architecture')">
          <span>Architecture</span>
        </button>
        <button class="nav-item" id="nav-settings" onclick="navigateTo('settings')">
          <span>Settings</span>
        </button>
      </nav>
    </aside>

    <!-- Main Content -->
    <div class="main-area">
      <div class="main-content">
        <div class="page active" id="page-overview"></div>
        <div class="page" id="page-agents"></div>
        <div class="page" id="page-events"></div>
        <div class="page" id="page-findings"></div>
        <div class="page" id="page-architecture"></div>
        <div class="page" id="page-settings"></div>
      </div>

      <!-- Health Bar -->
      <div class="health-bar" id="health-bar">
        <span class="health-dot" id="hb-db-dot"></span>
        <span id="hb-db-label">db</span>
        <span class="health-dot" id="hb-sessions-dot"></span>
        <span id="hb-sessions-label">sessions</span>
        <span class="health-dot" id="hb-gates-dot"></span>
        <span id="hb-gates-label">gates</span>
        <span id="hb-agents-label"></span>
        <span id="hb-refreshed"></span>
        <div class="theme-toggle">
          <button data-theme="system" onclick="setTheme('system')">sys</button>
          <button data-theme="dark" onclick="setTheme('dark')">dark</button>
          <button data-theme="light" onclick="setTheme('light')">light</button>
        </div>
      </div>
    </div>
  </div>
  <script src="./app.js"></script>
</body>
</html>
```

Note: Page containers are empty — JS renders content dynamically. This is a shift from the old approach where HTML was static and JS filled in data.

- [ ] **Step 2: Update styles.css for main-area wrapper**

Add to `styles.css`:
```css
.main-area { display: flex; flex-direction: column; flex: 1; overflow: hidden; }
```

- [ ] **Step 3: Update test assertions**

In `TestDashboardHTML_HasExpectedContent`, update the HTML content checks to match the new sidebar structure:

```go
for _, want := range []string{
    "codero", "Overview", "Agents", "Events",
    "Findings", "Architecture", "Settings",
    "health-bar", "./styles.css", "./app.js",
} {
    if !strings.Contains(html, want) {
        t.Errorf("index.html missing %q", want)
    }
}
```

Remove checks for old tab names ("Processes", "Event Logs") and old-specific content ("System Health", "Review Findings", "agents active") that no longer appear in the HTML shell.

- [ ] **Step 4: Run tests**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
go test ./internal/dashboard/... -run "TestStaticEmbed|TestDashboardHTML" -v
```

Expected: PASS.

- [ ] **Step 5: Run full dashboard + daemon tests**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
go test ./internal/dashboard/... ./internal/daemon/... -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
git add internal/dashboard/static/index.html internal/dashboard/static/styles.css internal/dashboard/dashboard_test.go
git commit -m "feat: replace top-tab nav with sidebar layout and 6-tab structure"
```

---

## Task 4: Agents Tab

**Goal:** Implement the full Agents tab with metric strip, sessions panel, assignments panel, compliance rules, and agent event timeline. All data from existing Codero endpoints.

**Files:**
- Modify: `static/app.js` (add `renderAgents` and section renderers)

### Steps

- [ ] **Step 1: Register Agents tab refresher and section fetchers**

Add to `app.js`:
```javascript
tabRefreshers.agents = async function() {
  await Promise.allSettled([
    fetchSection('agents-sessions', '/active-sessions'),
    fetchSection('agents-assignments', '/assignments'),
    fetchSection('agents-compliance', '/compliance'),
    fetchSection('agents-events', '/agent-events'),
    fetchSection('agents-health', '/health'),
  ]);
  renderAgentsTab();
};
```

- [ ] **Step 2: Implement renderAgentsTab()**

```javascript
function renderAgentsTab() {
  var container = clearEl('page-agents');
  if (!container) return;

  // Page header
  container.appendChild(el('div', { className: 'page-header' }, [
    el('h2', {}, 'Agents'),
    el('p', {}, 'Active sessions, assignments, compliance, and events'),
  ]));

  // Metric strip
  var metricsDiv = el('div', { id: 'agents-metrics' });
  container.appendChild(metricsDiv);
  renderAgentsMetrics(metricsDiv);

  // Sessions section
  var sessDiv = el('div', { className: 'section', id: 'agents-sessions-section' });
  container.appendChild(sessDiv);
  renderAgentsSessions(sessDiv);

  // Assignments section
  var assignDiv = el('div', { className: 'section', id: 'agents-assignments-section' });
  container.appendChild(assignDiv);
  renderAgentsAssignments(assignDiv);

  // Bottom split: compliance + events
  var splitDiv = el('div', { className: 'split-panel' });
  var compDiv = el('div', { className: 'section', id: 'agents-compliance-section' });
  var evtDiv = el('div', { className: 'section', id: 'agents-events-section' });
  splitDiv.appendChild(compDiv);
  splitDiv.appendChild(evtDiv);
  container.appendChild(splitDiv);
  renderAgentsCompliance(compDiv);
  renderAgentsTimeline(evtDiv);

  // Update sidebar badge
  var sessData = sectionState['agents-sessions'];
  var count = sessData && sessData.data ? sessData.data.active_count : 0;
  var badge = document.getElementById('badge-agents');
  if (badge) badge.textContent = count || '\u2014';
}
```

- [ ] **Step 3: Implement metric strip renderer**

```javascript
function renderAgentsMetrics(container) {
  var sess = sectionState['agents-sessions'];
  var assign = sectionState['agents-assignments'];
  var comp = sectionState['agents-compliance'];
  var evt = sectionState['agents-events'];

  if ((sess && sess.loading) || (assign && assign.loading)) {
    container.innerHTML = skeletonCards(4);
    return;
  }

  var sessCount = sess && sess.data ? sess.data.active_count : 0;
  var assignCount = assign && assign.data ? assign.data.count : 0;

  // Compliance score: pass / total checks, "—" if zero
  var compScore = '\u2014';
  if (comp && comp.data && comp.data.checks && comp.data.checks.length > 0) {
    var total = comp.data.checks.length;
    var passing = comp.data.checks.filter(function(c) { return c.result === 'pass'; }).length;
    compScore = Math.round((passing / total) * 100) + '%';
  }

  var evtCount = evt && evt.data ? evt.data.count : 0;

  container.innerHTML = '<div class="metric-strip">' +
    metricCard(sessCount, 'Active Sessions', '--status-active') +
    metricCard(assignCount, 'Assignments', '--primary') +
    metricCard(compScore, 'Compliance Score', compScore === '\u2014' ? null : '--status-active') +
    metricCard(evtCount, 'Recent Events', '--status-completed') +
    '</div>';
}
```

- [ ] **Step 4: Implement sessions panel renderer**

```javascript
function renderAgentsSessions(container) {
  var state = sectionState['agents-sessions'];

  container.innerHTML = '<div class="section-header"><div class="section-title">Active Sessions</div></div>';

  if (!state || state.loading) { container.innerHTML += skeletonTable(5); return; }
  if (state.error) { container.innerHTML += errorBanner(state.error, function() { tabRefreshers.agents(); }); return; }
  if (!state.data || !state.data.sessions || state.data.sessions.length === 0) {
    container.innerHTML += emptyState('\u{1F916}', 'No active sessions');
    return;
  }

  var rows = state.data.sessions;
  var html = '<div class="data-table-wrap"><table class="data-table"><thead><tr>' +
    '<th></th><th>Agent</th><th>Repo / Branch</th><th>Task</th><th>Phase</th><th>Heartbeat</th><th>Elapsed</th>' +
    '</tr></thead><tbody>';

  rows.forEach(function(s) {
    var rowKey = 'sess:' + s.session_id;
    var expId = 'expand-' + rowKey.replace(/[^a-z0-9]/gi, '-');
    html += '<tr class="expandable" onclick="toggleExpand(\'sess\',\'' + esc(s.session_id) + '\');renderAgentsTab()">' +
      '<td>' + expandChevron('sess', s.session_id) + '</td>' +
      '<td>' + statusChip(s.activity_state) + ' ' + esc(s.agent_id || s.owner_agent || '\u2014') +
        (s.mode ? ' <span class="enforcement-badge soft">' + esc(s.mode) + '</span>' : '') + '</td>' +
      '<td>' + esc(s.repo || '\u2014') + ' / ' + esc(s.branch || '\u2014') + '</td>' +
      '<td>' + (s.task ? esc(s.task.id) + ' ' + esc(s.task.title) : '\u2014') + '</td>' +
      '<td>' + (s.task ? statusChip(s.task.phase || 'unknown') : '\u2014') + '</td>' +
      '<td>' + relativeTime(s.last_heartbeat_at) + '</td>' +
      '<td>' + formatDuration(s.elapsed_sec) + '</td>' +
      '</tr>';
    html += '<tr class="expand-row" id="' + expId + '" style="display:' + (isExpanded('sess', s.session_id) ? '' : 'none') + '">' +
      '<td colspan="7"><div class="expand-content">' + detailGrid([
        { label: 'Session ID', value: s.session_id },
        { label: 'Worktree', value: s.worktree },
        { label: 'PR', value: s.pr_number ? '#' + s.pr_number : null },
        { label: 'Started', value: s.started_at ? new Date(s.started_at).toLocaleString() : null },
        { label: 'Progress', value: s.progress_at ? relativeTime(s.progress_at) : null },
      ]) + '</div></td></tr>';
  });

  html += '</tbody></table></div>';
  container.innerHTML = container.innerHTML + html;
}
```

- [ ] **Step 5: Implement assignments panel renderer**

```javascript
function renderAgentsAssignments(container) {
  var state = sectionState['agents-assignments'];

  container.innerHTML = '<div class="section-header"><div class="section-title">Assignments</div></div>';

  if (!state || state.loading) { container.innerHTML += skeletonTable(5); return; }
  if (state.error) { container.innerHTML += errorBanner(state.error, function() { tabRefreshers.agents(); }); return; }
  if (!state.data || !state.data.assignments || state.data.assignments.length === 0) {
    container.innerHTML += emptyState('\u{1F4CB}', 'No assignments');
    return;
  }

  var rows = state.data.assignments;
  var html = '<div class="data-table-wrap"><table class="data-table"><thead><tr>' +
    '<th></th><th>State</th><th>Assignment</th><th>Agent</th><th>Repo / Branch</th><th>Substatus</th><th>Blocked</th><th>Started</th><th>Ended</th>' +
    '</tr></thead><tbody>';

  rows.forEach(function(a) {
    var rowKey = 'assign:' + a.assignment_id;
    var expId = 'expand-' + rowKey.replace(/[^a-z0-9]/gi, '-');
    html += '<tr class="expandable" onclick="toggleExpand(\'assign\',\'' + esc(a.assignment_id) + '\');renderAgentsTab()">' +
      '<td>' + expandChevron('assign', a.assignment_id) + '</td>' +
      '<td>' + statusChip(a.state) + '</td>' +
      '<td style="font-family:var(--font-mono)">' + truncId(a.assignment_id) + '</td>' +
      '<td>' + esc(a.agent_id || '\u2014') + '</td>' +
      '<td>' + esc(a.repo || '\u2014') + ' / ' + esc(a.branch || '\u2014') + '</td>' +
      '<td>' + (a.substatus ? '<span class="status-chip waiting">' + esc(a.substatus) + '</span>' : '\u2014') + '</td>' +
      '<td style="color:var(--status-lost)">' + esc(a.blocked_reason || '') + '</td>' +
      '<td>' + relativeTime(a.started_at) + '</td>' +
      '<td>' + relativeTime(a.ended_at) + '</td>' +
      '</tr>';
    html += '<tr class="expand-row" id="' + expId + '" style="display:' + (isExpanded('assign', a.assignment_id) ? '' : 'none') + '">' +
      '<td colspan="9"><div class="expand-content">' + detailGrid([
        { label: 'Session ID', value: a.session_id },
        { label: 'Task ID', value: a.task_id },
        { label: 'Worktree', value: a.worktree },
        { label: 'End Reason', value: a.end_reason },
        { label: 'Superseded By', value: a.superseded_by },
        { label: 'Branch State', value: a.branch_state },
        { label: 'PR', value: a.pr_number ? '#' + a.pr_number : null },
        { label: 'Mode', value: a.mode },
      ]) + '</div></td></tr>';
  });

  html += '</tbody></table></div>';
  container.innerHTML = container.innerHTML + html;
}
```

- [ ] **Step 6: Implement compliance rules renderer**

```javascript
function renderAgentsCompliance(container) {
  var state = sectionState['agents-compliance'];

  container.innerHTML = '<div class="section-header"><div class="section-title">Compliance Rules</div></div>';

  if (!state || state.loading) { container.innerHTML += skeletonTable(3); return; }
  if (state.error) { container.innerHTML += errorBanner(state.error, function() { tabRefreshers.agents(); }); return; }
  if (!state.data || !state.data.rules || state.data.rules.length === 0) {
    container.innerHTML += emptyState('\u{1F6E1}', 'No compliance rules configured');
    return;
  }

  var rules = state.data.rules;
  var checks = state.data.checks || [];

  var html = '<div class="rules-grid">';
  rules.forEach(function(r) {
    var ruleChecks = checks.filter(function(c) { return c.rule_id === r.rule_id; });
    var passCount = ruleChecks.filter(function(c) { return c.result === 'pass'; }).length;
    var failCount = ruleChecks.filter(function(c) { return c.result === 'fail'; }).length;
    var activeViolations = ruleChecks.filter(function(c) { return c.result === 'fail' && !c.resolved_at; }).length;

    html += '<div class="rule-card">' +
      '<div class="rule-card-header">' +
        '<div class="rule-card-name">' + esc(r.rule_name) + '</div>' +
        '<span class="enforcement-badge ' + (r.enforcement === 'block' ? 'hard' : 'soft') + '">' + esc(r.enforcement) + '</span>' +
      '</div>' +
      '<div class="rule-card-desc">' + esc(r.description) + '</div>' +
      '<div class="rule-card-stats">' +
        '<span style="color:var(--rule-pass)">' + passCount + ' pass</span>' +
        '<span style="color:var(--rule-fail)">' + failCount + ' fail</span>' +
        (activeViolations > 0 ? '<span style="color:var(--rule-fail);font-weight:600">' + activeViolations + ' active</span>' : '') +
      '</div>' +
      '<div class="rule-card-version">v' + r.rule_version + ' \u00b7 ' + esc(r.rule_kind) + '</div>' +
      '</div>';
  });
  html += '</div>';
  container.innerHTML = container.innerHTML + html;
}
```

- [ ] **Step 7: Implement agent event timeline renderer**

```javascript
function renderAgentsTimeline(container) {
  var state = sectionState['agents-events'];

  container.innerHTML = '<div class="section-header"><div class="section-title">Agent Events</div></div>';

  if (!state || state.loading) { container.innerHTML += skeletonTable(5); return; }
  if (state.error) { container.innerHTML += errorBanner(state.error, function() { tabRefreshers.agents(); }); return; }
  if (!state.data || !state.data.events || state.data.events.length === 0) {
    container.innerHTML += emptyState('\u{1F4E1}', 'No agent events yet');
    return;
  }

  var events = state.data.events;
  var html = '<div class="timeline" style="max-height:24rem;overflow-y:auto">';
  events.forEach(function(ev, i) {
    var dotColor = eventTypeColor(ev.event_type);
    var isLast = i === events.length - 1;
    html += '<div class="timeline-entry">' +
      '<div class="timeline-track">' +
        '<div class="timeline-dot" style="background:var(' + dotColor + ')"></div>' +
        (isLast ? '' : '<div class="timeline-line"></div>') +
      '</div>' +
      '<div class="timeline-body">' +
        '<div class="timeline-time">' + relativeTime(ev.created_at) + '</div>' +
        '<div class="timeline-text">' + esc(ev.event_type.replace(/_/g, ' ')) + '</div>' +
        '<div class="timeline-agent">' + esc(ev.agent_id || '\u2014') + (ev.session_id ? ' \u00b7 ' + truncId(ev.session_id) : '') + '</div>' +
      '</div>' +
      '</div>';
  });
  html += '</div>';
  container.innerHTML = container.innerHTML + html;
}

function eventTypeColor(type) {
  if (type.indexOf('register') >= 0 || type.indexOf('start') >= 0) return '--status-active';
  if (type.indexOf('end') >= 0 || type.indexOf('complete') >= 0) return '--status-completed';
  if (type.indexOf('block') >= 0 || type.indexOf('fail') >= 0 || type.indexOf('error') >= 0) return '--status-lost';
  if (type.indexOf('attach') >= 0 || type.indexOf('assign') >= 0) return '--status-waiting';
  return '--text-muted';
}
```

- [ ] **Step 8: Run tests**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
go test ./internal/dashboard/... -v
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
git add internal/dashboard/static/app.js
git commit -m "feat: implement Agents tab with sessions, assignments, compliance, and events"
```

---

## Task 5: Refresh Existing Tabs

**Goal:** Implement Overview, Events, Findings, Settings, and Architecture tab renderers using the new design system. Each tab uses the shared primitives (metric cards, data tables, status chips, etc.) and fetches data from existing endpoints.

**Files:**
- Modify: `static/app.js` (add/replace tab renderers)

### Steps

- [ ] **Step 1: Implement Overview tab**

Add `tabRefreshers.overview` and `renderOverviewTab()`. Fetches `/overview`, `/repos`, `/active-sessions`, `/gate-health`. Shows:
- Metric strip: runs today, pass rate, blocked count, avg gate time
- Repo table with gate pills (status chips per provider)
- Active sessions summary (count + top 3 sessions with state dots)

```javascript
tabRefreshers.overview = async function() {
  await Promise.allSettled([
    fetchSection('overview-stats', '/overview'),
    fetchSection('overview-repos', '/repos'),
    fetchSection('overview-sessions', '/active-sessions'),
    fetchSection('overview-gates', '/gate-health'),
  ]);
  renderOverviewTab();
};
```

Render patterns:
- Metric cards for runs_today, pass_rate (format as X%), blocked_count, avg_gate_sec (format with formatDuration)
- Repos as data-table with columns: Repo, Branch, State (status chip), Head, Last Run, Gate Summary (pills)
- Gate health as small inline cards
- **Note:** Sparkline chart rendering is deferred per spec Section 12. The sparkline data (`sparkline_7d`) is fetched but not rendered as a chart in this pass.

- [ ] **Step 2: Implement Events tab**

Add `tabRefreshers.events` and `renderEventsTab()`. Fetches `/activity`. Shows:
- Severity filter bar: ALL / CRITICAL / HIGH / MEDIUM / LOW (preserves existing severity filtering from spec Section 7)
- Activity events as data-table with expandable rows
- Columns: Time, Repo, Branch, Type, Preview

```javascript
tabRefreshers.events = async function() {
  await fetchSection('events-activity', '/activity');
  renderEventsTab();
};
```

- [ ] **Step 3: Implement Findings tab**

Add `tabRefreshers.findings` and `renderFindingsTab()`. Fetches `/gate-checks`, `/block-reasons`. Shows:
- Block reasons as ranked bar chart or table
- Gate check report details
- Toggle between card and table view

```javascript
tabRefreshers.findings = async function() {
  await Promise.allSettled([
    fetchSection('findings-checks', '/gate-checks'),
    fetchSection('findings-blocks', '/block-reasons'),
  ]);
  renderFindingsTab();
};
```

- [ ] **Step 4: Implement Architecture tab**

Keep the architecture diagram mostly intact from the old dashboard. Restyle with new tokens:
- Use surface-1 for the diagram background
- Update border colors to use --border
- Keep the node layout and connections
- Apply new font styles

```javascript
tabRefreshers.architecture = async function() {
  // Architecture is static — no fetch needed.
  renderArchitectureTab();
};
```

Render the static SVG/HTML diagram directly. Reuse the structure from the old index.html but apply new CSS classes.

- [ ] **Step 5: Implement Settings tab**

Add `tabRefreshers.settings` and `renderSettingsTab()`. Fetches `/settings`. Shows:
- Integration cards grid (connected/disconnected badges)
- Gate pipeline config table with toggle switches
- Save button (PUT /settings)

```javascript
tabRefreshers.settings = async function() {
  await fetchSection('settings-data', '/settings');
  renderSettingsTab();
};
```

Render patterns:
- Integration cards as a grid of cards with connected status chip
- Gate pipeline as data-table with columns: Name, Provider, Enabled (toggle), Blocks Commit (toggle), Timeout

- [ ] **Step 6: Implement health bar renderer**

Add `renderHealthBar()` called on every poll cycle:
```javascript
function renderHealthBar(health) {
  if (!health) return;
  setHealthDot('hb-db-dot', health.database ? health.database.status : 'down');
  setHealthDot('hb-sessions-dot', health.feeds ? health.feeds.active_sessions.status : 'unavailable');
  setHealthDot('hb-gates-dot', health.feeds ? health.feeds.gate_checks.status : 'unavailable');
  var agentsLabel = document.getElementById('hb-agents-label');
  if (agentsLabel) agentsLabel.textContent = (health.active_agent_count || 0) + ' agents';
  var refreshed = document.getElementById('hb-refreshed');
  if (refreshed) refreshed.textContent = 'refreshed ' + new Date().toLocaleTimeString();
}

function setHealthDot(id, status) {
  var dot = document.getElementById(id);
  if (!dot) return;
  dot.className = 'health-dot ' + (status === 'ok' ? 'ok' : status === 'stale' ? 'stale' : 'down');
}
```

Wire health bar refresh into the polling cycle — after any tab refresh that fetches `/health`, call `renderHealthBar`.

- [ ] **Step 7: Remove all old tab rendering code and chat UI**

Delete any remaining old-style rendering functions from `app.js` that were carried over from the original `index.html` split. This includes:
- Old tab renderers for processes, eventlogs, findings, architecture, settings
- **Chat/review assistant code** — the "Ask Codero" chat interface, chat suggestions, chat actions, review-process surfaces, and `POST /chat` calls. Chat is deferred per spec Section 12 and the new HTML shell has no chat container.
- Old SSE handler code (SSE is deferred per spec)

All tab content should now be rendered by the new `renderXxxTab()` functions.

- [ ] **Step 8: Run tests**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
go test ./internal/dashboard/... -v
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
git add internal/dashboard/static/app.js internal/dashboard/static/styles.css
git commit -m "feat: refresh Overview, Events, Findings, Architecture, Settings tabs with new design system"
```

---

## Task 6: Tests and Validation

**Goal:** Ensure all tests pass, static embed coverage is complete, and the dashboard serves correctly. Final validation before merge.

**Files:**
- Modify: `dashboard_test.go` (final assertion updates)

### Steps

- [ ] **Step 1: Update test assertions for final content**

Review `TestDashboardHTML_HasExpectedContent` and ensure it checks for content that actually exists in the final files:
- `index.html`: "codero", "Overview", "Agents", "Events", "Findings", "Architecture", "Settings", "health-bar", `./styles.css`, `./app.js`
- `app.js`: "apiFetch", "/health", "/active-sessions", "/assignments", "/compliance", "/agent-events", "renderAgentsTab", "renderOverviewTab"
- `styles.css`: "--bg-base", "--surface-1", "--status-active", ".status-chip", ".metric-card", ".data-table"

```go
// In TestDashboardHTML_HasExpectedContent, add CSS checks:
cssF, err := subFS.Open("styles.css")
if err != nil {
    t.Fatalf("open styles.css: %v", err)
}
cssData, _ := io.ReadAll(cssF)
cssF.Close()
css := string(cssData)

for _, want := range []string{
    "--bg-base", "--surface-1", "--status-active",
    ".status-chip", ".metric-card", ".data-table",
} {
    if !strings.Contains(css, want) {
        t.Errorf("styles.css missing %q", want)
    }
}
```

- [ ] **Step 2: Run targeted dashboard and daemon tests**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
go test ./internal/dashboard/... ./internal/daemon/... -v -count=1
```

Expected: ALL PASS. Watch for:
- `TestStaticEmbedHasIndexHTML` — verifies all 3 files embedded with correct references
- `TestDashboardHTML_HasExpectedContent` — verifies HTML/JS/CSS content
- All API endpoint tests — unchanged behavior
- All query tests — unchanged behavior

- [ ] **Step 3: Run full test suite**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
go test ./... -count=1
```

Expected: ALL PASS.

- [ ] **Step 4: Verify Go build**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
go build ./...
```

Expected: Clean build, no errors.

- [ ] **Step 5: Commit final test updates**

```bash
cd /srv/storage/repo/codero/.worktrees/COD-071-v3-closeout
git add internal/dashboard/dashboard_test.go
git commit -m "test: update static embed assertions for 3-file dashboard structure"
```

- [ ] **Step 6: Verify file list**

Run `git diff --stat` against the base branch to confirm changed files match expectations:
- `internal/dashboard/static/index.html` — rewritten
- `internal/dashboard/static/styles.css` — new
- `internal/dashboard/static/app.js` — new
- `internal/dashboard/dashboard_test.go` — updated
- `docs/superpowers/specs/2026-03-22-dashboard-refresh-design.md` — new
- `docs/superpowers/plans/2026-03-22-dashboard-refresh.md` — new

No changes to: `handlers.go`, `models.go`, `queries.go`, `queries_internal_test.go`, `static_embed.go`, `observability.go`.

---

## Ordering Dependencies

Task execution order is strict: 1 → 2 → 3 → 4 → 5 → 6.

Key dependency: Task 4 (Agents tab) adds `/active-sessions`, `/assignments`, `/compliance`, `/agent-events` references to `app.js`. Task 5 Step 7 removes old code. The test assertion for `"active-sessions"` in `app.js` is safe because Task 4 runs before Task 5. If tasks were reordered, this test would transiently fail.
