// agents.js — Agents page renderer: fleet roster, load distribution, tracking toggles.

import store from '../store.js';
import { loadAgents, loadAgentSessions, loadTrackingConfig, toggleAgentTracking } from '../api.js';
import {
  esc, html, statusChip, relativeTime, formatDuration, setHtml, $,
} from '../utils.js';
import { metricCard, dataTable, detailGrid, glassCard, skeleton, barChart, toast } from '../components.js';

// --- Internal state ---
let _initialized = false;

// --- Public API ---

export function initAgents() {
  if (_initialized) return;
  _initialized = true;
  store.subscribe('agents', () => renderAgents());
  store.subscribe('trackingConfig', () => renderAgents());
}

export async function refreshAgents() {
  const results = await Promise.allSettled([loadAgents(), loadTrackingConfig()]);
  if (results.some(r => r.status === 'rejected')) {
    toast('Some agent data failed to load', 'error');
  }
}

export function renderAgents() {
  const container = $('page-agents');
  if (!container) return;
  if (store.state.ui.activeTab !== 'agents') return;

  if (!store.select('agents')) {
    setHtml(container, skeleton(6));
    return;
  }

  const agents = store.select('agents') || [];
  const trackingConfig = store.select('trackingConfig');

  const parts = [
    _renderMetricStrip(agents),
    _renderRosterTable(agents),
    _renderLoadDistribution(agents),
    _renderTrackingPanel(agents, trackingConfig),
  ];
  setHtml(container, parts.join(''));

  _bindTrackingToggles();
  _bindExpandToggles();
}

// --- Private renderers ---

function _renderMetricStrip(agents) {
  const total = agents.length;
  const active = agents.filter(a => a.status === 'active').length;
  const idle = agents.filter(a => a.status === 'idle').length;
  const offline = agents.filter(a => a.status === 'offline').length;

  const cards = [
    metricCard(String(total), 'Total Agents', 'var(--fg-muted)'),
    metricCard(String(active), 'Active Now', 'var(--success)'),
    metricCard(String(idle), 'Idle', 'var(--fg-muted)'),
    metricCard(String(offline), 'Offline', 'var(--warning)'),
  ];
  return `<div class="metric-strip">${cards.join('')}</div>`;
}

function _statusChipAgent(status) {
  const map = {
    active: 'active',
    idle: 'idle',
    offline: 'warning',
    disabled: 'disabled',
  };
  return statusChip(map[status] || status);
}

function _renderRosterTable(agents) {
  const columns = [
    {
      key: 'agentId',
      label: 'Agent',
      render: r => esc(r.agentId || '—'),
    },
    {
      key: 'status',
      label: 'Status',
      render: r => _statusChipAgent(r.status),
    },
    {
      key: 'activeSessions',
      label: 'Active',
      class: 'col-num',
      render: r => esc(String(r.activeSessions)),
    },
    {
      key: 'totalSessions',
      label: 'Total (30d)',
      class: 'col-num',
      render: r => esc(String(r.totalSessions)),
    },
    {
      key: 'avgElapsedSec',
      label: 'Avg Elapsed',
      render: r => r.avgElapsedSec > 0 ? esc(formatDuration(r.avgElapsedSec)) : '<span style="color:var(--fg-muted)">—</span>',
    },
    {
      key: 'tokensPerSec',
      label: 'Tokens/sec',
      class: 'col-num',
      render: r => (r.tokensPerSec ?? 0) > 0
        ? `<span title="${(r.totalTokens ?? 0).toLocaleString()} total tokens">${(r.tokensPerSec ?? 0).toFixed(1)}</span>`
        : '<span style="color:var(--fg-muted)">—</span>',
    },
    {
      key: 'lastSeen',
      label: 'Last Seen',
      render: r => r.lastSeen ? esc(relativeTime(r.lastSeen)) : '<span style="color:var(--fg-muted)">—</span>',
    },
  ];

  const rows = agents.map(a => ({
    ...a,
    _id: a.agentId,
    _expandHtml: _buildAgentExpandContent(a),
  }));

  const tableHtml = dataTable('agents-table', columns, rows, {
    expandable: true,
    empty: 'No agents discovered',
  });

  return glassCard('Agent Roster', tableHtml, { padding: 'none', class: 'card-agents' });
}

