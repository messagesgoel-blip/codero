// repos.js — Node Repository Scanner page renderer.
// Shows all git repositories on the node, highlighting which are connected/orphans.

import store from '../store.js';
import { loadNodeRepos } from '../api.js';
import {
  esc, $, setHtml, statusChip, relativeTime,
} from '../utils.js';
import { glassCard, metricCard, skeleton } from '../components.js';

let _initialized = false;

// --- Public API ---

export function initRepos() {
  if (_initialized) return;
  _initialized = true;
  store.subscribe('nodeRepos', () => renderRepos());
  // Bind connect button clicks once on the container (event delegation survives setHtml re-renders)
  const container = $('page-repos');
  if (container) _bindConnectButtons(container);
}

export async function refreshRepos() {
  try {
    await loadNodeRepos();
  } catch (err) {
    store.set({ nodeRepos: { error: err.message } });
  }
}

export function renderRepos() {
  const container = $('page-repos');
  if (!container) return;
  if (store.state.ui.activeTab !== 'repos') return;

  const data = store.select('nodeRepos');

  if (!data) {
    setHtml(container, skeleton(4));
    return;
  }

  if (data.error) {
    setHtml(container, glassCard('Node Repositories',
      `<div class="empty-state">Failed to scan: ${esc(data.error)}</div>`));
    return;
  }

  const { repos = [], connected = 0, orphans = 0, total = 0 } = data;

  const metricsHtml = `<div class="metric-strip">
    ${metricCard(String(total), 'Total Repos', 'var(--fg-muted)')}
    ${metricCard(String(connected), 'Connected', 'var(--success)')}
    ${metricCard(String(orphans), 'Orphans', orphans > 0 ? 'var(--warning)' : 'var(--fg-muted)')}
  </div>`;

  const columns = [
    { key: 'name', label: 'Name' },
    {
      key: 'connected',
      label: 'Status',
      render: r => r.connected ? statusChip('connected') : statusChip('orphan'),
    },
    { key: 'path', label: 'Path', class: 'col-mono' },
    {
      key: 'last_scan',
      label: 'Last Scanned',
      render: r => esc(relativeTime(r.last_scan)),
    },
    {
      key: '_action',
      label: 'Action',
      render: r => r.connected
        ? `<button class="status-chip status-muted" disabled>Connected</button>`
        : `<button class="status-chip status-info connect-btn" data-repo="${esc(r.name)}">Connect</button>`,
    },
  ];

  const tableHtml = glassCard('All Repositories on Node',
    metricsHtml + `<div style="padding:0 16px 16px">` +
    `<p class="text-muted" style="font-size:12px;margin:0 0 12px">Showing all git repositories detected on this node. Connect orphan repos to bring them under Codero management.</p>` +
    // dataTable expects rows; build inline since column render is custom enough
    _buildTable(columns, repos) +
    `</div>`,
    { padding: 'none', class: 'card-repos' });

  setHtml(container, tableHtml);
}

function _buildTable(columns, rows) {
  if (rows.length === 0) {
    return '<div class="empty-state">No repositories found on node</div>';
  }

  const thead = `<tr>${columns.map(c => `<th>${esc(c.label)}</th>`).join('')}</tr>`;
  const tbody = rows.map(r => {
    const cells = columns.map(c => {
      const val = c.render ? c.render(r) : esc(String(r[c.key] ?? ''));
      return `<td${c.class ? ` class="${c.class}"` : ''}>${val}</td>`;
    }).join('');
    return `<tr>${cells}</tr>`;
  }).join('');

  return `<table class="data-table"><thead>${thead}</thead><tbody>${tbody}</tbody></table>`;
}

function _bindConnectButtons(container) {
  container.addEventListener('click', e => {
    const btn = e.target.closest('.connect-btn');
    if (!btn) return;
    const repoName = btn.dataset.repo || '';
    // Connection workflow placeholder — will be wired to API in a future PR
    alert(`Connection workflow for "${repoName}" coming soon`);
  });
}
