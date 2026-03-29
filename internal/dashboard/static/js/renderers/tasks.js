// renderers/tasks.js — Tasks + Repo Management page renderer.

import store from '../store.js';
import { loadQueue, loadRepos } from '../api.js';
import {
  esc, $, setHtml, statusChip, relativeTime, truncId, formatDuration,
} from '../utils.js';
import {
  glassCard, metricCard, dataTable, skeleton, toast,
} from '../components.js';

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

let _pageWired = false;
let _repoFilter = '';

export function initTasks() {
  store.subscribe('queue', () => renderTasks());
  store.subscribe('queueStats', () => renderTasks());
  store.subscribe('repos', () => renderTasks());
}

export function renderTasks() {
  const container = $('page-tasks');
  if (!container) return;
  if (store.state.ui.activeTab !== 'tasks') return;

  const stats = store.state.queueStats;
  const queue = store.state.queue || [];
  const repos = store.state.repos || [];

  // Guard: show skeleton while first load is in-flight
  if (!stats && queue.length === 0 && repos.length === 0) {
    setHtml(container, skeleton(6));
    return;
  }

  // Clear stale filter if its repo is no longer in the list
  if (_repoFilter && !repos.some(r => r.repo === _repoFilter)) {
    _repoFilter = '';
  }

  const filteredQueue = _repoFilter
    ? queue.filter(q => q.repo === _repoFilter)
    : queue;

  const sections = [
    renderQueueStats(stats, queue),
    renderRepoFilterStrip(repos),
    renderQueueTable(filteredQueue),
    renderReposSection(repos),
  ];
  setHtml(container, sections.join(''));

  if (!_pageWired) {
    wireExpandRows(container);
    wireRepoFilter(container);
    _pageWired = true;
  }
}

export async function refreshTasks() {
  try {
    await Promise.all([loadQueue(), loadRepos()]);
  } catch (err) {
    toast('Failed to refresh tasks: ' + err.message, 'error');
  }
}

// ---------------------------------------------------------------------------
// Queue stats metric strip
// ---------------------------------------------------------------------------

function renderQueueStats(stats, queue) {
  const pending  = stats?.pending  ?? queue.filter(q => q.state === 'pending').length;
  const active   = stats?.active   ?? queue.filter(q => q.state === 'active').length;
  const blocked  = stats?.blocked  ?? queue.filter(q => q.state === 'blocked').length;
  const total    = stats?.total    ?? queue.length;

  const strip = [
    metricCard(pending,  'Pending',  'var(--stage-submitted)'),
    metricCard(active,   'Active',   'var(--stage-gating)'),
    metricCard(blocked,  'Blocked',  'var(--color-destructive)'),
    metricCard(total,    'Total',    'var(--fg-muted)'),
  ].join('');

  return `<div class="metric-strip">${strip}</div>`;
}

// ---------------------------------------------------------------------------
// Queue table
// ---------------------------------------------------------------------------

function renderQueueTable(queue) {
  const columns = [
    { label: 'Task ID',  key: 'id',       render: r => `<code>${esc(truncId(r.id))}</code>` },
    { label: 'Repo',     key: 'repo' },
    { label: 'Branch',   key: 'branch' },
    { label: 'State',    key: 'state',    render: r => statusChip(r.state) },
    { label: 'Priority', key: 'priority', render: r => priorityBadge(r.priority) },
    { label: 'Owner Session', key: 'ownerSession', render: r => r.ownerSession ? `<code>${esc(truncId(r.ownerSession))}</code>` : '<span class="text-muted">unassigned</span>' },
    { label: 'Wait Time', key: 'submissionTime', render: r => relativeTime(r.submissionTime) },
  ];

  const table = dataTable('queue-table', columns, queue, { empty: 'No queued tasks' });
  return glassCard('Queue', table, { padding: 'none' });
}

// ---------------------------------------------------------------------------
// Priority badge helper
// ---------------------------------------------------------------------------

function priorityBadge(p) {
  const val = p ?? 0;
  let cls = 'status-muted';
  if (val >= 9)      cls = 'status-destructive';
  else if (val >= 5) cls = 'status-warning';
  else if (val >= 1) cls = 'status-info';
  return `<span class="status-chip ${cls}">P${esc(String(val))}</span>`;
}

// ---------------------------------------------------------------------------
// Repo filter chip strip
// ---------------------------------------------------------------------------

function renderRepoFilterStrip(repos) {
  if (repos.length === 0) return '';
  const repoNames = [...new Set(repos.map(r => r.repo))].sort();
  if (repoNames.length <= 1) return ''; // no point filtering a single repo

  const allCls = !_repoFilter ? 'active' : '';
  let chips = `<button class="repo-filter-chip ${allCls}" data-repo="">All</button>`;
  for (const name of repoNames) {
    const cls = _repoFilter === name ? 'active' : '';
    chips += `<button class="repo-filter-chip ${cls}" data-repo="${esc(name)}">${esc(name)}</button>`;
  }
  return `<div class="repo-filter-strip">${chips}</div>`;
}

function wireRepoFilter(container) {
  container.addEventListener('click', e => {
    const chip = e.target.closest('.repo-filter-chip');
    if (!chip) return;
    _repoFilter = chip.dataset.repo || '';
    renderTasks();
  });
}

// ---------------------------------------------------------------------------
// Repos section — cards per repo with branch/state summary
// ---------------------------------------------------------------------------

function renderReposSection(repos) {
  if (repos.length === 0) {
    return glassCard('Repositories', '<div class="empty-state">No repos tracked</div>');
  }

  // Group by repo name
  const byRepo = new Map();
  for (const r of repos) {
    if (!byRepo.has(r.repo)) byRepo.set(r.repo, []);
    byRepo.get(r.repo).push(r);
  }

  let cards = '';
  for (const [repoName, branches] of byRepo) {
    const branchRows = branches.map(b => {
      const stateStr = b.state || b.lastRunStatus || 'unknown';
      return `<div class="repo-branch-row">
        <span class="repo-branch-name">${esc(b.branch)}</span>
        ${statusChip(stateStr)}
        <span class="text-muted">${relativeTime(b.updatedAt || b.lastRunAt)}</span>
      </div>`;
    }).join('');

    const summary = `${branches.length} branch${branches.length !== 1 ? 'es' : ''}`;

    cards += `<div class="glass-card pad-default repo-card">
      <div class="glass-card-header">${esc(repoName)}</div>
      <div class="repo-summary text-muted">${esc(summary)}</div>
      <div class="repo-branches">${branchRows}</div>
    </div>`;
  }

  return `<div class="section-header">Repositories</div>
    <div class="repo-grid">${cards}</div>`;
}

// ---------------------------------------------------------------------------
// Expand-row wire-up (reusable click delegate)
// ---------------------------------------------------------------------------

function wireExpandRows(container) {
  container.addEventListener('click', e => {
    const row = e.target.closest('tr.expandable');
    if (!row) return;
    const rowId = row.dataset.rowId;
    const expandRow = container.querySelector(`tr.expand-row[data-expand-for="${rowId}"]`);
    if (expandRow) {
      expandRow.classList.toggle('hidden');
      const chevron = row.querySelector('.chevron');
      if (chevron) chevron.classList.toggle('open');
    }
  });
}
