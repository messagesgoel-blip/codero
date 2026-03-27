// overview.js — Overview / Mission Control page renderer.
// Reads from the global store and renders metrics, repo table, and gate health.

import store from '../store.js';
import { loadOverview, loadRepos, loadHealth, loadGateHealth } from '../api.js';
import { formatPct, formatDuration, relativeTime, esc, html, setHtml, $, statusChip } from '../utils.js';
import { metricCard, dataTable, glassCard, skeleton } from '../components.js';

// --- Internal state ---
let _initialized = false;
const _unsubs = [];

// --- Public API ---

export function initOverview() {
  if (_initialized) return;
  _initialized = true;

  _unsubs.push(store.subscribe('overview', () => renderOverview()));
  _unsubs.push(store.subscribe('repos', () => renderOverview()));
  _unsubs.push(store.subscribe('health', () => _updateHealthBar()));
  _unsubs.push(store.subscribe('gateHealth', () => renderOverview()));
}

export async function refreshOverview() {
  await Promise.allSettled([
    loadOverview(),
    loadRepos(),
    loadHealth(),
    loadGateHealth(),
  ]);
}

export function renderOverview() {
  const container = $('page-overview');
  if (!container) return;

  const ov = store.select('overview');
  const repos = store.select('repos');
  const gateHealth = store.select('gateHealth');

  // Show skeleton while data loads
  if (!ov) {
    setHtml(container, skeleton(6));
    return;
  }

  const parts = [];

  // ---- Metric strip (4 glass cards) ----
  parts.push(_renderMetricStrip(ov));

  // ---- Repos table ----
  parts.push(_renderReposTable(repos || []));

  // ---- Gate health table ----
  parts.push(_renderGateHealthTable(gateHealth || []));

  setHtml(container, parts.join(''));
}

// --- Private renderers ---

function _renderMetricStrip(ov) {
  const cards = [
    metricCard(String(ov.runsToday), 'Runs Today', 'var(--accent)'),
    metricCard(formatPct(ov.passRate), 'Pass Rate', _passRateColor(ov.passRate)),
    metricCard(String(ov.blockedCount), 'Blocked', ov.blockedCount > 0 ? 'var(--destructive)' : 'var(--success)'),
    metricCard(formatDuration(ov.avgGateSec), 'Avg Gate Time', 'var(--info)'),
  ];
  return `<div class="metric-strip">${cards.join('')}</div>`;
}

function _passRateColor(rate) {
  if (rate == null || rate < 0) return 'var(--fg-muted)';
  if (rate >= 90) return 'var(--success)';
  if (rate >= 70) return 'var(--warning)';
  return 'var(--destructive)';
}

function _renderReposTable(repos) {
  const columns = [
    { key: 'repo', label: 'Repo' },
    { key: 'branch', label: 'Branch' },
    {
      key: 'state',
      label: 'State',
      render: (r) => statusChip(r.state || 'unknown'),
    },
    {
      key: 'lastRunStatus',
      label: 'Last Run',
      render: (r) => statusChip(r.lastRunStatus || 'none'),
    },
    {
      key: 'lastRunAt',
      label: 'Last Run Time',
      render: (r) => esc(relativeTime(r.lastRunAt)),
    },
    {
      key: 'gateSummary',
      label: 'Gate Summary',
      render: (r) => esc(r.gateSummary || '—'),
    },
  ];

  const tableHtml = dataTable('overview-repos-table', columns, repos, {
    empty: 'No repos tracked',
  });

  return glassCard('Repos', tableHtml, { padding: 'none', class: 'card-repos' });
}

function _renderGateHealthTable(gates) {
  const columns = [
    { key: 'provider', label: 'Provider' },
    {
      key: 'total',
      label: 'Total',
      class: 'col-num',
      render: (g) => esc(String(g.total ?? 0)),
    },
    {
      key: 'passed',
      label: 'Passed',
      class: 'col-num',
      render: (g) => esc(String(g.passed ?? 0)),
    },
    {
      key: 'passRate',
      label: 'Pass Rate',
      class: 'col-num',
      render: (g) => {
        const total = g.total ?? 0;
        const passed = g.passed ?? 0;
        const pct = total > 0 ? (passed / total) * 100 : -1;
        return esc(formatPct(pct));
      },
    },
  ];

  const rows = gates.map((g) => ({
    provider: g.provider || g.gate || '—',
    total: g.total ?? 0,
    passed: g.passed ?? 0,
  }));

  const tableHtml = dataTable('overview-gate-health-table', columns, rows, {
    empty: 'No gate health data',
  });

  return glassCard('Gate Health', tableHtml, { padding: 'none', class: 'card-gate-health' });
}

// --- Health bar (DB / Redis / GitHub status dots) ---

function _updateHealthBar() {
  const h = store.select('health');
  if (!h) return;

  const dotMap = {
    'health-db': h.db,
    'health-redis': h.redis,
    'health-github': h.github,
  };

  for (const [id, status] of Object.entries(dotMap)) {
    const el = $(id);
    if (!el) continue;
    el.classList.remove('dot-ok', 'dot-warn', 'dot-fail', 'dot-unknown');
    if (status === true || status === 'ok' || status === 'connected') {
      el.classList.add('dot-ok');
    } else if (status === false || status === 'fail' || status === 'error') {
      el.classList.add('dot-fail');
    } else if (status === 'degraded' || status === 'slow') {
      el.classList.add('dot-warn');
    } else {
      el.classList.add('dot-unknown');
    }
  }
}
