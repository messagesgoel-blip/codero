// sessions.js — Sessions page renderer.
// Active tab: live session table with expandable context-pressure sparkline.
// History tab: archived runs with timing analytics strip.

import store from '../store.js';
import { loadSessions, loadAssignments, loadArchives, apiFetch } from '../api.js';
import {
  esc, html, statusChip, relativeTime, formatDuration, truncId, setHtml, $,
} from '../utils.js';
import { metricCard, dataTable, detailGrid, glassCard, skeleton, sparklineChart, toast } from '../components.js';

// --- Tab + filter state ---
let _tab = 'active';   // 'active' | 'history'
let _filter = { repo: '', branch: '' };

// --- Internal state ---
let _initialized = false;
const _unsubs = [];

// --- Public API ---

export function initSessions() {
  if (_initialized) return;
  _initialized = true;

  _unsubs.push(store.subscribe('sessions', () => renderSessions()));
  _unsubs.push(store.subscribe('assignments', () => renderSessions()));
  _unsubs.push(store.subscribe('archives', () => { if (_tab === 'history') renderSessions(); }));
}

export async function refreshSessions() {
  const results = await Promise.allSettled([loadSessions(), loadAssignments(), loadArchives()]);
  if (results.some(r => r.status === 'rejected')) {
    toast('Some session data failed to load', 'error');
  }
}

export function renderSessions() {
  const container = $('page-sessions');
  if (!container) return;
  if (store.state.ui.activeTab !== 'sessions') return;

  if (_tab === 'active' && !store.select('sessions')) {
    setHtml(container, skeleton(6));
    return;
  }

  const sessions = store.select('sessions') || [];
  const assignments = store.select('assignments') || [];
  const archives = store.select('archives') || [];

  const filterVal = (_filter.repo || '').toLowerCase();
  const totalCount = _tab === 'active' ? sessions.length : archives.length;
  const filteredCount = filterVal
    ? (_tab === 'active' ? sessions : archives).filter(s =>
        (s.repo || '').toLowerCase().includes(filterVal) ||
        (s.branch || '').toLowerCase().includes(filterVal) ||
        (s.agent || s.ownerAgent || '').toLowerCase().includes(filterVal)).length
    : totalCount;

  const parts = [
    _renderTabBar(totalCount, filteredCount),
    _tab === 'active'
      ? _renderActiveTab(sessions, assignments)
      : _renderHistoryTab(archives),
  ];
  setHtml(container, parts.join(''));

  _bindTabBar();
  _bindExpandToggles();
}

// --- Tab bar ---

function _renderTabBar(totalCount, filteredCount) {
  const activeBtn = `<button class="tab-btn${_tab === 'active' ? ' active' : ''}" data-tab="active">Active</button>`;
  const historyBtn = `<button class="tab-btn${_tab === 'history' ? ' active' : ''}" data-tab="history">History</button>`;
  const filterVal = _filter.repo || _filter.branch || '';
  const filterInput = `<input class="filter-input" id="sessions-filter" placeholder="filter repo / branch…" value="${esc(filterVal)}">`;
  const clearBtn = filterVal
    ? `<button class="filter-clear-btn" id="sessions-filter-clear" title="Clear filter">&#x2715;</button>`
    : '';
  const countBadge = filterVal && filteredCount !== totalCount
    ? `<span class="filter-count">${filteredCount} of ${totalCount}</span>`
    : '';
  return `<div class="tab-bar">${activeBtn}${historyBtn}${filterInput}${clearBtn}${countBadge}</div>`;
}

function _bindTabBar() {
  const container = $('page-sessions');
  if (!container) return;
  container.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', e => {
      const t = e.target.dataset.tab;
      if (t && t !== _tab) { _tab = t; renderSessions(); }
    });
  });
  const filterEl = document.getElementById('sessions-filter');
  if (filterEl) {
    filterEl.addEventListener('input', e => {
      const v = e.target.value.trim();
      _filter = { repo: v, branch: v };
      renderSessions();
    });
  }
  const clearEl = document.getElementById('sessions-filter-clear');
  if (clearEl) {
    clearEl.addEventListener('click', () => {
      _filter = { repo: '', branch: '' };
      renderSessions();
    });
  }
}

// --- Active tab ---

