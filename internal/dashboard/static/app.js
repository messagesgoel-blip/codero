'use strict';
/* ══════════════════════════════════════════════════════════
   CODERO DASHBOARD — app.js
   Hash-based SPA with polling. No build pipeline.
   ══════════════════════════════════════════════════════════ */

/* ── CONSTANTS ─────────────────────────────────────────── */
var API  = '/api/v1/dashboard';
var POLL = 10000;

/* ── STATE ─────────────────────────────────────────────── */
var activeTab    = 'overview';
var theme        = 'dark';
var expandedRows = {};
var sectionState = {};
var eventFilter  = 'ALL';
var findingsView = 'cards';
var settingsLocal = null;
var settingsDirty = false;

/* ── HASH ROUTER ───────────────────────────────────────── */
var TABS = ['overview','agents','events','findings','architecture','settings'];

function initRouter() {
  window.addEventListener('hashchange', onHashChange);
  onHashChange();
}
function onHashChange() {
  var hash = window.location.hash.replace('#','') || 'overview';
  if (TABS.indexOf(hash) === -1) hash = 'overview';
  switchTab(hash);
}
function switchTab(tab) {
  activeTab = tab;
  TABS.forEach(function(t) {
    var page = document.getElementById('page-' + t);
    var nav  = document.getElementById('nav-' + t);
    if (page) page.classList.toggle('active', t === tab);
    if (nav)  nav.classList.toggle('active', t === tab);
  });
  refreshActiveTab();
}
function navigateTo(tab) { window.location.hash = '#' + tab; }

/* ── API WRAPPER ───────────────────────────────────────── */
var API_TIMEOUT = 15000;
async function apiFetch(path, opts) {
  opts = opts || {};
  var controller = new AbortController();
  var timer = setTimeout(function() { controller.abort(); }, API_TIMEOUT);
  if (!opts.signal) opts.signal = controller.signal;
  try {
    var res = await fetch(API + path, opts);
    clearTimeout(timer);
    if (!res.ok) {
      var msg = res.statusText;
      try { var j = await res.json(); msg = j.error || msg; } catch(_){}
      var err = new Error(msg); err.status = res.status; throw err;
    }
    return res.json();
  } catch(e) {
    clearTimeout(timer);
    if (e.name === 'AbortError') throw new Error('Request timed out');
    throw e;
  }
}
async function fetchSection(id, path) {
  var prev = sectionState[id];
  sectionState[id] = { loading: true, error: null, data: prev ? prev.data : null };
  try {
    var data = await apiFetch(path);
    sectionState[id] = { loading: false, error: null, data: data };
  } catch(e) {
    sectionState[id] = { loading: false, error: e.message, data: prev ? prev.data : null };
  }
  return sectionState[id].data;
}

