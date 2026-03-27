// components.js — Reusable UI components: glass cards, tables, modals, etc.

import { esc, html, statusChip, relativeTime, formatDuration, truncId } from './utils.js';

// --- Glass Card ---
export function glassCard(title, content, opts = {}) {
  const cls = opts.class || '';
  const pad = opts.padding || 'default';
  const padCls = pad === 'none' ? '' : pad === 'sm' ? 'pad-sm' : pad === 'lg' ? 'pad-lg' : 'pad-default';
  return `<div class="glass-card ${padCls} ${cls}">${title ? `<div class="glass-card-header">${esc(title)}</div>` : ''}${content}</div>`;
}

// --- Metric Card ---
export function metricCard(value, label, color) {
  const style = color ? `border-top: 3px solid ${color}` : '';
  return `<div class="metric-card glass-card" style="${style}">
    <div class="metric-value">${esc(String(value))}</div>
    <div class="metric-label">${esc(label)}</div>
  </div>`;
}

// --- Data Table ---
export function dataTable(id, columns, rows, opts = {}) {
  const expandable = opts.expandable || false;
  const emptyMsg = opts.empty || 'No data';
  const virtual = opts.virtual && rows.length > 100;
  const displayRows = virtual ? rows.slice(0, 100) : rows;

  let thead = '<thead><tr>';
  if (expandable) thead += '<th class="col-expand"></th>';
  for (const col of columns) {
    thead += `<th class="${col.class || ''}">${esc(col.label)}</th>`;
  }
  thead += '</tr></thead>';

  let tbody = '<tbody>';
  if (displayRows.length === 0) {
    const colSpan = columns.length + (expandable ? 1 : 0);
    tbody += `<tr><td colspan="${colSpan}" class="empty-state">${esc(emptyMsg)}</td></tr>`;
  }
  for (const row of displayRows) {
    const rowId = row._id || '';
    const rowCls = expandable ? 'expandable' : '';
    tbody += `<tr class="${rowCls}" data-row-id="${esc(rowId)}">`;
    if (expandable) {
      tbody += `<td class="col-expand"><span class="chevron">&#9656;</span></td>`;
    }
    for (const col of columns) {
      const val = col.render ? col.render(row) : esc(String(row[col.key] ?? '—'));
      tbody += `<td class="${col.class || ''}">${val}</td>`;
    }
    tbody += '</tr>';
    if (expandable && row._expandHtml) {
      tbody += `<tr class="expand-row hidden" data-expand-for="${esc(rowId)}"><td colspan="${columns.length + 1}">${row._expandHtml}</td></tr>`;
    }
  }
  tbody += '</tbody>';

  let footer = '';
  if (virtual) {
    footer = `<div class="table-footer">Showing 100 of ${rows.length} rows</div>`;
  }

  return `<table id="${esc(id)}" class="data-table">${thead}${tbody}</table>${footer}`;
}

// --- Detail Grid (for expand rows) ---
export function detailGrid(items) {
  let out = '<div class="detail-grid">';
  for (const { label, value } of items) {
    out += `<div class="detail-item"><span class="detail-label">${esc(label)}</span><span class="detail-value">${value}</span></div>`;
  }
  return out + '</div>';
}

// --- Timeline ---
export function timeline(events) {
  if (!events || events.length === 0) return '<div class="empty-state">No events</div>';
  let out = '<div class="timeline">';
  for (const e of events) {
    const cls = e.type === 'error' ? 'timeline-error' : e.type === 'warning' ? 'timeline-warn' : '';
    out += `<div class="timeline-item ${cls}">
      <div class="timeline-dot"></div>
      <div class="timeline-content">
        <span class="timeline-time">${esc(relativeTime(e.time))}</span>
        <span class="timeline-text">${esc(e.text)}</span>
      </div>
    </div>`;
  }
  return out + '</div>';
}