function _renderActiveTab(sessions, assignments) {
  const activeSessions = sessions.filter(s => s.state === 'active').length;
  const stalledSessions = sessions.filter(s => s.state === 'stalled').length;
  const strip = [
    metricCard(String(sessions.length), 'Sessions', 'var(--accent)'),
    metricCard(String(activeSessions), 'Active', 'var(--success)'),
    metricCard(String(stalledSessions), 'Stalled', stalledSessions > 0 ? 'var(--warning)' : 'var(--fg-muted)'),
    metricCard(String(assignments.length), 'Assignments', 'var(--info)'),
  ];
  const metricsHtml = `<div class="metric-strip">${strip.join('')}</div>`;
  return metricsHtml + _renderSessionsTable(sessions, assignments);
}

function _renderSessionsTable(sessions, assignments) {
  const assignmentsBySession = new Map();
  for (const a of assignments) {
    if (!assignmentsBySession.has(a.sessionId)) {
      assignmentsBySession.set(a.sessionId, []);
    }
    assignmentsBySession.get(a.sessionId).push(a);
  }

  // Client-side filter
  const filterVal = (_filter.repo || '').toLowerCase();
  const filtered = filterVal
    ? sessions.filter(s =>
        (s.repo || '').toLowerCase().includes(filterVal) ||
        (s.branch || '').toLowerCase().includes(filterVal) ||
        (s.agent || s.ownerAgent || '').toLowerCase().includes(filterVal))
    : sessions;

  const columns = [
    {
      key: 'agent',
      label: 'Agent',
      render: r => html`${r.agent || r.ownerAgent || '—'}`,
    },
    {
      key: 'repo',
      label: 'Repo / Branch',
      render: r => {
        const repo = esc(r.repo || '—');
        const branch = esc(r.branch || '');
        return branch ? `${repo} / <code>${branch}</code>` : repo;
      },
    },
    {
      key: 'task',
      label: 'Task',
      render: r => esc(r.task || '—'),
    },
    {
      key: 'state',
      label: 'State',
      render: r => statusChip(r.state || 'unknown'),
    },
    {
      key: 'lastIOAt',
      label: 'Last Output',
      render: r => {
        if (!r.lastIOAt) return '<span style="color:var(--fg-muted)">—</span>';
        const age = (Date.now() - new Date(r.lastIOAt).getTime()) / 1000;
        const style = age > 90 ? 'color:var(--warning)' : 'color:var(--success)';
        return `<span style="${style}">${esc(relativeTime(r.lastIOAt))}</span>`;
      },
    },
    {
      key: 'contextPressure',
      label: 'Context',
      render: r => {
        const p = r.contextPressure || 'normal';
        if (p === 'normal') return '<span style="color:var(--fg-muted)">—</span>';
        const col = p === 'critical' ? 'var(--error)' : 'var(--warning)';
        const icon = p === 'critical' ? '🔴' : '🟡';
        const compact = r.compactCount > 0 ? ` ×${r.compactCount}` : '';
        return `<span style="color:${col}" title="${p} context pressure${compact ? '; compacted ' + r.compactCount + ' time(s)' : ''}">${icon} ${p}${compact}</span>`;
      },
    },
    {
      key: 'lastHeartbeat',
      label: 'Heartbeat',
      render: r => esc(relativeTime(r.lastHeartbeat)),
    },
    {
      key: 'elapsedSec',
      label: 'Elapsed',
      render: r => esc(formatDuration(r.elapsedSec)),
    },
  ];

  const rows = filtered.map(s => {
    const row = { ...s, _id: s.id };
    const sessionAssigns = assignmentsBySession.get(s.id) || [];
    // Inject sparkline placeholder — filled in lazily on expand
    row._expandHtml = _buildExpandContent(s, sessionAssigns);
    return row;
  });

  const tableHtml = dataTable('sessions-table', columns, rows, {
    expandable: true,
    empty: 'No active sessions',
  });

  return glassCard('Sessions', tableHtml, { padding: 'none', class: 'card-sessions' });
}

