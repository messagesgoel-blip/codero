// api.js — Fetch wrapper, request caching, response normalizers.

import store from './store.js';

const API = '/api/v1/dashboard';
const TIMEOUT = 15000;
const _cache = new Map();
const _inflight = new Map();

export async function apiFetch(path, opts = {}) {
  const url = path.startsWith('/') ? path : `${API}/${path}`;
  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), opts.timeout || TIMEOUT);
  try {
    const resp = await fetch(url, { signal: ac.signal, ...opts });
    clearTimeout(timer);
    if (!resp.ok) {
      const body = await resp.text().catch(() => '');
      throw new Error(`${resp.status}: ${body}`);
    }
    return opts.raw ? resp : await resp.json();
  } catch (e) {
    clearTimeout(timer);
    if (e.name === 'AbortError') throw new Error('Request timed out');
    throw e;
  }
}

export async function apiFetchCached(key, path, ttlMs = 30000) {
  const entry = _cache.get(key);
  if (entry && Date.now() - entry.ts < ttlMs) return entry.data;
  if (_inflight.has(key)) return _inflight.get(key);
  const promise = apiFetch(path).then(data => {
    _cache.set(key, { data, ts: Date.now() });
    _inflight.delete(key);
    return data;
  }).catch(e => {
    _inflight.delete(key);
    throw e;
  });
  _inflight.set(key, promise);
  return promise;
}

export function invalidateCache(key) { _cache.delete(key); }
export function clearCache() { _cache.clear(); }

