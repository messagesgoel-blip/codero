// gate.js — Gate page renderer: pre-commit checks, findings, toggle controls.

import store from '../store.js';
import { loadGateChecks, loadFindings, loadGateConfig, apiPut } from '../api.js';
import {
  $, setHtml, esc, statusChip, severityChip, formatDuration,
} from '../utils.js';
import { glassCard, dataTable, toggleSwitch, barChart } from '../components.js';

let activeSeverityFilter = 'ALL';

// LOG-001: maps raw GC-001 status → normalized display state.
function toDisplayState(status) {
  if (status === 'pass') return 'passing';
  if (status === 'fail') return 'failing';
  return 'disabled';
}

// ---- public API ---------------------------------------------------------

export function initGate() {
  store.subscribe('gateChecks', () => renderGate());
  store.subscribe('blockReasons', () => renderGate());
  store.subscribe('gateConfig', () => renderGate());
}

export async function refreshGate() {
  await Promise.all([loadGateChecks(), loadFindings(), loadGateConfig()]);
}

export function renderGate() {
  const container = $('page-gate');
  if (!container) return;

  const gateChecks = store.select('gateChecks');
  const blockReasons = store.select('blockReasons') || [];
  const gateConfig = store.select('gateConfig');

  const pipelineHtml = buildGatePipeline(gateChecks);
  const togglesHtml = buildToggleControls(gateChecks, gateConfig);
  const chartHtml = buildBlockReasonsChart(blockReasons);
  const findingsHtml = buildFindingsBrowser(gateChecks);

  setHtml(container,
    glassCard('Gate Pipeline', pipelineHtml, { class: 'gate-pipeline-card' }) +
    glassCard('Gate Controls', togglesHtml, { class: 'gate-toggles-card' }) +
    glassCard('Block Reasons', chartHtml, { class: 'gate-chart-card' }) +
    glassCard('Findings', findingsHtml, { padding: 'none', class: 'gate-findings-card' })
  );

  attachToggleListeners(gateConfig);
  attachFilterListeners();
}

// ---- gate pipeline visualization ----------------------------------------

function buildGatePipeline(gateChecks) {
  if (!gateChecks || !gateChecks.checks || gateChecks.checks.length === 0) {
    return '<div class="empty-state">No gate check data</div>';
  }

  let html = '<div class="gate-pipeline">';
  for (const check of gateChecks.checks) {
    const status = String(check.status || 'unknown').toLowerCase();
    const findingsCount = check.findings_count || check.findingsCount || 0;
    const durationSec = check.duration_sec || check.durationSec || 0;

    html +=
      `<div class="gate-check-row">` +
        `<span class="gate-check-name">${esc(check.name)}</span>` +
        `<span class="gate-check-status">${statusChip(toDisplayState(status))}</span>` +
        `<span class="gate-check-duration">${formatDuration(durationSec)}</span>` +
        `<span class="gate-check-findings">${findingsCount > 0
          ? `<span class="findings-badge">${findingsCount}</span>`
          : '<span class="text-muted">0</span>'
        }</span>` +
      `</div>`;
  }
  html += '</div>';
  return html;
}

// ---- toggle controls ----------------------------------------------------

const ALWAYS_ON_CHECKS = ['gitleaks'];

function buildToggleControls(gateChecks, gateConfig) {
  if (!gateChecks || !gateChecks.checks) {
    return '<div class="empty-state">No gate configuration</div>';
  }

  const configMap = new Map();
  if (gateConfig && gateConfig.vars) {
    for (const v of gateConfig.vars) {
      configMap.set(v.name, v);
    }
  }

  let html = '<div class="gate-toggles">';
  for (const check of gateChecks.checks) {
    const name = check.name;
    const varName = check.config_var || check.configVar || name;
    const isAlwaysOn = ALWAYS_ON_CHECKS.includes(name.toLowerCase());
    const configEntry = configMap.get(varName);

    // Determine checked state: use config value if available, else infer from status
    let isEnabled = true;
    if (configEntry) {
      isEnabled = configEntry.value === true || configEntry.value === 'true' || configEntry.value === '1';
    } else if (check.status === 'disabled' || check.status === 'skip') {
      isEnabled = false;
    }

    html += `<div class="gate-toggle-item">` +
      toggleSwitch(
        `gate-toggle-${esc(varName)}`,
        isEnabled,
        name,
        { disabled: isAlwaysOn, locked: isAlwaysOn }
      ) +
    `</div>`;
  }
  html += '</div>';
  return html;
}

