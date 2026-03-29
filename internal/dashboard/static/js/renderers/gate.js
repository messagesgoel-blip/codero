// gate.js — Gate page renderer: pre-commit checks, findings, toggle controls.

import store from '../store.js';
import { loadGateChecks, loadFindings, loadGateConfig, loadCompliance, apiPut } from '../api.js';
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
  store.subscribe('compliance', () => renderGate());
}

export async function refreshGate() {
  await Promise.all([loadGateChecks(), loadFindings(), loadGateConfig(), loadCompliance()]);
}

export function renderGate() {
  const container = $('page-gate');
  if (!container) return;

  const gateChecks = store.select('gateChecks');
  const blockReasons = store.select('blockReasons') || [];
  const gateConfig = store.select('gateConfig');
  const compliance = store.select('compliance');

  const pipelineHtml = buildGatePipeline(gateChecks);
  const togglesHtml = buildToggleControls(gateChecks, gateConfig);
  const chartHtml = buildBlockReasonsChart(blockReasons);
  const findingsHtml = buildFindingsBrowser(gateChecks);
  const complianceHtml = buildComplianceTable(compliance);

  // Findings is the primary focal point — shown first with a count badge.
  const findingsCount = extractFindings(gateChecks).length;
  const findingsHeader = findingsCount > 0
    ? `<div class="glass-card-header">Findings <span class="findings-count-badge">${findingsCount}</span></div>`
    : `<div class="glass-card-header">Findings</div>`;

  setHtml(container,
    glassCard('', findingsHeader + findingsHtml, { padding: 'none', class: 'gate-findings-card' }) +
    glassCard('Block Reasons', chartHtml, { class: 'gate-chart-card' }) +
    glassCard('Gate Pipeline', pipelineHtml, { class: 'gate-pipeline-card' }) +
    glassCard('Gate Controls', togglesHtml, { class: 'gate-toggles-card' }) +
    complianceHtml
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

// ---- compliance rules -------------------------------------------------------

function buildComplianceTable(compliance) {
  if (!compliance || !compliance.rules || compliance.rules.length === 0) {
    return glassCard('Compliance Rules', '<div class="empty-state">No compliance data</div>', {
      class: 'gate-compliance-card',
    });
  }

  const columns = [
    { key: 'name', label: 'Rule' },
    {
      key: 'enforcement',
      label: 'Enforcement',
      render: r => enforcementBadge(r.enforcement),
    },
    {
      key: 'passed',
      label: 'Passed',
      class: 'col-num',
      render: r => esc(String(r.passed ?? 0)),
    },
    {
      key: 'failed',
      label: 'Failed',
      class: 'col-num',
      render: r => {
        const f = r.failed ?? 0;
        const cls = f > 0 ? 'text-destructive' : '';
        return `<span class="${cls}">${esc(String(f))}</span>`;
      },
    },
  ];

  const rows = compliance.rules.map(r => ({
    name: r.name || r.rule || '—',
    enforcement: r.enforcement || 'advisory',
    passed: r.passed ?? 0,
    failed: r.failed ?? 0,
  }));

  const tableHtml = dataTable('compliance-table', columns, rows, { empty: 'No compliance rules' });
  return glassCard('Compliance Rules', tableHtml, { padding: 'none', class: 'gate-compliance-card' });
}

function enforcementBadge(level) {
  const l = String(level).toLowerCase();
  let cls = 'badge-muted';
  if (l === 'blocking' || l === 'required') cls = 'badge-destructive';
  else if (l === 'warning' || l === 'advisory') cls = 'badge-warning';
  else if (l === 'info') cls = 'badge-info';
  return `<span class="enforcement-badge ${cls}">${esc(level)}</span>`;
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
