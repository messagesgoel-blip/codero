// router.js — Hash-based router with lifecycle hooks.

import store from './store.js';

const routes = new Map();
let currentPage = null;

export function registerRoute(name, { init, render, refresh }) {
  routes.set(name, { init, render, refresh, initialized: false });
}

export function navigate(tab) {
  window.location.hash = '#' + tab;
}

export function currentTab() {
  return store.state.ui.activeTab;
}

export function startRouter() {
  window.addEventListener('hashchange', handleHash);
  handleHash();
}

function handleHash() {
  const hash = window.location.hash.replace('#', '') || 'overview';
  const route = routes.get(hash);
  if (!route) {
    window.location.hash = '#overview';
    return;
  }

  // Deactivate old tab
  document.querySelectorAll('.nav-item').forEach(el => el.classList.remove('active'));
  const navItem = document.querySelector(`.nav-item[data-tab="${hash}"]`);
  if (navItem) navItem.classList.add('active');

  // Update store
  store.set({ ui: { activeTab: hash } });

  // Show correct page
  document.querySelectorAll('.page').forEach(el => el.classList.remove('active'));
  const page = document.getElementById('page-' + hash);
  if (page) page.classList.add('active');

  // Initialize once, then render
  if (!route.initialized && route.init) {
    route.init();
    route.initialized = true;
  }
  if (route.render) route.render();
  if (route.refresh) route.refresh();

  currentPage = hash;
}

export function refreshCurrentPage() {
  const route = routes.get(currentPage);
  if (route && route.refresh) route.refresh();
}