function _buildExpandContent(session, assigns) {
  const _mkPlaceholder = (uid) => `<div id="sparkline-${esc(uid)}" data-sparkline-for="${esc(session.id)}" class="sparkline-placeholder">
    <div class="skeleton sparkline-skeleton" style="width:120px;height:30px;display:inline-block;border-radius:4px"></div>
    <a href="/api/v1/dashboard/sessions/metrics/${esc(session.id)}" target="_blank" class="drilldown-link" style="margin-left:8px;font-size:11px;color:var(--accent)">metrics →</a>
  </div>`;

  if (assigns.length === 0) {
    const items = [
      { label: 'Session ID', value: esc(truncId(session.id, 12)) },
      { label: 'Mode', value: esc(session.mode || '—') },
      { label: 'Worktree', value: esc(session.worktree || '—') },
      { label: 'PR', value: session.prNumber ? esc('#' + session.prNumber) : '—' },
      { label: 'Started', value: esc(relativeTime(session.startedAt)) },
      { label: 'Last Output', value: session.lastIOAt ? esc(relativeTime(session.lastIOAt)) : '—' },
      { label: 'Context Trend', value: _mkPlaceholder(session.id) },
    ];
    return detailGrid(items);
  }

  let out = '';
  for (const a of assigns) {
    const items = [
      { label: 'Assignment', value: esc(truncId(a.id, 12)) },
      { label: 'Substatus', value: a.substatus ? statusChip(a.substatus) : '—' },
      { label: 'Blocked Reason', value: esc(a.blockedReason || '—') },
      { label: 'Task ID', value: esc(a.taskId || '—') },
      { label: 'Worktree', value: esc(session.worktree || '—') },
      { label: 'Branch State', value: a.branchState ? statusChip(a.branchState) : '—' },
      { label: 'PR', value: a.prNumber ? esc('#' + a.prNumber) : '—' },
      { label: 'Started', value: esc(relativeTime(a.startedAt)) },
      { label: 'Ended', value: a.endedAt ? esc(relativeTime(a.endedAt)) : '—' },
      { label: 'Context Trend', value: _mkPlaceholder(session.id + '-' + a.id) },
    ];
    out += detailGrid(items);
  }
  return out;
}

// Lazy-load sparkline on row expand.
// Targets all placeholders for the session via [data-sparkline-for] so
// sessions with multiple assignment grids all get updated.
function _loadSparkline(sessionId) {
  const placeholders = document.querySelectorAll(`[data-sparkline-for="${sessionId}"]`);
  if (!placeholders.length) return;
  const unloaded = [...placeholders].filter(el => !el.dataset.loaded);
  if (!unloaded.length) return;

  const safeId = encodeURIComponent(sessionId);
  const fallbackLink = `<a href="/api/v1/dashboard/sessions/metrics/${safeId}" target="_blank" class="drilldown-link" style="font-size:11px;color:var(--accent)">metrics \u2192</a>`;
  apiFetch(`/api/v1/dashboard/sessions/metrics/${safeId}`).then(data => {
    if (!Array.isArray(data?.requests)) {
      // Malformed/empty response — leave unloaded so a future expand can retry
      const noData = '<span style="color:var(--fg-muted);font-size:11px">no metrics yet</span> ' + fallbackLink;
      // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
      for (const p of unloaded) p.innerHTML = noData;
      return;
    }
    const values = data.requests.map(r => {
      const n = Number(r.cumulative_prompt_tokens);
      return Number.isFinite(n) ? n : 0;
    });
    const compact = (data?.compact_count ?? 0) > 0
      ? `<span style="margin-left:6px;font-size:11px;color:var(--fg-muted)">compacted \xd7${Number(data.compact_count)}</span>`
      : '';
    const link = `<a href="/api/v1/dashboard/sessions/metrics/${safeId}" target="_blank" class="drilldown-link" style="margin-left:8px;font-size:11px;color:var(--accent)">metrics \u2192</a>`;
    const chart = sparklineChart(values, { color: 'var(--accent)' });
    const inner = chart + compact + link;
    for (const p of unloaded) {
      p.dataset.loaded = '1';
      // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
      p.innerHTML = inner;
    }
  }).catch(() => {
    // Network/parse error — leave unloaded so a future expand can retry
    const noData = '<span style="color:var(--fg-muted);font-size:11px">no metrics yet</span> ' + fallbackLink;
    // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    for (const p of unloaded) p.innerHTML = noData;
  });
}

// --- Expand-row toggle binding ---

function _bindExpandToggles() {
  for (const tableId of ['sessions-table', 'history-table']) {
    const table = $(tableId);
    if (!table) continue;

    table.addEventListener('click', (e) => {
      const tr = e.target.closest('tr.expandable');
      if (!tr) return;
      const rowId = tr.dataset.rowId;
      if (!rowId) return;

      const expandRow = table.querySelector(`tr.expand-row[data-expand-for="${rowId}"]`);
      if (!expandRow) return;

      const isHidden = expandRow.classList.contains('hidden');
      expandRow.classList.toggle('hidden', !isHidden);

      const chevron = tr.querySelector('.chevron');
      if (chevron) chevron.classList.toggle('open', isHidden);

      const expanded = store.select('ui').expandedRows;
      if (isHidden) {
        expanded.add(rowId);
        // Lazy-load sparkline when row is opened (no-op for history rows)
        _loadSparkline(rowId);
      } else {
        expanded.delete(rowId);
      }
    });
  }
}

// --- History tab ---