// --- Modal ---
export function showModal(title, bodyHtml, actions) {
  const existing = document.getElementById('modal-overlay');
  if (existing) existing.remove();

  let actionsHtml = '';
  for (const a of (actions || [])) {
    const cls = a.destructive ? 'btn btn-destructive' : a.primary ? 'btn btn-primary' : 'btn btn-secondary';
    actionsHtml += `<button class="${cls}" data-action="${esc(a.id)}">${esc(a.label)}</button>`;
  }

  const overlay = document.createElement('div');
  overlay.id = 'modal-overlay';
  overlay.className = 'modal-overlay';
  // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  overlay.innerHTML = `<div class="modal glass-card">
    <div class="modal-header">${esc(title)}<button class="modal-close" data-action="close">&times;</button></div>
    <div class="modal-body">${bodyHtml}</div>
    <div class="modal-actions">${actionsHtml}</div>
  </div>`;

  document.body.appendChild(overlay);

  return new Promise(resolve => {
    overlay.addEventListener('click', e => {
      const action = e.target.dataset?.action;
      if (action) { overlay.remove(); resolve(action); }
      if (e.target === overlay) { overlay.remove(); resolve('close'); }
    });
  });
}

// --- Toast ---
let toastTimer;
export function toast(message, type = 'info') {
  let container = document.getElementById('toast-container');
  if (!container) {
    container = document.createElement('div');
    container.id = 'toast-container';
    document.body.appendChild(container);
  }
  const el = document.createElement('div');
  el.className = `toast toast-${type}`;
  el.textContent = message;
  container.appendChild(el);
  setTimeout(() => el.remove(), 4000);
}

// --- Skeleton ---
export function skeleton(lines = 3) {
  let out = '<div class="skeleton-container">';
  for (let i = 0; i < lines; i++) {
    const w = 40 + Math.random() * 50;
    out += `<div class="skeleton-line" style="width:${w}%"></div>`;
  }
  return out + '</div>';
}

// --- Toggle Switch ---
export function toggleSwitch(id, checked, label, opts = {}) {
  const disabled = opts.disabled ? 'disabled' : '';
  const locked = opts.locked ? '<span class="lock-icon">&#128274;</span>' : '';
  return `<label class="toggle-label" for="${esc(id)}">
    <input type="checkbox" id="${esc(id)}" class="toggle-input" ${checked ? 'checked' : ''} ${disabled}>
    <span class="toggle-track"><span class="toggle-thumb"></span></span>
    <span class="toggle-text">${esc(label)}${locked}</span>
  </label>`;
}

// --- Pipeline Card ---
export function pipelineCard(card) {
  const agingClass = card.stageSec > 1800 ? 'aging-critical' : card.stageSec > 600 ? 'aging-warn' : '';
  return `<div class="pipeline-card ${agingClass}">
    <div class="pipeline-card-agent">${esc(card.agent)}</div>
    <div class="pipeline-card-branch">${esc(card.branch)}</div>
    <div class="pipeline-card-meta">v${card.version || 0} &middot; ${formatDuration(card.stageSec)}</div>
  </div>`;
}

// --- File Drop Zone ---
export function fileDropZone(id) {
  return `<div id="${esc(id)}" class="file-drop-zone">
    <div class="file-drop-icon">&#128196;</div>
    <div class="file-drop-text">Drop files here or click to upload</div>
    <input type="file" class="file-drop-input" multiple>
  </div>`;
}

// --- Horizontal Bar ---
export function barChart(items, maxVal) {
  if (!items || items.length === 0) return '<div class="empty-state">No data</div>';
  const max = maxVal || Math.max(...items.map(i => i.value), 1);
  let out = '<div class="bar-chart">';
  for (const item of items) {
    const pct = Math.round((item.value / max) * 100);
    out += `<div class="bar-row">
      <span class="bar-label">${esc(item.label)}</span>
      <div class="bar-track"><div class="bar-fill" style="width:${pct}%"></div></div>
      <span class="bar-value">${item.value}</span>
    </div>`;
  }
  return out + '</div>';
}
