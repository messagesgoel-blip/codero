'use strict';
const API  = '/api/v1/dashboard';
const POLL = 10000;

let activeTab     = 'processes';
let settingsLocal = null;
let settingsDirty = false;
let sseSource     = null;
let pollTimer     = null;
let allEvents     = [];
let eventsUnavailable = false;
let eventFilter   = 'ALL';
let findingsCache = null;
let integrationGridBound = false;
let findingsView  = 'cards'; // 'cards' | 'table'
let chatHistory   = [];
let chatBusy      = false;
let chatSuggestionState = [];
let chatActionState = [];
let chatPromptDelegationBound = false;

/* THEME */
const mq = window.matchMedia('(prefers-color-scheme: dark)');
let currentTheme = 'dark';
function applyTheme(t) {
  const resolved = t === 'system' ? (mq.matches ? 'dark' : 'light') : t;
  document.documentElement.setAttribute('data-theme', resolved);
  ['system','dark','light'].forEach(x => el('tt-'+x)?.classList.toggle('active', x === t));
  currentTheme = t;
}
function setTheme(t) { applyTheme(t); }
mq.addEventListener('change', () => { if (currentTheme === 'system') applyTheme('system'); });
applyTheme('dark');

/* TABS */
function showTab(id, tab) {
  if (!tab) return;
  document.querySelectorAll('.page').forEach(p => p.classList.remove('active'));
  document.querySelectorAll('.tb-tab').forEach(t => t.classList.remove('active'));
  el('page-'+id).classList.add('active');
  tab.classList.add('active');
  document.querySelectorAll('.tb-tab').forEach(function(t) {
    t.setAttribute('aria-selected', t === tab ? 'true' : 'false');
  });
  activeTab = id;
  if (id === 'settings' && !settingsDirty) loadSettings();
  if (id === 'findings')     renderFindings(findingsCache);
  if (id === 'eventlogs')    renderEvents();
  renderChatSuggestions(defaultChatSuggestionsForTab(id));
  renderChatActions(defaultChatActionsForTab(id));
  updateChatContext(id);
}

function tabKey(ev, tab) {
  if (ev.key === 'Enter' || ev.key === ' ') {
    ev.preventDefault();
    showTab(tab.getAttribute('data-tab'), tab);
    return false;
  }
  if (ev.key !== 'ArrowLeft' && ev.key !== 'ArrowRight') return true;
  var tabs = Array.from(document.querySelectorAll('.tb-tab'));
  if (!tabs.length) return false;
  var idx = tabs.indexOf(tab);
  if (idx < 0) return false;
  ev.preventDefault();
  var next = ev.key === 'ArrowRight'
    ? tabs[(idx + 1) % tabs.length]
    : tabs[(idx - 1 + tabs.length) % tabs.length];
  next.focus();
  showTab(next.getAttribute('data-tab'), next);
  return false;
}

/* API */
async function apiFetch(path, opts) {
  const res = await fetch(API + path, opts || {});
  if (!res.ok) {
    let msg = res.statusText;
    try { const j = await res.json(); msg = j.error || msg; } catch (_e) {}
    const err = new Error(msg); err.status = res.status; throw err;
  }
  return res.json();
}

/* MERGE STATUS */
function updateMergeBar(health, sessions) {
  const mbS = el('mb-status'), mbD = el('mb-detail');
  if (!health || health.unavailable || (health.database && health.database.status === 'unavailable')) {
    mbS.textContent = 'UNAVAILABLE'; mbS.className = 'mb-status unknown';
    mbD.textContent = 'system health unavailable';
    updateTopbarMergeBadge('unknown', 'UNAVAIL');
    return;
  }
  if (sessions === null) {
    mbS.textContent = 'UNAVAILABLE'; mbS.className = 'mb-status unknown';
    mbD.textContent = 'active sessions feed unavailable';
    updateTopbarMergeBadge('unknown', 'UNAVAIL');
    return;
  }
  const blocked = (sessions || []).filter(s => s.activity_state === 'blocked').length;
  if (blocked > 0) {
    mbS.textContent = 'BLOCKED'; mbS.className = 'mb-status blocked';
    mbD.textContent = blocked + ' session' + (blocked !== 1 ? 's' : '') + ' blocked';
    updateTopbarMergeBadge('blocked', 'Merge Blocked');
  } else if (health && health.database && health.database.status !== 'ok') {
    mbS.textContent = 'DEGRADED'; mbS.className = 'mb-status degraded';
    mbD.textContent = 'database ' + (health.database.status || 'error');
    updateTopbarMergeBadge('active', 'Degraded');
  } else {
    const active = (sessions || []).length;
    mbS.textContent = active > 0 ? 'ACTIVE' : 'IDLE';
    mbS.className   = 'mb-status ok';
    mbD.textContent = active > 0
      ? active + ' session' + (active !== 1 ? 's' : '') + ' in progress'
      : 'no active sessions';
    updateTopbarMergeBadge(active > 0 ? 'active' : 'ready', active > 0 ? 'Active' : 'Ready');
  }
  // Update topbar branch and PR info from first session
  var first = sessions && sessions[0];
  if (first) {
    el('tb-pr-num').textContent = first.pr_number > 0 ? 'PR #' + first.pr_number : 'PR #\u2014';
    el('tb-branch').textContent = first.branch || '\u2014';
  }
}

function updateTopbarMergeBadge(cls, label) {
  var b = el('tb-merge-badge');
  if (!b) return;
  b.textContent = label;
  b.className = 'tb-merge-badge ' + cls;
}