function _renderHistoryTab(archives) {
  const filterVal = (_filter.repo || '').toLowerCase();
  const filtered = filterVal
    ? archives.filter(a =>
        (a.repo || '').toLowerCase().includes(filterVal) ||
        (a.branch || '').toLowerCase().includes(filterVal) ||
        (a.agent || '').toLowerCase().includes(filterVal))
    : archives;

  return _renderTimingAnalytics(filtered) + _renderHistoryTable(filtered);
}

function _renderTimingAnalytics(archives) {
  const completed = archives.filter(a => a.durationSec != null && a.durationSec >= 0);
  const totalRuns = archives.length;
  const avgDuration = completed.length > 0
    ? completed.reduce((sum, a) => sum + a.durationSec, 0) / completed.length
    : -1;
  const successCount = archives.filter(a => ['success', 'merged', 'completed'].includes((a.result || '').toLowerCase())).length;
  const passRate = totalRuns > 0 ? Math.round((successCount / totalRuns) * 100) : -1;
  // Throughput: sessions per day over the window spanned by the archives
  let throughput = 0;
  if (archives.length > 1) {
    const times = archives.map(a => a.startedAt ? new Date(a.startedAt).getTime() : 0).filter(Boolean);
    if (times.length > 1) {
      let minTime = times[0], maxTime = times[0];
      for (const t of times) { if (t < minTime) minTime = t; if (t > maxTime) maxTime = t; }
      const spanDays = (maxTime - minTime) / (1000 * 60 * 60 * 24);
      if (spanDays > 0) throughput = +(archives.length / spanDays).toFixed(1);
    }
  }

  const strip = [
    metricCard(totalRuns, 'Total Runs', 'var(--fg-muted)'),
    metricCard(formatDuration(avgDuration), 'Avg Duration', 'var(--stage-gating)'),
    metricCard(passRate >= 0 ? passRate + '%' : '—', 'Pass Rate', passRate >= 80 ? 'var(--success)' : passRate >= 50 ? 'var(--warning)' : 'var(--destructive)'),
    metricCard(throughput > 0 ? throughput + '/day' : '—', 'Throughput', 'var(--info)'),
  ].join('');

  return `<div class="metric-strip">${strip}</div>`;
}

function _renderHistoryTable(archives) {
  const columns = [
    { key: 'agent', label: 'Agent' },
    {
      key: 'repo',
      label: 'Repo / Branch',
      render: r => `${esc(r.repo || '—')} / <code>${esc(r.branch || '—')}</code>`,
    },
    {
      key: 'result',
      label: 'Result',
      render: r => _resultChip(r.result),
    },
    {
      key: 'durationSec',
      label: 'Duration',
      render: r => formatDuration(r.durationSec),
    },
    {
      key: 'endedAt',
      label: 'Ended',
      render: r => r.endedAt ? esc(relativeTime(r.endedAt)) : '<span style="color:var(--fg-muted)">—</span>',
    },
    {
      key: 'commitCount',
      label: 'Commits',
      class: 'col-num',
      render: r => esc(String(r.commitCount ?? 0)),
    },
  ];

  const rows = archives.map(a => ({
    ...a,
    _id: a.id || a.sessionId,
    _expandHtml: _buildHistoryExpandContent(a),
  }));

  const tableHtml = dataTable('history-table', columns, rows, {
    expandable: true,
    empty: 'No archived runs',
  });

  return glassCard('Run History', tableHtml, { padding: 'none', class: 'card-history' });
}

function _buildHistoryExpandContent(archive) {
  const items = [
    { label: 'Session ID', value: archive.sessionId ? `<code>${esc(truncId(archive.sessionId, 16))}</code>` : '—' },
    { label: 'Task', value: esc(archive.taskId || '—') },
    { label: 'Task Source', value: esc(archive.taskSource || '—') },
    { label: 'Started', value: archive.startedAt ? esc(new Date(archive.startedAt).toLocaleString()) : '—' },
    { label: 'Ended', value: archive.endedAt ? esc(new Date(archive.endedAt).toLocaleString()) : '—' },
    { label: 'Merge SHA', value: archive.mergeSha ? `<code>${esc(truncId(archive.mergeSha, 10))}</code>` : '—' },
  ];
  return detailGrid(items);
}

function _resultChip(result) {
  if (!result) return statusChip('unknown');
  const r = String(result).toLowerCase();
  if (['success', 'merged', 'completed'].includes(r)) return statusChip('success');
  if (['failure', 'failed', 'error'].includes(r)) return statusChip('fail');
  if (['cancelled', 'abandoned', 'timeout'].includes(r)) return statusChip('cancelled');
  return statusChip(result);
}
