#!/usr/bin/env node

import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import net from 'node:net';
import os from 'node:os';
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
const evidenceDir = path.join(repoRoot, 'docs', 'evidence', 'COD-063');

const GO_ENV = {
  ...process.env,
  GOFLAGS: process.env.GOFLAGS || '-buildvcs=false',
};

function escapeHtml(value) {
  const entities = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  };
  return value.replace(/[&<>"']/g, (ch) => entities[ch] || ch);
}

function sanitizePaths(value) {
  if (typeof value === 'string') {
    return value.startsWith('/') ? '<redacted-path>' : value;
  }
  if (Array.isArray(value)) {
    return value.map((item) => sanitizePaths(item));
  }
  if (value && typeof value === 'object') {
    const out = {};
    for (const [key, val] of Object.entries(value)) {
      out[key] = sanitizePaths(val);
    }
    return out;
  }
  return value;
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
  child.stdout.on('data', (chunk) => process.stdout.write(chunk));
  child.stderr.on('data', (chunk) => process.stderr.write(chunk));
  return child;
}

async function findFreePort() {
  return await new Promise((resolve, reject) => {
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

async function waitForUrl(url, timeoutMs = 15000) {
  const started = Date.now();
  let lastErr = null;
  while (Date.now() - started < timeoutMs) {
    try {
      const resp = await fetch(url, { method: 'GET' });
      if (resp.ok) {
        await resp.arrayBuffer();
        return;
      }
      lastErr = new Error(`status ${resp.status}`);
    } catch (err) {
      lastErr = err;
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`timed out waiting for ${url}${lastErr ? `: ${lastErr.message}` : ''}`);
}

async function stopProcess(child, timeoutMs = 5000) {
  if (child.exitCode != null || child.signalCode != null) {
    return;
  }
  const waitForClose = new Promise((resolve) => {
    child.once('close', resolve);
  });
  const waitForTimeout = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  child.kill('SIGINT');
  await Promise.race([waitForClose, waitForTimeout(timeoutMs)]);
  if (child.exitCode == null && child.signalCode == null) {
    child.kill('SIGKILL');
    await Promise.race([waitForClose, waitForTimeout(1000)]);
  }
}

async function renderTuiSnapshot(snapshotText, outPath) {
  const browser = await chromium.launch({ headless: true });
  try {
    const page = await browser.newPage({
      viewport: { width: 1480, height: 1080 },
      deviceScaleFactor: 1,
    });
    const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>codero tui snapshot</title>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  html, body {
    margin: 0;
    background: #0d0d0d;
    color: #dedad2;
    font-family: "IBM Plex Mono", "SFMono-Regular", Consolas, monospace;
  }
  body {
    padding: 24px;
  }
  .frame {
    border: 1px solid rgba(255,255,255,.09);
    border-radius: 10px;
    background: linear-gradient(180deg, rgba(255,255,255,.02), rgba(255,255,255,.01));
    box-shadow: 0 20px 60px rgba(0,0,0,.35);
    overflow: hidden;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 36px;
    padding: 0 14px;
    border-bottom: 1px solid rgba(255,255,255,.08);
    background: rgba(255,255,255,.03);
  }
  .dots { display: flex; gap: 6px; }
  .dot { width: 11px; height: 11px; border-radius: 50%; background: #444; }
  .dot.red { background: #ff5f56; }
  .dot.yellow { background: #ffbd2e; }
  .dot.green { background: #27c93f; }
  .title { font-size: 12px; color: #a3a09b; letter-spacing: .03em; }
  pre {
    margin: 0;
    padding: 18px 22px 22px;
    font-size: 13px;
    line-height: 1.45;
    white-space: pre;
    overflow: hidden;
  }
  .label {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 10px;
    padding: 5px 10px;
    font-size: 11px;
    letter-spacing: .08em;
    text-transform: uppercase;
    color: #3fffa0;
    background: rgba(63,255,160,.08);
    border: 1px solid rgba(63,255,160,.18);
    border-radius: 999px;
  }
</style>
</head>
<body>
  <div class="frame">
    <div class="bar">
      <div class="dots"><span class="dot red"></span><span class="dot yellow"></span><span class="dot green"></span></div>
      <div class="title">codero • TUI snapshot progress evidence</div>
    </div>
    <pre><span class="label">Gate-check snapshot</span>
${escapeHtml(snapshotText)}</pre>
  </div>
</body>
</html>`;
    await page.setContent(html, { waitUntil: 'load' });
    await page.screenshot({ path: outPath, fullPage: true });
  } finally {
    await browser.close();
  }
}

async function main() {
  await mkdir(evidenceDir, { recursive: true });
  const tmpDir = await mkdtemp(path.join(os.tmpdir(), 'codero-cod063-'));

  const reportPath = path.join(tmpDir, 'dashboard-report.json');
  const dashboardReportPath = path.join(evidenceDir, 'dashboard-report.json');
  const tuiTextPath = path.join(evidenceDir, 'tui-snapshot.txt');
  const dashboardPng = path.join(evidenceDir, 'dashboard.png');
  const tuiPng = path.join(evidenceDir, 'tui.png');

  const tuiRun = await spawnCapture('go', [
    'run',
    './cmd/codero',
    'gate-check',
    '--tui-snapshot',
    '--profile',
    'portable',
    '--repo-path',
    repoRoot,
  ]);

  const tuiSnapshot = canonicalizeSnapshot(tuiRun.stdout || tuiRun.stderr || 'No TUI snapshot output captured.');
  await writeFile(tuiTextPath, tuiSnapshot, 'utf8');
  await renderTuiSnapshot(tuiSnapshot, tuiPng);

  const reportRun = await spawnCapture('go', [
    'run',
    './cmd/codero',
    'gate-check',
    '--profile',
    'portable',
    '--repo-path',
    repoRoot,
    '--report-path',
    reportPath,
  ]);

  if (reportRun.code !== 0) {
    if (reportRun.stderr) {
      process.stderr.write(reportRun.stderr);
    }
    throw new Error(`dashboard report command failed with exit code ${reportRun.code}${reportRun.signal ? ` signal ${reportRun.signal}` : ''}`);
  }

  const reportJson = JSON.parse(await readFile(reportPath, 'utf8'));
  const normalizedReport = sanitizePaths(reportJson);
  normalizedReport.run_at = 'NORMALIZED_TIMESTAMP';
  await writeFile(dashboardReportPath, JSON.stringify(normalizedReport, null, 2) + '\n', 'utf8');

  const port = await findFreePort();
  const dashboard = spawnStreaming('go', [
    'run',
    './cmd/codero',
    'dashboard',
    '--serve-fixture',
    '--host',
    '127.0.0.1',
    '--port',
    String(port),
    '--repo-path',
    repoRoot,
    '--report-path',
    reportPath,
  ]);

  try {
    await waitForUrl(`http://127.0.0.1:${port}/dashboard/`);

    const browser = await chromium.launch({ headless: true });
    try {
      const page = await browser.newPage({
        viewport: { width: 1600, height: 1400 },
        deviceScaleFactor: 1,
      });
      await page.goto(`http://127.0.0.1:${port}/dashboard/`, { waitUntil: 'load' });
      await page.waitForSelector('#page-processes');
      await page.click('#tab-findings');
      await page.waitForSelector('#page-findings.active');
      await page.waitForFunction(() => {
        const cards = document.querySelector('#findings-cards');
        return Boolean(cards && !cards.textContent.includes('loading gate checks'));
      });
      await page.screenshot({ path: dashboardPng, fullPage: true });
    } finally {
      await browser.close();
    }
  } finally {
    await stopProcess(dashboard);
  }

  process.exit(0);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
