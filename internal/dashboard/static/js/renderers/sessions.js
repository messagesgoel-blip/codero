// sessions.js — Sessions page renderer.
// Redesigned with 3-layer info architecture: Row -> Peek -> Deep Dive.

import store from '../store.js';
import {
  loadSessions, loadAssignments, loadArchives, apiFetch,
  sessionAction, loadSessionTail,
} from '../api.js';
import {
  esc, html, statusChip, relativeTime, formatDuration, truncId, setHtml, $,
} from '../utils.js';
import {
  metricCard, dataTable, detailGrid, glassCard, skeleton,
  sparklineChart, toast,
} from '../components.js';

// --- Tab + filter state ---
let _tab = 'active';   // 'active' | 'history'
let _filter = { repo: '', branch: '' };
let _statusFilter = ''; // '' | 'working' | 'waiting_for_input' | 'idle'

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

  // Event delegation for status filter chips + terminal refreshes
  const container = $('page-sessions');
  if (container) {
    container.addEventListener('click', (e) => {
      const chip = e.target.closest('[data-status]');
      if (chip) {
        _statusFilter = chip.dataset.status || '';
        renderSessions();
        return;
      }

      const refreshBtn = e.target.closest('[data-action="refresh-tail"]');
      if (refreshBtn) {
        const sid = refreshBtn.dataset.sessionId;
        if (sid) _loadTail(sid);
      }
    });
  }
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
    ? `<button type="button" class="filter-clear-btn" id="sessions-filter-clear" title="Clear filter" aria-label="Clear sessions filter">&#x2715;</button>`
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
  const waitingSessions = sessions.filter(s => (s.inferredStatus || s.inferred_status) === 'waiting_for_input').length;
  const strip = [
    metricCard(String(sessions.length), 'Sessions', 'var(--accent-warm)'),
    metricCard(String(activeSessions), 'Active', 'var(--success)'),
    metricCard(String(waitingSessions), 'Waiting', waitingSessions > 0 ? 'var(--warning)' : 'var(--fg-muted)'),
    metricCard(String(stalledSessions), 'Stalled', stalledSessions > 0 ? 'var(--warning)' : 'var(--fg-muted)'),
    metricCard(String(assignments.length), 'Assignments', 'var(--info)'),
  ];
  const metricsHtml = `<div class="metric-strip">${strip.join('')}</div>`;
  return metricsHtml + _renderStatusFilterStrip(sessions) + _renderSessionsTable(sessions, assignments);
}

function _renderStatusFilterStrip(sessions) {
  const counts = { working: 0, waiting_for_input: 0, idle: 0 };
  for (const s of sessions) {
    const st = s.inferredStatus || s.inferred_status || 'unknown';
    if (counts[st] !== undefined) counts[st]++;
  }
  const allCls = !_statusFilter ? 'active' : '';
  const chips = [
    `<button class="repo-filter-chip ${allCls}" data-status="">All</button>`,
    `<button class="repo-filter-chip ${_statusFilter === 'working' ? 'active' : ''}" data-status="working">Working (${counts.working})</button>`,
    `<button class="repo-filter-chip ${_statusFilter === 'waiting_for_input' ? 'active' : ''}" data-status="waiting_for_input">Waiting (${counts.waiting_for_input})</button>`,
    `<button class="repo-filter-chip ${_statusFilter === 'idle' ? 'active' : ''}" data-status="idle">Idle (${counts.idle})</button>`,
  ];
  return `<div class="status-filter-strip">${chips.join('')}</div>`;
}

