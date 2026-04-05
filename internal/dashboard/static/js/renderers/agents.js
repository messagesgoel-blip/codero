// agents.js — Agents page renderer: profile inventory, aliases, setup, and runtime summary.

import store from '../store.js';
import { loadAgents, loadSessions, loadTrackingConfig, toggleAgentTracking, updateAgentEnvVars } from '../api.js';
import {
  esc, statusChip, relativeTime, setHtml, $,
} from '../utils.js';
import { dataTable, glassCard, metricCard, skeleton, showModal, toast } from '../components.js';

// --- Internal state ---
let _initialized = false;

// --- Public API ---

export function initAgents() {
  if (_initialized) return;
  _initialized = true;
  store.subscribe('agents', () => renderAgents());
  store.subscribe('sessions', () => renderAgents());
  store.subscribe('trackingConfig', () => renderAgents());
}

export async function refreshAgents() {
  const results = await Promise.allSettled([loadAgents(), loadSessions(), loadTrackingConfig()]);
  if (results.some(r => r.status === 'rejected')) {
    toast('Some agent data failed to load', 'error');
  }
}

export function renderAgents() {
  const container = $('page-agents');
  if (!container) return;
  if (store.state.ui.activeTab !== 'agents') return;

  if (!store.select('agents') || !store.select('trackingConfig')) {
    setHtml(container, skeleton(6));
    return;
  }

  const agents = store.select('agents') || [];
  const sessions = store.select('sessions') || [];
  const trackingConfig = store.select('trackingConfig');
  const profiles = _buildAgentProfiles(agents, sessions, trackingConfig);

  const parts = [
    _renderProfileSummary(profiles, sessions),
    '<div style="height:16px"></div>',
    _renderAgentProfiles(profiles),
  ];
  setHtml(container, parts.join(''));

  _bindTrackingToggles();
  _bindEnvVarButtons(trackingConfig);
}

function _buildAgentProfiles(agents, sessions, trackingConfig) {
  const rosterByID = new Map((agents || []).map(agent => [agent.agentId, agent]));
  const configAgents = (trackingConfig && trackingConfig.agents) || [];
  const configByID = new Map(configAgents.map(agent => [agent.agent_id, agent]));
  const liveSessionsByID = new Map();

  for (const session of sessions || []) {
    const agentID = session.agentId || session.agent || session.ownerAgent;
    if (!agentID) continue;
    if (!liveSessionsByID.has(agentID)) {
      liveSessionsByID.set(agentID, []);
    }
    liveSessionsByID.get(agentID).push(session);
  }

  const ids = new Set([
    ...rosterByID.keys(),
    ...configByID.keys(),
    ...liveSessionsByID.keys(),
  ]);

  const rows = [];
  for (const agentID of ids) {
    const config = configByID.get(agentID) || null;
    const roster = rosterByID.get(agentID) || null;
    const liveSessions = liveSessionsByID.get(agentID) || [];
    const aliases = _uniqueStrings([
      ...(config?.aliases || []),
      config?.primary_alias || '',
      config?.shim_name || '',
    ]);
    const primaryAlias = config?.primary_alias || config?.shim_name || aliases[0] || '';
    const liveCount = liveSessions.length;
    const orphanedCount = liveSessions.filter(s => s.attachmentState === 'orphaned').length;
    const inferredCount = liveSessions.filter(s => s.attachmentState === 'inferred').length;
    const duplicateCount = liveCount > 1 ? liveCount - 1 : 0;
    const pressure = _profilePressure(roster?.activePressure, liveSessions);
    const tracked = config ? !config.disabled : (roster ? roster.status !== 'disabled' : false);
    const installed = config ? !!config.installed : false;
    const lastUsed = _latestTimestamp([
      roster?.lastSeen,
      ...liveSessions.map(session => session.lastActivityAt || session.lastHeartbeat || session.startedAt),
    ]);
    const source = config ? 'managed' : 'observed';
    const status = liveCount > 0 ? 'live' : (roster ? roster.status : (config ? 'idle' : 'observed'));

    rows.push({
      _id: agentID,
      agentId: agentID,
      family: config?.agent_kind || '',
      source,
      aliases,
      primaryAlias,
      installed,
      tracked,
      permissionProfile: config?.permission_profile || '',
      homeStrategy: config?.home_strategy || '',
      homeDir: config?.home_dir || '',
      liveCount,
      orphanedCount,
      inferredCount,
      duplicateCount,
      pressure,
      lastUsed,
      status,
    });
  }

  rows.sort((a, b) => {
    if (b.liveCount !== a.liveCount) return b.liveCount - a.liveCount;
    if (a.installed !== b.installed) return a.installed ? -1 : 1;
    return a.agentId.localeCompare(b.agentId);
  });
  return rows;
}

