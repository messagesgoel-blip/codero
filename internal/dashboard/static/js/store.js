// store.js — Centralized state store with keyed pub/sub.
// Single source of truth for all dashboard data and UI state.

const store = {
  _state: {
    overview: null,
    sessions: [],
    assignments: [],
    pipeline: [],
    queue: [],
    queueStats: null,
    repos: [],
    events: [],
    archives: [],
    health: null,
    settings: null,
    gateConfig: null,
    gateChecks: null,
    compliance: null,
    agents: null,
    nodeRepos: null,
    trackingConfig: null,
    scorecard: null,
    blockReasons: [],
    gateHealth: [],
    chat: { messages: [], conversationId: null, streaming: false },
    ui: {
      activeTab: 'overview',
      theme: localStorage.getItem('codero-theme') || 'dark',
      filters: {},
      expandedRows: new Set(),
      commandPaletteOpen: false,
      chatFloating: false,
      modal: null,
    },
  },

  _listeners: new Map(),

  get state() {
    return this._state;
  },

  set(partial) {
    const changed = new Set();
    for (const [key, value] of Object.entries(partial)) {
      if (key === 'ui') {
        this._state.ui = { ...this._state.ui, ...value };
        changed.add('ui');
      } else {
        this._state[key] = value;
        changed.add(key);
      }
    }
    for (const key of changed) {
      const fns = this._listeners.get(key);
      if (fns) fns.forEach(fn => fn(this._state[key], this._state));
    }
    // Wildcard listeners
    const all = this._listeners.get('*');
    if (all) all.forEach(fn => fn(this._state));
  },

  subscribe(key, fn) {
    if (!this._listeners.has(key)) this._listeners.set(key, []);
    this._listeners.get(key).push(fn);
    return () => {
      const fns = this._listeners.get(key);
      if (fns) {
        const idx = fns.indexOf(fn);
        if (idx >= 0) fns.splice(idx, 1);
      }
    };
  },

  select(key) {
    return this._state[key];
  },
};

export default store;
