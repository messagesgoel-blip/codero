// scorecard.js — Scorecard page renderer.
// Shows aggregated quality, velocity, and compliance metrics.

import store from '../store.js';
import { loadScorecard } from '../api.js';
import { esc, $, setHtml } from '../utils.js';
import { glassCard, metricCard, skeleton, toast } from '../components.js';

let _initialized = false;

export function initScorecard() {
  if (_initialized) return;
  _initialized = true;
  store.subscribe('scorecard', () => renderScorecard());
}

export async function refreshScorecard() {
  try {
    await loadScorecard();
  } catch (err) {
    toast('Failed to load scorecard: ' + err.message, 'error');
  }
}

export function renderScorecard() {
  const container = $('page-scorecard');
  if (!container) return;

  const data = store.select('scorecard');
  if (!data) {
    setHtml(container, skeleton(6));
    return;
  }

  const metrics = [
    metricCard(String(data.gatePassRate ?? '—'), 'Gate Pass Rate', 'var(--accent)'),
    metricCard(String(data.avgCycleTime ?? '—'), 'Avg Cycle Time', 'var(--accent-warm)'),
    metricCard(String(data.mergeRate ?? '—'), 'Merge Rate', 'var(--accent)'),
    metricCard(String(data.complianceScore ?? '—'), 'Compliance', 'var(--accent)'),
  ];

  const metricsHtml = `<div class="metrics-grid">${metrics.join('')}</div>`;

  // Summary card
  const summary = data.summary
    ? `<div class="scorecard-summary">${esc(data.summary)}</div>`
    : '';

  setHtml(container, metricsHtml + glassCard('Scorecard', summary || '<p style="color:var(--fg-muted)">No scorecard data available.</p>'));
}