// renderers/settings.js — Settings + Config + Upload page renderer.

import store from '../store.js';
import { loadSettings, loadGateConfig, apiPut, apiFetch } from '../api.js';
import {
  esc, $, setHtml, statusChip, html,
} from '../utils.js';
import {
  glassCard, toggleSwitch, fileDropZone, skeleton, toast,
} from '../components.js';

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

export function initSettings() {
  store.subscribe('settings', () => renderSettings());
  store.subscribe('gateConfig', () => renderSettings());
}

export function renderSettings() {
  const container = $('page-settings');
  if (!container) return;

  const settings = store.state.settings;
  const gateConfig = store.state.gateConfig;

  if (!settings && !gateConfig) {
    setHtml(container, skeleton(6));
    return;
  }

  const sections = [
    renderIntegrationsGrid(settings),
    renderGatePipelineConfig(gateConfig),
    renderUploadZone(),
    renderConfigDrift(gateConfig),
  ];
  setHtml(container, sections.join(''));

  wireGateConfigSave(container, gateConfig);
  wireFileUpload(container);
}

export async function refreshSettings() {
  try {
    await Promise.all([loadSettings(), loadGateConfig()]);
  } catch (err) {
    toast('Failed to refresh settings: ' + err.message, 'error');
  }
}

// ---------------------------------------------------------------------------
// Integrations grid
// ---------------------------------------------------------------------------

function renderIntegrationsGrid(settings) {
  const integrations = settings?.integrations || settings?.providers || [];

  if (integrations.length === 0) {
    // Provide sensible defaults if API returns no explicit list
    const defaults = [
      { name: 'GitHub', connected: true,  description: 'Pull requests, checks, and webhooks' },
      { name: 'CodeRabbit', connected: false, description: 'AI code review on PRs' },
      { name: 'Semgrep', connected: false, description: 'Static analysis gate check' },
      { name: 'Gitleaks', connected: false, description: 'Secret scanning' },
      { name: 'LiteLLM', connected: false, description: 'LLM proxy for chat' },
      { name: 'SonarCloud', connected: false, description: 'Code quality metrics' },
    ];
    return renderIntegrationCards(settings ? integrations : defaults);
  }

  return renderIntegrationCards(integrations);
}

function renderIntegrationCards(integrations) {
  const cards = integrations.map(intg => {
    const connected = intg.connected || intg.enabled || false;
    const statusLabel = connected ? 'connected' : 'disconnected';
    const chip = statusChip(connected ? 'connected' : 'stale');

    return `<div class="glass-card pad-default integration-card">
      <div class="integration-header">
        <span class="integration-name">${esc(intg.name)}</span>
        ${chip}
      </div>
      <div class="integration-desc text-muted">${esc(intg.description || '')}</div>
    </div>`;
  }).join('');

  return `<div class="section-header">Integrations</div>
    <div class="integration-grid">${cards}</div>`;
}

// ---------------------------------------------------------------------------
// Gate pipeline config table
// ---------------------------------------------------------------------------

function renderGatePipelineConfig(gateConfig) {
  const gates = gateConfig?.gates || gateConfig?.checks || [];

  if (gates.length === 0) {
    return glassCard('Gate Pipeline Configuration',
      '<div class="empty-state">No gate configuration loaded</div>');
  }

  let thead = `<thead><tr>
    <th>Check Name</th>
    <th>Enabled</th>
    <th>Blocks Commit</th>
    <th>Timeout</th>
    <th>Provider</th>
  </tr></thead>`;

  let tbody = '<tbody>';
  for (let i = 0; i < gates.length; i++) {
    const g = gates[i];
    const name     = g.name || g.check || 'unnamed';
    const enabled  = g.enabled !== false;
    const blocks   = g.blocks_commit ?? g.blocksCommit ?? true;
    const timeout  = g.timeout ?? g.timeout_sec ?? '';
    const provider = g.provider || g.type || '';

    tbody += `<tr data-gate-idx="${i}">
      <td><code>${esc(name)}</code></td>
      <td>${toggleSwitch(`gate-enabled-${i}`, enabled, '')}</td>
      <td>${toggleSwitch(`gate-blocks-${i}`, blocks, '')}</td>
      <td><input type="number" class="gate-timeout-input" value="${esc(String(timeout))}" min="0" step="1" data-gate-idx="${i}"></td>
      <td class="text-muted">${esc(provider)}</td>
    </tr>`;
  }
  tbody += '</tbody>';

  const table = `<table class="data-table" id="gate-config-table">${thead}${tbody}</table>`;
  const saveBtn = `<div class="gate-config-actions">
    <button id="gate-config-save" class="btn btn-primary">Save Configuration</button>
  </div>`;

  return glassCard('Gate Pipeline Configuration', table + saveBtn, { padding: 'none' });
}

// ---------------------------------------------------------------------------
// Wire gate config save
// ---------------------------------------------------------------------------