function _renderSessionsTable(sessions, assignments) {
  const assignmentsBySession = new Map();
  for (const a of assignments) {
    if (!assignmentsBySession.has(a.sessionId)) {
      assignmentsBySession.set(a.sessionId, []);
    }
    assignmentsBySession.get(a.sessionId).push(a);
  }

  // Client-side filter: text + status
  const filterVal = (_filter.repo || '').toLowerCase();
  let filtered = filterVal
    ? sessions.filter(s =>
        (s.repo || '').toLowerCase().includes(filterVal) ||
        (s.branch || '').toLowerCase().includes(filterVal) ||
        (s.agent || s.ownerAgent || '').toLowerCase().includes(filterVal))
    : sessions;
  if (_statusFilter) {
    filtered = filtered.filter(s => (s.inferredStatus || s.inferred_status || 'unknown') === _statusFilter);
  }
  // Attention-first sort
  const statusOrder = { waiting_for_input: 0, working: 1, idle: 2, unknown: 3 };
  filtered = [...filtered].sort((a, b) => {
    const sa = statusOrder[a.inferredStatus || a.inferred_status || 'unknown'] ?? 3;
    const sb = statusOrder[b.inferredStatus || b.inferred_status || 'unknown'] ?? 3;
    if (sa !== sb) return sa - sb;
    if (sa === 0) return new Date(a.startedAt || 0) - new Date(b.startedAt || 0);
    return new Date(b.startedAt || 0) - new Date(a.startedAt || 0);
  });

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
      key: 'inferredStatus',
      label: 'Status',
      render: r => {
        const s = r.inferredStatus || r.inferred_status || 'unknown';
        const map = {
          working:           { cls: 'status-working', label: 'Working' },
          waiting_for_input: { cls: 'status-waiting', label: 'Waiting' },
          idle:              { cls: 'status-idle',    label: 'Idle' },
          unknown:           { cls: 'status-unknown', label: '—' },
        };
        const e = map[s] || map.unknown;
        let label = esc(e.label);
        const updatedAt = r.inferredStatusUpdatedAt || r.inferred_status_updated_at;
        if (s === 'waiting_for_input' && updatedAt) {
          const waitingAge = (Date.now() - new Date(updatedAt).getTime()) / 60000;
          if (waitingAge > 10) label += ' <span class="stale-badge">stale</span>';
        }
        return `<span class="agent-status ${e.cls}">${label}</span>`;
      },
    },
    {
      key: 'activity',
      label: 'Activity',
      class: 'col-sparkline',
      render: r => `<div class="sparkline-row-placeholder" data-sparkline-for="${esc(r.id)}"></div>`,
    },
    {
      key: 'context',
      label: 'Context',
      render: r => {
        const p = r.contextPressure || 'normal';
        if (p === 'normal') return '<span style="color:var(--fg-muted)">normal</span>';
        const col = p === 'critical' ? 'var(--destructive)' : 'var(--warning)';
        const compact = r.compactCount > 0 ? ` \u00d7${r.compactCount}` : '';
        return `<span style="color:${col};font-weight:600">${esc(p)}${compact}</span>`;
      },
    },
    {
      key: 'lastIOAt',
      label: 'Last I/O',
      render: r => {
        if (!r.lastIOAt) return '<span style="color:var(--fg-muted)">—</span>';
        const age = (Date.now() - new Date(r.lastIOAt).getTime()) / 1000;
        const style = age > 90 ? 'color:var(--warning)' : 'color:var(--success)';
        return `<span style="${style}">${esc(relativeTime(r.lastIOAt))}</span>`;
      },
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
  const safeId = esc(session.id);
  
  // Left: Metadata
  const metaItems = [
    { label: 'Session ID', value: `<code>${esc(truncId(session.id, 12))}</code>` },
    { label: 'Task', value: esc(session.task || '—') },
    { label: 'Worktree', value: esc(session.worktree || '—') },
    { label: 'PR', value: session.prNumber ? esc('#' + session.prNumber) : '—' },
    { label: 'Started', value: esc(relativeTime(session.startedAt)) },
  ];
  const metaHtml = detailGrid(metaItems);

  // Center: Operational Stats + Sparkline
  const statsHtml = `
    <div class="expand-stats">
      <div class="expand-stat-group">
        <span class="detail-label">Context Pressure Trend</span>
        <div id="sparkline-${safeId}" data-sparkline-for="${safeId}" class="sparkline-placeholder">
          ${skeleton(1)}
        </div>
      </div>
      <div class="metrics-link-row">
        <a href="/api/v1/dashboard/sessions/metrics/${safeId}" target="_blank" class="drilldown-link">deep metrics →</a>
      </div>
      ${_renderAssignmentActions(assigns)}
    </div>
  `;

  // Right: Console Peek
  const consoleHtml = `
    <div class="console-peek">
      <div class="console-peek-header">
        <span>Console Peek</span>
        <div class="console-peek-actions">
          <button class="btn-ghost btn-xs" data-action="refresh-tail" data-session-id="${safeId}">refresh</button>
        </div>
      </div>
      <div id="tail-${safeId}" class="console-peek-body mono">
        <div class="skeleton-container" style="padding:0">
          <div class="skeleton-line" style="width:90%"></div>
          <div class="skeleton-line" style="width:70%"></div>
          <div class="skeleton-line" style="width:80%"></div>
        </div>
      </div>
    </div>
  `;

  return `
    <div class="session-expand-container">
      <div class="expand-col meta-col">${metaHtml}</div>
      <div class="expand-col stats-col">${statsHtml}</div>
      <div class="expand-col console-col">${consoleHtml}</div>
    </div>
  `;
}