function _buildAgentExpandContent(agent) {
  const pressureLabel = agent.activePressure === 'critical'
    ? `<span class="pressure-dot critical"></span>${esc(agent.activePressure)}`
    : agent.activePressure === 'warning'
      ? `<span class="pressure-dot warning"></span>${esc(agent.activePressure)}`
      : '<span style="color:var(--fg-muted)">normal</span>';

  const items = [
    { label: 'Agent ID', value: `<code>${esc(agent.agentId)}</code>` },
    { label: 'Status', value: _statusChipAgent(agent.status) },
    { label: 'Total Tokens (30d)', value: esc((agent.totalTokens ?? 0).toLocaleString()) },
    { label: 'Tokens/sec', value: (agent.tokensPerSec ?? 0) > 0 ? esc((agent.tokensPerSec ?? 0).toFixed(2)) : '—' },
    { label: 'Avg Elapsed', value: agent.avgElapsedSec > 0 ? esc(formatDuration(agent.avgElapsedSec)) : '—' },
    { label: 'Active Pressure', value: pressureLabel },
  ];
  const sessionsPlaceholder = `<div id="agent-sessions-${esc(agent.agentId)}" style="margin-top:12px">` +
    `<div class="skeleton" style="height:72px;border-radius:6px"></div></div>`;
  return detailGrid(items) + sessionsPlaceholder;
}

// --- Expand toggle + lazy session history ---

function _bindExpandToggles() {
  const table = $('agents-table');
  if (!table) return;
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
    if (isHidden) _loadAgentSessions(rowId);
  });
}

async function _loadAgentSessions(agentId) {
  const container = document.getElementById(`agent-sessions-${agentId}`);
  if (!container || container.dataset.loaded) return;

  let data;
  try {
    data = await loadAgentSessions(agentId);
  } catch (_) {
    setHtml(container, '<span style=\"color:var(--fg-muted);font-size:12px\">Could not load session history</span>');
    return;
  }

  const sessions = data?.sessions || [];
  if (sessions.length === 0) {
    container.dataset.loaded = '1';
    setHtml(container, '<span style=\"color:var(--fg-muted);font-size:12px\">No recent sessions</span>');
    return;
  }

  const rows = sessions.map(s => {
    const elapsedSec = s.ended_at
      ? (new Date(s.ended_at) - new Date(s.started_at)) / 1000
      : null;
    const elapsed = elapsedSec != null ? esc(formatDuration(elapsedSec)) : '—';
    const state = s.ended_at
      ? `<span style=\"color:var(--fg-muted)\">${esc(s.end_reason || 'done')}</span>`
      : statusChip('active');
    return `<tr>
      <td style=\"padding:2px 8px 2px 0\">${esc(relativeTime(s.started_at))}</td>
      <td style=\"padding:2px 8px 2px 0\">${esc(s.repo || '—')}</td>
      <td style=\"padding:2px 8px 2px 0\"><code>${esc(s.branch || '—')}</code></td>
      <td style=\"padding:2px 8px 2px 0\">${elapsed}</td>
      <td style=\"padding:2px 0\">${state}</td>
    </tr>`;
  }).join('');

  container.dataset.loaded = '1';
  setHtml(container, `
    <h4 style=\"margin:0 0 6px;font-size:12px;color:var(--fg-muted)\">Recent Sessions</h4>
    <table style=\"width:100%;font-size:12px;border-collapse:collapse\">
      <thead><tr style=\"color:var(--fg-muted)\">
        <th style=\"text-align:left;padding:2px 8px 2px 0;font-weight:500\">Started</th>
        <th style=\"text-align:left;padding:2px 8px 2px 0;font-weight:500\">Repo</th>
        <th style=\"text-align:left;padding:2px 8px 2px 0;font-weight:500\">Branch</th>
        <th style=\"text-align:left;padding:2px 8px 2px 0;font-weight:500\">Elapsed</th>
        <th style=\"text-align:left;padding:2px 0;font-weight:500\">State</th>
      </tr></thead>
      <tbody>${rows}</tbody>
    </table>`);
}

function _renderLoadDistribution(agents) {
  const active = agents.filter(a => a.activeSessions > 0);
  if (active.length === 0) {
    return glassCard('Load Distribution', '<div class="empty-state">No active sessions</div>', { class: 'card-load-dist' });
  }
  const items = active.map(a => ({ label: a.agentId, value: a.activeSessions }));
  return glassCard('Load Distribution', barChart(items), { class: 'card-load-dist' });
}

function _renderTrackingPanel(agents, trackingConfig) {
  const configAgents = (trackingConfig && trackingConfig.agents) || [];
  const activeAgents = new Set(agents.map(a => a.agentId).filter(Boolean));

  if (configAgents.length === 0 && activeAgents.size === 0) return '';

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
  const toggles = document.querySelectorAll('#page-agents .tracking-toggle input[type="checkbox"]');
  for (const el of toggles) {
    el.addEventListener('change', async (e) => {
      const agent = e.target.dataset.agent;
      const disabled = !e.target.checked;
      // Capture original state (before the change) to enable rollback on failure.
      const originalChecked = !e.target.checked;
      e.target.disabled = true;
      try {
        await toggleAgentTracking(agent, disabled);
      } catch (_err) {
        e.target.checked = originalChecked;
      } finally {
        e.target.disabled = false;
      }
    });
  }
}