/* HEALTH BAR */
function renderHealthBar(h) {
  const unavailable = !h || h.unavailable;
  const db    = unavailable ? { status: 'unavailable' } : (h.database || {});
  const sessF = unavailable ? { status: 'unavailable' } : (h.feeds || {}).active_sessions || {};
  const gateF = unavailable ? { status: 'unavailable' } : (h.feeds || {}).gate_checks     || {};
  elSet('hb-db', db.status || 'unknown', 'hb-val ' + (db.status || 'unknown'));
  dotSet('hb-db-dot', db.status || 'unknown');
  var agentCount = unavailable ? null : h.active_agent_count;
  el('hb-agents').textContent    = agentCount != null ? agentCount : '\u2014';
  el('agents-count').textContent = agentCount != null ? agentCount : '\u2014';
  elSet('hb-sessions-status', sessF.status || 'unavailable', 'hb-val ' + (sessF.status || 'unavailable'));
  dotSet('hb-sessions-dot', sessF.status || 'unavailable');
  el('hb-sessions-age').textContent = sessF.freshness_sec != null ? fmtAge(sessF.freshness_sec) : '';
  elSet('hb-gate-status', gateF.status || 'unavailable', 'hb-val ' + (gateF.status || 'unavailable'));
  dotSet('hb-gate-dot', gateF.status || 'unavailable');
  el('hb-gate-age').textContent = gateF.freshness_sec != null ? fmtAge(gateF.freshness_sec) : '';
  el('hb-refreshed').textContent = unavailable ? '\u2014' : fmtTime(h.generated_at);
  // Security score ring (from gate-check report)
  var ss = unavailable ? null : h.security_score;
  var srScore = el('sr-score'), srPct = el('sr-pct'), srRing = el('sr-ring-fill');
  if (srScore) srScore.textContent = ss ? ss.score + '/10' : '\u2014';
  if (srPct)   srPct.textContent   = ss ? ss.pct.toFixed(0) + '%' : '\u2014';
  if (srRing && ss) {
    var circumference = 100.5;
    var offset = circumference - (ss.pct / 100) * circumference;
    srRing.setAttribute('stroke-dashoffset', offset.toFixed(1));
  }
  // Coverage percentage
  var cov = unavailable ? null : h.coverage_pct;
  var covEl = el('stat-coverage');
  if (covEl) covEl.textContent = cov != null ? cov.toFixed(1) + '%' : '\u2014';
  // Review ETA from health data
  var etaMin = unavailable ? null : h.eta_min;
  var etaEl = el('stat-eta');
  var hbEta = el('hb-eta'), hbEtaVal = el('hb-eta-val');
  if (etaEl) etaEl.textContent = etaMin != null ? etaMin + ' min' : '\u2014';
  if (hbEta && hbEtaVal) {
    if (etaMin != null) { hbEta.style.display = ''; hbEtaVal.textContent = etaMin + ' min'; }
    else { hbEta.style.display = 'none'; }
  }
}
function elSet(id, text, cls) { const e = el(id); if (e) { e.textContent = text; e.className = cls; } }
function dotSet(id, st)       { const e = el(id); if (e) e.className = 'hb-dot ' + st; }

function pipelineStatusClass(status) {
  status = String(status || '').toLowerCase();
  if (status === 'pass') return 'pass';
  if (status === 'fail') return 'fail';
  if (status === 'skip') return 'skip';
  if (status === 'disabled') return 'disabled';
  if (status === 'running') return 'running';
  if (status === 'infra_bypass') return 'infra_bypass';
  return 'pending';
}

function renderPipelineRail(data) {
  var summary = el('pipeline-rail-summary');
  var list = el('pipeline-stage-list');
  var progFill = el('pipeline-progress-fill');
  if (!summary || !list) return;

  if (!data || data.unavailable || !data.report || !data.report.checks) {
    summary.textContent = 'waiting for gate-check report';
    list.innerHTML = '<div class="pipeline-stage pending"><span class="dot"></span><span class="pipeline-stage-name">pipeline idle</span><span class="pipeline-stage-meta">no report</span></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    if (progFill) { progFill.style.width = '0%'; progFill.className = 'pipeline-progress-fill'; }
    updatePipelineHero(0, 0, 0);
    return;
  }

  var checks = data.report.checks || [];
  if (!checks.length) {
    summary.textContent = 'waiting for gate-check report';
    list.innerHTML = '<div class="pipeline-stage pending"><span class="dot"></span><span class="pipeline-stage-name">pipeline idle</span><span class="pipeline-stage-meta">no checks</span></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    if (progFill) { progFill.style.width = '0%'; progFill.className = 'pipeline-progress-fill'; }
    updatePipelineHero(0, 0, 0);
    return;
  }

  var counts = { pass: 0, fail: 0, skip: 0, disabled: 0, infra_bypass: 0, pending: 0 };
  list.innerHTML = checks.map(function(c) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    var status = pipelineStatusClass(c.status);
    if (Object.prototype.hasOwnProperty.call(counts, status)) counts[status]++;
    var meta = c.reason_code || c.reason || c.group || '';
    var duration = c.duration_ms > 0 ? (c.duration_ms / 1000).toFixed(2) + 's' : '';
    var metaText = duration || meta;
    return '<div class="pipeline-stage ' + status + '">' +
      '<span class="dot"></span>' +
      '<span class="pipeline-stage-name">' + esc(c.name || c.id || '\u2014') + '</span>' +
      (metaText ? '<span class="pipeline-stage-meta">' + esc(metaText) + '</span>' : '') +
    '</div>';
  }).join('');

  summary.textContent = checks.length + ' steps · ' + counts.pass + ' pass · ' + counts.fail + ' fail · ' + counts.skip + ' skip · ' + counts.disabled + ' disabled' + (counts.infra_bypass > 0 ? ' · ' + counts.infra_bypass + ' bypass' : '');

  // Pipeline progress bar
  // infra_bypass counts as terminal (completed), not pending
  var completed = counts.pass + counts.fail + counts.skip + counts.disabled + counts.infra_bypass;
  var pct = checks.length > 0 ? Math.round(completed / checks.length * 100) : 0;
  if (progFill) {
    progFill.style.width = pct + '%';
    progFill.className = 'pipeline-progress-fill' + (counts.fail > 0 ? ' has-fail' : '');
  }
  updatePipelineHero(pct, counts.pass, checks.length);
}

/* ACTIVE AGENTS */
function renderSessions(sessions) {
  const c = el('proc-content');
  if (sessions === null) {
    c.innerHTML = '<div class="state-msg error">active sessions feed unavailable</div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    var prEl = el('tb-pr-num');
    if (prEl) prEl.textContent = 'PR #\u2014';
    return;
  }
  if (!sessions || !sessions.length) {
    c.innerHTML = '<div class="state-msg-rich"><span class="state-icon">&#x1F4AD;</span><span class="state-title">No Active Sessions</span><span class="state-detail">When agents begin reviewing, their progress will appear here. Start a review with <code>codero gate-check</code>.</span></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    var prEl = el('tb-pr-num');
    if (prEl) prEl.textContent = 'PR #\u2014';
    return;
  }
  c.innerHTML = sessions.map(function(s) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    var task  = s.task || {};
    var tid   = (task.id && task.id !== 'unknown') ? '<span class="agent-task-id">#' + esc(task.id) + '</span>' : '';
    var title = (task.title && task.title !== 'unknown') ? esc(task.title) : esc(s.branch || s.session_id);
    var phase = (task.phase && task.phase !== 'unknown') ? esc(task.phase) : '';
    var state = s.activity_state || 'unknown';
    var pct   = agentProgressPct(state);
    return '<div class="agent-row">' +
      '<div class="agent-icon">' + agentIcon(s.owner_agent) + '</div>' +
      '<div class="agent-main">' +
        '<div class="agent-header">' +
          '<span class="agent-name">' + esc(s.owner_agent || 'unknown') + '</span>' +
          (phase ? '<span class="agent-sep">&mdash;</span><span class="agent-phase">' + phase + '</span>' : '') +
        '</div>' +
        '<div class="agent-sub">' + title + tid + '</div>' +
        '<div class="agent-meta">' +
          '<div class="am-item"><span class="key">repo</span>&nbsp;' + esc(s.repo || '\u2014') + '</div>' +
          '<div class="am-item"><span class="key">branch</span>&nbsp;' + esc(s.branch || '\u2014') + '</div>' +
          '<div class="am-item"><span class="key">elapsed</span>&nbsp;' + fmtElapsed(s.elapsed_sec || 0) + '</div>' +
          '<div class="am-item"><span class="key">heartbeat</span>&nbsp;' + (s.last_heartbeat_at ? fmtRelative(s.last_heartbeat_at) : '&mdash;') + '</div>' +
          '<div class="am-item"><span class="key">PR</span>&nbsp;' + (s.pr_number > 0 ? '#' + s.pr_number : '\u2014') + '</div>' +
          '<span class="am-badge ' + attrEsc(state) + '">' + esc(state) + '</span>' +
        '</div>' +
        '<div class="agent-progress-wrap"><div class="agent-progress-bar ' + attrEsc(state) + '" style="width:' + pct + '%"></div></div>' +
      '</div>' +
      '<div class="agent-right"><div class="agent-state-dot ' + attrEsc(state) + '"></div></div>' +
    '</div>';
  }).join('');
  renderActivityFeed();
}
function agentProgressPct(state) {
  if (state === 'active')   return 85;
  if (state === 'waiting')  return 30;
  if (state === 'blocked')  return 100;
  return 0;
}
function agentIcon(agent) {
  var a = (agent || '').toLowerCase();
  if (a.indexOf('claude') >= 0)   return '&#x1F916;';
  if (a.indexOf('copilot') >= 0)  return '&#x1F419;';
  if (a.indexOf('gemini') >= 0)   return '&#x2728;';
  if (a.indexOf('codero') >= 0)   return '&#x203A;_';
  return '&#x2B21;';
}

