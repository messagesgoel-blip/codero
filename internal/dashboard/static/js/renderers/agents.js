// agents.js — Agents page renderer: open agents, available agents, and env var tracking.

import store from '../store.js';
import { loadAgents, loadSessions, loadTrackingConfig, toggleAgentTracking, updateAgentEnvVars } from '../api.js';
import {
  esc, html, statusChip, relativeTime, formatDuration, setHtml, $,
} from '../utils.js';
import { dataTable, glassCard, skeleton, showModal, toast } from '../components.js';

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

  const parts = [
    _renderOpenAgents(sessions),
    '<div style="height:16px"></div>',
    _renderAvailableAgents(agents, sessions, trackingConfig)
  ];
  setHtml(container, parts.join(''));

  _bindTrackingToggles();
  _bindEnvVarButtons(trackingConfig);
}

// --- Private renderers ---

function _renderOpenAgents(sessions) {
  // Only active sessions
  const activeSessions = sessions.filter(s => s.status === 'active' || !s.endedAt);
  
  const columns = [
    { key: 'agentId', label: 'Agent', render: r => {
        const agent = esc(r.agentId || r.ownerAgent || '—');
        const family = r.family ? `<span class="mode-badge" style="margin-left:6px">${esc(r.family)}</span>` : '';
        return `${agent}${family}`;
      }
    },
    { key: 'repo', label: 'Repo / Branch / Task', render: r => {
        const repo = esc(r.repo || '—');
        const branch = esc(r.branch || '');
        const pr = r.prNumber ? ` <a href="https://github.com/${repo}/pull/${r.prNumber}" target="_blank" class="pr-link">#${r.prNumber}</a>` : '';
        const task = r.task && r.task.id ? `<div style="margin-top:4px;color:var(--fg-muted);font-size:11px">${esc(r.task.id)}</div>` : '';
        return `${branch ? `${repo} / <code>${branch}</code>${pr}` : repo}${task}`;
      }
    },
    { key: 'runtime', label: 'Runtime', render: r => {
        const mode = r.mode ? `<span class="mode-badge">${esc(r.mode)}</span>` : '';
        const launch = r.launchMode ? `<span class="mode-badge" style="margin-left:4px">${esc(r.launchMode)}</span>` : '';
        const lifecycle = statusChip(r.lifecycleState || 'unknown');
        const attachment = statusChip(r.attachmentState || 'unknown');
        return `${mode}${launch}<div style="margin-top:6px">${lifecycle} ${attachment}</div>`;
      }
    },
    { key: 'activity', label: 'Activity', render: r => {
        const activity = statusChip(r.activityState || 'unknown');
        const inferred = r.inferredStatus ? `<div style="margin-top:6px;color:var(--fg-muted);font-size:11px">${esc(r.inferredStatus)}</div>` : '';
        return `${activity}${inferred}`;
      }
    },
    { key: 'attribution', label: 'Attribution', render: r => {
        const source = statusChip(r.attributionSource || 'unknown');
        const confidence = r.attributionConfidence ? `<div style="margin-top:6px;color:var(--fg-muted);font-size:11px">${esc(r.attributionConfidence)}</div>` : '';
        return `${source}${confidence}`;
      }
    },
    { key: 'lastActivity', label: 'Last Activity', render: r => r.lastActivityAt ? esc(relativeTime(r.lastActivityAt)) : '<span style="color:var(--fg-muted)">—</span>' },
    { key: 'outputMb', label: 'Output', render: r => {
        const output = r.outputMb > 0 ? `${r.outputMb.toFixed(2)} MB` : '—';
        const times = `<div style="margin-top:6px;color:var(--fg-muted);font-size:11px">active ${esc(formatDuration(r.workingDurationSec || 0))} · idle ${esc(formatDuration(r.idleDurationSec || 0))}</div>`;
        return `${output}${times}`;
      }
    }
  ];

  const rows = activeSessions.map(s => ({
    _id: s.sessionId || s.id,
    agentId: s.agentId || s.ownerAgent,
    family: s.family,
    launchMode: s.launchMode,
    repo: s.repo,
    branch: s.branch,
    prNumber: s.prNumber || s.pr_number,
    mode: s.mode,
    lifecycleState: s.lifecycleState || s.lifecycle_state,
    attachmentState: s.attachmentState || s.attachment_state,
    attributionSource: s.attributionSource || s.attribution_source,
    attributionConfidence: s.attributionConfidence || s.attribution_confidence,
    activityState: s.state || s.activityState || s.activity_state,
    task: s.task,
    lastActivityAt: s.lastActivityAt || s.last_activity_at || null,
    contextPressure: s.contextPressure || s.context_pressure,
    outputMb: s.outputMb || s.output_mb || 0,
    workingDurationSec: s.workingDurationSec || s.working_duration_sec || 0,
    idleDurationSec: s.idleDurationSec || s.idle_duration_sec || 0,
    inferredStatus: s.inferredStatus || s.inferred_status
  }));

  const tableHtml = dataTable('open-agents-table', columns, rows, { empty: 'No open agents' });
  return glassCard('Open Agents', tableHtml, { padding: 'none', class: 'card-agents' });
}