function wireGateConfigSave(container, gateConfig) {
  const saveBtn = container.querySelector('#gate-config-save');
  if (!saveBtn) return;

  saveBtn.addEventListener('click', async () => {
    const gates = gateConfig?.gates || gateConfig?.checks || [];
    const updated = gates.map((g, i) => {
      const enabledCheckbox = container.querySelector(`#gate-enabled-${i}`);
      const blocksCheckbox  = container.querySelector(`#gate-blocks-${i}`);
      const timeoutInput    = container.querySelector(`.gate-timeout-input[data-gate-idx="${i}"]`);

      return {
        ...g,
        enabled: enabledCheckbox ? enabledCheckbox.checked : g.enabled,
        blocks_commit: blocksCheckbox ? blocksCheckbox.checked : (g.blocks_commit ?? true),
        timeout: timeoutInput ? parseInt(timeoutInput.value, 10) || g.timeout : g.timeout,
      };
    });

    const updatedSettings = { ...store.state.settings, gates: updated };

    saveBtn.disabled = true;
    saveBtn.textContent = 'Saving...';

    try {
      await apiPut('settings', updatedSettings);
      toast('Gate configuration saved', 'success');
      // Reload to reflect server-side state
      await loadGateConfig();
    } catch (err) {
      toast('Failed to save: ' + err.message, 'error');
    } finally {
      saveBtn.disabled = false;
      saveBtn.textContent = 'Save Configuration';
    }
  });
}

// ---------------------------------------------------------------------------
// File upload zone
// ---------------------------------------------------------------------------

function renderUploadZone() {
  const zone = fileDropZone('manual-review-upload');
  const resultArea = '<div id="upload-result" class="upload-result"></div>';
  return `<div class="section-header">Manual Review Upload</div>
    <div class="upload-section">${zone}${resultArea}</div>`;
}

function wireFileUpload(container) {
  const dropZone = container.querySelector('#manual-review-upload');
  if (!dropZone) return;

  const fileInput = dropZone.querySelector('.file-drop-input');
  const resultEl  = container.querySelector('#upload-result');

  // Click to trigger file input
  dropZone.addEventListener('click', e => {
    if (e.target !== fileInput) fileInput.click();
  });

  // Drag and drop
  dropZone.addEventListener('dragover', e => {
    e.preventDefault();
    dropZone.classList.add('drag-active');
  });

  dropZone.addEventListener('dragleave', () => {
    dropZone.classList.remove('drag-active');
  });

  dropZone.addEventListener('drop', e => {
    e.preventDefault();
    dropZone.classList.remove('drag-active');
    if (e.dataTransfer.files.length > 0) {
      uploadFiles(e.dataTransfer.files, resultEl);
    }
  });

  // File input change
  fileInput.addEventListener('change', () => {
    if (fileInput.files.length > 0) {
      uploadFiles(fileInput.files, resultEl);
    }
  });
}

async function uploadFiles(files, resultEl) {
  const formData = new FormData();
  for (const file of files) {
    formData.append('files', file);
  }

  if (resultEl) {
    setHtml(resultEl, '<span class="text-muted">Uploading...</span>');
  }

  try {
    const resp = await apiFetch('manual-review-upload', {
      method: 'POST',
      body: formData,
      // No Content-Type header — let browser set multipart boundary
    });

    const count = files.length;
    const msg = `${count} file${count !== 1 ? 's' : ''} uploaded successfully`;
    toast(msg, 'success');
    if (resultEl) {
      setHtml(resultEl, `<span class="upload-success">${esc(msg)}</span>`);
    }
  } catch (err) {
    toast('Upload failed: ' + err.message, 'error');
    if (resultEl) {
      setHtml(resultEl, `<span class="upload-error">Upload failed: ${esc(err.message)}</span>`);
    }
  }
}

// ---------------------------------------------------------------------------
// Config drift warnings
// ---------------------------------------------------------------------------

function renderConfigDrift(gateConfig) {
  const drifts = gateConfig?.drifts || gateConfig?.drift_warnings || [];

  if (drifts.length === 0) return '';

  const rows = drifts.map(d => {
    const severity = d.severity || 'warning';
    const cls = severity === 'error' ? 'drift-error' : severity === 'warning' ? 'drift-warn' : 'drift-info';
    return `<div class="drift-item ${cls}">
      <span class="drift-icon">${severity === 'error' ? '&#9888;' : '&#9432;'}</span>
      <div class="drift-content">
        <div class="drift-field">${esc(d.field || d.check || 'unknown')}</div>
        <div class="drift-msg text-muted">${esc(d.message || d.description || '')}</div>
        ${d.expected != null ? `<div class="drift-expected">Expected: <code>${esc(String(d.expected))}</code></div>` : ''}
        ${d.actual != null ? `<div class="drift-actual">Actual: <code>${esc(String(d.actual))}</code></div>` : ''}
      </div>
    </div>`;
  }).join('');

  return glassCard('Config Drift Warnings', `<div class="drift-list">${rows}</div>`);
}