/* ACTIVITY FEED */
function renderActivityFeed() {
  var af = el('activity-feed');
  var rows = el('af-rows');
  if (!af || !rows) return;
  if (!allEvents || !allEvents.length) { af.style.display = 'none'; return; }
  af.style.display = '';
  var slice = allEvents.slice(0, 5);
  rows.innerHTML = slice.map(function(ev) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    var sev  = inferSev(ev);
    var dot  = sev !== 'INFO' ? sev.toLowerCase() : 'info';
    var msg  = evMsg(ev);
    var ts   = fmtTime(ev.created_at);
    return '<div class="af-row">' +
      '<div class="af-dot ' + dot + '"></div>' +
      '<span class="af-ts">' + ts + '</span>' +
      '<span class="af-msg">' + esc(msg) + '</span>' +
    '</div>';
  }).join('');
}

/* EVENT LOGS */
function setEvtFilter(f, btn) {
  eventFilter = f;
  document.querySelectorAll('.filter-btn').forEach(function(b) { b.classList.remove('active'); });
  btn.classList.add('active');
  renderEvents();
}
function renderEvents(events) {
  var source = typeof events === 'undefined' ? allEvents : events;
  var list = el('events-list'), cnt = el('ef-count');
  if (source === null || (eventsUnavailable && source === allEvents && !source.length)) {
    list.innerHTML = '<div class="state-msg error">activity feed unavailable</div>'; cnt.textContent = ''; return; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  }
  if (!source.length) { list.innerHTML = '<div class="state-msg">no events yet</div>'; cnt.textContent = ''; return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  var filtered = eventFilter === 'ALL' ? source : source.filter(function(e) { return inferSev(e) === eventFilter; });
  cnt.textContent = filtered.length + ' events';
  if (!filtered.length) { list.innerHTML = '<div class="state-msg">no ' + eventFilter.toLowerCase() + ' events</div>'; return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  list.innerHTML = filtered.map(function(ev) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    var sev  = inferSev(ev);
    var ts   = fmtTime(ev.created_at);
    var msg  = evMsg(ev);
    var dot  = sev !== 'INFO' ? sev.toLowerCase() : 'info';
    var msgC = sev !== 'INFO' ? sev.toLowerCase() : '';
    var badge = ['CRITICAL','HIGH','MEDIUM','LOW'].indexOf(sev) >= 0
      ? '<span class="ev-badge ' + sev + '">' + sev + '</span>' : '';
    return '<div class="event-row"><span class="ev-ts">' + ts + '</span>' +
      '<div class="ev-dot ' + dot + '"></div>' +
      '<span class="ev-msg ' + msgC + '">' + esc(msg) + '</span>' + badge + '</div>';
  }).join('');
}
function inferSev(ev) {
  var t = (ev.event_type || '').toLowerCase();
  if (t.indexOf('secret') >= 0 || t.indexOf('critical') >= 0) return 'CRITICAL';
  if (t.indexOf('fail') >= 0 || t.indexOf('blocked') >= 0 || t.indexOf('error') >= 0) return 'HIGH';
  if (t.indexOf('warn') >= 0 || t.indexOf('degrad') >= 0) return 'MEDIUM';
  if (t.indexOf('low') >= 0) return 'LOW';
  try { var p = JSON.parse(ev.payload || '{}'); var s = (p.severity || '').toUpperCase(); if (['CRITICAL','HIGH','MEDIUM','LOW'].indexOf(s) >= 0) return s; } catch (_e) {}
  return 'INFO';
}
function evMsg(ev) {
  try {
    var p = JSON.parse(ev.payload || '{}');
    if (p.message) return p.message;
    if (p.to_state) return (ev.repo || '') + '/' + (ev.branch || '') + ' -> ' + p.to_state;
  } catch (_e) {}
  return (ev.event_type || '') + (ev.repo ? ' on ' + ev.repo + (ev.branch ? '/' + ev.branch : '') : '');
}

/* FINDINGS VIEW TOGGLE */
function setFindingsView(view, btn) {
  findingsView = view;
  document.querySelectorAll('.view-toggle-btn').forEach(function(b) { b.classList.remove('active'); });
  btn.classList.add('active');
  renderFindings(findingsCache);
}

/* FINDINGS */
function renderFindings(data) {
  findingsCache = data;
  var cards  = el('findings-cards');
  var gtwrap = el('gate-table-wrap');

  if (data && data.unavailable) {
    ['f-total','f-failed','f-overall','f-sev-fail','f-sev-skip','f-sev-bypass','f-sev-pass'].forEach(function(id) { var e = el(id); if (e) e.textContent = '\u2014'; });
    if (cards)  cards.innerHTML  = '<div class="state-msg-rich"><span class="state-icon">&#x26A0;</span><span class="state-title">Gate-Check Report Unavailable</span><span class="state-detail">The gate-check service could not be reached. Check system health.</span></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    if (gtwrap) gtwrap.innerHTML = '<div class="state-msg-rich"><span class="state-icon">&#x26A0;</span><span class="state-title">Gate-Check Report Unavailable</span><span class="state-detail">The gate-check service could not be reached.</span></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    setStatBadges(0, 0);
    updateActionBanner(0, 0);
    updateReviewHero(data);
    renderResolutionPanel([]);
    renderPipelineRail(data);
    return;
  }
  if (!data || !data.report) {
    ['f-total','f-failed','f-overall','f-sev-fail','f-sev-skip','f-sev-bypass','f-sev-pass'].forEach(function(id) { var e = el(id); if (e) e.textContent = '\u2014'; });
    if (cards)  cards.innerHTML  = '<div class="state-msg-rich"><span class="state-icon">&#x2261;</span><span class="state-title">No Gate-Check Report</span><span class="state-detail">Run <code>codero gate-check</code> to generate a report and populate the pipeline.</span></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    if (gtwrap) gtwrap.innerHTML = '<div class="state-msg-rich"><span class="state-icon">&#x2261;</span><span class="state-title">No Gate-Check Report</span><span class="state-detail">Run <code>codero gate-check</code> to generate one.</span></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    setStatBadges(0, 0);
    updateActionBanner(0, 0);
    updateReviewHero(data);
    renderResolutionPanel([]);
    renderPipelineRail(data);
    return;
  }
  var rpt = data.report, sum = rpt.summary || {};
  el('f-total').textContent   = sum.total  != null ? sum.total  : '\u2014';
  el('f-failed').textContent  = sum.failed != null ? sum.failed : '\u2014';
  var os = (sum.overall_status || '').toLowerCase();
  elSet('f-overall', sum.overall_status || '\u2014', 'stat-val ' + (os === 'pass' ? 'green' : os === 'fail' ? 'red' : 'amber'));
  el('f-sev-fail').textContent   = sum.failed           != null ? sum.failed           : '\u2014';
  el('f-sev-skip').textContent   = ((sum.skipped || 0) + (sum.disabled || 0));
  el('f-sev-bypass').textContent = sum.infra_bypassed   != null ? sum.infra_bypassed   : '\u2014';
  el('f-sev-pass').textContent   = sum.passed           != null ? sum.passed           : '\u2014';

  var checks = rpt.checks || [];

  // Derive banner counts from actual check severity, not gate summary counters.
  // required_failed / failed are gate counters, not severity labels.
  var failedChecks = checks.filter(function(c) { return (c.status||'').toLowerCase() === 'fail'; });
  var hasSeverity  = failedChecks.some(function(c) { return c.severity; });
  var critCount, highCount;
  if (hasSeverity) {
    critCount = failedChecks.filter(function(c) { return (c.severity||'').toLowerCase() === 'critical'; }).length;
    highCount = failedChecks.filter(function(c) { return (c.severity||'').toLowerCase() === 'high'; }).length;
  } else {
    // Fallback: required+fail → critical proxy, non-required fail → high proxy
    critCount = sum.required_failed || 0;
    highCount = Math.max(0, failedChecks.length - critCount);
  }
  setStatBadges(critCount, highCount);
  updateActionBanner(critCount, highCount);
  updateReviewHero(data);

  // Render resolution panel and blocker summary from failing checks
  var failChecks = checks.filter(function(c) { return (c.status || '').toLowerCase() === 'fail'; });
  renderResolutionPanel(failChecks);
  updateBlockerSummary(failChecks);
  renderPipelineRail(data);

  // --- CARD VIEW ---
  if (findingsView === 'cards') {
    if (cards)  cards.style.display  = '';
    if (gtwrap) gtwrap.style.display = 'none';

    if (!checks.length) {
      cards.innerHTML = '<div class="state-msg">no checks in report</div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    } else {
      // Group by severity bucket based on status
      var critical = checks.filter(function(c) { var st = (c.status||'').toLowerCase(); return st === 'fail' && (c.severity === 'critical' || ((c.name||'').toLowerCase().indexOf('secret') >= 0)); });
      var high     = checks.filter(function(c) { var st = (c.status||'').toLowerCase(); return st === 'fail' && critical.indexOf(c) < 0; });
      var medium   = checks.filter(function(c) { var st = (c.status||'').toLowerCase(); return st === 'skip' || st === 'disabled' || st === 'infra_bypass'; });
      var low      = checks.filter(function(c) { var st = (c.status||'').toLowerCase(); return st === 'pass'; });

      var html = '';
      html += renderFindingGroup('CRITICAL', 'critical', critical);
      html += renderFindingGroup('HIGH', 'high', high);
      html += renderFindingGroup('MEDIUM', 'medium', medium);
      if (low.length) html += renderFindingGroup('LOW', 'low', low);
      if (!html) html = '<div class="state-msg">no findings to display</div>';
      cards.innerHTML = html; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    }
  } else {
    // --- TABLE VIEW ---
    if (cards)  cards.style.display  = 'none';
    if (gtwrap) gtwrap.style.display = '';

    if (!checks.length) {
      gtwrap.innerHTML = '<div class="state-msg">no checks in report</div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    } else {
      var rows = checks.map(function(c, i) {
        var st  = (c.status || '').toLowerCase();
        var ds = c.display_state || toDisplayState(st);
        if (['passing', 'failing', 'disabled'].indexOf(ds) < 0) ds = toDisplayState(st);
        var tool = c.tool_name || c.group || '\u2014';
        var dur  = c.duration_ms > 0 ? (c.duration_ms / 1000).toFixed(2) + 's' : '\u2014';
        return '<div class="gate-table-row">' +
          '<span class="gtr-id">' + String(i+1).padStart(3,'0') + '</span>' +
          '<span>' + esc(c.name || '\u2014') + '</span>' +
          '<span class="gtr-file" title="' + attrEsc(c.tool_path || '') + '">' + esc(tool) + '</span>' +
          '<span class="gtr-res ' + attrEsc(ds) + '">' + ds.toUpperCase() + '</span>' +
          '<span class="gtr-status ' + attrEsc(st) + '">' + esc(c.status || '\u2014') + '</span>' +
          '<span class="gtr-dur">' + dur + '</span>' +
        '</div>';
      }).join('');
      gtwrap.innerHTML = // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
        '<div class="gate-table-head"><span>ID</span><span>CHECK</span><span>TOOL</span><span>RESULT</span><span>STATUS</span><span>TIME</span></div>' + rows;
    }
  }
}

function setStatBadges(crit, high) {
  var ec = el('stat-critical'), eh = el('stat-high');
  if (ec) ec.textContent = crit != null ? crit : '\u2014';
  if (eh) eh.textContent = high != null ? high : '\u2014';
}

function renderFindingGroup(label, cls, checks) {
  if (!checks.length) return '';
  var html = '<div class="finding-group-label">' + label + ' (' + checks.length + ')</div>';
  html += checks.map(function(c, i) {
    var snippet = c.reason || c.tool_path || c.name || '';
    var isCritical = cls === 'critical';
    var isHigh     = cls === 'high';
    var actionBtns = '';
    if (isCritical) {
      actionBtns = '<button class="fc-btn fix" type="button">Fix</button><button class="fc-btn secret" type="button">Set Secret</button>';
    } else if (isHigh) {
      actionBtns = '<button class="fc-btn view" type="button">View Fix</button>';
    } else {
      actionBtns = '<button class="fc-btn view" type="button">View</button>';
    }
    return '<div class="finding-card ' + cls + '">' +
      '<div class="finding-header">' +
        '<span class="finding-sev-badge ' + cls + '">' + label + '</span>' +
        '<span class="finding-name">' + esc(c.name || '\u2014') + '</span>' +
        '<span class="finding-file">' + esc(c.tool_path || c.group || '\u2014') + '</span>' +
      '</div>' +
      (snippet ? '<div class="finding-snippet">' + esc(snippet.slice(0, 120)) + '</div>' : '') +
      '<div class="finding-actions">' + actionBtns + '<span class="fc-check-id">' + esc(c.check_id || String(i+1).padStart(3,'0')) + '</span></div>' +
    '</div>';
  }).join('');
  return html;
}

function renderResolutionPanel(failChecks) {
  var countEl  = el('res-count');
  var itemsEl  = el('res-items');
  var resolveBtn = el('res-resolve-btn');
  if (!countEl || !itemsEl || !resolveBtn) return;

  var n = failChecks.length;
  countEl.textContent  = n + ' item' + (n !== 1 ? 's' : '') + ' to resolve';
  resolveBtn.textContent = 'Resolve All (' + n + ')';

  if (!n) {
    itemsEl.innerHTML = '<div class="state-msg" style="padding:16px 0;font-size:10px">no findings to resolve</div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    return;
  }

  var badges = ['inprogress','open','scheduled','high'];
  var badgeLabels = ['In Progress','Open','Scheduled','High'];
  itemsEl.innerHTML = failChecks.map(function(c, i) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    var badgeCls   = badges[i % badges.length];
    var badgeLabel = badgeLabels[i % badgeLabels.length];
    return '<div class="res-item">' +
      '<span class="res-item-name">' + esc(c.name || '\u2014') + '</span>' +
      '<span class="res-item-badge ' + badgeCls + '">' + badgeLabel + '</span>' +
      '<a class="res-open-link" href="#" onclick="return false">Open</a>' +
    '</div>';
  }).join('');
}

