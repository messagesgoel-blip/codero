// utils.js — DOM helpers, formatters, security utilities.

const ESC_MAP = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };

export function esc(s) {
  return String(s).replace(/[&<>"']/g, c => ESC_MAP[c]);
}

// Tagged template literal for safe HTML construction.
// Usage: html`<div>${unsafeUserInput}</div>`
export function html(strings, ...values) {
  return strings.reduce((acc, str, i) =>
    acc + str + (values[i] != null ? esc(String(values[i])) : ''), '');
}

// Inject raw HTML (already escaped or static template). Use sparingly.
export function rawHtml(s) { return s; }

export function $(id) { return document.getElementById(id); }
export function $$(sel) { return document.querySelectorAll(sel); }

export function setText(el, text) {
  if (typeof el === 'string') el = $(el);
  if (el) el.textContent = text;
}

export function setHtml(el, markup) {
  if (typeof el === 'string') el = $(el);
  if (el) el.innerHTML = markup; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
}

export function show(el) {
  if (typeof el === 'string') el = $(el);
  if (el) el.style.display = '';
}

export function hide(el) {
  if (typeof el === 'string') el = $(el);
  if (el) el.style.display = 'none';
}

export function toggleClass(el, cls, force) {
  if (typeof el === 'string') el = $(el);
  if (el) el.classList.toggle(cls, force);
}

// Status chip HTML
export function statusChip(state) {
  const cls = statusClass(state);
  return `<span class="status-chip ${cls}">${esc(state)}</span>`;
}

export function statusClass(state) {
  const s = String(state).toLowerCase();
  if (['active', 'pass', 'passing', 'success', 'merged', 'connected'].includes(s)) return 'status-success';
  if (['blocked', 'fail', 'failing', 'failure', 'changes_requested', 'stalled'].includes(s)) return 'status-destructive';
  if (['waiting', 'pending', 'queued', 'in_progress', 'gating'].includes(s)) return 'status-warning';
  if (['stale', 'expired', 'lost', 'abandoned', 'cancelled'].includes(s)) return 'status-muted';
  if (['approved', 'merge_ready', 'completed'].includes(s)) return 'status-info';
  return 'status-muted';
}

export function severityChip(sev) {
  const cls = severityClass(sev);
  return `<span class="severity-chip ${cls}">${esc(sev)}</span>`;
}

function severityClass(sev) {
  const s = String(sev).toLowerCase();
  if (s === 'critical') return 'sev-critical';
  if (s === 'high') return 'sev-high';
  if (s === 'medium') return 'sev-medium';
  if (s === 'low') return 'sev-low';
  return 'sev-info';
}

// Time formatting
export function relativeTime(ts) {
  if (!ts) return '—';
  const d = new Date(ts);
  const sec = Math.floor((Date.now() - d.getTime()) / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const days = Math.floor(hr / 24);
  return `${days}d ago`;
}

export function formatDuration(sec) {
  if (sec == null || sec < 0) return '—';
  if (sec < 60) return `${Math.round(sec)}s`;
  const m = Math.floor(sec / 60);
  const s = Math.round(sec % 60);
  if (m < 60) return s > 0 ? `${m}m ${s}s` : `${m}m`;
  const h = Math.floor(m / 60);
  const rm = m % 60;
  return rm > 0 ? `${h}h ${rm}m` : `${h}h`;
}

export function formatPct(v) {
  if (v == null || v < 0) return '—';
  return `${Math.round(v)}%`;
}

export function truncId(id, len = 8) {
  if (!id) return '—';
  return id.length > len ? id.slice(0, len) + '...' : id;
}

export function debounce(fn, ms) {
  let timer;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), ms);
  };
}

// Kanban stage color
export function stageColor(stage) {
  const s = String(stage).toUpperCase();
  const map = {
    SUBMITTED: 'var(--stage-submitted)', GATING: 'var(--stage-gating)',
    COMMITTED: 'var(--stage-committed)', PUSHED: 'var(--stage-pushed)',
    PR_ACTIVE: 'var(--stage-pr-active)', MONITORING: 'var(--stage-monitoring)',
    MERGE_READY: 'var(--stage-merge-ready)', MERGED: 'var(--stage-merged)',
  };
  return map[s] || 'var(--fg-muted)';
}