function _renderProfileSummary(profiles, sessions) {
  const liveProfiles = profiles.filter(profile => profile.liveCount > 0).length;
  const duplicateProfiles = profiles.filter(profile => profile.duplicateCount > 0).length;
  const constrainedProfiles = profiles.filter(profile => profile.pressure && profile.pressure !== 'normal').length;
  const disabledProfiles = profiles.filter(profile => !profile.tracked).length;

  const metrics = `
    <div class="metric-strip">
      ${metricCard(String(profiles.length), 'Profiles', 'var(--accent-warm)')}
      ${metricCard(String(sessions.length), 'Live Sessions', 'var(--success)')}
      ${metricCard(String(liveProfiles), 'Live Agents', 'var(--info)')}
      ${metricCard(String(duplicateProfiles), 'Duplicate Instances', duplicateProfiles > 0 ? 'var(--warning)' : 'var(--fg-muted)')}
      ${metricCard(String(constrainedProfiles), 'Constrained', constrainedProfiles > 0 ? 'var(--warning)' : 'var(--fg-muted)')}
      ${metricCard(String(disabledProfiles), 'Disabled', disabledProfiles > 0 ? 'var(--destructive)' : 'var(--fg-muted)')}
    </div>`;

  const explainer = `
    <div style="display:flex;justify-content:space-between;gap:16px;align-items:flex-start;flex-wrap:wrap">
      <div style="max-width:720px">
        <div style="font-weight:600;margin-bottom:6px">Agent profiles are setup entities. Sessions are runtime entities.</div>
        <div style="color:var(--fg-muted);font-size:12px;line-height:1.5">
          Use this page to inspect profile identity, aliases, permission/home configuration, tracking, env vars, and whether a profile currently has duplicate live instances.
          Use <code>Sessions</code> for the per-runtime view.
        </div>
      </div>
    </div>`;

  return glassCard('Agent Profiles', `${explainer}<div style="height:12px"></div>${metrics}`, { class: 'card-agents' });
}