/* CHAT / COMMAND BOX */
function handleChatKeydown(ev) {
  if (ev.key === 'Enter' && !ev.shiftKey) {
    ev.preventDefault();
    submitChat();
  }
}

function defaultChatSuggestionsForTab(tab) {
  tab = String(tab || '').toLowerCase();
  if (tab === 'findings') {
    return [
      { label: 'top blocker', prompt: 'What is the top blocker in the review findings?' },
      { label: 'gate', prompt: 'Summarize the gate checks and blockers.' },
      { label: 'merge', prompt: 'Is the branch ready to merge?' },
      { label: 'status', prompt: 'Summarize the current review status.' },
    ];
  }
  if (tab === 'eventlogs') {
    return [
      { label: 'latest activity', prompt: 'Summarize the latest review activity.' },
      { label: 'status', prompt: 'Summarize the current review status.' },
      { label: 'gate', prompt: 'Summarize the gate checks and blockers.' },
      { label: 'findings', prompt: 'Show the main findings from the review.' },
    ];
  }
  if (tab === 'processes') {
    return [
      { label: 'status', prompt: 'Summarize the current review status.' },
      { label: 'queue', prompt: 'What is happening in the review queue right now?' },
      { label: 'gate', prompt: 'What gate checks are blocking progress?' },
      { label: 'findings', prompt: 'Summarize the open findings.' },
    ];
  }
  return [
    { label: 'status', prompt: 'Give me the current review status.' },
    { label: 'help', prompt: 'Show me the useful review commands.' },
    { label: 'gate', prompt: 'Summarize the gate checks and blockers.' },
    { label: 'queue', prompt: 'What is happening in the review queue right now?' },
  ];
}

