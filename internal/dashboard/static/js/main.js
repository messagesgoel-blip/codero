// main.js — Dashboard entry point. Wires store, router, SSE, renderers.

import store from './store.js';
import { registerRoute, startRouter, refreshCurrentPage } from './router.js';
import { connectSSE, bus } from './sse.js';
import { $, setText, esc } from './utils.js';

import { initOverview, renderOverview, refreshOverview } from './renderers/overview.js';
import { initSessions, renderSessions, refreshSessions } from './renderers/sessions.js';
import { initPipeline, renderPipeline, refreshPipeline } from './renderers/pipeline.js';
import { initTasks, renderTasks, refreshTasks } from './renderers/tasks.js';
import { initGate, renderGate, refreshGate } from './renderers/gate.js';
import { initChat, renderChat, refreshChat, renderFloatingChat } from './renderers/chat.js';
import { initArchives, renderArchives, refreshArchives } from './renderers/archives.js';
import { initSettings, renderSettings, refreshSettings } from './renderers/settings.js';

// --- Register all routes ---
registerRoute('overview', { init: initOverview, render: renderOverview, refresh: refreshOverview });
registerRoute('sessions', { init: initSessions, render: renderSessions, refresh: refreshSessions });
registerRoute('pipeline', { init: initPipeline, render: renderPipeline, refresh: refreshPipeline });
registerRoute('tasks', { init: initTasks, render: renderTasks, refresh: refreshTasks });
registerRoute('gate', { init: initGate, render: renderGate, refresh: refreshGate });
registerRoute('chat', { init: initChat, render: renderChat, refresh: refreshChat });
registerRoute('archives', { init: initArchives, render: renderArchives, refresh: refreshArchives });
registerRoute('settings', { init: initSettings, render: renderSettings, refresh: refreshSettings });

// --- Theme ---
function applyTheme(theme) {
  document.documentElement.classList.toggle('light', theme === 'light');
  document.documentElement.classList.toggle('dark', theme !== 'light');
  localStorage.setItem('codero-theme', theme);
  store.set({ ui: { theme } });
}

const btnTheme = $('btn-theme');
if (btnTheme) {
  btnTheme.addEventListener('click', () => {
    const current = store.state.ui.theme;
    applyTheme(current === 'dark' ? 'light' : 'dark');
  });
}
applyTheme(store.state.ui.theme);

// --- Header title ---
const titleMap = {
  overview: 'Overview', sessions: 'Sessions & Agents', pipeline: 'Delivery Pipeline',
  tasks: 'Tasks & Repos', gate: 'Gate Checks', chat: 'Chat Assistant',
  archives: 'Archives & Timing', settings: 'Settings',
};
store.subscribe('ui', (ui) => {
  setText('header-title', titleMap[ui.activeTab] || 'Codero');
  // Show/hide floating chat button
  const chatBtn = $('chat-floating-btn');
  if (chatBtn) chatBtn.style.display = ui.activeTab === 'chat' ? 'none' : '';
});

// --- Floating chat ---
const chatFloatingBtn = $('chat-floating-btn');
const chatFloatingPanel = $('chat-floating-panel');
if (chatFloatingBtn && chatFloatingPanel) {
  chatFloatingBtn.addEventListener('click', () => {
    const visible = chatFloatingPanel.style.display !== 'none';
    chatFloatingPanel.style.display = visible ? 'none' : 'flex';
    if (!visible) {
      renderFloatingChat(chatFloatingPanel);
    }
  });
}

// --- Sidebar collapse ---
const sidebar = $('sidebar');
const btnSidebarCollapse = $('btn-sidebar-collapse');
if (sidebar && btnSidebarCollapse) {
  const STORAGE_KEY = 'codero-sidebar-collapsed';
  if (localStorage.getItem(STORAGE_KEY) === '1') {
    sidebar.classList.add('collapsed');
  }
  btnSidebarCollapse.addEventListener('click', () => {
    sidebar.classList.toggle('collapsed');
    localStorage.setItem(STORAGE_KEY, sidebar.classList.contains('collapsed') ? '1' : '0');
  });
}

// --- Command palette (Cmd+K) ---
document.addEventListener('keydown', e => {
  if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
    e.preventDefault();
    toggleCommandPalette();
  }
  if (e.key === 'Escape') {
    closeCommandPalette();
    if (chatFloatingPanel) chatFloatingPanel.style.display = 'none';
  }
});

