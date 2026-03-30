// overview.js — Overview / Mission Control page renderer.
// Reads from the global store and renders metrics, repo table, and gate health.

import store from '../store.js';
import { loadOverview, loadRepos, loadHealth, loadGateHealth, loadEvents } from '../api.js';
import { formatPct, formatDuration, relativeTime, esc, html, setHtml, $, statusChip, truncId } from '../utils.js';
import { metricCard, dataTable, glassCard, skeleton, toast } from '../components.js';

// --- Internal state ---
let _initialized = false;
const _unsubs = [];

// --- Public API ---

export function initOverview() {
  if (_initialized) return;
  _initialized = true;

  _unsubs.push(store.subscribe('overview', () => renderOverview()));
  _unsubs.push(store.subscribe('repos', () => renderOverview()));
  _unsubs.push(store.subscribe('gateHealth', () => renderOverview()));
  _unsubs.push(store.subscribe('events', () => renderOverview()));
}

export async function refreshOverview() {
  const results = await Promise.allSettled([
    loadOverview(),
    loadRepos(),
    loadHealth(),
    loadGateHealth(),
    loadEvents(),
  ]);
  if (results.some(r => r.status === 'rejected')) {
    toast('Some overview data failed to load', 'error');
  }
}

export function renderOverview() {
  const container = $('page-overview');
  if (!container) return;
  if (store.state.ui.activeTab !== 'overview') return;

  const ov = store.select('overview');
  const repos = store.select('repos');
  const gateHealth = store.select('gateHealth');
  const events = store.select('events') || [];

  // Show skeleton while data loads
  if (!ov) {
    setHtml(container, skeleton(6));
    return;
  }

  const parts = [];

  // ---- Metric strip (4 glass cards) ----
  parts.push(_renderMetricStrip(ov));

  // ---- Live Activity Feed (SSE-driven) ----
  parts.push(_renderActivityFeed(events));

  // ---- Repos table ----
  parts.push(_renderReposTable(repos || []));

  // ---- Gate health table ----
  parts.push(_renderGateHealthTable(gateHealth || []));

  setHtml(container, parts.join(''));
}

// --- Private renderers ---

function _renderActivityFeed(events) {
  const recent = events
    .filter(e => e.createdAt && !Number.isNaN(Date.parse(e.createdAt)))
    .sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt))
    .slice(0, 10);
  if (recent.length === 0) {
    return glassCard('Live Activity', '<div class="empty-state">No recent events</div>', { class: 'card-activity' });
  }

  const rows = recent.map(e => {
    const type = (e.type || 'event').replace(/_/g, ' ');
    const repoStr = e.repo ? (e.branch ? `${e.repo}/${e.branch}` : e.repo) : '';
    const agentStr = e.sessionId ? truncId(e.sessionId) : '';
    return `<div class="activity-row">
      <span class="activity-time">${e.createdAt ? esc(relativeTime(e.createdAt)) : ''}</span>
      ${statusChip(type)}
      ${repoStr ? `<span class="activity-repo">${esc(repoStr)}</span>` : '<span class="activity-repo"></span>'}
      ${agentStr ? `<code class="activity-agent">${esc(agentStr)}</code>` : ''}
    </div>`;
  }).join('');

  return glassCard('Live Activity', `<div style="padding:4px 16px 8px">${rows}</div>`, { class: 'card-activity' });
}

function _renderMetricStrip(ov) {
  const cards = [
    metricCard(String(ov.runsToday), 'Runs Today', 'var(--accent-warm)'),
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

