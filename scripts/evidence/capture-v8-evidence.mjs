#!/usr/bin/env node
// capture-v8-evidence.mjs — deterministic dashboard/TUI variant evidence capture.
//
// Starts the dashboard in fixture mode using scripts/evidence/fixtures/v8/,
// asserts that the expected fixture state is visible in the DOM before
// taking any screenshots, then captures all dashboard tabs and a TUI text
// snapshot, writing everything to /srv/storage/local/Mocks/TUI/v1/.
//
// Aborts with a non-zero exit code if:
//  - The binary cannot be built or the server does not start.
//  - Pre-screenshot DOM assertions fail (fixture state not loaded).
//  - Any tab screenshot fails.
//
// Usage:
//   CODERO_PLAYWRIGHT_MODULE=<path> node capture-v8-evidence.mjs
// KEEP_LOCAL: required for shipped runtime — evidence capture harness

import { spawn } from 'node:child_process';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import net from 'node:net';
import path from 'node:path';
import process from 'node:process';

const playwrightModule = process.env.CODERO_PLAYWRIGHT_MODULE;
if (!playwrightModule) {
  throw new Error('CODERO_PLAYWRIGHT_MODULE must point to the Playwright module entrypoint');
}
const pw = await import(playwrightModule);
const { chromium } = pw.default ?? pw;

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, '..', '..');
const fixtureDir = path.join(__dirname, 'fixtures', 'v8');
const fixtureReportPath = path.join(fixtureDir, 'report.json');
const outputDir = '/srv/storage/local/Mocks/TUI/v1';
const VERSION = process.env.CODERO_EVIDENCE_VERSION || 'v9';

const GO_ENV = {
  ...process.env,
  GOFLAGS: process.env.GOFLAGS || '-buildvcs=false',
};

// ── helpers ──────────────────────────────────────────────────────────────────

function escapeHtml(value) {
  const entities = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
  return value.replace(/[&<>"']/g, (ch) => entities[ch] || ch);
}

function canonicalizeSnapshot(text) {
  const trimmed = text.replace(/[ \t]+$/gm, '');
  return trimmed.endsWith('\n') ? trimmed : `${trimmed}\n`;
}

function spawnCapture(cmd, args, opts = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, args, {
      cwd: repoRoot,
      env: opts.env || GO_ENV,
      stdio: ['ignore', 'pipe', 'pipe'],
      ...opts,
    });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (chunk) => { stdout += chunk.toString('utf8'); });
    child.stderr.on('data', (chunk) => { stderr += chunk.toString('utf8'); });
    child.on('error', reject);
    child.on('close', (code, signal) => resolve({ code, signal, stdout, stderr }));
  });
}

function spawnStreaming(cmd, args, opts = {}) {
  const child = spawn(cmd, args, {
    cwd: repoRoot,
    env: opts.env || GO_ENV,
    stdio: ['ignore', 'pipe', 'pipe'],
    ...opts,
  });
  child.startup = new Promise((resolve, reject) => {
    child.once('spawn', resolve);
    child.once('error', (err) => {
      process.stderr.write(`[spawn] failed to start ${cmd}: ${err.message}\n`);
      reject(err);
    });
  });
  child.stdout.on('data', (chunk) => process.stdout.write(chunk));
  child.stderr.on('data', (chunk) => process.stderr.write(chunk));
  return child;
}

async function findFreePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once('error', reject);
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      if (!addr || typeof addr === 'string') {
        srv.close(() => reject(new Error('failed to allocate local port')));
        return;
      }
      const port = addr.port;
      srv.close(() => resolve(port));
    });
  });
}

async function waitForUrl(url, timeoutMs = 20000) {
  const started = Date.now();
  let lastErr = null;
  while (Date.now() - started < timeoutMs) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 1500);
    try {
      const resp = await fetch(url, { method: 'GET', signal: controller.signal });
      if (resp.ok) { await resp.arrayBuffer(); return; }
      lastErr = new Error(`status ${resp.status}`);
    } catch (err) {
      lastErr = err;
    } finally {
      clearTimeout(timer);
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`timed out waiting for ${url}${lastErr ? `: ${lastErr.message}` : ''}`);
}