function defaultChatActionsForTab(tab) {
  tab = String(tab || '').toLowerCase();
  if (tab === 'findings') {
    return [
      { title: 'Review top blocker', detail: 'Use the current findings to identify the highest-priority blocker.', prompt: 'What is the top blocker in the review findings?' },
      { title: 'Check merge readiness', detail: 'Confirm whether the gate and findings allow a merge-ready state.', prompt: 'Is the branch ready to merge?' },
    ];
  }
  if (tab === 'eventlogs') {
    return [
      { title: 'Summarize latest activity', detail: 'Convert the newest event log entries into a review-process summary.', prompt: 'Summarize the latest review activity.' },
      { title: 'Trace review blockers', detail: 'Follow the event trail that led to the current review state.', prompt: 'Trace the events that led to the current review blockers.' },
    ];
  }
  if (tab === 'processes') {
    return [
      { title: 'Inspect active review', detail: 'Focus on the currently active sessions and their review phase.', prompt: 'Summarize the active review sessions and their phase.' },
      { title: 'Check queue pressure', detail: 'Describe whether the review queue is growing or clearing.', prompt: 'What is happening in the review queue right now?' },
    ];
  }
  return [
    { title: 'Review status', detail: 'Ask for the current review state and blockers.', prompt: 'Summarize the current review status.' },
    { title: 'Gate checks', detail: 'Ask for the current gate status and blocking checks.', prompt: 'Summarize the gate checks and blockers.' },
  ];
}

function renderChatSuggestions(items) {
  var c = el('chat-suggestions');
  if (!c) return;
  chatSuggestionState = (items && items.length ? items : defaultChatSuggestionsForTab(activeTab)).slice();
  c.innerHTML = chatSuggestionState.map(function(item, idx) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    var cls = idx === 0 ? 'chat-chip primary' : 'chat-chip';
    return '<button class="' + cls + '" type="button" data-prompt="' + attrEsc(item.prompt) + '">' + esc(item.label) + '</button>';
  }).join('');
  bindChatPromptDelegation();
}

function renderChatActions(items) {
  var c = el('chat-actions');
  if (!c) return;
  chatActionState = (items && items.length ? items : defaultChatActionsForTab(activeTab)).slice();
  if (!chatActionState.length) {
    c.innerHTML = ''; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    return;
  }
  c.innerHTML = chatActionState.map(function(item) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    return '<div class="chat-action-card">' +
      '<div class="chat-action-title">' + esc(item.title) + '</div>' +
      '<div class="chat-action-detail">' + esc(item.detail) + '</div>' +
      '<button class="chat-action-btn" type="button" data-prompt="' + attrEsc(item.prompt) + '">Ask</button>' +
    '</div>';
  }).join('');
  bindChatPromptDelegation();
}

function submitChatPreset(prompt) {
  if (typeof prompt !== 'string') prompt = String(prompt || '');
  var ta = el('comment-input');
  if (ta) ta.value = prompt;
  submitChat(prompt);
}

function bindChatPromptDelegation() {
  if (chatPromptDelegationBound) return;
  chatPromptDelegationBound = true;
  document.addEventListener('click', function(ev) {
    var target = ev.target && ev.target.closest ? ev.target.closest('.chat-chip[data-prompt], .chat-action-btn[data-prompt]') : null;
    if (!target) return;
    var prompt = target.getAttribute('data-prompt');
    if (!prompt) return;
    ev.preventDefault();
    submitChatPreset(prompt);
  });
}

