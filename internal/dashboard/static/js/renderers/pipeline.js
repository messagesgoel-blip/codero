// pipeline.js — Pipeline page renderer: delivery FSM kanban + PR status.

import store from '../store.js';
import { loadPipeline, loadAssignments } from '../api.js';
import {
  $, setHtml, esc, statusChip, formatDuration, stageColor, relativeTime,
} from '../utils.js';
import { pipelineCard, glassCard, dataTable, barChart, toast } from '../components.js';

const STAGES = [
  'SUBMITTED', 'GATING', 'COMMITTED', 'PUSHED',
  'PR_ACTIVE', 'MONITORING', 'MERGE_READY', 'MERGED',
];

let _initialized = false;

// ---- public API ---------------------------------------------------------

export function initPipeline() {
  if (_initialized) return;
  _initialized = true;
  store.subscribe('pipeline', () => renderPipeline());
  store.subscribe('assignments', () => renderPipeline());
}

export async function refreshPipeline() {
  try {
    await Promise.all([loadPipeline(), loadAssignments()]);
  } catch (err) {
    toast('Failed to refresh pipeline: ' + err.message, 'error');
  }
}

export function renderPipeline() {
  const container = $('page-pipeline');
  if (!container) return;
  if (store.state.ui.activeTab !== 'pipeline') return;

  const pipeline = store.select('pipeline') || [];
  const assignments = store.select('assignments') || [];

  const kanbanHtml = buildKanban(pipeline);
  const prTableHtml = buildPrTable(assignments);
  const timingHtml = buildTimingStrip(pipeline);

  setHtml(container,
    glassCard('Delivery Pipeline', kanbanHtml, { padding: 'none', class: 'pipeline-kanban-card' }) +
    timingHtml +
    glassCard('PR Status', prTableHtml, { padding: 'none', class: 'pipeline-pr-card' })
  );
}

// ---- kanban board --------------------------------------------------------

function buildKanban(pipeline) {
  // Group cards by stage
  const buckets = new Map();
  for (const stage of STAGES) buckets.set(stage, []);

  for (const item of pipeline) {
    const key = String(item.state || item.substatus || '').toUpperCase();
    const bucket = buckets.get(key);
    if (bucket) {
      bucket.push(item);
    } else {
      // Fall back to SUBMITTED for unknown states
      buckets.get('SUBMITTED').push(item);
    }
  }

  let cols = '';
  for (const stage of STAGES) {
    const cards = buckets.get(stage);
    const bg = stageColor(stage);

    const headerHtml =
      `<div class="kanban-col-header" style="background: ${bg}; color: white">` +
        `${esc(stage)} <span class="wip-badge">${cards.length}</span>` +
      `</div>`;

    let bodyHtml = '';
    if (cards.length === 0) {
      bodyHtml = '<div class="kanban-empty">idle</div>';
    } else {
      for (const card of cards) {
        bodyHtml += pipelineCard(card);
      }
    }

    cols +=
      `<div class="kanban-col">` +
        headerHtml +
        `<div class="kanban-col-body">${bodyHtml}</div>` +
      `</div>`;
  }

  return `<div class="kanban">${cols}</div>`;
}

// ---- PR status table -----------------------------------------------------

function buildPrTable(assignments) {
  const withPr = assignments.filter(a => a.prNumber || a.substatus || a.state);

  const columns = [
    { label: 'Agent', key: 'agent' },
    { label: 'Branch', key: 'branch' },
    {
      label: 'Status',
      key: 'substatus',
      render: row => statusChip(row.substatus || row.state || 'unknown'),
    },
    {
      label: 'PR',
      key: 'prNumber',
      render: row => row.prNumber
        ? `<span class="pr-number">#${esc(String(row.prNumber))}</span>`
        : '<span class="text-muted">--</span>',
    },
    {
      label: 'Timing',
      key: 'startedAt',
      render: row => {
        if (!row.startedAt) return '<span class="text-muted">--</span>';
        const elapsed = (Date.now() - new Date(row.startedAt).getTime()) / 1000;
        return formatDuration(elapsed);
      },
    },
  ];

  return dataTable('pipeline-pr-table', columns, withPr, {
    empty: 'No active PRs',
  });
}

// ---- timing strip --------------------------------------------------------

function buildTimingStrip(pipeline) {
  if (pipeline.length === 0) return '';

  // Compute average stageSec per stage
  const sums = new Map();
  const counts = new Map();

  for (const item of pipeline) {
    const key = String(item.state || '').toUpperCase();
    if (!STAGES.includes(key)) continue;
    sums.set(key, (sums.get(key) || 0) + (item.stageSec || 0));
    counts.set(key, (counts.get(key) || 0) + 1);
  }

  const items = [];
  for (const stage of STAGES) {
    const count = counts.get(stage) || 0;
    if (count === 0) continue;
    const avg = Math.round((sums.get(stage) || 0) / count);
    items.push({ label: stage, value: avg });
  }

  if (items.length === 0) return '';

  const chartHtml = barChart(
    items.map(i => ({ label: i.label, value: i.value })),
  );

  return glassCard('Average Stage Duration (sec)', chartHtml, { class: 'pipeline-timing-card' });
}