async function stopProcess(child, timeoutMs = 5000) {
  if (child.exitCode != null || child.signalCode != null) return;
  const waitClose = new Promise((r) => child.once('close', r));
  child.kill('SIGINT');
  await Promise.race([waitClose, new Promise((r) => setTimeout(r, timeoutMs))]);
  if (child.exitCode == null && child.signalCode == null) {
    child.kill('SIGKILL');
    await Promise.race([waitClose, new Promise((r) => setTimeout(r, 1000))]);
  }
}

async function renderTuiSnapshot(snapshotText, outPath) {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({ viewport: { width: 1480, height: 1080 }, deviceScaleFactor: 1 });
    const html = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>codero tui snapshot</title>
<style>
  :root{color-scheme:dark}*{box-sizing:border-box}html,body{margin:0;background:#0d0d0d;color:#dedad2;font-family:"IBM Plex Mono",Consolas,monospace}
  body{padding:24px}.frame{border:1px solid rgba(255,255,255,.09);border-radius:10px;background:linear-gradient(180deg,rgba(255,255,255,.02),rgba(255,255,255,.01));box-shadow:0 20px 60px rgba(0,0,0,.35);overflow:hidden}
  .bar{display:flex;align-items:center;gap:10px;height:36px;padding:0 14px;border-bottom:1px solid rgba(255,255,255,.08);background:rgba(255,255,255,.03)}
  .dots{display:flex;gap:6px}.dot{width:11px;height:11px;border-radius:50%;background:#444}.dot.red{background:#ff5f56}.dot.yellow{background:#ffbd2e}.dot.green{background:#27c93f}
  .title{font-size:12px;color:#a3a09b;letter-spacing:.03em}
  pre{margin:0;padding:18px 22px 22px;font-size:13px;line-height:1.45;white-space:pre;overflow:hidden}
  .label{display:inline-flex;align-items:center;gap:8px;margin-bottom:10px;padding:5px 10px;font-size:11px;letter-spacing:.08em;text-transform:uppercase;color:#3fffa0;background:rgba(63,255,160,.08);border:1px solid rgba(63,255,160,.18);border-radius:999px}
</style></head><body>
  <div class="frame">
    <div class="bar"><div class="dots"><span class="dot red"></span><span class="dot yellow"></span><span class="dot green"></span></div><div class="title">codero • TUI snapshot ${VERSION} evidence</div></div>
    <pre><span class="label">Gate-check snapshot</span>\n${escapeHtml(snapshotText)}</pre>
  </div>
</body></html>`;
    await page.setContent(html, { waitUntil: 'load' });
    await page.screenshot({ path: outPath, fullPage: true });
  } finally {
    await browser.close();
  }
}

function assertTuiSnapshotText(snapshotText) {
  for (const required of [
    'OVERALL  FAIL',
    'PROFILE  portable',
    'pass=8',
    'fail=3',
    'skip=1',
    'disabled=1',
    'total=14',
    'infra=1',
    'REQUIRED failed=2',
    'sonarcloud',
  ]) {
    if (!snapshotText.includes(required)) {
      throw new Error(`[assert] TUI snapshot missing "${required}"`);
    }
  }
  console.log('[assert] ✓ TUI snapshot matches fixture report');
}

// ── pre-screenshot DOM assertions ────────────────────────────────────────────

async function assertFixtureState(page, baseUrl) {
  console.log('[assert] Waiting for gate-check report to load in findings tab...');

  // Navigate to the findings tab so we can check the populated state.
  await page.goto(`${baseUrl}/dashboard/`, { waitUntil: 'load' });
  await page.waitForSelector('#page-processes');
  await page.click('#tab-findings');
  await page.waitForSelector('#page-findings.active');

  // Wait for findings-cards to no longer show loading/waiting text.
  await page.waitForFunction(() => {
    const cards = document.querySelector('#findings-cards');
    return Boolean(cards && !cards.textContent.includes('loading gate checks'));
  }, { timeout: 15000 });

  // Assert 1: overall status should be FAIL.
  const overallStatus = await page.$eval('#f-overall', (el) => el.textContent.trim());
  if (!overallStatus.toUpperCase().includes('FAIL')) {
    throw new Error(`[assert] Expected #f-overall to contain FAIL, got: "${overallStatus}"`);
  }
  console.log(`[assert] ✓ Review status: ${overallStatus}`);

  // Assert 2: pipeline summary should contain "14 steps".
  await page.waitForFunction(() => {
    const el = document.querySelector('#pipeline-rail-summary');
    return el && el.textContent.includes('steps') && !el.textContent.includes('waiting');
  }, { timeout: 10000 });
  const pipelineSummary = await page.$eval('#pipeline-rail-summary', (el) => el.textContent.trim());
  if (!pipelineSummary.includes('14')) {
    throw new Error(`[assert] Expected pipeline summary to contain "14", got: "${pipelineSummary}"`);
  }
  console.log(`[assert] ✓ Pipeline summary: ${pipelineSummary}`);

  // Assert 3: blocker count should be 2 (required_failed = 2 in the fixture report).
  const blockerCount = await page.$eval('#blocker-count', (el) => el.textContent.trim());
  if (blockerCount !== '2') {
    throw new Error(`[assert] Expected #blocker-count = 2, got: "${blockerCount}"`);
  }
  console.log(`[assert] ✓ Blocker count: ${blockerCount}`);

  // Assert 4: bypass count should be visible (infra_bypassed = 1 in fixture report).
  const bypassCount = await page.$eval('#f-sev-bypass', (el) => el.textContent.trim());
  if (bypassCount === '—' || bypassCount === '' || bypassCount === '0') {
    throw new Error(`[assert] Expected #f-sev-bypass to show bypass count, got: "${bypassCount}"`);
  }
  console.log(`[assert] ✓ Bypass count: ${bypassCount}`);

  // Assert 5: findings-cards should not contain "waiting for gate-check report".
  const cardsText = await page.$eval('#findings-cards', (el) => el.textContent);
  if (cardsText.includes('waiting for gate-check report') || cardsText.includes('no gate-check report')) {
    throw new Error(`[assert] findings-cards still shows empty/waiting state: "${cardsText.slice(0, 120)}"`);
  }
  console.log('[assert] ✓ Findings cards populated (no empty state)');

  // Assert 6: agents-count should reflect seeded sessions (>= 2).
  // Give the active-sessions API a moment to load.
  await page.waitForFunction(() => {
    const el = document.querySelector('#agents-count');
    return el && parseInt(el.textContent, 10) >= 2;
  }, { timeout: 10000 });
  const agentsCount = await page.$eval('#agents-count', (el) => el.textContent.trim());
  console.log(`[assert] ✓ Agents active: ${agentsCount}`);

  console.log('[assert] All pre-screenshot assertions passed.');
}

// ── tab capture helpers ───────────────────────────────────────────────────────

async function captureTab(page, tabId, pageId, screenshotPath) {
  if (tabId) {
    await page.click(`#${tabId}`);
    await page.waitForSelector(`#${pageId}.active`);
    // Brief pause for any in-flight render after tab switch.
    await new Promise((r) => setTimeout(r, 400));
  }
  await page.screenshot({ path: screenshotPath, fullPage: true });
  console.log(`  → ${path.basename(screenshotPath)}`);
}

// ── main ─────────────────────────────────────────────────────────────────────

async function main() {
  await mkdir(outputDir, { recursive: true });

  // Capture TUI text snapshot first from the same fixture report used by the dashboard.
  console.log('\n[step 1] Capturing TUI text snapshot from fixture report...');
  const tuiRun = await spawnCapture('go', [
    'run', './cmd/codero',
    'gate-check', '--tui-snapshot', '--load-report', fixtureReportPath,
  ]);
  if (tuiRun.code !== 0 && tuiRun.code !== 1) {
    throw new Error(`tui snapshot command failed with exit code ${tuiRun.code}${tuiRun.stderr ? `: ${tuiRun.stderr.trim()}` : ''}`);
  }
  const tuiText = canonicalizeSnapshot(tuiRun.stdout || '');
  if (!tuiText.trim()) {
    throw new Error(`tui snapshot command produced no stdout${tuiRun.stderr ? `: ${tuiRun.stderr.trim()}` : ''}`);
  }
  assertTuiSnapshotText(tuiText);
  const tuiTextPath = path.join(outputDir, `tui-${VERSION}-view.txt`);
  await writeFile(tuiTextPath, tuiText, 'utf8');
  console.log(`  → ${path.basename(tuiTextPath)}`);
  const tuiImagePath = path.join(outputDir, `tui-${VERSION}-view.png`);
  await renderTuiSnapshot(tuiText, tuiImagePath);
  console.log(`  → ${path.basename(tuiImagePath)}`);

  // Start fixture dashboard server.
  console.log('\n[step 2] Starting dashboard fixture server...');
  const port = await findFreePort();
  const serverProc = spawnStreaming('go', [
    'run', './cmd/codero',
    'dashboard',
    '--serve-fixture',
    '--fixture-dir', fixtureDir,
    '--host', '127.0.0.1',
    '--port', String(port),
    '--repo-path', repoRoot,
  ]);

  const baseUrl = `http://127.0.0.1:${port}`;
  try {
    await serverProc.startup;
    await waitForUrl(`${baseUrl}/gate`);
    console.log(`  → Server ready at ${baseUrl}`);

    const browser = await chromium.launch({ headless: true });
    try {
      const page = await browser.newPage({
        viewport: { width: 1600, height: 1400 },
        deviceScaleFactor: 1,
      });

      // Step 3: run pre-screenshot DOM assertions.
      console.log('\n[step 3] Running pre-screenshot DOM assertions...');
      await assertFixtureState(page, baseUrl);

      // Step 4: capture all tabs.
      console.log('\n[step 4] Capturing screenshots...');

      // Main screenshot (processes tab = default / hero view).
      await page.goto(`${baseUrl}/dashboard/`, { waitUntil: 'load' });
      await page.waitForSelector('#page-processes');
      await new Promise((r) => setTimeout(r, 600));
      await page.screenshot({ path: path.join(outputDir, `dashboard-${VERSION}.png`), fullPage: true });
      console.log(`  → dashboard-${VERSION}.png`);

      // Processes tab (same as default but explicit).
      await captureTab(page, null, 'page-processes', path.join(outputDir, `dashboard-${VERSION}-processes.png`));

      // Event logs tab.
      await captureTab(page, 'tab-eventlogs', 'page-eventlogs', path.join(outputDir, `dashboard-${VERSION}-eventlogs.png`));

      // Findings tab (shows populated failing fixture).
      await page.click('#tab-findings');
      await page.waitForSelector('#page-findings.active');
      await page.waitForFunction(() => {
        const cards = document.querySelector('#findings-cards');
        return Boolean(cards && !cards.textContent.includes('loading gate checks'));
      });
      await new Promise((r) => setTimeout(r, 400));
      await page.screenshot({ path: path.join(outputDir, `dashboard-${VERSION}-findings.png`), fullPage: true });
      console.log(`  → dashboard-${VERSION}-findings.png`);

      // Architecture tab.
      await captureTab(page, 'tab-architecture', 'page-architecture', path.join(outputDir, `dashboard-${VERSION}-architecture.png`));

      // Settings tab.
      await captureTab(page, 'tab-settings', 'page-settings', path.join(outputDir, `dashboard-${VERSION}-settings.png`));

      // Chat screenshot: scroll to chat panel while on findings tab.
      await page.click('#tab-findings');
      await page.waitForSelector('#page-findings.active');
      await new Promise((r) => setTimeout(r, 400));
      await page.screenshot({ path: path.join(outputDir, `dashboard-${VERSION}-chat.png`), fullPage: true });
      console.log(`  → dashboard-${VERSION}-chat.png`);

    } finally {
      await browser.close();
    }

    // Copy fixture report to output dir as the v8 reference report.
    const reportJson = JSON.parse(await readFile(fixtureReportPath, 'utf8'));
    reportJson.run_at = 'NORMALIZED_TIMESTAMP';
    const reportOutPath = path.join(outputDir, `dashboard-${VERSION}-report.json`);
    await writeFile(reportOutPath, JSON.stringify(reportJson, null, 2) + '\n', 'utf8');
    console.log(`  → dashboard-${VERSION}-report.json`);

  } finally {
    await stopProcess(serverProc);
  }

  console.log(`\n✓ ${VERSION} evidence bundle complete.`);
  console.log(`  Output directory: ${outputDir}`);
  process.exit(0);
}

main().catch((err) => {
  console.error('\n✗ Capture failed:', err.message || err);
  process.exit(1);
});