function renderChatThread() {
  var thread = el('chat-thread');
  if (!thread) return;
  if (!chatHistory.length) {
    thread.innerHTML = '<div class="chat-empty" id="chat-empty">Ask Codero about status, queue, gate checks, or findings.</div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    return;
  }
  thread.innerHTML = chatHistory.map(function(msg) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    var cls = msg.role || 'assistant';
    var meta = msg.meta || '';
    var body = esc(msg.content || '').replace(/\n/g, '<br>');
    return '<div class="chat-message ' + attrEsc(cls) + '">' +
      '<div class="chat-message-meta">' +
        '<span class="chat-message-role">' + esc(cls) + '</span>' +
        (meta ? '<span class="chat-message-model">' + esc(meta) + '</span>' : '') +
      '</div>' +
      '<div class="chat-message-body">' + body + '</div>' +
    '</div>';
  }).join('');
  thread.scrollTop = thread.scrollHeight;
}

function updateChatAssistantContent(index, delta) {
  if (index < 0 || index >= chatHistory.length) return;
  chatHistory[index].content = (chatHistory[index].content || '') + delta;
  renderChatThread();
}

function finalizeChatAssistant(index, payload) {
  if (index < 0 || index >= chatHistory.length) return;
  chatHistory[index].content = payload.reply || chatHistory[index].content || 'No assistant response was returned.';
  chatHistory[index].meta = payload.provider ? (payload.provider + (payload.model ? ' / ' + payload.model : '')) : 'assistant';
  renderChatThread();
  renderChatSuggestions(payload.suggestions || []);
  renderChatActions(payload.actions || []);
}

function terminalChatPayload() {
  return {
    reply: 'No assistant response was returned.',
    provider: 'fallback',
    model: 'local-summary',
    suggestions: [],
    actions: [],
    generated_at: new Date().toISOString()
  };
}

async function consumeChatStream(resp, assistantIndex) {
  if (!resp.body || !resp.body.getReader) {
    throw new Error('streaming response not supported by browser');
  }
  var reader = resp.body.getReader();
  var decoder = new TextDecoder();
  var buffer = '';
  var eventName = 'message';
  var dataLines = [];
  var finalPayload = null;

  function flushEvent() {
    if (!dataLines.length) {
      eventName = 'message';
      return null;
    }
    var payload = dataLines.join('\n');
    var parsed = null;
    if (eventName === 'delta') {
      try {
        parsed = JSON.parse(payload);
        if (parsed && parsed.delta) updateChatAssistantContent(assistantIndex, parsed.delta);
      } catch (_e) {}
    } else if (eventName === 'done') {
      try { parsed = JSON.parse(payload); } catch (_e) {}
      if (parsed) finalPayload = parsed;
    } else if (eventName === 'error') {
      try {
        parsed = JSON.parse(payload);
      } catch (_e) {
        parsed = { error: payload };
      }
      throw new Error(parsed.error || 'assistant stream error');
    }
    eventName = 'message';
    dataLines = [];
    return parsed;
  }

  while (true) {
    var step = await reader.read();
    buffer += decoder.decode(step.value || new Uint8Array(), { stream: !step.done });
    var lines = buffer.split(/\r?\n/);
    buffer = lines.pop();
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i];
      if (line === '') {
        flushEvent();
        continue;
      }
      if (line.indexOf('event:') === 0) {
        eventName = line.slice(6).trim();
        continue;
      }
      if (line.indexOf('data:') === 0) {
        dataLines.push(line.slice(5).trimStart());
      }
    }
    if (step.done) break;
  }
  if (buffer.trim() !== '') {
    var tailLines = buffer.split(/\r?\n/);
    for (var j = 0; j < tailLines.length; j++) {
      var tail = tailLines[j];
      if (tail.indexOf('event:') === 0) {
        eventName = tail.slice(6).trim();
      } else if (tail.indexOf('data:') === 0) {
        dataLines.push(tail.slice(5).trimStart());
      }
    }
  }
  if (dataLines.length) flushEvent();
  return finalPayload;
}

async function submitChat(promptOverride) {
  if (chatBusy) return;
  var ta = el('comment-input');
  var prompt = String(promptOverride || (ta ? ta.value : '') || '').trim();
  if (!prompt) return;

  chatBusy = true;
  var sendBtn = el('chat-send-btn');
  if (sendBtn) sendBtn.disabled = true;
  if (ta) ta.disabled = true;

  chatHistory.push({ role: 'user', content: prompt });
  chatHistory.push({ role: 'assistant', content: '…', meta: 'streaming' });
  if (chatHistory.length > 8) chatHistory = chatHistory.slice(-8);
  var assistantIndex = chatHistory.length - 1;
  renderChatThread();
  renderChatSuggestions(defaultChatSuggestionsForTab(activeTab));
  renderChatActions(defaultChatActionsForTab(activeTab));

  try {
    var resp = await fetch(API + '/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Accept': 'text/event-stream' },
      body: JSON.stringify({ prompt: prompt, tab: activeTab, context: 'review tab: ' + activeTab, stream: true })
    });
    if (!resp.ok) {
      var errMsg = resp.statusText;
      try {
        var errJson = await resp.json();
        errMsg = errJson.error || errMsg;
      } catch (_e) {}
      throw new Error(errMsg);
    }
    var contentType = String(resp.headers.get('Content-Type') || '').toLowerCase();
    var finalPayload;
    if (contentType.indexOf('application/json') !== -1 || contentType.indexOf('text/event-stream') === -1) {
      finalPayload = await resp.json();
      finalizeChatAssistant(assistantIndex, finalPayload || terminalChatPayload());
    } else {
      finalPayload = await consumeChatStream(resp, assistantIndex);
      finalizeChatAssistant(assistantIndex, finalPayload || terminalChatPayload());
    }
    if (ta) ta.value = '';
  } catch (err) {
    if (assistantIndex >= 0 && assistantIndex < chatHistory.length) {
      chatHistory[assistantIndex].role = 'error';
      chatHistory[assistantIndex].content = 'Review assistant unavailable: ' + err.message;
      chatHistory[assistantIndex].meta = 'fallback';
    } else {
      chatHistory.push({
        role: 'error',
        content: 'Review assistant unavailable: ' + err.message,
        meta: 'fallback'
      });
    }
    renderChatThread();
    renderChatSuggestions(defaultChatSuggestionsForTab(activeTab));
    renderChatActions(defaultChatActionsForTab(activeTab));
  } finally {
    chatBusy = false;
    if (sendBtn) sendBtn.disabled = false;
    if (ta) ta.disabled = false;
  }
}

