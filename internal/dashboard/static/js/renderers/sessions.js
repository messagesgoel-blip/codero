// sessions.js — Sessions page renderer for live runtime instances.
// One agent profile can own multiple concurrent sessions.

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
let _statusFilter = ''; // '' | 'working' | 'waiting' | 'idle' | 'orphaned'

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

  const totalCount = _tab === 'active' ? sessions.length : archives.length;
  const filteredCount = (_tab === 'active' ? sessions : archives)
    .filter(s => _matchesSessionFilter(s, _tab === 'active')).length;

  const parts = [
    _renderTabBar(totalCount, filteredCount),
    _tab === 'active'
      ? _renderActiveTab(sessions, assignments)
      : _renderHistoryTab(archives),
  ];
  setHtml(container, parts.join(''));

  _bindTabBar();
  _bindExpandToggles();
  _loadActivityBars();
}

// --- Tab bar ---

function _renderTabBar(totalCount, filteredCount) {
  const activeBtn = `<button class="tab-btn${_tab === 'active' ? ' active' : ''}" data-tab="active">Active</button>`;
  const historyBtn = `<button class="tab-btn${_tab === 'history' ? ' active' : ''}" data-tab="history">History</button>`;
  const filterVal = _filter.repo || _filter.branch || '';
  const filterInput = `<input class="filter-input" id="sessions-filter" placeholder="filter session, agent, repo / branch…" value="${esc(filterVal)}">`;
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
  const workingSessions = sessions.filter(s => _sessionBucket(s) === 'working').length;
  const waitingSessions = sessions.filter(s => _sessionBucket(s) === 'waiting').length;
  const orphanedSessions = sessions.filter(s => s.attachmentState === 'orphaned').length;
  const attachedSessions = sessions.filter(s => s.attachmentState === 'attached').length;
  const strip = [
    metricCard(String(sessions.length), 'Runtime Sessions', 'var(--accent-warm)'),
    metricCard(String(workingSessions), 'Working', 'var(--success)'),
    metricCard(String(waitingSessions), 'Waiting', waitingSessions > 0 ? 'var(--warning)' : 'var(--fg-muted)'),
    metricCard(String(attachedSessions), 'Attached', 'var(--info)'),
    metricCard(String(orphanedSessions), 'Orphaned', orphanedSessions > 0 ? 'var(--warning)' : 'var(--fg-muted)'),
    metricCard(String(assignments.length), 'Assignments', 'var(--fg-muted)'),
  ];
  const metricsHtml = `<div class="metric-strip">${strip.join('')}</div>`;
  const explainer = glassCard('Runtime Instances', `
    <div style="font-weight:600;margin-bottom:6px">Sessions are runtime instances. Agents are reusable profiles.</div>
    <div style="color:var(--fg-muted);font-size:12px;line-height:1.5">
      This page tracks live session identity, runtime lifecycle, attachment, attribution, and activity.
      Configure aliases, permission/home strategy, and tracking on the <code>Agents</code> page.
    </div>
  `, { class: 'card-sessions', padding: 'sm' });
  return explainer + '<div style="height:16px"></div>' + metricsHtml + _renderStatusFilterStrip(sessions) + _renderSessionsTable(sessions, assignments);
}

function _renderStatusFilterStrip(sessions) {
  const counts = { working: 0, waiting: 0, idle: 0, orphaned: 0 };
  for (const s of sessions) {
    const st = _sessionBucket(s);
    if (counts[st] !== undefined) counts[st]++;
  }
  const allCls = !_statusFilter ? 'active' : '';
  const chips = [
    `<button class="repo-filter-chip ${allCls}" data-status="">All</button>`,
    `<button class="repo-filter-chip ${_statusFilter === 'working' ? 'active' : ''}" data-status="working">Working (${counts.working})</button>`,
    `<button class="repo-filter-chip ${_statusFilter === 'waiting' ? 'active' : ''}" data-status="waiting">Waiting (${counts.waiting})</button>`,
    `<button class="repo-filter-chip ${_statusFilter === 'idle' ? 'active' : ''}" data-status="idle">Idle (${counts.idle})</button>`,
    `<button class="repo-filter-chip ${_statusFilter === 'orphaned' ? 'active' : ''}" data-status="orphaned">Orphaned (${counts.orphaned})</button>`,
  ];
  return `<div class="status-filter-strip">${chips.join('')}</div>`;
}