function attachToggleListeners(gateConfig) {
  const toggles = document.querySelectorAll('.gate-toggles-card .toggle-input');
  for (const toggle of toggles) {
    // Remove any previous listener by cloning
    const fresh = toggle.cloneNode(true);
    toggle.parentNode.replaceChild(fresh, toggle);

    fresh.addEventListener('change', async (e) => {
      const id = e.target.id; // gate-toggle-{varName}
      const varName = id.replace('gate-toggle-', '');
      const newValue = e.target.checked;
      try {
        await apiPut('settings/gate-config/' + encodeURIComponent(varName), { value: newValue });
        await loadGateConfig();
      } catch (err) {
        // Revert on failure
        e.target.checked = !newValue;
        console.error('Failed to update gate config:', err);
      }
    });
  }
}

// ---- block reasons chart ------------------------------------------------

function buildBlockReasonsChart(blockReasons) {
  if (!blockReasons || blockReasons.length === 0) {
    return '<div class="empty-state">No block reasons recorded</div>';
  }

  const items = blockReasons.map(r => ({
    label: r.source,
    value: r.count,
  }));

  return barChart(items);
}

// ---- findings browser ---------------------------------------------------

const SEVERITY_LEVELS = ['ALL', 'CRITICAL', 'HIGH', 'MEDIUM', 'LOW', 'INFO'];

function buildFindingsBrowser(gateChecks) {
  const findings = extractFindings(gateChecks);

  // Filter buttons
  let filterHtml = '<div class="findings-filter">';
  for (const sev of SEVERITY_LEVELS) {
    const activeCls = sev === activeSeverityFilter ? 'active' : '';
    filterHtml += `<button class="filter-btn ${activeCls}" data-severity="${esc(sev)}">${esc(sev)}</button>`;
  }
  filterHtml += '</div>';

  // Filter findings
  const filtered = activeSeverityFilter === 'ALL'
    ? findings
    : findings.filter(f => String(f.severity).toUpperCase() === activeSeverityFilter);

  const columns = [
    { label: 'File', key: 'file' },
    { label: 'Line', key: 'line', class: 'col-narrow' },
    {
      label: 'Severity',
      key: 'severity',
      render: row => severityChip(row.severity || 'info'),
    },
    { label: 'Message', key: 'message' },
    { label: 'Source', key: 'source', class: 'col-narrow' },
  ];

  const tableHtml = dataTable('findings-table', columns, filtered, {
    empty: 'No findings',
  });

  return filterHtml + tableHtml;
}

function extractFindings(gateChecks) {
  if (!gateChecks || !gateChecks.checks) return [];

  const all = [];
  for (const check of gateChecks.checks) {
    const items = check.findings || [];
    for (const f of items) {
      all.push({
        file: f.file || f.path || '--',
        line: f.line || f.line_number || '--',
        severity: f.severity || 'info',
        message: f.message || f.description || '',
        source: f.source || check.name,
      });
    }
  }
  return all;
}

function attachFilterListeners() {
  const buttons = document.querySelectorAll('.findings-filter .filter-btn');
  for (const btn of buttons) {
    btn.addEventListener('click', (e) => {
      activeSeverityFilter = e.target.dataset.severity || 'ALL';
      renderGate();
    });
  }
}