function _renderAssignmentActions(assigns) {
  if (!assigns || assigns.length === 0) return '';
  let out = '<div class="session-actions-container">';
  for (const a of assigns) {
    out += `<div class="assignment-action-row">
      <span class="action-label">${esc(truncId(a.id, 8))}</span>
      <div class="session-actions">`;
    const actions = ['pause', 'resume', 'abandon', 'close', 'replay'];
    const destructive = new Set(['abandon', 'close']);
    for (const act of actions) {
      const cls = destructive.has(act) ? 'btn-sm destructive' : 'btn-sm';
      out += `<button class="${cls}" data-action="${esc(act)}" data-assignment-id="${esc(a.id)}">${esc(act)}</button>`;
    }
    out += `</div></div>`;
  }
  return out + '</div>';
}

// Lazy-load sparkline and tail on expansion
function _loadSparkline(sessionId) {
  const placeholders = document.querySelectorAll(`[data-sparkline-for="${sessionId}"]`);
  if (!placeholders.length) return;

  const safeId = encodeURIComponent(sessionId);
  apiFetch(`/api/v1/dashboard/sessions/metrics/${safeId}`).then(data => {
    if (!Array.isArray(data?.requests)) return;
    const values = data.requests.map(r => Number(r.cumulative_prompt_tokens) || 0);
    const chart = sparklineChart(values, { color: 'var(--accent-warm)' });
    const rowChart = sparklineChart(values, { color: 'var(--accent)', width: 60, height: 20 });

    for (const p of placeholders) {
      if (p.classList.contains('sparkline-row-placeholder')) {
        p.innerHTML = rowChart;
      } else {
        const compact = (data?.compact_count ?? 0) > 0
          ? `<span class="compact-count">compacted \xd7${data.compact_count}</span>`
          : '';
        p.innerHTML = chart + compact;
      }
      p.dataset.loaded = '1';
    }
  }).catch(() => {});
}

async function _loadTail(sessionId) {
  const el = document.getElementById(`tail-${sessionId}`);
  if (!el) return;

  try {
    const data = await loadSessionTail(sessionId, 20);
    if (!data.lines || data.lines.length === 0) {
      el.innerHTML = '<span class="text-muted">No logs available</span>';
      return;
    }
    // Render lines with simple ANSI-like color mapping if needed, or just esc
    const htmlLines = data.lines.map(l => `<div class="console-line">${esc(l)}</div>`).join('');
    el.innerHTML = htmlLines;
    el.scrollTop = el.scrollHeight;
  } catch (err) {
    el.innerHTML = `<span class="text-destructive">Failed to load logs: ${esc(err.message)}</span>`;
  }
}

// --- Expand-row toggle binding ---