const btnCmdPalette = $('btn-cmd-palette');
if (btnCmdPalette) btnCmdPalette.addEventListener('click', toggleCommandPalette);

function toggleCommandPalette() {
  let overlay = $('cmd-overlay');
  if (overlay) { overlay.remove(); return; }

  overlay = document.createElement('div');
  overlay.id = 'cmd-overlay';
  overlay.className = 'cmd-overlay';
  overlay.innerHTML = `<div class="cmd-palette">
    <input class="cmd-input" placeholder="Search sessions, tasks, repos..." autofocus>
    <div class="cmd-results" id="cmd-results"></div>
  </div>`;
  document.body.appendChild(overlay);

  const input = overlay.querySelector('.cmd-input');
  input.addEventListener('input', () => renderCmdResults(input.value));
  input.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeCommandPalette();
    if (e.key === 'Enter') {
      const first = overlay.querySelector('.cmd-result');
      if (first) first.click();
    }
  });
  overlay.addEventListener('click', e => { if (e.target === overlay) closeCommandPalette(); });
}

function closeCommandPalette() {
  const overlay = $('cmd-overlay');
  if (overlay) overlay.remove();
}

function renderCmdResults(query) {
  const results = $('cmd-results');
  if (!results) return;
  const q = query.toLowerCase().trim();
  if (!q) { results.innerHTML = '<div class="cmd-result"><span class="cmd-result-hint">Type to search...</span></div>'; return; }

  let items = [];
  // Search pages
  for (const [tab, label] of Object.entries(titleMap)) {
    if (label.toLowerCase().includes(q) || tab.includes(q)) {
      items.push({ icon: '&#128196;', text: label, hint: 'Page', action: () => { window.location.hash = '#' + tab; closeCommandPalette(); } });
    }
  }
  // Search sessions
  for (const s of store.state.sessions || []) {
    if ((s.agent || '').toLowerCase().includes(q) || (s.repo || '').toLowerCase().includes(q) || (s.branch || '').toLowerCase().includes(q)) {
      items.push({ icon: '&#9881;', text: `${s.agent} — ${s.repo}/${s.branch}`, hint: 'Session', action: () => { window.location.hash = '#sessions'; closeCommandPalette(); } });
    }
  }
  // Search queue
  for (const t of store.state.queue || []) {
    if ((t.repo || '').toLowerCase().includes(q) || (t.branch || '').toLowerCase().includes(q)) {
      items.push({ icon: '&#9776;', text: `${t.repo}/${t.branch}`, hint: 'Queue', action: () => { window.location.hash = '#tasks'; closeCommandPalette(); } });
    }
  }

  if (items.length === 0) {
    // Route to chat if looks like a question
    items.push({ icon: '&#9993;', text: `Ask: "${query}"`, hint: 'Chat', action: () => {
      store.set({ chat: { ...store.state.chat, pendingPrompt: query } });
      window.location.hash = '#chat';
      closeCommandPalette();
    }});
  }

  // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  results.innerHTML = items.slice(0, 10).map((item, i) =>
    `<div class="cmd-result" data-idx="${i}">${item.icon ? `<span class="cmd-result-icon">${item.icon}</span>` : ''}<span class="cmd-result-text">${esc(item.text)}</span><span class="cmd-result-hint">${esc(item.hint)}</span></div>`
  ).join('');

  results.querySelectorAll('.cmd-result').forEach((el, i) => {
    el.addEventListener('click', () => items[i].action());
  });
}

// --- SSE status indicator ---
bus.on('sse:connected', () => { setText('hb-sse-status', 'SSE'); });
bus.on('sse:disconnected', () => { setText('hb-sse-status', 'SSE off'); });

// --- Health bar clock ---
function updateClock() {
  setText('hb-time', new Date().toLocaleTimeString());
}
setInterval(updateClock, 1000);
updateClock();

// --- Global error boundary ---
window.onerror = (msg, src, line, col, err) => {
  console.error('Dashboard error:', msg, src, line, col, err);
};
window.addEventListener('unhandledrejection', e => {
  console.error('Unhandled rejection:', e.reason);
});

// --- Boot ---
startRouter();
connectSSE();

// Background refresh every 15s
setInterval(() => { refreshCurrentPage(); }, 15000);
