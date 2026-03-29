// sessions.js — Sessions & Assignments page renderer.
// Reads from the global store and renders session table, assignments, and compliance.

import store from '../store.js';
import { loadSessions, loadAssignments, loadCompliance, loadTrackingConfig, toggleAgentTracking } from '../api.js';
import {
  esc, html, statusChip, relativeTime, formatDuration, truncId, setHtml, $,
} from '../utils.js';
import { metricCard, dataTable, detailGrid, glassCard, skeleton } from '../components.js';

// --- Internal state ---
let _initialized = false;
const _unsubs = [];

// --- Public API ---

export function initSessions() {
  if (_initialized) return;
  _initialized = true;

  _unsubs.push(store.subscribe('sessions', () => renderSessions()));
  _unsubs.push(store.subscribe('assignments', () => renderSessions()));
  _unsubs.push(store.subscribe('compliance', () => renderSessions()));
  _unsubs.push(store.subscribe('trackingConfig', () => renderSessions()));
}

export async function refreshSessions() {
  await Promise.allSettled([
    loadSessions(),
    loadAssignments(),
    loadCompliance(),
    loadTrackingConfig(),
  ]);
}

export function renderSessions() {
  const container = $('page-sessions');
  if (!container) return;

  const sessions = store.select('sessions') || [];
  const assignments = store.select('assignments') || [];
  const compliance = store.select('compliance');

  // Show skeleton while initial data loads
  if (!store.select('sessions')) {
    setHtml(container, skeleton(6));
    return;
  }

  const trackingConfig = store.select('trackingConfig');
  const parts = [];

  // ---- Metric strip (4 glass cards) ----
  parts.push(_renderMetricStrip(sessions, assignments, compliance));

  // ---- Sessions table (expandable) ----
  parts.push(_renderSessionsTable(sessions, assignments));

  // ---- Agent tracking toggles ----
  parts.push(_renderTrackingPanel(sessions, trackingConfig));

  // ---- Compliance rules summary ----
  parts.push(_renderComplianceTable(compliance));

  setHtml(container, parts.join(''));

  // Bind expand-row click handlers after DOM insertion
  _bindExpandToggles();
  _bindTrackingToggles();
}

// --- Private renderers ---

function _renderMetricStrip(sessions, assignments, compliance) {
  const activeSessions = sessions.filter(
    (s) => s.state === 'active' || s.state === 'in_progress'
  ).length;
  const totalAssignments = assignments.length;
  const complianceScore = _computeComplianceScore(compliance);
  const eventCount = _countComplianceEvents(compliance);

  const cards = [
    metricCard(String(activeSessions), 'Active Sessions', 'var(--accent)'),
    metricCard(String(totalAssignments), 'Assignments', 'var(--info)'),
    metricCard(complianceScore, 'Compliance', _complianceColor(complianceScore)),
    metricCard(String(eventCount), 'Events', 'var(--fg-muted)'),
  ];
  return `<div class="metric-strip">${cards.join('')}</div>`;
}

function _computeComplianceScore(compliance) {
  if (!compliance || !compliance.rules || compliance.rules.length === 0) return '—';
  const rules = compliance.rules;
  let totalPass = 0;
  let totalChecks = 0;
  for (const r of rules) {
    totalPass += r.passed ?? 0;
    totalChecks += (r.passed ?? 0) + (r.failed ?? 0);
  }
  if (totalChecks === 0) return '—';
  return Math.round((totalPass / totalChecks) * 100) + '%';
}

function _complianceColor(score) {
  if (score === '—') return 'var(--fg-muted)';
  const v = parseInt(score, 10);
  if (v >= 90) return 'var(--success)';
  if (v >= 70) return 'var(--warning)';
  return 'var(--destructive)';
}

function _countComplianceEvents(compliance) {
  if (!compliance || !compliance.rules) return 0;
  let total = 0;
  for (const r of compliance.rules) {
    total += (r.passed ?? 0) + (r.failed ?? 0);
  }
  return total;
}