function _bindExpandToggles() {
  const table = $('sessions-table');
  if (!table) return;

  table.addEventListener('click', (e) => {
    // Action buttons
    const actionBtn = e.target.closest('.btn-sm[data-action]');
    if (actionBtn) {
      e.stopPropagation();
      const { action, assignmentId } = actionBtn.dataset;
      actionBtn.disabled = true;
      sessionAction(assignmentId, action)
        .then(res => {
          toast(`${action}: ${res.message || 'done'}`, res.status === 'not_implemented' ? 'info' : 'success');
          refreshSessions();
        })
        .catch(err => toast(`${action} failed: ${err.message}`, 'error'))
        .finally(() => actionBtn.disabled = false);
      return;
    }

    const tr = e.target.closest('tr.expandable');
    if (!tr) return;
    const rowId = tr.dataset.rowId;
    const expandRow = table.querySelector(`tr.expand-row[data-expand-for="${rowId}"]`);
    if (!expandRow) return;

    const isHidden = expandRow.classList.contains('hidden');
    expandRow.classList.toggle('hidden', !isHidden);
    tr.querySelector('.chevron')?.classList.toggle('open', isHidden);

    if (isHidden) {
      _loadSparkline(rowId);
      _loadTail(rowId);
    }
  });

  // Initial load for row sparklines (if visible)
  const rowPlaceholders = table.querySelectorAll('.sparkline-row-placeholder:not([data-loaded])');
  rowPlaceholders.forEach(p => _loadSparkline(p.dataset.sparklineFor));
}

// --- History tab --- (remains largely same but updated result chips)

function _renderHistoryTab(archives) {
  const filterVal = (_filter.repo || '').toLowerCase();
  const filtered = filterVal
    ? archives.filter(a =>
        (a.repo || '').toLowerCase().includes(filterVal) ||
        (a.branch || '').toLowerCase().includes(filterVal) ||
        (a.agent || a.ownerAgent || '').toLowerCase().includes(filterVal))
    : archives;

  return _renderTimingAnalytics(filtered) + _renderHistoryTable(filtered);
}

function _renderTimingAnalytics(archives) {
  const completed = archives.filter(a => a.durationSec != null && a.durationSec >= 0);
  const avgDuration = completed.length > 0
    ? completed.reduce((sum, a) => sum + a.durationSec, 0) / completed.length
    : -1;
  const successCount = archives.filter(a => ['success', 'merged', 'completed'].includes((a.result || '').toLowerCase())).length;
  const passRate = archives.length > 0 ? Math.round((successCount / archives.length) * 100) : -1;

  const strip = [
    metricCard(archives.length, 'Total Runs', 'var(--fg-muted)'),
    metricCard(formatDuration(avgDuration), 'Avg Duration', 'var(--accent-warm)'),
    metricCard(passRate >= 0 ? passRate + '%' : '—', 'Pass Rate', passRate >= 80 ? 'var(--success)' : 'var(--warning)'),
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
    { key: 'durationSec', label: 'Duration', render: r => formatDuration(r.durationSec) },
    { key: 'endedAt', label: 'Ended', render: r => r.endedAt ? esc(relativeTime(r.endedAt)) : '—' },
  ];

  const rows = archives.map(a => ({
    ...a,
    _id: a.id || a.sessionId,
    _expandHtml: _buildHistoryExpandContent(a),
  }));

  return glassCard('Run History', dataTable('history-table', columns, rows, { expandable: true }), { padding: 'none' });
}

function _buildHistoryExpandContent(archive) {
  const items = [
    { label: 'Session ID', value: `<code>${esc(archive.sessionId || '—')}</code>` },
    { label: 'Task', value: esc(archive.taskId || '—') },
    { label: 'Started', value: archive.startedAt ? esc(new Date(archive.startedAt).toLocaleString()) : '—' },
    { label: 'Ended', value: archive.endedAt ? esc(new Date(archive.endedAt).toLocaleString()) : '—' },
  ];
  return detailGrid(items);
}

function _resultChip(result) {
  if (!result) return statusChip('unknown');
  const r = String(result).toLowerCase();
  if (['success', 'merged', 'completed'].includes(r)) return statusChip('success');
  if (['failure', 'failed', 'error'].includes(r)) return statusChip('fail');
  return statusChip(result);
}
