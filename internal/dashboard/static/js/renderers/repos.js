// repos.js — Node Repository Scanner page renderer.
// Shows all git repositories on the node, highlighting which are connected/orphans.

import store from '../store.js';
import { loadNodeRepos } from '../api.js';
import {
  esc, $, setHtml, statusChip, relativeTime,
} from '../utils.js';
import { glassCard, metricCard } from '../components.js';

export default async function renderRepos() {
  setHtml('header-title', 'Node Repositories');
  setHtml('main-content', '<div class=\"loading\">Scanning node repositories...</div>');

  try {
    const data = await loadNodeRepos();
    store.set({ nodeRepos: data });
    _render(data);
  } catch (err) {
    setHtml('main-content', `<div class=\"error\">Failed to scan repositories: ${esc(err.message)}</div>`);
  }
}

function _render(data) {
  const { repos, connected, orphans, total } = data;

  const html = `
    <div class=\"metric-strip\">
      ${metricCard('Total Repos', total)}
      ${metricCard('Connected', connected, { accent: 'success' })}
      ${metricCard('Orphans', orphans, { accent: orphans > 0 ? 'warn' : '' })}
    </div>

    ${glassCard('All Repositories on Node', `
      <table class=\"data-table\">
        <thead>
          <tr>
            <th>Name</th>
            <th>Status</th>
            <th>Path</th>
            <th>Last Scanned</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          ${repos.map(r => `
            <tr>
              <td class=\"bold\">${esc(r.name)}</td>
              <td>${r.connected ? statusChip('connected', { accent: 'success' }) : statusChip('orphan', { accent: 'warn' })}</td>
              <td class=\"mono smaller secondary\">${esc(r.path)}</td>
              <td class=\"secondary\">${relativeTime(r.last_scan)}</td>
              <td>
                ${r.connected 
                  ? `<button class=\"btn-ghost btn-sm\" disabled>Connected</button>` 
                  : `<button class=\"btn-primary btn-sm\" onclick=\"alert('Connection workflow for ${esc(r.name)} coming soon')\">Connect</button>`
                }
              </td>
            </tr>
          `).join('')}
        </tbody>
      </table>
    `)}
  `;

  setHtml('main-content', html);
}