function _renderAgentProfiles(profiles) {
  const columns = [
    {
      key: 'agentId',
      label: 'Profile',
      render: profile => {
        let label = `<span style="font-weight:600">${esc(profile.agentId)}</span>`;
        if (profile.family) {
          label += ` <span class="mode-badge" style="margin-left:6px">${esc(profile.family)}</span>`;
        }
        label += ` <span style="color:var(--fg-muted);font-size:11px;margin-left:6px">${esc(profile.source)}</span>`;
        if (!profile.installed) {
          label += ` <span style="color:var(--destructive);font-size:10px;margin-left:6px" title="Binary not found">&#x2717; missing</span>`;
        }
        return label;
      },
    },
    {
      key: 'aliases',
      label: 'Aliases',
      render: profile => {
        if (!profile.aliases.length) {
          return '<span style="color:var(--fg-muted)">—</span>';
        }
        const extras = profile.aliases.filter(alias => alias !== profile.primaryAlias);
        const primary = profile.primaryAlias ? `<code>${esc(profile.primaryAlias)}</code>` : '<span style="color:var(--fg-muted)">—</span>';
        const secondary = extras.length
          ? `<div style="margin-top:6px;color:var(--fg-muted);font-size:11px">${esc(extras.join(', '))}</div>`
          : '';
        return `${primary}${secondary}`;
      },
    },
    {
      key: 'setup',
      label: 'Setup',
      render: profile => {
        const badges = [];
        if (profile.permissionProfile) badges.push(statusChip(profile.permissionProfile));
        if (profile.homeStrategy) badges.push(statusChip(profile.homeStrategy));
        if (!badges.length) badges.push('<span style="color:var(--fg-muted)">—</span>');
        const homeDir = profile.homeDir
          ? `<div style="margin-top:6px;color:var(--fg-muted);font-size:11px" title="${esc(profile.homeDir)}">${esc(_abbrevPath(profile.homeDir))}</div>`
          : '';
        return `${badges.join(' ')}${homeDir}`;
      },
    },
    {
      key: 'runtime',
      label: 'Runtime Summary',
      render: profile => {
        const status = statusChip(profile.status);
        const live = `<div style="margin-top:6px">${profile.liveCount > 0 ? `${profile.liveCount} live session${profile.liveCount === 1 ? '' : 's'}` : '<span style="color:var(--fg-muted)">no live sessions</span>'}</div>`;
        const flags = [];
        if (profile.duplicateCount > 0) flags.push(statusChip('duplicate'));
        if (profile.orphanedCount > 0) flags.push(statusChip('orphaned'));
        if (profile.inferredCount > 0) flags.push(statusChip('inferred'));
        const lastUsed = profile.lastUsed
          ? `<div style="margin-top:6px;color:var(--fg-muted);font-size:11px">last used ${esc(relativeTime(profile.lastUsed))}</div>`
          : '';
        return `${status}${live}${flags.length ? `<div style="margin-top:6px">${flags.join(' ')}</div>` : ''}${lastUsed}`;
      },
    },
    {
      key: 'pressure',
      label: 'Pressure',
      render: profile => {
        const pressure = profile.pressure || 'normal';
        const explanation = pressure === 'normal'
          ? 'healthy'
          : pressure === 'critical'
            ? 'operator attention'
            : 'watch closely';
        return `${statusChip(pressure)}<div style="margin-top:6px;color:var(--fg-muted);font-size:11px">${esc(explanation)}</div>`;
      },
    },
    {
      key: 'tracked',
      label: 'Tracked',
      render: profile => `
        <label class="tracking-toggle" style="display:flex;align-items:center;cursor:pointer;">
          <input type="checkbox" data-agent="${esc(profile.agentId)}" ${profile.tracked ? 'checked' : ''} style="cursor:pointer;margin-right:6px">
          <span style="font-size:12px">${profile.tracked ? 'Yes' : 'No'}</span>
        </label>
      `,
    },
    {
      key: 'actions',
      label: '',
      render: profile => `
        <button class="btn-ghost btn-env" data-agent="${esc(profile.agentId)}" style="font-size:11px;border:1px solid var(--border);border-radius:4px">Env Vars</button>
      `,
    },
  ];

  const tableHtml = dataTable('agent-profiles-table', columns, profiles, { empty: 'No agent profiles discovered' });
  return glassCard('Profiles And Configuration', tableHtml, { padding: 'none', class: 'card-agents' });
}

function _bindTrackingToggles() {
  const toggles = document.querySelectorAll('.tracking-toggle input[type="checkbox"]');
  for (const el of toggles) {
    el.addEventListener('change', async (e) => {
      const agent = e.target.dataset.agent;
      const disabled = !e.target.checked;
      const originalChecked = !e.target.checked;
      e.target.disabled = true;
      try {
        await toggleAgentTracking(agent, disabled);
        refreshAgents();
      } catch (_err) {
        e.target.checked = originalChecked;
      } finally {
        e.target.disabled = false;
      }
    });
  }
}