function _renderSessionsTable(sessions, assignments) {
  // Build assignment lookup by session ID for expand rows
  const assignmentsBySession = new Map();
  for (const a of assignments) {
    if (!assignmentsBySession.has(a.sessionId)) {
      assignmentsBySession.set(a.sessionId, []);
    }
    assignmentsBySession.get(a.sessionId).push(a);
  }

  const columns = [
    {
      key: 'agent',
      label: 'Agent',
      render: (r) => html`${r.agent || r.ownerAgent || '—'}`,
    },
    {
      key: 'repo',
      label: 'Repo / Branch',
      render: (r) => {
        const repo = esc(r.repo || '—');
        const branch = esc(r.branch || '');
        return branch ? `${repo} / <code>${branch}</code>` : repo;
      },
    },
    {
      key: 'task',
      label: 'Task',
      render: (r) => esc(r.task || '—'),
    },
    {
      key: 'state',
      label: 'State',
      render: (r) => statusChip(r.state || 'unknown'),
    },
    {
      key: 'lastIOAt',
      label: 'Last Output',
      render: (r) => {
        if (!r.lastIOAt) return '<span style="color:var(--fg-muted)">—</span>';
        const age = (Date.now() - new Date(r.lastIOAt).getTime()) / 1000;
        const style = age > 90 ? 'color:var(--warning)' : 'color:var(--success)';
        return `<span style="${style}">${esc(relativeTime(r.lastIOAt))}</span>`;
      },
    },
    {
      key: 'lastHeartbeat',
      label: 'Heartbeat',
      render: (r) => esc(relativeTime(r.lastHeartbeat)),
    },
    {
      key: 'elapsedSec',
      label: 'Elapsed',
      render: (r) => esc(formatDuration(r.elapsedSec)),
    },
  ];

  // Prepare rows with expand content
  const rows = sessions.map((s) => {
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
  if (assigns.length === 0) {
    // Show session-level detail only
    const items = [
      { label: 'Session ID', value: esc(truncId(session.id, 12)) },
      { label: 'Mode', value: esc(session.mode || '—') },
      { label: 'Worktree', value: esc(session.worktree || '—') },
      { label: 'PR', value: session.prNumber ? esc('#' + session.prNumber) : '—' },
      { label: 'Started', value: esc(relativeTime(session.startedAt)) },
      { label: 'Last Output', value: session.lastIOAt ? esc(relativeTime(session.lastIOAt)) : '—' },
    ];
    return detailGrid(items);
  }

  // Render each assignment as a detail grid
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
    ];
    out += detailGrid(items);
  }
  return out;
}

function _renderComplianceTable(compliance) {
  if (!compliance || !compliance.rules || compliance.rules.length === 0) {
    return glassCard('Compliance Rules', '<div class="empty-state">No compliance data</div>', {
      class: 'card-compliance',
    });
  }

  const columns = [
    { key: 'name', label: 'Rule' },
    {
      key: 'enforcement',
      label: 'Enforcement',
      render: (r) => _enforcementBadge(r.enforcement),
    },
    {
      key: 'passed',
      label: 'Passed',
      class: 'col-num',
      render: (r) => esc(String(r.passed ?? 0)),
    },
    {
      key: 'failed',
      label: 'Failed',
      class: 'col-num',
      render: (r) => {
        const f = r.failed ?? 0;
        const cls = f > 0 ? 'text-destructive' : '';
        return `<span class="${cls}">${esc(String(f))}</span>`;
      },
    },
  ];

  const rows = compliance.rules.map((r) => ({
    name: r.name || r.rule || '—',
    enforcement: r.enforcement || 'advisory',
    passed: r.passed ?? 0,
    failed: r.failed ?? 0,
  }));

  const tableHtml = dataTable('compliance-table', columns, rows, {
    empty: 'No compliance rules',
  });

  return glassCard('Compliance Rules', tableHtml, { padding: 'none', class: 'card-compliance' });
}

function _enforcementBadge(level) {
  const l = String(level).toLowerCase();
  let cls = 'badge-muted';
  if (l === 'blocking' || l === 'required') cls = 'badge-destructive';
  else if (l === 'warning' || l === 'advisory') cls = 'badge-warning';
  else if (l === 'info') cls = 'badge-info';
  return `<span class="enforcement-badge ${cls}">${esc(level)}</span>`;
}