/* SETTINGS */
function renderSettings() {
  if (!settingsLocal) return;
  el('int-grid').innerHTML = (settingsLocal.integrations || []).map(function(c) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    return '<div class="int-card ' + (c.connected ? 'connected' : '') + '">' +
      '<div class="int-top"><div class="int-icon">' + intIcon(c.id) + '</div>' +
      '<div class="int-info"><div class="int-name">' + esc(c.name) + '</div><div class="int-desc">' + esc(c.desc) + '</div></div></div>' +
      '<div class="int-status ' + (c.connected ? 'conn' : 'disc') + '">' + (c.connected ? '&#x25CF; connected' : '&#x25CB; not connected') + '</div>' +
      '<button class="int-btn ' + (c.connected ? 'disconnect' : '') + '" type="button" data-int-id="' + attrEsc(c.id) + '">' + (c.connected ? 'disconnect' : 'connect') + '</button>' +
    '</div>';
  }).join('');
  el('gate-config-body').innerHTML = (settingsLocal.gate_pipeline || []).map(function(g, i) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    return '<tr><td>' + esc(g.name) + '</td>' +
      '<td><div class="toggle-wrap"><button type="button" class="toggle ' + (g.enabled ? 'on' : '') + '" aria-pressed="' + (g.enabled ? 'true' : 'false') + '" aria-label="Toggle enabled for ' + attrEsc(g.name) + '" onclick="toggleGate(' + i + ',\'enabled\')"><div class="toggle-knob"></div></button></div></td>' +
      '<td><div class="toggle-wrap"><button type="button" class="toggle ' + (g.blocks_commit ? 'on' : '') + '" aria-pressed="' + (g.blocks_commit ? 'true' : 'false') + '" aria-label="Toggle blocks commit for ' + attrEsc(g.name) + '" onclick="toggleGate(' + i + ',\'blocks_commit\')"><div class="toggle-knob"></div></button></div></td>' +
      '<td><span style="color:var(--muted)">' + g.timeout_sec + 's</span></td>' +
      '<td><span style="color:var(--muted)">' + esc(g.provider) + '</span></td></tr>';
  }).join('');
  bindIntegrationGrid();
  el('save-btn').disabled = !settingsDirty;
}
async function loadSettings(force) {
  if (settingsDirty && !force) return;
  try {
    var st = el('save-status');
    if (st) { st.textContent = ''; st.className = 'save-status'; }
    var data = await apiFetch('/settings');
    settingsLocal = JSON.parse(JSON.stringify(data));
    settingsDirty = false;
    renderSettings();
  } catch (err) {
    el('int-grid').innerHTML = '<div class="state-msg error">failed to load settings: ' + esc(err.message) + '</div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  }
}
function bindIntegrationGrid() {
  if (integrationGridBound) return;
  integrationGridBound = true;
  document.addEventListener('click', function(ev) {
    var btn = ev.target && ev.target.closest ? ev.target.closest('.int-btn[data-int-id]') : null;
    if (!btn) return;
    toggleInt(btn.getAttribute('data-int-id'));
  });
}
function markSettingsDirty() {
  if (!settingsDirty) {
    var st = el('save-status');
    if (st) { st.textContent = ''; st.className = 'save-status'; }
  }
  settingsDirty = true;
}
function toggleInt(id) {
  var c = (settingsLocal && settingsLocal.integrations || []).find(function(x) { return x.id === id; });
  if (!c) return;
  c.connected = !c.connected;
  markSettingsDirty();
  renderSettings();
}
function toggleGate(i, f) {
  var g = settingsLocal && settingsLocal.gate_pipeline && settingsLocal.gate_pipeline[i];
  if (!g || !Object.prototype.hasOwnProperty.call(g, f)) return;
  g[f] = !g[f];
  markSettingsDirty();
  renderSettings();
}
async function saveSettings() {
  var btn = el('save-btn'), st = el('save-status');
  btn.disabled = true; st.textContent = 'saving\u2026'; st.className = 'save-status';
  try {
    var result = await apiFetch('/settings', { method: 'PUT', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ integrations: settingsLocal.integrations, gate_pipeline: settingsLocal.gate_pipeline }) });
    settingsLocal = result; settingsDirty = false; st.textContent = '\u2713 saved'; st.className = 'save-status ok'; renderSettings();
  } catch (err) { st.textContent = '\u2717 ' + err.message; st.className = 'save-status err'; btn.disabled = false; }
}
function intIcon(id) { return { coderabbit:'\uD83D\uDC30', 'pr-agent':'\uD83E\uDD16', 'gh-actions':'\u26A1', 'gitlab-ci':'\uD83E\uDD8A', 'semgrep-cloud':'\uD83D\uDD0D' }[id] || '\uD83D\uDD17'; }

/* ACTION BANNER */
function updateActionBanner(critCount, highCount) {
  var banner = el('action-banner');
  if (!banner) return;
  if (critCount > 0) {
    banner.classList.add('visible');
    el('action-banner-text').textContent = critCount + ' critical finding' + (critCount !== 1 ? 's' : '') + ' require immediate action';
    el('action-banner-detail').textContent = highCount > 0 ? '+' + highCount + ' high severity' : '';
  } else if (highCount > 0) {
    banner.classList.add('visible');
    el('action-banner-text').textContent = highCount + ' high-severity finding' + (highCount !== 1 ? 's' : '') + ' need attention';
    el('action-banner-detail').textContent = '';
  } else {
    banner.classList.remove('visible');
  }
}

/* REVIEW HERO */
function updateReviewHero(data) {
  var heroStatus = el('hero-status'), heroStatusSub = el('hero-status-sub'), heroStatusCard = el('hero-status-card');
  var heroBlockers = el('hero-blockers'), heroBlockersSub = el('hero-blockers-sub'), heroBlockersCard = el('hero-blockers-card');
  if (!heroStatus || !heroBlockers) return;

  if (!data || !data.report) {
    heroStatus.textContent = '\u2014';
    heroStatusSub.textContent = 'Waiting for gate-check report';
    heroStatusCard.className = 'review-hero-card hero-info';
    heroBlockers.textContent = '0';
    heroBlockersSub.textContent = 'No blocking findings';
    heroBlockersCard.className = 'review-hero-card hero-pass';
    return;
  }

  var sum = data.report.summary || {};
  var os = (sum.overall_status || '').toUpperCase();
  heroStatus.textContent = os || '\u2014';
  if (os === 'PASS') {
    heroStatus.style.color = 'var(--teal)';
    heroStatusSub.textContent = 'All checks passing — merge ready';
    heroStatusCard.className = 'review-hero-card hero-pass';
  } else if (os === 'FAIL') {
    heroStatus.style.color = 'var(--red)';
    heroStatusSub.textContent = sum.failed + ' check' + (sum.failed !== 1 ? 's' : '') + ' failing — review required';
    heroStatusCard.className = 'review-hero-card hero-fail';
  } else {
    heroStatus.style.color = 'var(--amber)';
    heroStatusSub.textContent = 'Review in progress';
    heroStatusCard.className = 'review-hero-card hero-warn';
  }

  var blockerCount = sum.required_failed || 0;
  heroBlockers.textContent = blockerCount;
  if (blockerCount > 0) {
    heroBlockersSub.textContent = blockerCount + ' required check' + (blockerCount !== 1 ? 's' : '') + ' failing';
    heroBlockersCard.className = 'review-hero-card hero-fail';
  } else if (sum.failed > 0) {
    heroBlockersSub.textContent = sum.failed + ' non-blocking finding' + (sum.failed !== 1 ? 's' : '');
    heroBlockersCard.className = 'review-hero-card hero-warn';
  } else {
    heroBlockersSub.textContent = 'No blocking findings';
    heroBlockersCard.className = 'review-hero-card hero-pass';
  }
}