/* ── DOM HELPERS ───────────────────────────────────────── */
function esc(s) { var d = document.createElement('div'); d.textContent = s || ''; return d.innerHTML; }
function clearEl(id) { var e = document.getElementById(id); if (e) e.innerHTML = ''; return e; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
function $(id) { return document.getElementById(id); }

/* ── SHARED RENDERERS ──────────────────────────────────── */
function mapStateToClass(state) {
  /* Core lifecycle states */
  var m = {active:'active',waiting:'waiting',blocked:'blocked',completed:'completed',
           cancelled:'cancelled',lost:'lost',ended:'completed',
           coding:'active',cli_reviewing:'active',reviewed:'completed',merge_ready:'completed',
           paused:'waiting',queued_cli:'waiting',local_review:'active',queued_for_review:'waiting'};
  if (m[state]) return m[state];
  /* Assignment substatuses */
  if (state && state.indexOf('waiting_for') === 0) return 'waiting';
  if (state && state.indexOf('blocked_') === 0) return 'blocked';
  if (state && state.indexOf('terminal_') === 0) return 'completed';
  return '';
}
function mapSeverityToClass(sev) {
  var m = {critical:'lost',high:'blocked',medium:'waiting',low:'active',info:'cancelled'};
  return m[sev] || '';
}
function severityChip(sev) {
  if (!sev) return '<span class="status-chip">\u2014</span>';
  var cls = mapSeverityToClass(sev.toLowerCase());
  return '<span class="status-chip ' + cls + '">' + esc(sev) + '</span>';
}
function statusChip(state) {
  if (!state) return '<span class="status-chip">\u2014</span>';
  var cls = mapStateToClass(state);
  var dot = (state === 'active' || state === 'coding' || state === 'cli_reviewing')
    ? '<span class="pulse-dot animate" style="background:var(--status-active)"></span>' : '';
  return '<span class="status-chip ' + cls + '">' + dot + esc(state) + '</span>';
}
function metricCard(value, label, colorVar) {
  var style = colorVar ? 'color:var(' + colorVar + ')' : '';
  return '<div class="metric-card"><div class="metric-value" style="' + style + '">' +
    esc(String(value)) + '</div><div class="metric-label">' + esc(label) + '</div></div>';
}
function skeletonCards(n) {
  var h = '<div class="metric-strip">';
  for (var i = 0; i < n; i++) h += '<div class="skeleton skeleton-card"></div>';
  return h + '</div>';
}
function skeletonTable(rows) {
  var h = '<div class="data-table-wrap" style="padding:1rem">';
  for (var i = 0; i < rows; i++) h += '<div class="skeleton skeleton-line" style="width:' + (55 + Math.floor(Math.random()*35)) + '%"></div>';
  return h + '</div>';
}
function emptyState(icon, text) {
  return '<div class="empty-state"><div class="empty-state-icon">' + icon +
    '</div><div class="empty-state-text">' + esc(text) + '</div></div>';
}
function errorBanner(msg, retryFn) {
  var id = 'err-' + Math.random().toString(36).slice(2,8);
  setTimeout(function(){ var b = $(id); if (b) b.onclick = retryFn; }, 0);
  return '<div class="error-banner"><span>' + esc(msg) + '</span><button id="'+id+'">Retry</button></div>';
}
function relativeTime(ts) {
  if (!ts) return '\u2014';
  var diff = (Date.now() - new Date(ts).getTime()) / 1000;
  if (diff < 0) return 'just now';
  if (diff < 60) return Math.floor(diff) + 's ago';
  if (diff < 3600) return Math.floor(diff/60) + 'm ago';
  if (diff < 86400) return Math.floor(diff/3600) + 'h ago';
  return Math.floor(diff/86400) + 'd ago';
}
function formatDuration(sec) {
  if (sec === null || sec === undefined) return '\u2014';
  if (sec < 60) return sec + 's';
  if (sec < 3600) return Math.floor(sec/60) + 'm ' + (sec%60) + 's';
  return Math.floor(sec/3600) + 'h ' + Math.floor((sec%3600)/60) + 'm';
}
function truncId(id) {
  if (!id) return '\u2014';
  return id.length > 12 ? id.slice(0,12) + '\u2026' : id;
}
function fmtPct(v) {
  if (v === null || v === undefined || v < 0) return '\u2014';
  return Math.round(v) + '%';
}

/* ── EXPANDABLE ROW HELPERS ────────────────────────────── */
function toggleExpand(tableId, rowId) {
  var key = tableId + ':' + rowId;
  expandedRows[key] = !expandedRows[key];
  var el = $('expand-' + key.replace(/[^a-z0-9]/gi,'-'));
  if (el) el.style.display = expandedRows[key] ? '' : 'none';
}
function isExpanded(tableId, rowId) { return !!expandedRows[tableId+':'+rowId]; }
function chevron(tableId, rowId) { return isExpanded(tableId,rowId) ? '\u25BE' : '\u25B8'; }
function detailGrid(items) {
  var h = '<div class="detail-grid">';
  items.forEach(function(it) {
    if (it.value === undefined || it.value === null) it.value = '';
    h += '<div><div class="detail-item-label">'+esc(it.label)+'</div>' +
         '<div class="detail-item-value">'+esc(String(it.value) || '\u2014')+'</div></div>';
  });
  return h + '</div>';
}

/* ── THEME ─────────────────────────────────────────────── */
function setTheme(t) {
  theme = t;
  if (t === 'system') {
    var dark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light');
  } else {
    document.documentElement.setAttribute('data-theme', t);
  }
  document.querySelectorAll('.theme-toggle button').forEach(function(b) {
    b.classList.toggle('active', b.dataset.theme === t);
  });
  try { localStorage.setItem('codero-theme', t); } catch(_){}
}
function initTheme() {
  var s = null; try { s = localStorage.getItem('codero-theme'); } catch(_){}
  setTheme(s || 'dark');
}

/* ── HEALTH BAR ────────────────────────────────────────── */
function renderHealthBar(h) {
  if (!h) return;
  setDot('hb-db-dot', h.database ? h.database.status : 'down');
  setDot('hb-sessions-dot', h.feeds ? h.feeds.active_sessions.status : 'unavailable');
  setDot('hb-gates-dot', h.feeds ? h.feeds.gate_checks.status : 'unavailable');
  var al = $('hb-agents-label'); if (al) al.textContent = (h.active_agent_count||0)+' agents';
  var rl = $('hb-refreshed'); if (rl) rl.textContent = 'refreshed ' + new Date().toLocaleTimeString();
}
function setDot(id, status) {
  var d = $(id); if (!d) return;
  d.className = 'health-dot ' + (status==='ok'?'ok':status==='stale'?'stale':'down');
}

/* ══════════════════════════════════════════════════════════
   TAB RENDERERS
   ══════════════════════════════════════════════════════════ */
var tabRefreshers = {};

/* ── OVERVIEW ──────────────────────────────────────────── */
tabRefreshers.overview = async function() {
  await Promise.allSettled([
    fetchSection('ov-stats','/overview'),
    fetchSection('ov-repos','/repos'),
    fetchSection('ov-sessions','/active-sessions'),
    fetchSection('ov-gates','/gate-health'),
    fetchSection('ov-health','/health'),
  ]);
  renderOverviewTab();
  var hd = sectionState['ov-health'];
  if (hd && hd.data) renderHealthBar(hd.data);
};

function renderOverviewTab() {
  var c = clearEl('page-overview'); if (!c) return;
  c.innerHTML = '<div class="page-header"><h2>Overview</h2><p>System health and review pipeline status</p></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  /* metrics */
  var st = sectionState['ov-stats'];
  if (!st || st.loading) { c.innerHTML += skeletonCards(4); } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  else if (st.error) { c.innerHTML += errorBanner(st.error, tabRefreshers.overview); } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  else if (st.data) {
    var d = st.data;
    c.innerHTML += '<div class="metric-strip">' + // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
      metricCard(d.runs_today, 'Runs Today', '--primary') +
      metricCard(fmtPct(d.pass_rate), 'Pass Rate', d.pass_rate >= 80 ? '--status-active' : '--status-blocked') +
      metricCard(d.blocked_count, 'Blocked', d.blocked_count > 0 ? '--status-lost' : '--text-muted') +
      metricCard(d.avg_gate_sec >= 0 ? formatDuration(Math.round(d.avg_gate_sec)) : '\u2014', 'Avg Gate Time', null) +
      '</div>';
  }

  /* repos */
  var rp = sectionState['ov-repos'];
  c.innerHTML += '<div class="section"><div class="section-header"><div class="section-title">Repositories</div></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (!rp || rp.loading) { c.innerHTML += skeletonTable(4); } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  else if (rp.error) { c.innerHTML += errorBanner(rp.error, tabRefreshers.overview); } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  else if (rp.data && rp.data.repos && rp.data.repos.length) {
    var rows = rp.data.repos;
    var h = '<div class="data-table-wrap"><table class="data-table"><thead><tr>' +
      '<th>Repo</th><th>Branch</th><th>State</th><th>Head</th><th>Last Run</th><th>Gates</th></tr></thead><tbody>';
    rows.forEach(function(r) {
      var pills = (r.gate_summary||[]).map(function(g) {
        return '<span class="gate-pill '+esc(g.status)+'">'+esc(g.name)+'</span>';
      }).join(' ');
      h += '<tr><td>'+esc(r.repo)+'</td><td>'+esc(r.branch)+'</td>' +
        '<td>'+statusChip(r.state)+'</td>' +
        '<td>'+esc((r.head_hash||'').slice(0,8))+'</td>' +
        '<td>'+relativeTime(r.last_run_at)+'</td>' +
        '<td>'+pills+'</td></tr>';
    });
    h += '</tbody></table></div>';
    c.innerHTML += h; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  } else {
    c.innerHTML += emptyState('\u{1F4E6}','No repositories tracked yet'); // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  }
  c.innerHTML += '</div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  /* gate health */
  var gh = sectionState['ov-gates'];
  if (gh && gh.data && gh.data.gates && gh.data.gates.length) {
    var ghtml = '<div class="section"><div class="section-header"><div class="section-title">Gate Health</div></div>' +
      '<div class="data-table-wrap"><table class="data-table"><thead><tr><th>Provider</th><th>Total</th><th>Passed</th><th>Pass Rate</th></tr></thead><tbody>';
    gh.data.gates.forEach(function(g) {
      ghtml += '<tr><td>'+esc(g.provider)+'</td><td>'+g.total+'</td><td>'+g.passed+'</td><td>'+fmtPct(g.pass_rate)+'</td></tr>';
    });
    ghtml += '</tbody></table></div></div>';
    c.innerHTML += ghtml; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  }
}

/* ── AGENTS ────────────────────────────────────────────── */
tabRefreshers.agents = async function() {
  await Promise.allSettled([
    fetchSection('ag-sessions','/active-sessions'),
    fetchSection('ag-assign','/assignments'),
    fetchSection('ag-comp','/compliance'),
    fetchSection('ag-events','/agent-events'),
    fetchSection('ag-health','/health'),
  ]);
  renderAgentsTab();
  var hd = sectionState['ag-health'];
  if (hd && hd.data) renderHealthBar(hd.data);
};

function renderAgentsTab() {
  var c = clearEl('page-agents'); if (!c) return;
  c.innerHTML = '<div class="page-header"><h2>Agents</h2><p>Active sessions, assignments, compliance, and events</p></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  /* metric strip */
  var sess = sectionState['ag-sessions'], asgn = sectionState['ag-assign'],
      comp = sectionState['ag-comp'], evt = sectionState['ag-events'];
  if ((sess&&sess.loading)||(asgn&&asgn.loading)) { c.innerHTML += skeletonCards(4); } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  else {
    var sc = sess&&sess.data ? sess.data.active_count : 0;
    var ac = asgn&&asgn.data ? asgn.data.count : 0;
    var compScore = '\u2014';
    if (comp&&comp.data&&comp.data.checks&&comp.data.checks.length>0) {
      var total = comp.data.checks.length;
      var passing = comp.data.checks.filter(function(x){return x.result==='pass';}).length;
      compScore = Math.round((passing/total)*100)+'%';
    }
    var ec = evt&&evt.data ? evt.data.count : 0;
    c.innerHTML += '<div class="metric-strip">' + // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
      metricCard(sc,'Active Sessions','--status-active') +
      metricCard(ac,'Assignments','--primary') +
      metricCard(compScore,'Compliance Score', compScore==='\u2014' ? null : '--status-active') +
      metricCard(ec,'Recent Events','--status-completed') + '</div>';
    var badge = $('badge-agents'); if (badge) badge.textContent = sc || '\u2014';
  }

  /* sessions */
  renderAgentsSessions(c);
  /* assignments */
  renderAgentsAssignments(c);
  /* bottom split: compliance + events */
  var splitHtml = '<div class="split-panel"><div id="ag-comp-section" class="section"></div><div id="ag-evt-section" class="section"></div></div>';
  c.innerHTML += splitHtml; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  renderAgentsCompliance($('ag-comp-section'));
  renderAgentsTimeline($('ag-evt-section'));
}

function renderAgentsSessions(c) {
  var s = sectionState['ag-sessions'];
  var html = '<div class="section"><div class="section-header"><div class="section-title">Active Sessions</div>' +
    '<div class="section-subtitle">' + (s&&s.data ? s.data.active_count+' active' : '') + '</div></div>';
  if (!s||s.loading) { html += skeletonTable(5); }
  else if (s.error) { html += errorBanner(s.error, tabRefreshers.agents); }
  else if (!s.data||!s.data.sessions||!s.data.sessions.length) { html += emptyState('\u{1F916}','No active sessions'); }
  else {
    var rows = s.data.sessions;
    html += '<div class="data-table-wrap"><table class="data-table"><thead><tr>' +
      '<th class="chevron"></th><th>Agent</th><th>Repo / Branch</th><th>Task</th><th>Phase</th><th>Heartbeat</th><th>Elapsed</th>' +
      '</tr></thead><tbody>';
    rows.forEach(function(r) {
      var eid = 'expand-sess-'+r.session_id.replace(/[^a-z0-9]/gi,'-');
      html += '<tr class="expandable" onclick="toggleExpand(\'sess\',\''+esc(r.session_id)+'\');renderAgentsTab()">' +
        '<td class="chevron">'+chevron('sess',r.session_id)+'</td>' +
        '<td>'+statusChip(r.activity_state)+' '+esc(r.agent_id||r.owner_agent||'\u2014') +
          (r.mode ? ' <span class="enforcement-badge soft">'+esc(r.mode)+'</span>':'')+
        '</td>' +
        '<td>'+esc(r.repo||'\u2014')+' / '+esc(r.branch||'\u2014')+'</td>' +
        '<td>'+(r.task ? esc(r.task.id)+' '+esc(r.task.title):'\u2014')+'</td>' +
        '<td>'+(r.task ? statusChip(r.task.phase||''):'\u2014')+'</td>' +
        '<td>'+relativeTime(r.last_heartbeat_at)+'</td>' +
        '<td>'+formatDuration(r.elapsed_sec)+'</td></tr>';
      html += '<tr class="expand-row" id="'+eid+'" style="display:'+(isExpanded('sess',r.session_id)?'':'none')+'">' +
        '<td colspan="7"><div class="expand-content">'+detailGrid([
          {label:'Session ID',value:r.session_id}, {label:'Worktree',value:r.worktree},
          {label:'PR',value:r.pr_number?'#'+r.pr_number:''}, {label:'Started',value:r.started_at?new Date(r.started_at).toLocaleString():''},
          {label:'Progress',value:r.progress_at?relativeTime(r.progress_at):''},
        ])+'</div></td></tr>';
    });
    html += '</tbody></table></div>';
  }
  html += '</div>';
  c.innerHTML += html; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
}

function renderAgentsAssignments(c) {
  var s = sectionState['ag-assign'];
  var html = '<div class="section"><div class="section-header"><div class="section-title">Assignments</div>' +
    '<div class="section-subtitle">'+(s&&s.data ? s.data.count+' total' : '')+'</div></div>';
  if (!s||s.loading) { html += skeletonTable(5); }
  else if (s.error) { html += errorBanner(s.error, tabRefreshers.agents); }
  else if (!s.data||!s.data.assignments||!s.data.assignments.length) { html += emptyState('\u{1F4CB}','No assignments'); }
  else {
    var rows = s.data.assignments;
    html += '<div class="data-table-wrap"><table class="data-table"><thead><tr>' +
      '<th class="chevron"></th><th>State</th><th>Assignment</th><th>Agent</th><th>Repo / Branch</th>' +
      '<th>Substatus</th><th>Blocked</th><th>Started</th><th>Ended</th></tr></thead><tbody>';
    rows.forEach(function(a) {
      var eid = 'expand-assign-'+a.assignment_id.replace(/[^a-z0-9]/gi,'-');
      html += '<tr class="expandable" onclick="toggleExpand(\'assign\',\''+esc(a.assignment_id)+'\');renderAgentsTab()">' +
        '<td class="chevron">'+chevron('assign',a.assignment_id)+'</td>' +
        '<td>'+statusChip(a.state)+'</td>' +
        '<td>'+truncId(a.assignment_id)+'</td>' +
        '<td>'+esc(a.agent_id||'\u2014')+'</td>' +
        '<td>'+esc(a.repo||'\u2014')+' / '+esc(a.branch||'\u2014')+'</td>' +
        '<td>'+(a.substatus?statusChip(a.substatus):'\u2014')+'</td>' +
        '<td style="color:var(--status-lost)">'+esc(a.blocked_reason||'')+'</td>' +
        '<td>'+relativeTime(a.started_at)+'</td>' +
        '<td>'+relativeTime(a.ended_at)+'</td></tr>';
      html += '<tr class="expand-row" id="'+eid+'" style="display:'+(isExpanded('assign',a.assignment_id)?'':'none')+'">' +
        '<td colspan="9"><div class="expand-content">'+detailGrid([
          {label:'Session ID',value:a.session_id}, {label:'Task ID',value:a.task_id},
          {label:'Worktree',value:a.worktree}, {label:'End Reason',value:a.end_reason},
          {label:'Superseded By',value:a.superseded_by}, {label:'Branch State',value:a.branch_state},
          {label:'PR',value:a.pr_number?'#'+a.pr_number:''}, {label:'Mode',value:a.mode},
        ])+'</div></td></tr>';
    });
    html += '</tbody></table></div>';
  }
  html += '</div>';
  c.innerHTML += html; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
}

function renderAgentsCompliance(el) {
  if (!el) return;
  var s = sectionState['ag-comp'];
  el.innerHTML = '<div class="section-header"><div class="section-title">Compliance Rules</div></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (!s||s.loading) { el.innerHTML += skeletonTable(3); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (s.error) { el.innerHTML += errorBanner(s.error, tabRefreshers.agents); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (!s.data||!s.data.rules||!s.data.rules.length) { el.innerHTML += emptyState('\u{1F6E1}','No compliance rules configured'); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  var rules = s.data.rules, checks = s.data.checks||[];
  var h = '<div class="rules-grid">';
  rules.forEach(function(r) {
    var rc = checks.filter(function(x){return x.rule_id===r.rule_id;});
    var pass = rc.filter(function(x){return x.result==='pass';}).length;
    var fail = rc.filter(function(x){return x.result==='fail';}).length;
    var violations = rc.filter(function(x){return x.result==='fail'&&!x.resolved_at;}).length;
    h += '<div class="rule-card"><div class="rule-card-header">' +
      '<div class="rule-card-name">'+esc(r.rule_name)+'</div>' +
      '<span class="enforcement-badge '+(r.enforcement==='block'?'hard':'soft')+'">'+esc(r.enforcement)+'</span></div>' +
      '<div class="rule-card-desc">'+esc(r.description)+'</div>' +
      '<div class="rule-card-stats">' +
        '<span style="color:var(--rule-pass)">'+pass+' pass</span>' +
        '<span style="color:var(--rule-fail)">'+fail+' fail</span>' +
        (violations>0?'<span style="color:var(--rule-fail);font-weight:600">'+violations+' active</span>':'') +
      '</div><div class="rule-card-version">v'+r.rule_version+' \u00b7 '+esc(r.rule_kind)+'</div></div>';
  });
  h += '</div>';
  el.innerHTML += h; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
}

function renderAgentsTimeline(el) {
  if (!el) return;
  var s = sectionState['ag-events'];
  el.innerHTML = '<div class="section-header"><div class="section-title">Agent Events</div></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (!s||s.loading) { el.innerHTML += skeletonTable(5); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (s.error) { el.innerHTML += errorBanner(s.error, tabRefreshers.agents); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (!s.data||!s.data.events||!s.data.events.length) { el.innerHTML += emptyState('\u{1F4E1}','No agent events yet'); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  var events = s.data.events;
  var h = '<div class="timeline" style="max-height:24rem;overflow-y:auto">';
  events.forEach(function(ev, i) {
    var color = evtColor(ev.event_type);
    var last = i === events.length-1;
    h += '<div class="timeline-entry"><div class="timeline-track">' +
      '<div class="timeline-dot" style="background:var('+color+')"></div>' +
      (last?'':'<div class="timeline-line"></div>') +
      '</div><div class="timeline-body">' +
      '<div class="timeline-time">'+relativeTime(ev.created_at)+'</div>' +
      '<div class="timeline-text">'+esc(ev.event_type.replace(/_/g,' '))+'</div>' +
      '<div class="timeline-agent">'+esc(ev.agent_id||'\u2014')+(ev.session_id?' \u00b7 '+truncId(ev.session_id):'')+'</div>' +
      '</div></div>';
  });
  h += '</div>';
  el.innerHTML += h; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
}
function evtColor(t) {
  if (t.indexOf('register')>=0||t.indexOf('start')>=0) return '--status-active';
  if (t.indexOf('end')>=0||t.indexOf('complete')>=0) return '--status-completed';
  if (t.indexOf('block')>=0||t.indexOf('fail')>=0||t.indexOf('error')>=0) return '--status-lost';
  if (t.indexOf('attach')>=0||t.indexOf('assign')>=0) return '--status-waiting';
  return '--text-muted';
}

/* ── EVENTS ────────────────────────────────────────────── */
tabRefreshers.events = async function() {
  await fetchSection('ev-activity','/activity');
  renderEventsTab();
};
function renderEventsTab() {
  var c = clearEl('page-events'); if (!c) return;
  c.innerHTML = '<div class="page-header"><h2>Events</h2><p>Delivery events and activity feed</p></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  /* severity filter bar */
  var filters = ['ALL','CRITICAL','HIGH','MEDIUM','LOW','INFO'];
  var fhtml = '<div class="filter-bar">';
  filters.forEach(function(f) {
    fhtml += '<button class="filter-btn'+(eventFilter===f?' active':'')+'" onclick="eventFilter=\''+f+'\';renderEventsTab()">'+f+'</button>';
  });
  fhtml += '</div>';
  c.innerHTML += fhtml; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  var s = sectionState['ev-activity'];
  if (!s||s.loading) { c.innerHTML += skeletonTable(8); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (s.error) { c.innerHTML += errorBanner(s.error, tabRefreshers.events); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (!s.data||!s.data.events||!s.data.events.length) { c.innerHTML += emptyState('\u{1F4E1}','No events yet'); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  var events = s.data.events;
  if (eventFilter !== 'ALL') {
    events = events.filter(function(ev) { return inferSev(ev) === eventFilter; });
  }
  if (!events.length) { c.innerHTML += emptyState('\u{1F50D}','No '+eventFilter.toLowerCase()+' events'); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  var h = '<div class="data-table-wrap"><table class="data-table"><thead><tr>' +
    '<th class="chevron"></th><th>Time</th><th>Repo</th><th>Branch</th><th>Type</th><th>Severity</th></tr></thead><tbody>';
  events.forEach(function(ev) {
    var sev = inferSev(ev);
    var eid = 'expand-ev-'+ev.seq;
    h += '<tr class="expandable" onclick="toggleExpand(\'ev\',\''+ev.seq+'\');renderEventsTab()">' +
      '<td class="chevron">'+chevron('ev',String(ev.seq))+'</td>' +
      '<td>'+relativeTime(ev.created_at)+'</td>' +
      '<td>'+esc(ev.repo)+'</td><td>'+esc(ev.branch)+'</td>' +
      '<td>'+esc(ev.event_type)+'</td>' +
      '<td>'+severityChip(sev)+'</td></tr>';
    h += '<tr class="expand-row" id="'+eid+'" style="display:'+(isExpanded('ev',String(ev.seq))?'':'none')+'">' +
      '<td colspan="6"><div class="expand-content"><pre style="font-size:0.6875rem;font-family:var(--font-mono);white-space:pre-wrap;word-break:break-all">'+
      esc(ev.payload||'{}')+'</pre></div></td></tr>';
  });
  h += '</tbody></table></div>';
  c.innerHTML += h; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
}
function inferSev(ev) {
  var p = (ev.payload||'').toLowerCase();
  var t = (ev.event_type||'').toLowerCase();
  if (p.indexOf('"critical"')>=0||t.indexOf('critical')>=0) return 'CRITICAL';
  if (p.indexOf('"high"')>=0||t.indexOf('block')>=0||t.indexOf('fail')>=0) return 'HIGH';
  if (p.indexOf('"medium"')>=0||t.indexOf('warn')>=0) return 'MEDIUM';
  if (p.indexOf('"low"')>=0) return 'LOW';
  return 'INFO';
}

/* ── FINDINGS ──────────────────────────────────────────── */
tabRefreshers.findings = async function() {
  await Promise.allSettled([
    fetchSection('fn-checks','/gate-checks'),
    fetchSection('fn-blocks','/block-reasons'),
  ]);
  renderFindingsTab();
};
function renderFindingsTab() {
  var c = clearEl('page-findings'); if (!c) return;
  c.innerHTML = '<div class="page-header"><h2>Findings</h2><p>Gate check results and blocking patterns</p></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  /* view toggle */
  c.innerHTML += '<div class="filter-bar"><button class="filter-btn'+(findingsView==='cards'?' active':'')+'" onclick="findingsView=\'cards\';renderFindingsTab()">Cards</button>' + // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    '<button class="filter-btn'+(findingsView==='table'?' active':'')+'" onclick="findingsView=\'table\';renderFindingsTab()">Table</button></div>';

  /* block reasons */
  var br = sectionState['fn-blocks'];
  if (br&&br.data&&br.data.reasons&&br.data.reasons.length) {
    var bh = '<div class="section"><div class="section-header"><div class="section-title">Top Block Reasons</div></div>' +
      '<div class="data-table-wrap"><table class="data-table"><thead><tr><th>Source</th><th>Count</th><th></th></tr></thead><tbody>';
    var maxCount = br.data.reasons[0].count || 1;
    br.data.reasons.forEach(function(r) {
      var pct = Math.round((r.count/maxCount)*100);
      bh += '<tr><td>'+esc(r.source)+'</td><td>'+r.count+'</td>' +
        '<td><div style="background:var(--status-lost-bg);height:0.375rem;border-radius:2px;width:100%">' +
        '<div style="background:var(--status-lost);height:100%;border-radius:2px;width:'+pct+'%"></div></div></td></tr>';
    });
    bh += '</tbody></table></div></div>';
    c.innerHTML += bh; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  }

  /* gate checks */
  var gc = sectionState['fn-checks'];
  if (!gc||gc.loading) { c.innerHTML += skeletonTable(5); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (gc.error) { c.innerHTML += errorBanner(gc.error, tabRefreshers.findings); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (!gc.data||!gc.data.report) { c.innerHTML += emptyState('\u{1F50D}','No gate-check report. Run codero gate-check to generate one.'); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  var report = gc.data.report;
  var checks = report.checks||[];
  if (!checks.length) { c.innerHTML += emptyState('\u2705','No checks in report'); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  if (findingsView === 'cards') {
    var gh = '<div class="rules-grid">';
    checks.forEach(function(chk) {
      gh += '<div class="rule-card"><div class="rule-card-header">' +
        '<div class="rule-card-name">'+esc(chk.name||chk.id||'\u2014')+'</div>' +
        '<span class="enforcement-badge '+(['fail','error'].indexOf(chk.status)>=0?'hard':'soft')+'">'+esc(chk.status)+'</span></div>' +
        '<div class="rule-card-desc">'+esc(chk.group||chk.tool||'')+'</div>' +
        '<div class="rule-card-stats">' +
          (chk.duration_ms?'<span style="color:var(--text-muted)">'+(chk.duration_ms/1000).toFixed(2)+'s</span>':'') +
          (chk.reason?'<span style="color:var(--status-lost)">'+esc(chk.reason)+'</span>':'') +
        '</div></div>';
    });
    gh += '</div>';
    c.innerHTML += gh; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  } else {
    var th = '<div class="data-table-wrap"><table class="data-table"><thead><tr>' +
      '<th>Name</th><th>Tool</th><th>Status</th><th>Duration</th><th>Reason</th></tr></thead><tbody>';
    checks.forEach(function(chk) {
      th += '<tr><td>'+esc(chk.name||chk.id||'\u2014')+'</td><td>'+esc(chk.tool||chk.group||'')+'</td>' +
        '<td>'+statusChip(chk.status)+'</td>' +
        '<td>'+(chk.duration_ms?(chk.duration_ms/1000).toFixed(2)+'s':'\u2014')+'</td>' +
        '<td style="color:var(--status-lost)">'+esc(chk.reason||'')+'</td></tr>';
    });
    th += '</tbody></table></div>';
    c.innerHTML += th; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  }
}

/* ── ARCHITECTURE ──────────────────────────────────────── */
tabRefreshers.architecture = async function() { renderArchitectureTab(); };
function renderArchitectureTab() {
  var c = clearEl('page-architecture'); if (!c) return;
  c.innerHTML = // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
    '<div class="page-header"><h2>Architecture</h2><p>Service flow and integration map</p></div>' +
    '<div class="arch-container" style="text-align:center;padding:3rem">' +
    '<div style="display:inline-flex;flex-direction:column;gap:1.5rem;align-items:center">' +
    '<div class="arch-node" style="border-color:var(--primary)">codero gate-check</div>' +
    '<div style="color:var(--text-muted)">\u2193</div>' +
    '<div style="display:flex;gap:1.5rem;flex-wrap:wrap;justify-content:center">' +
      '<div class="arch-node">gitleaks</div>' +
      '<div class="arch-node">semgrep</div>' +
      '<div class="arch-node">path-guard</div>' +
      '<div class="arch-node">LiteLLM</div>' +
      '<div class="arch-node">CodeRabbit</div>' +
    '</div>' +
    '<div style="color:var(--text-muted)">\u2193</div>' +
    '<div class="arch-node" style="border-color:var(--status-active)">findings &amp; delivery events</div>' +
    '<div style="color:var(--text-muted)">\u2193</div>' +
    '<div style="display:flex;gap:1.5rem;flex-wrap:wrap;justify-content:center">' +
      '<div class="arch-node">dashboard</div>' +
      '<div class="arch-node">TUI</div>' +
      '<div class="arch-node">webhooks</div>' +
    '</div>' +
    '</div></div>';
}

/* ── SETTINGS ──────────────────────────────────────────── */
tabRefreshers.settings = async function() {
  await fetchSection('st-data','/settings');
  renderSettingsTab();
};
function renderSettingsTab() {
  var c = clearEl('page-settings'); if (!c) return;
  c.innerHTML = '<div class="page-header"><h2>Settings</h2><p>Integration connections and gate pipeline configuration</p></div>'; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method

  var s = sectionState['st-data'];
  if (!s||s.loading) { c.innerHTML += skeletonTable(4); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (s.error) { c.innerHTML += errorBanner(s.error, tabRefreshers.settings); return; } // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  if (!s.data) return;

  if (!settingsLocal) settingsLocal = JSON.parse(JSON.stringify(s.data));

  /* integrations */
  var ints = settingsLocal.integrations||[];
  if (ints.length) {
    var ih = '<div class="section"><div class="section-header"><div class="section-title">Integrations</div></div><div class="int-grid">';
    ints.forEach(function(it) {
      ih += '<div class="int-card"><div class="int-card-header">' +
        '<div class="int-card-icon">'+intIcon(it.id)+'</div>' +
        '<div><div class="int-card-name">'+esc(it.name)+'</div><div class="int-card-desc">'+esc(it.desc)+'</div></div></div>' +
        '<div class="int-card-footer">'+statusChip(it.connected?'active':'cancelled')+'</div></div>';
    });
    ih += '</div></div>';
    c.innerHTML += ih; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  }

  /* gate pipeline */
  var gates = settingsLocal.gate_pipeline||[];
  if (gates.length) {
    var gh = '<div class="section"><div class="section-header"><div class="section-title">Gate Pipeline</div>' +
      (settingsDirty ? '<button class="filter-btn active" onclick="saveSettings()">Save Changes</button>' : '') +
      '</div><div class="data-table-wrap"><table class="data-table"><thead><tr>' +
      '<th>Name</th><th>Provider</th><th>Enabled</th><th>Blocks Commit</th><th>Timeout</th></tr></thead><tbody>';
    gates.forEach(function(g, i) {
      gh += '<tr><td>'+esc(g.name)+'</td><td>'+esc(g.provider)+'</td>' +
        '<td><button class="toggle'+(g.enabled?' on':'')+'" onclick="toggleGate('+i+',\'enabled\')"><div class="toggle-knob"></div></button></td>' +
        '<td><button class="toggle'+(g.blocks_commit?' on':'')+'" onclick="toggleGate('+i+',\'blocks_commit\')"><div class="toggle-knob"></div></button></td>' +
        '<td>'+g.timeout_sec+'s</td></tr>';
    });
    gh += '</tbody></table></div></div>';
    c.innerHTML += gh; // nosemgrep: javascript.browser.security.insecure-document-method.insecure-document-method
  }
}
function intIcon(id) {
  var m = {coderabbit:'\u{1F407}',litellm:'\u{1F916}',copilot:'\u{1F419}',semgrep:'\u{1F6E1}',gitleaks:'\u{1F512}'};
  return m[id]||'\u{2699}';
}
function toggleGate(idx, field) {
  if (!settingsLocal||!settingsLocal.gate_pipeline) return;
  settingsLocal.gate_pipeline[idx][field] = !settingsLocal.gate_pipeline[idx][field];
  settingsDirty = true;
  renderSettingsTab();
}
async function saveSettings() {
  if (!settingsLocal) return;
  try {
    await apiFetch('/settings', {
      method: 'PUT',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({integrations:settingsLocal.integrations, gate_pipeline:settingsLocal.gate_pipeline}),
    });
    settingsDirty = false;
    await fetchSection('st-data','/settings');
    settingsLocal = JSON.parse(JSON.stringify(sectionState['st-data'].data));
    renderSettingsTab();
  } catch(e) {
    alert('Failed to save: ' + e.message);
  }
}

/* ── POLLING ───────────────────────────────────────────── */
var pollTimer = null;
function startPolling() {
  if (pollTimer) clearTimeout(pollTimer);
  pollTimer = setTimeout(function() {
    refreshActiveTab().catch(function(){}).then(function() { startPolling(); });
  }, POLL);
}
async function refreshActiveTab() {
  var fn = tabRefreshers[activeTab];
  if (fn) await fn();
}

/* ── INIT ──────────────────────────────────────────────── */
document.addEventListener('DOMContentLoaded', function() {
  initTheme();
  initRouter();
  startPolling();
});