// --- Expand-row toggle binding ---

function _bindExpandToggles() {
  const table = $('sessions-table');
  if (!table) return;

  // Remove any prior listener by re-cloning the tbody
  // (not needed on fresh renders since we replace innerHTML, but safe)
  table.addEventListener('click', (e) => {
    const tr = e.target.closest('tr.expandable');
    if (!tr) return;
    const rowId = tr.dataset.rowId;
    if (!rowId) return;

    const expandRow = table.querySelector(`tr.expand-row[data-expand-for="${rowId}"]`);
    if (!expandRow) return;

    const isHidden = expandRow.classList.contains('hidden');
    expandRow.classList.toggle('hidden', !isHidden);

    // Rotate chevron
    const chevron = tr.querySelector('.chevron');
    if (chevron) {
      chevron.classList.toggle('chevron-open', isHidden);
    }

    // Track in store UI state
    const expanded = store.select('ui').expandedRows;
    if (isHidden) {
      expanded.add(rowId);
    } else {
      expanded.delete(rowId);
    }
  });
}

function _renderTrackingPanel(sessions, trackingConfig) {
  const configAgents = (trackingConfig && trackingConfig.agents) || [];
  const activeAgents = new Set(sessions.map(s => s.agent).filter(Boolean));

  if (configAgents.length === 0 && activeAgents.size === 0) {
    return '';
  }

  // Supplement config list with active agents not yet discovered as shims.
  const configIds = new Set(configAgents.map(a => a.agent_id));
  const extraAgents = [...activeAgents]
    .filter(id => !configIds.has(id))
    .map(id => ({ agent_id: id, shim_name: id, real_binary: '', installed: true, disabled: false }));
  const agentList = [...configAgents, ...extraAgents];

  const rows = agentList.map(a => {
    const isActive = activeAgents.has(a.agent_id);
    const activeDot = isActive
      ? '<span style="color:var(--success);margin-right:4px" title="Currently active">&#9679;</span>'
      : '';
    const installedBadge = a.installed
      ? ''
      : '<span style="color:var(--destructive);font-size:10px;margin-left:4px" title="Binary not found">&#x2717; missing</span>';
    const alias = a.shim_name !== a.agent_id
      ? `<span style="color:var(--fg-muted);font-size:11px;margin-left:4px">(${esc(a.shim_name)})</span>`
      : '';
    return `<label class="tracking-toggle" style="display:flex;align-items:center;gap:6px;padding:5px 10px;margin:2px 0;border-radius:6px;background:var(--glass-bg);cursor:pointer;min-width:280px">
      <input type="checkbox" data-agent="${esc(a.agent_id)}" ${a.disabled ? '' : 'checked'} style="cursor:pointer;flex-shrink:0">
      ${activeDot}<span style="font-weight:500;min-width:70px">${esc(a.agent_id)}</span>${alias}${installedBadge}
    </label>`;
  }).join('');

  return `<div class="glass-card" style="margin-top:16px;padding:16px">
    <h3 style="margin:0 0 8px;font-size:14px;color:var(--fg-muted)">Agent Tracking</h3>
    <div style="display:flex;flex-wrap:wrap;gap:2px">${rows}</div>
    <p style="margin:8px 0 0;font-size:11px;color:var(--fg-muted)">Uncheck to disable tracking on next launch. &#9679; = active now. New shims in ~/.codero/bin/ auto-appear.</p>
  </div>`;
}

function _bindTrackingToggles() {
  const toggles = document.querySelectorAll('.tracking-toggle input[type="checkbox"]');
  for (const el of toggles) {
    el.addEventListener('change', async (e) => {
      const agent = e.target.dataset.agent;
      const disabled = !e.target.checked;
      const prevChecked = !e.target.checked; // previous state before user clicked
      e.target.disabled = true;
      try {
        await toggleAgentTracking(agent, disabled);
      } catch (err) {
        e.target.checked = prevChecked;
      } finally {
        e.target.disabled = false;
      }
    });
  }
}