function _sessionBucket(session) {
  if (!session) return 'other';
  if (session.attachmentState === 'orphaned') return 'orphaned';

  const activity = String(session.activityState || session.state || '').toLowerCase();
  const inferred = String(session.inferredStatus || '').toLowerCase();

  if (activity === 'waiting_input' || inferred === 'waiting_for_input') return 'waiting';
  if (activity === 'idle' || inferred === 'idle') return 'idle';
  if (['starting', 'thinking', 'editing', 'running_command', 'syncing', 'blocked'].includes(activity)) return 'working';
  if (inferred === 'working') return 'working';
  return 'other';
}

function _matchesSessionFilter(session, includeStatusFilter = true) {
  const filterVal = (_filter.repo || '').toLowerCase();
  if (filterVal) {
    const matchesText =
      (session.repo || '').toLowerCase().includes(filterVal) ||
      (session.branch || '').toLowerCase().includes(filterVal) ||
      (session.agent || session.ownerAgent || '').toLowerCase().includes(filterVal) ||
      (session.id || '').toLowerCase().includes(filterVal);
    if (!matchesText) return false;
  }
  if (includeStatusFilter && _statusFilter && _sessionBucket(session) !== _statusFilter) {
    return false;
  }
  return true;
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
  let filtered = sessions.filter(s => _matchesSessionFilter(s, true));
  // Attention-first sort
  const statusOrder = { waiting: 0, working: 1, orphaned: 2, idle: 3, other: 4 };
  filtered = [...filtered].sort((a, b) => {
    const sa = statusOrder[_sessionBucket(a)] ?? 4;
    const sb = statusOrder[_sessionBucket(b)] ?? 4;
    if (sa !== sb) return sa - sb;
    if (sa === 0) return new Date(a.startedAt || 0) - new Date(b.startedAt || 0);
    return new Date(b.startedAt || 0) - new Date(a.startedAt || 0);
  });

  const columns = [
    {
      key: 'agent',
      label: 'Agent',
      render: r => {
        const agent = esc(r.agent || r.ownerAgent || '—');
        const family = r.family ? ` <span class="mode-badge" style="margin-left:6px">${esc(r.family)}</span>` : '';
        return `${agent}${family}`;
      },
    },
    {
      key: 'session',
      label: 'Session',
      render: r => {
        const sessionID = r.id ? `<code>${esc(truncId(r.id, 12))}</code>` : '<span style="color:var(--fg-muted)">—</span>';
        const launch = r.launchMode ? `<div style="margin-top:6px">${statusChip(r.launchMode)}</div>` : '';
        return `${sessionID}${launch}`;
      },
    },
    {
      key: 'repo',
      label: 'Repo / Branch / Task',
      render: r => {
        const repo = esc(r.repo || '—');
        const branch = esc(r.branch || '');
        const task = r.task && r.task.id ? `<div style="margin-top:6px;color:var(--fg-muted);font-size:11px">${esc(r.task.id)}</div>` : '';
        return `${branch ? `${repo} / <code>${branch}</code>` : repo}${task}`;
      },
    },
    {
      key: 'runtime',
      label: 'Runtime',
      render: r => {
        const mode = r.mode ? `<span class="mode-badge">${esc(r.mode)}</span>` : '';
        const lifecycle = statusChip(r.lifecycleState || 'unknown');
        const attachment = statusChip(r.attachmentState || 'unknown');
        return `${mode}<div style="margin-top:6px">${lifecycle} ${attachment}</div>`;
      },
    },
    {
      key: 'activityState',
      label: 'Activity',
      render: r => {
        const activity = statusChip(r.activityState || 'unknown');
        const inferred = r.inferredStatus ? `<div style="margin-top:6px;color:var(--fg-muted);font-size:11px">${esc(r.inferredStatus)}</div>` : '';
        return `${activity}${inferred}`;
      },
    },
    {
      key: 'attribution',
      label: 'Attribution',
      render: r => {
        const source = statusChip(r.attributionSource || 'unknown');
        const confidence = r.attributionConfidence ? `<div style="margin-top:6px;color:var(--fg-muted);font-size:11px">${esc(r.attributionConfidence)}</div>` : '';
        return `${source}${confidence}`;
      },
    },
    {
      key: 'activity',
      label: 'Signals',
      class: 'col-sparkline',
      render: r => {
        return `<div class="activity-bar-placeholder" data-activity-for="${esc(r.id)}" style="width:120px;height:20px"></div>`;
      },
    },
    {
      key: 'lastIOAt',
      label: 'Last Activity',
      render: r => {
        const ts = r.lastActivityAt || r.lastIOAt;
        const io = ts ? `<span style="display:block">${esc(relativeTime(ts))}</span>` : '<span style="color:var(--fg-muted)">—</span>';
        const out = r.outputMb ? `<span style="font-size:0.65rem;color:var(--fg-muted)">out: ${esc(r.outputMb.toFixed(1))} MB</span>` : '';
        const pressure = r.contextPressure && r.contextPressure !== 'normal'
          ? `<span style="font-size:0.65rem;color:var(--warning)">pressure: ${esc(r.contextPressure)}</span>`
          : '';
        return `<div>${io}<div>${out}${out && pressure ? ' · ' : ''}${pressure}</div></div>`;
      },
    },
    {
      key: 'elapsedSec',
      label: 'Runtime',
      render: r => {
        const total = `<span style="display:block">${esc(formatDuration(r.elapsedSec))}</span>`;
        const work = r.workingDurationSec ? `<span style="font-size:0.65rem;color:var(--success)">w: ${esc(formatDuration(r.workingDurationSec))}</span>` : '';
        const idle = r.idleDurationSec ? `<span style="font-size:0.65rem;color:var(--warning);margin-left:6px">i: ${esc(formatDuration(r.idleDurationSec))}</span>` : '';
        return `<div>${total}<div>${work}${idle}</div></div>`;
      },
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
  const safeIdHtml = esc(session.id);
  const safeIdUrl = encodeURIComponent(session.id);
  
  // Left: Metadata
  const metaItems = [
    { label: 'Session ID', value: `<code>${esc(truncId(session.id, 12))}</code>` },
    { label: 'Agent', value: esc(session.agent || session.ownerAgent || '—') },
    { label: 'Family', value: esc(session.family || '—') },
    { label: 'Launch', value: esc(session.launchMode || '—') },
    { label: 'Lifecycle', value: esc(session.lifecycleState || '—') },
    { label: 'Activity', value: esc(session.activityState || '—') },
    { label: 'Attachment', value: esc(session.attachmentState || '—') },
    { label: 'Attribution', value: esc(session.attributionSource || '—') },
    { label: 'Task', value: esc(session.task?.id || '—') },
    { label: 'Worktree', value: esc(session.worktree || '—') },
    { label: 'PR', value: session.prNumber ? esc('#' + session.prNumber) : '—' },
    { label: 'Started', value: esc(relativeTime(session.startedAt)) },
    { label: 'Last Activity', value: session.lastActivityAt ? esc(relativeTime(session.lastActivityAt)) : '—' },
    { label: 'Working', value: session.workingDurationSec ? esc(formatDuration(session.workingDurationSec)) : '—' },
    { label: 'Idle', value: session.idleDurationSec ? esc(formatDuration(session.idleDurationSec)) : '—' },
    { label: 'Output', value: session.outputMb ? esc(session.outputMb.toFixed(2)) + ' MB' : '—' },
  ];
  const metaHtml = detailGrid(metaItems);

  // Center: Operational Stats + Sparkline
  const statsHtml = `
    <div class="expand-stats">
      <div class="expand-stat-group">
        <span class="detail-label">Context Pressure Trend</span>
        <div id="sparkline-${safeIdHtml}" data-sparkline-for="${safeIdHtml}" class="sparkline-placeholder">
          ${skeleton(1)}
        </div>
      </div>
      <div class="metrics-link-row">
        <a href="/api/v1/dashboard/sessions/metrics/${safeIdUrl}" target="_blank" class="drilldown-link">deep metrics →</a>
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
          <button class="btn-ghost btn-xs" data-action="refresh-tail" data-session-id="${safeIdHtml}">refresh</button>
        </div>
      </div>
      <div id="tail-${safeIdHtml}" class="console-peek-body mono">
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
  const placeholders = document.querySelectorAll(`[data-sparkline-for="${CSS.escape(sessionId)}"]`);
  if (!placeholders.length) return;

  const safeId = encodeURIComponent(sessionId);
  apiFetch(`/api/v1/dashboard/sessions/metrics/${safeId}`).then(data => {
    if (!Array.isArray(data?.requests)) return;
    const values = data.requests.map(r => Number(r.cumulative_prompt_tokens) || 0);
    const chart = sparklineChart(values, { color: 'var(--accent-warm)' });
    const rowChart = sparklineChart(values, { color: 'var(--accent)', width: 60, height: 20 });

    for (const p of placeholders) {
      if (p.classList.contains('sparkline-row-placeholder')) {
        setHtml(p, rowChart);
      } else {
        const compact = (data?.compact_count ?? 0) > 0
          ? `<span class="compact-count">compacted \xd7${data.compact_count}</span>`
          : '';
        setHtml(p, chart + compact);
      }
      p.dataset.loaded = '1';
    }
  }).catch(err => {
    // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    if (location.hostname === 'localhost' || location.hostname === '127.0.0.1') {
      console.warn('Sparkline load failed:', err);
    }
  });
}

// Render output activity bars for all visible sessions.
// Each bar shows output byte deltas over the last 30 minutes in 1-min buckets.
// Active minutes are colored, idle/rate-limited minutes are grey gaps.
function _loadActivityBars() {
  const placeholders = document.querySelectorAll('[data-activity-for]:not([data-loaded])');
  for (const p of placeholders) {
    const sessionId = p.dataset.activityFor;
    if (!sessionId) continue;
    p.dataset.loaded = '1';

    const safeId = encodeURIComponent(sessionId);
    apiFetch(`/api/v1/dashboard/sessions/metrics/${safeId}`).then(data => {
      const activity = data?.activity;
      if (!Array.isArray(activity) || activity.length === 0) {
        setHtml(p, _activityBarEmpty());
        return;
      }
      setHtml(p, _activityBarSVG(activity));
    }).catch(() => {
      setHtml(p, _activityBarEmpty());
    });
  }
}

function _activityBarEmpty() {
  return `<svg width="120" height="20" viewBox="0 0 120 20" class="activity-bar">
    <rect x="0" y="8" width="120" height="4" rx="2" fill="var(--bg-muted)" opacity="0.3"/>
  </svg>`;
}

function _activityBarSVG(activity) {
  // activity: [{bucket, delta_bytes}, ...]
  // Render as a bar chart: 30 slots (last 30 minutes), each 4px wide with 0px gap.
  const w = 120, h = 20, slots = 30;
  const barW = w / slots;

  // Build a map of minute buckets to delta values.
  const bucketMap = {};
  let maxDelta = 1;
  for (const a of activity) {
    const d = Number(a.delta_bytes) || 0;
    bucketMap[a.bucket] = d;
    if (d > maxDelta) maxDelta = d;
  }

  // Generate 30 minute buckets ending at current minute.
  const now = new Date();
  const bars = [];
  for (let i = slots - 1; i >= 0; i--) {
    const t = new Date(now.getTime() - i * 60000);
    const key = t.toISOString().slice(0, 16); // "2026-04-03T10:05"
    const val = bucketMap[key] || 0;
    bars.push(val);
  }

  // Render SVG bars.
  let svg = `<svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" class="activity-bar">`;
  // Background track.
  svg += `<rect x="0" y="0" width="${w}" height="${h}" rx="2" fill="var(--bg-muted)" opacity="0.15"/>`;

  for (let i = 0; i < bars.length; i++) {
    const val = bars[i];
    const x = i * barW;
    if (val > 0) {
      const barH = Math.max(2, (val / maxDelta) * (h - 2));
      const y = h - barH;
      const color = val > maxDelta * 0.7 ? 'var(--success)' : 'var(--accent)';
      svg += `<rect x="${x.toFixed(1)}" y="${y.toFixed(1)}" width="${(barW - 0.5).toFixed(1)}" height="${barH.toFixed(1)}" rx="1" fill="${color}" opacity="0.8"/>`;
    }
  }
  svg += '</svg>';
  return svg;
}

async function _loadTail(sessionId) {
  const el = document.getElementById(`tail-${sessionId}`);
  if (!el) return;

  try {
    const data = await loadSessionTail(sessionId, 20);
    if (!data.lines || data.lines.length === 0) {
      setHtml(el, '<span class="text-muted">No logs available</span>');
      return;
    }
    // Render lines with simple ANSI-like color mapping if needed, or just esc
    const htmlLines = data.lines.map(l => `<div class="console-line">${esc(l)}</div>`).join('');
    setHtml(el, htmlLines);
    el.scrollTop = el.scrollHeight;
  } catch (err) {
    setHtml(el, `<span class="text-destructive">Failed to load logs: ${esc(err.message)}</span>`);
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
          return refreshSessions();
        })
        .catch(err => toast(`${action} failed: ${err.message}`, 'error'))
        .finally(() => actionBtn.disabled = false);
      return;
    }

    const tr = e.target.closest('tr.expandable');
    if (!tr) return;
    const rowId = tr.dataset.rowId;
    const expandRow = table.querySelector(`tr.expand-row[data-expand-for="${CSS.escape(rowId)}"]`);
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
  const filtered = archives.filter(a => _matchesSessionFilter(a, false));

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
