// renderers/archives.js — Archives + Handover + Timing page renderer.

import store from '../store.js';
import { loadArchives } from '../api.js';
import {
  esc, $, setHtml, statusChip, relativeTime, formatDuration, truncId,
} from '../utils.js';
import {
  glassCard, metricCard, dataTable, detailGrid, skeleton, toast,
} from '../components.js';

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

export function initArchives() {
  store.subscribe('archives', () => renderArchives());
}

export function renderArchives() {
  const container = $('page-archives');
  if (!container) return;

  const archives = store.state.archives || [];

  if (archives.length === 0 && !store.state.archives) {
    setHtml(container, skeleton(6));
    return;
  }

  const sections = [
    renderTimingAnalytics(archives),
    renderArchiveTable(archives),
  ];
  setHtml(container, sections.join(''));

  wireExpandRows(container);
}

export async function refreshArchives() {
  try {
    await loadArchives();
  } catch (err) {
    toast('Failed to refresh archives: ' + err.message, 'error');
  }
}

// ---------------------------------------------------------------------------
// Timing analytics — metric cards computed from archive data
// ---------------------------------------------------------------------------

function renderTimingAnalytics(archives) {
  const completed = archives.filter(a => a.durationSec != null && a.durationSec >= 0);

  const totalRuns = archives.length;
  const avgDuration = completed.length > 0
    ? completed.reduce((sum, a) => sum + a.durationSec, 0) / completed.length
    : -1;
  const fastestRun = completed.length > 0
    ? Math.min(...completed.map(a => a.durationSec))
    : -1;
  const slowestRun = completed.length > 0
    ? Math.max(...completed.map(a => a.durationSec))
    : -1;

  // Result breakdown
  const resultCounts = {};
  for (const a of archives) {
    const key = a.result || 'unknown';
    resultCounts[key] = (resultCounts[key] || 0) + 1;
  }
  const successCount = resultCounts['success'] || resultCounts['merged'] || 0;
  const failCount    = resultCounts['failure'] || resultCounts['failed'] || 0;
  const passRate     = totalRuns > 0 ? Math.round((successCount / totalRuns) * 100) : -1;

  const totalCommits = archives.reduce((sum, a) => sum + (a.commitCount || 0), 0);

  const strip = [
    metricCard(totalRuns,                  'Total Runs',     'var(--fg-muted)'),
    metricCard(formatDuration(avgDuration), 'Avg Duration',  'var(--stage-gating)'),
    metricCard(formatDuration(fastestRun),  'Fastest',       'var(--stage-merged)'),
    metricCard(formatDuration(slowestRun),  'Slowest',       'var(--color-destructive)'),
    metricCard(passRate >= 0 ? passRate + '%' : '\u2014', 'Pass Rate', 'var(--stage-merge-ready)'),
    metricCard(totalCommits,               'Total Commits',  'var(--stage-committed)'),
  ].join('');

  return `<div class="section-header">Timing Analytics</div>
    <div class="metric-strip">${strip}</div>`;
}

// ---------------------------------------------------------------------------
// Archive table
// ---------------------------------------------------------------------------

function renderArchiveTable(archives) {
  const columns = [
    { label: 'Agent',     key: 'agent' },
    { label: 'Task',      key: 'taskId',   render: r => r.taskId ? `<code>${esc(truncId(r.taskId))}</code>` : '<span class="text-muted">\u2014</span>' },
    { label: 'Repo / Branch', key: 'repo', render: r => `${esc(r.repo || '\u2014')} / <code>${esc(r.branch || '\u2014')}</code>` },
    { label: 'Result',    key: 'result',   render: r => resultChip(r.result) },
    { label: 'Duration',  key: 'durationSec', render: r => formatDuration(r.durationSec) },
    { label: 'Commits',   key: 'commitCount', render: r => esc(String(r.commitCount ?? 0)) },
    { label: 'Merge SHA', key: 'mergeSha', render: r => r.mergeSha ? `<code>${esc(truncId(r.mergeSha, 10))}</code>` : '<span class="text-muted">\u2014</span>' },
    { label: 'Archived',  key: 'archivedAt', render: r => relativeTime(r.archivedAt) },
  ];

  // Build rows with expandable handover details
  const rows = archives.map(a => ({
    ...a,
    _id: a.id || a.sessionId,
    _expandHtml: buildHandoverDetail(a),
  }));

  const table = dataTable('archive-table', columns, rows, {
    expandable: true,
    empty: 'No archived runs',
  });

  return glassCard('Archive History', table, { padding: 'none' });
}

// ---------------------------------------------------------------------------
// Result chip (maps result strings to themed chips)
// ---------------------------------------------------------------------------

function resultChip(result) {
  if (!result) return statusChip('unknown');
  const r = String(result).toLowerCase();
  // Map result values to appropriate status names for statusChip
  if (['success', 'merged', 'completed'].includes(r)) return statusChip('success');
  if (['failure', 'failed', 'error'].includes(r))     return statusChip('fail');
  if (['cancelled', 'abandoned', 'timeout'].includes(r)) return statusChip('cancelled');
  return statusChip(result);
}

// ---------------------------------------------------------------------------
// Handover detail (expandable row content)
// ---------------------------------------------------------------------------

function buildHandoverDetail(archive) {
  const items = [];

  items.push({ label: 'Session ID', value: archive.sessionId ? `<code>${esc(truncId(archive.sessionId, 16))}</code>` : '\u2014' });
  items.push({ label: 'Task Source', value: archive.taskSource ? esc(archive.taskSource) : '\u2014' });

  if (archive.startedAt) {
    items.push({ label: 'Started', value: esc(new Date(archive.startedAt).toLocaleString()) });
  }
  if (archive.endedAt) {
    items.push({ label: 'Ended', value: esc(new Date(archive.endedAt).toLocaleString()) });
  }

  // Predecessor/successor if available (these might be injected by the normalizer later)
  if (archive.predecessor) {
    items.push({ label: 'Predecessor', value: `<code>${esc(truncId(archive.predecessor))}</code>` });
  }
  if (archive.successor) {
    items.push({ label: 'Successor', value: `<code>${esc(truncId(archive.successor))}</code>` });
  }

  if (archive.repo && archive.branch) {
    items.push({ label: 'Full Branch', value: `${esc(archive.repo)} / ${esc(archive.branch)}` });
  }

  if (archive.mergeSha) {
    items.push({ label: 'Merge SHA (full)', value: `<code>${esc(archive.mergeSha)}</code>` });
  }

  return detailGrid(items);
}

// ---------------------------------------------------------------------------
// Expand-row wire-up
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