function _bindEnvVarButtons(trackingConfig) {
  const btns = document.querySelectorAll('.btn-env');
  for (const btn of btns) {
    btn.addEventListener('click', async (e) => {
      const agentId = e.target.dataset.agent;
      const agents = (trackingConfig && trackingConfig.agents) || [];
      const agent = agents.find(a => a.agent_id === agentId);
      const envVars = (agent && agent.env_vars) ? { ...agent.env_vars } : {};

      let varsHtml = Object.entries(envVars).map(([k, v]) => `
        <div style="display:flex;gap:8px;margin-bottom:8px;" class="env-row">
          <input type="text" value="${esc(k)}" class="env-key" placeholder="KEY" style="flex:1;padding:6px;background:var(--bg-surface-1);border:1px solid var(--border);color:var(--fg-primary);border-radius:4px;">
          <input type="text" value="${esc(v)}" class="env-val" placeholder="VALUE" style="flex:2;padding:6px;background:var(--bg-surface-1);border:1px solid var(--border);color:var(--fg-primary);border-radius:4px;">
          <button type="button" class="btn-destructive env-del" style="padding:4px 8px;border-radius:4px;">&times;</button>
        </div>
      `).join('');

      if (!varsHtml) {
        varsHtml = `
        <div style="display:flex;gap:8px;margin-bottom:8px;" class="env-row">
          <input type="text" class="env-key" placeholder="KEY" style="flex:1;padding:6px;background:var(--bg-surface-1);border:1px solid var(--border);color:var(--fg-primary);border-radius:4px;">
          <input type="text" class="env-val" placeholder="VALUE" style="flex:2;padding:6px;background:var(--bg-surface-1);border:1px solid var(--border);color:var(--fg-primary);border-radius:4px;">
          <button type="button" class="btn-destructive env-del" style="padding:4px 8px;border-radius:4px;">&times;</button>
        </div>`;
      }

      const bodyHtml = `
        <div id="env-var-list" style="max-height:300px;overflow-y:auto;padding-right:8px;margin-bottom:12px;">
          ${varsHtml}
        </div>
        <button id="add-env-var" class="btn-secondary" style="font-size:12px;padding:4px 8px;border-radius:4px;">+ Add Env Var</button>
      `;

      const action = await showModal(`Env Vars: ${esc(agentId)}`, bodyHtml, [
        { id: 'cancel', label: 'Cancel' },
        { id: 'save', label: 'Save', primary: true },
      ]);

      if (action !== 'save') return;

      const rows = document.querySelectorAll('#env-var-list .env-row');
      const next = {};
      for (const row of rows) {
        const k = row.querySelector('.env-key')?.value?.trim();
        const v = row.querySelector('.env-val')?.value ?? '';
        if (k) next[k] = v;
      }

      try {
        await updateAgentEnvVars(agentId, next);
        toast(`Updated env vars for ${agentId}`, 'success');
        refreshAgents();
      } catch (err) {
        toast(`Failed to update env vars: ${err.message}`, 'error');
      }
    });
  }
}

function _profilePressure(rosterPressure, liveSessions) {
  const all = [rosterPressure, ...liveSessions.map(session => session.contextPressure)].filter(Boolean);
  if (all.includes('critical')) return 'critical';
  if (all.includes('warning')) return 'warning';
  return 'normal';
}

function _latestTimestamp(values) {
  const timestamps = values
    .filter(Boolean)
    .map(value => new Date(value))
    .filter(value => !Number.isNaN(value.getTime()))
    .sort((a, b) => b.getTime() - a.getTime());
  return timestamps.length ? timestamps[0].toISOString() : null;
}

function _abbrevPath(path) {
  if (!path) return '';
  if (path.length <= 42) return path;
  const parts = path.split('/').filter(Boolean);
  if (parts.length <= 2) return `…${path.slice(-40)}`;
  return `…/${parts.slice(-2).join('/')}`;
}

function _uniqueStrings(values) {
  const seen = new Set();
  const out = [];
  for (const value of values) {
    const trimmed = String(value || '').trim();
    if (!trimmed || seen.has(trimmed)) continue;
    seen.add(trimmed);
    out.push(trimmed);
  }
  return out;
}

// Global delegated event listener for dynamic modal controls.
document.addEventListener('click', (e) => {
  if (e.target.id === 'add-env-var') {
    const list = document.getElementById('env-var-list');
    if (!list) return;
    const row = document.createElement('div');
    row.className = 'env-row';
    row.style.cssText = 'display:flex;gap:8px;margin-bottom:8px;';
    row.innerHTML = `
      <input type="text" class="env-key" placeholder="KEY" style="flex:1;padding:6px;background:var(--bg-surface-1);border:1px solid var(--border);color:var(--fg-primary);border-radius:4px;">
      <input type="text" class="env-val" placeholder="VALUE" style="flex:2;padding:6px;background:var(--bg-surface-1);border:1px solid var(--border);color:var(--fg-primary);border-radius:4px;">
      <button type="button" class="btn-destructive env-del" style="padding:4px 8px;border-radius:4px;">&times;</button>
    `;
    list.appendChild(row);
  } else if (e.target.classList && e.target.classList.contains('env-del')) {
    const row = e.target.closest('.env-row');
    if (row) row.remove();
  }
});