export async function apiPost(path, body) {
  return apiFetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export async function apiPut(path, body) {
  return apiFetch(path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

// --- Data loaders (fetch + normalize + store) ---

export async function loadOverview() {
  const data = await apiFetch('overview');
  store.set({ overview: normalizeOverview(data) });
}

export async function loadHealth() {
  const data = await apiFetch('health');
  store.set({ health: data });
}

export async function loadSessions() {
  const data = await apiFetch('active-sessions');
  store.set({ sessions: normalizeSessions(data) });
}

export async function loadAssignments() {
  const data = await apiFetch('assignments');
  store.set({ assignments: normalizeAssignments(data) });
}

export async function loadPipeline() {
  const data = await apiFetch('pipeline');
  store.set({ pipeline: normalizePipeline(data) });
}

export async function loadQueue() {
  const [q, stats] = await Promise.all([apiFetch('queue'), apiFetch('queue/stats')]);
  store.set({ queue: normalizeQueue(q), queueStats: stats });
}

export async function loadRepos() {
  const data = await apiFetchCached('repos', 'repos');
  store.set({ repos: normalizeRepos(data) });
}

export async function loadNodeRepos() {
  const data = await apiFetch('node-repos');
  store.set({ nodeRepos: data });
  return data;
}

export async function loadEvents() {
  const data = await apiFetch('activity');
  store.set({ events: normalizeEvents(data) });
}

export async function loadGateChecks() {
  const data = await apiFetch('gate-checks');
  store.set({ gateChecks: data });
}

export async function loadFindings() {
  const data = await apiFetch('block-reasons');
  store.set({ blockReasons: normalizeBlockReasons(data) });
}

export async function loadGateHealth() {
  const data = await apiFetch('gate-health');
  store.set({ gateHealth: data.gates || [] });
}

export async function loadSettings() {
  const data = await apiFetchCached('settings', 'settings', 60000);
  store.set({ settings: data });
}

export async function loadGateConfig() {
  const data = await apiFetch('settings/gate-config');
  store.set({ gateConfig: data });
}

export async function loadCompliance() {
  const data = await apiFetch('compliance');
  store.set({ compliance: data });
}

export async function loadArchives() {
  const data = await apiFetch('archives?limit=50');
  store.set({ archives: normalizeArchives(data) });
}

export async function loadTrackingConfig() {
  const data = await apiFetch('tracking-config');
  store.set({ trackingConfig: data });
}

export async function loadAgents() {
  const data = await apiFetch('/api/v1/dashboard/agents');
  store.set({ agents: normalizeAgents(data) });
}

export async function loadAgentSessions(agentId) {
  return apiFetch(`/api/v1/dashboard/agents/${encodeURIComponent(agentId)}/sessions`);
}

export async function loadSessionTail(sessionId, lines = 50) {
  return apiFetch(`/api/v1/dashboard/sessions/${encodeURIComponent(sessionId)}/tail?lines=${lines}`);
}

export async function toggleAgentTracking(agentId, disabled) {
  const data = await apiPut('tracking-config', { agent_id: agentId, disabled });
  store.set({ trackingConfig: data });
  return data;
}

export async function updateAgentEnvVars(agentId, envVars) {
  const data = await apiPut('tracking-config', { agent_id: agentId, env_vars: envVars });
  store.set({ trackingConfig: data });
  return data;
}
// --- Normalizers ---

function normalizeOverview(raw) {
  return {
    runsToday: raw.runs_today ?? 0,
    passRate: raw.pass_rate ?? -1,
    blockedCount: raw.blocked_count ?? 0,
    avgGateSec: raw.avg_gate_sec ?? 0,
    sparkline: (raw.sparkline_7d || []).map(d => ({
      date: d.date, total: d.total, passed: d.passed, failed: d.failed,
    })),
  };
}

function normalizeSessions(raw) {
  return (raw.sessions || []).map(s => ({
    id: s.session_id, agent: s.agent_id, repo: s.repo, branch: s.branch,
    worktree: s.worktree, prNumber: s.pr_number, mode: s.mode,
    state: s.activity_state, task: s.task,
    startedAt: s.started_at, lastHeartbeat: s.last_heartbeat_at,
    elapsedSec: s.elapsed_sec, ownerAgent: s.owner_agent,
    lastIOAt: s.last_io_at,
    contextPressure: s.context_pressure || 'normal',
    compactCount: s.compact_count || 0,
  }));
}

function normalizeAssignments(raw) {
  return (raw.assignments || []).map(a => ({
    id: a.assignment_id, sessionId: a.session_id, agent: a.agent_id,
    repo: a.repo, branch: a.branch, taskId: a.task_id,
    state: a.state, substatus: a.substatus, blockedReason: a.blocked_reason,
    prNumber: a.pr_number, branchState: a.branch_state,
    startedAt: a.started_at, endedAt: a.ended_at,
  }));
}

function normalizePipeline(raw) {
  return (raw.pipeline || []).map(p => ({
    sessionId: p.session_id, assignmentId: p.assignment_id,
    taskId: p.task_id, agent: p.agent_id,
    repo: p.repo, branch: p.branch,
    state: p.state, substatus: p.substatus,
    checkpoint: p.checkpoint, version: p.version,
    stageSec: p.stage_sec, startedAt: p.started_at,
    updatedAt: p.updated_at,
  }));
}

function normalizeQueue(raw) {
  return (raw.items || raw.queue || []).map(q => ({
    id: q.id, repo: q.repo, branch: q.branch,
    state: q.state, priority: q.priority,
    ownerSession: q.owner_session_id, submissionTime: q.submission_time,
  }));
}

function normalizeRepos(raw) {
  return (raw.repos || []).map(r => ({
    repo: r.repo, branch: r.branch, state: r.state,
    headHash: r.head_hash, lastRunStatus: r.last_run_status,
    lastRunAt: r.last_run_at, updatedAt: r.updated_at,
    gateSummary: r.gate_summary,
  }));
}

function normalizeEvents(raw) {
  return (raw.events || []).map(e => ({
    seq: e.seq, repo: e.repo, branch: e.branch,
    type: e.event_type, payload: e.payload,
    createdAt: e.created_at, sessionId: e.session_id,
    assignmentId: e.assignment_id,
  }));
}

function normalizeBlockReasons(raw) {
  return (raw.reasons || []).map(r => ({ source: r.source, count: r.count }));
}

function normalizeAgents(raw) {
  return (raw.agents || []).map(a => ({
    agentId: a.agent_id,
    activeSessions: a.active_sessions ?? 0,
    totalSessions: a.total_sessions ?? 0,
    lastSeen: a.last_seen || null,
    avgElapsedSec: a.avg_elapsed_sec ?? 0,
    totalTokens: a.total_tokens ?? 0,
    tokensPerSec: a.tokens_per_sec ?? 0,
    activePressure: a.active_pressure || '',
    status: a.status || 'idle',
  }));
}

function normalizeArchives(raw) {
  return (raw.archives || []).map(a => ({
    id: a.archive_id, sessionId: a.session_id, agent: a.agent_id,
    taskId: a.task_id, repo: a.repo, branch: a.branch,
    result: a.result, startedAt: a.started_at, endedAt: a.ended_at,
    durationSec: a.duration_seconds, commitCount: a.commit_count,
    mergeSha: a.merge_sha, taskSource: a.task_source, archivedAt: a.archived_at,
  }));
}

// ---- Operator Actions ----

export function sessionAction(assignmentId, action) {
  return apiPost(`assignments/${encodeURIComponent(assignmentId)}/${encodeURIComponent(action)}`, {});
}

export async function loadScorecard() {
  const data = await apiFetch('scorecard');
  store.set({ scorecard: data });
}