function _renderAvailableAgents(agents, sessions, trackingConfig) {
  const configAgents = (trackingConfig && trackingConfig.agents) || [];
  
  // Create a map of open agents to filter them out from available
  const activeSessions = sessions.filter(s => s.status === 'active' || !s.endedAt);
  const openAgentIds = new Set(activeSessions.map(s => s.agentId || s.ownerAgent));
  
  // Combine agents from roster and config
  const availableMap = new Map();
  
  // From config
  for (const c of configAgents) {
    if (!openAgentIds.has(c.agent_id)) {
      availableMap.set(c.agent_id, {
        agentId: c.agent_id,
        alias: c.shim_name,
        tracked: !c.disabled,
        installed: c.installed,
        envVars: c.env_vars || {},
        lastUsed: null
      });
    }
  }
  
  // From roster (for last used)
  for (const a of agents) {
    if (!openAgentIds.has(a.agentId)) {
      if (!availableMap.has(a.agentId)) {
        availableMap.set(a.agentId, {
          agentId: a.agentId,
          alias: a.agentId,
          tracked: a.status !== 'disabled',
          installed: true,
          envVars: {},
          lastUsed: a.lastSeen
        });
      } else {
        availableMap.get(a.agentId).lastUsed = a.lastSeen;
      }
    }
  }

  const rows = Array.from(availableMap.values());

  const columns = [
    { key: 'agentId', label: 'Agent', render: r => {
        let label = `<span style="font-weight:500">${esc(r.agentId)}</span>`;
        if (r.alias && r.alias !== r.agentId) {
          label += ` <span style="color:var(--fg-muted);font-size:11px">(${esc(r.alias)})</span>`;
        }
        if (!r.installed) {
          label += ` <span style="color:var(--destructive);font-size:10px;margin-left:4px" title="Binary not found">&#x2717; missing</span>`;
        }
        return label;
      }
    },
    { key: 'lastUsed', label: 'Last Used', render: r => r.lastUsed ? esc(relativeTime(r.lastUsed)) : '<span style="color:var(--fg-muted)">—</span>' },
    { key: 'tracked', label: 'Tracked', render: r => `
        <label class="tracking-toggle" style="display:flex;align-items:center;cursor:pointer;">
          <input type="checkbox" data-agent="${esc(r.agentId)}" ${r.tracked ? 'checked' : ''} style="cursor:pointer;margin-right:6px">
          <span style="font-size:12px">${r.tracked ? 'Yes' : 'No'}</span>
        </label>
      `
    },
    { key: 'actions', label: '', render: r => `
        <button class="btn-ghost btn-env" data-agent="${esc(r.agentId)}" style="font-size:11px;border:1px solid var(--border);border-radius:4px">Env Vars</button>
      `
    }
  ];

  const tableHtml = dataTable('available-agents-table', columns, rows, { empty: 'No available agents' });
  return glassCard('Available Agents', tableHtml, { padding: 'none', class: 'card-agents' });
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
        { id: 'save', label: 'Save', primary: true }
      ]);
      
      if (action === 'save') {
        const newVars = {};
        document.querySelectorAll('.env-row').forEach(row => {
          const k = row.querySelector('.env-key').value.trim();
          const v = row.querySelector('.env-val').value.trim();
          if (k) newVars[k] = v;
        });
        try {
          await updateAgentEnvVars(agentId, newVars);
          toast('Env vars updated successfully', 'success');
          refreshAgents();
        } catch (err) {
          toast('Failed to update env vars', 'error');
        }
      }
    });
  }
}

// Global delegated event listener for dynamically added modal buttons
document.addEventListener('click', (e) => {
  if (e.target.id === 'add-env-var') {
    const list = document.getElementById('env-var-list');
    const row = document.createElement('div');
    row.className = 'env-row';
    row.style.cssText = 'display:flex;gap:8px;margin-bottom:8px;';
    // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    row.innerHTML = `
      <input type="text" class="env-key" placeholder="KEY" style="flex:1;padding:6px;background:var(--bg-surface-1);border:1px solid var(--border);color:var(--fg-primary);border-radius:4px;">
      <input type="text" class="env-val" placeholder="VALUE" style="flex:2;padding:6px;background:var(--bg-surface-1);border:1px solid var(--border);color:var(--fg-primary);border-radius:4px;">
      <button type="button" class="btn-destructive env-del" style="padding:4px 8px;border-radius:4px;">&times;</button>
    `;
    list.appendChild(row);
  } else if (e.target.classList.contains('env-del')) {
    e.target.closest('.env-row').remove();
  }
});