function updatePipelineHero(pct, passCount, totalCount) {
  var heroVal = el('hero-pipeline-pct'), heroSub = el('hero-pipeline-sub'), heroCard = el('hero-pipeline-card');
  if (!heroVal) return;
  if (totalCount === 0) {
    heroVal.textContent = '\u2014';
    heroSub.textContent = 'Waiting for pipeline data';
    if (heroCard) heroCard.className = 'review-hero-card hero-info';
    return;
  }
  heroVal.textContent = pct + '%';
  heroSub.textContent = passCount + ' of ' + totalCount + ' steps passing';
  if (pct === 100) {
    heroVal.style.color = 'var(--teal)';
    if (heroCard) heroCard.className = 'review-hero-card hero-pass';
  } else if (pct >= 50) {
    heroVal.style.color = 'var(--amber)';
    if (heroCard) heroCard.className = 'review-hero-card hero-warn';
  } else {
    heroVal.style.color = 'var(--red)';
    if (heroCard) heroCard.className = 'review-hero-card hero-fail';
  }
}

function updateBlockerSummary(failChecks) {
  var wrap = el('blocker-summary'), countEl = el('blocker-count'), listEl = el('blocker-list');
  if (!wrap) return;
  var blockers = failChecks.filter(function(c) { return c.required; });
  if (blockers.length === 0) { wrap.classList.remove('visible'); return; }
  wrap.classList.add('visible');
  countEl.textContent = blockers.length;
  listEl.innerHTML = blockers.map(function(c) { // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    return '<div class="blocker-summary-item">' + esc(c.name || c.id || '\u2014') + '</div>';
  }).join('');
}

/* CHAT CONTEXT */
function updateChatContext(tab) {
  var ctxVal = el('chat-context-tab');
  if (ctxVal) {
    var labels = { processes: 'Active Sessions', eventlogs: 'Event Logs', findings: 'Findings & Gates', architecture: 'Architecture', settings: 'Settings' };
    ctxVal.textContent = labels[tab] || tab;
  }
}

/* MAIN REFRESH */
async function refresh() {
  try {
    var results = await Promise.allSettled([
      apiFetch('/health'),
      apiFetch('/active-sessions'),
      apiFetch('/gate-checks'),
      apiFetch('/activity'),
    ]);
    var h   = results[0].status === 'fulfilled' ? results[0].value : { unavailable: true, error: reasonText(results[0].reason) };
    var sr  = results[1].status === 'fulfilled' ? results[1].value : { unavailable: true, error: reasonText(results[1].reason) };
    var gc  = results[2].status === 'fulfilled' ? results[2].value : { unavailable: true, error: reasonText(results[2].reason) };
    var act = results[3].status === 'fulfilled' ? results[3].value : { unavailable: true, error: reasonText(results[3].reason) };
    var sessions = sr && sr.unavailable ? null : ((sr && sr.sessions) ? sr.sessions : []);
    renderHealthBar(h);
    renderSessions(sessions);
    updateMergeBar(h, sessions);
    findingsCache = gc;
    renderFindings(gc);
    if (act && act.events) {
      eventsUnavailable = false;
      allEvents = act.events;
      if (activeTab === 'eventlogs') renderEvents();
      renderActivityFeed();
    } else {
      eventsUnavailable = true;
      allEvents = [];
      if (activeTab === 'eventlogs') renderEvents(null);
      renderActivityFeed();
    }
  } catch (err) { console.error('refresh error:', err); }
}

/* SSE */
function startSSE() {
  if (sseSource) sseSource.close();
  sseSource = new EventSource(API + '/events');
  sseSource.addEventListener('activity', function(e) {
    try { var ev = JSON.parse(e.data); allEvents.unshift(ev); if (allEvents.length > 100) allEvents.length = 100; if (activeTab === 'eventlogs') renderEvents(); renderActivityFeed(); } catch (err) { console.warn('SSE activity parse error:', err, e.data); }
  });
  sseSource.onerror = function() { sseSource.close(); sseSource = null; schedulePoll(); };
}
function schedulePoll() {
  if (pollTimer) clearTimeout(pollTimer);
  pollTimer = setTimeout(function() { refresh().then(schedulePoll); }, POLL);
}

/* UTILS */
function el(id)  { return document.getElementById(id); }
function esc(s)  { var d = document.createElement('div'); d.textContent = String(s == null ? '' : s); return d.innerHTML; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
function attrEsc(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}
// toDisplayState maps raw gate-check status values to the LOG-001 normalized
// display state: pass→passing, fail→failing, skip/disabled→disabled.
function toDisplayState(status) {
  if (status === 'pass')  return 'passing';
  if (status === 'fail')  return 'failing';
  return 'disabled';
}
function reasonText(reason) {
  return reason && reason.message ? reason.message : String(reason || 'unavailable');
}
function fmtTime(iso) {
  if (!iso) return '\u2014';
  var d = new Date(iso);
  if (isNaN(d.getTime())) return '\u2014';
  return d.toLocaleTimeString('en-US',{hour12:false,hour:'2-digit',minute:'2-digit',second:'2-digit'});
}
function fmtRelative(iso) {
  if (!iso) return '\u2014';
  var d = new Date(iso);
  if (isNaN(d.getTime())) return '\u2014';
  var diff = Date.now() - d.getTime();
  if (diff < 0) return 'just now';
  if (diff < 60000) return Math.round(diff/1000) + 's ago';
  if (diff < 3600000) return Math.round(diff/60000) + 'm ago';
  return Math.round(diff/3600000) + 'h ago';
}
function fmtElapsed(sec) {
  if (sec == null || sec === '') return '\u2014';
  var total = Math.floor(sec), h = Math.floor(total/3600), m = Math.floor((total%3600)/60), s = total%60;
  if (h > 0) return h + 'h ' + m + 'm'; if (m > 0) return m + 'm ' + s + 's'; return s + 's';
}
function fmtAge(sec) { if (sec < 60) return sec + 's'; if (sec < 3600) return Math.round(sec/60) + 'm'; return Math.round(sec/3600) + 'h'; }

/* BOOT */
bindChatPromptDelegation();
renderChatSuggestions(defaultChatSuggestionsForTab(activeTab));
renderChatActions(defaultChatActionsForTab(activeTab));
refresh();
startSSE();
schedulePoll();
