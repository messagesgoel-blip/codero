// renderers/chat.js — Chat page (full + floating mode) renderer.

import store from '../store.js';
import { apiFetch } from '../api.js';
import { esc, $, setHtml } from '../utils.js';
import { toast } from '../components.js';

const QUICK_QUERIES = [
  { label: '/status',  prompt: '/status' },
  { label: '/queue',   prompt: '/queue' },
  { label: '/blocked', prompt: '/blocked' },
  { label: '/health',  prompt: '/health' },
  { label: '/recent',  prompt: '/recent' },
];

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

export function initChat() {
  store.subscribe('chat', () => {
    if (store.state.ui.activeTab === 'chat') renderChat();
    if (store.state.ui.chatFloating) {
      const floatEl = $('chat-floating-panel');
      if (floatEl) renderFloatingChat(floatEl);
    }
  });
}

export function renderChat() {
  const container = $('page-chat');
  if (!container) return;
  setHtml(container, buildChatUI('chat-full'));
  wireChat(container, 'chat-full');
  scrollToBottom(container.querySelector('.chat-messages'));
}

export function refreshChat() {
  // Chat is client-driven; nothing to refresh from API on page load.
  // Messages persist in store.state.chat.messages for the session.
}

/**
 * Render chat into an arbitrary target element (for floating panel).
 */
export function renderFloatingChat(targetEl) {
  if (!targetEl) return;
  setHtml(targetEl, buildChatUI('chat-float'));
  wireChat(targetEl, 'chat-float');
  scrollToBottom(targetEl.querySelector('.chat-messages'));
}

// ---------------------------------------------------------------------------
// Build the chat HTML
// ---------------------------------------------------------------------------

function buildChatUI(prefix) {
  const chat = store.state.chat;
  const messages = chat.messages || [];

  let messagesHtml = '';
  if (messages.length === 0) {
    messagesHtml = `<div class="chat-empty">
      <div class="chat-empty-icon">&#128172;</div>
      <div class="chat-empty-text">Ask Codero about your delivery pipeline</div>
    </div>`;
  } else {
    for (const msg of messages) {
      const cls = msg.role === 'user' ? 'user' : 'assistant';
      const label = msg.role === 'user' ? 'You' : 'Codero';
      messagesHtml += `<div class="chat-msg ${cls}">
        <div class="chat-msg-label">${esc(label)}</div>
        <div class="chat-msg-body">${formatMessage(msg.content)}</div>
      </div>`;
    }
  }

  const quickBtns = QUICK_QUERIES.map(q =>
    `<button class="btn btn-secondary btn-sm chat-suggestion" data-prompt="${esc(q.prompt)}">${esc(q.label)}</button>`
  ).join('');

  const streaming = chat.streaming;
  const disabled = streaming ? 'disabled' : '';
  const btnLabel = streaming ? 'Sending...' : 'Send';

  return `
    <div class="chat-container">
      <div class="chat-messages">${messagesHtml}</div>
      <div class="chat-suggestions">${quickBtns}</div>
      <div class="chat-input-row">
        <textarea id="${esc(prefix)}-input" class="chat-input" placeholder="Ask something..." rows="1" ${disabled}></textarea>
        <button id="${esc(prefix)}-send" class="btn btn-primary chat-send" ${disabled}>${btnLabel}</button>
      </div>
      <details class="chat-audit-section">
        <summary class="chat-audit-toggle">Query history</summary>
        <div class="chat-audit-list" id="${esc(prefix)}-audit-list">Loading...</div>
      </details>
    </div>`;
}

// ---------------------------------------------------------------------------
// Wire event handlers
// ---------------------------------------------------------------------------

function wireChat(container, prefix) {
  const input = container.querySelector(`#${prefix}-input`);
  const sendBtn = container.querySelector(`#${prefix}-send`);

  if (sendBtn) {
    sendBtn.addEventListener('click', () => sendMessage(input));
  }

  if (input) {
    input.addEventListener('keydown', e => {
      // Enter without shift sends
      if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        sendMessage(input);
      }
    });

    // Auto-resize textarea
    input.addEventListener('input', () => {
      input.style.height = 'auto';
      input.style.height = Math.min(input.scrollHeight, 120) + 'px';
    });
  }

  // Quick query buttons
  const quickBtns = container.querySelectorAll('.chat-suggestion');
  for (const btn of quickBtns) {
    btn.addEventListener('click', () => {
      const prompt = btn.dataset.prompt;
      if (input) input.value = prompt;
      sendMessage(input);
    });
  }

  // Lazy-load audit history only when the details section is opened
  const auditDetails = container.querySelector('.chat-audit-section');
  if (auditDetails) {
    auditDetails.addEventListener('toggle', () => {
      if (auditDetails.open) loadAuditHistory(prefix);
    });
  }
}

// ---------------------------------------------------------------------------
// Send message
// ---------------------------------------------------------------------------

async function sendMessage(inputEl) {
  if (!inputEl) return;
  if (store.state.chat.streaming) return;
  const prompt = inputEl.value.trim();
  if (!prompt) return;

  const chat = store.state.chat;

  // Append user message
  const userMsg = { role: 'user', content: prompt, ts: Date.now() };
  const updatedMessages = [...chat.messages, userMsg];
  store.set({ chat: { messages: updatedMessages, streaming: true, conversationId: chat.conversationId } });

  inputEl.value = '';
  inputEl.style.height = 'auto';

  try {
    const resp = await apiFetch('/api/v1/openclaw/query', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        prompt,
        conversation_id: chat.conversationId || null,
      }),
      raw: true,
    });

    // Handle non-2xx responses before parsing body
    if (!resp.ok) {
      let errorText;
      try { errorText = (await resp.json()).error || resp.statusText; } catch { errorText = resp.statusText || `HTTP ${resp.status}`; }
      throw new Error(errorText);
    }

    // Check if the response supports streaming
    if (resp.body && resp.headers.get('content-type')?.includes('text/event-stream')) {
      await handleStreamingResponse(resp);
    } else {
      // Standard JSON response
      const data = await resp.json();
      const convId = data.conversation_id || chat.conversationId;
      const assistantMsg = { role: 'assistant', content: data.response || data.reply || data.message || '', ts: Date.now() };
      const current = store.state.chat;
      store.set({
        chat: {
          messages: [...current.messages, assistantMsg],
          conversationId: convId,
          streaming: false,
        },
      });
    }
  } catch (err) {
    const msg = err.message.includes('502') ? 'OpenClaw unavailable — try again later' : err.message;
    const errMsg = { role: 'assistant', content: `Error: ${msg}`, ts: Date.now() };
    const current = store.state.chat;
    store.set({
      chat: {
        messages: [...current.messages, errMsg],
        streaming: false,
        conversationId: current.conversationId,
      },
    });
    toast('Chat request failed: ' + err.message, 'error');
  }
}

// ---------------------------------------------------------------------------
// Streaming response handler (TextDecoderStream)
// ---------------------------------------------------------------------------

async function handleStreamingResponse(resp) {
  const reader = resp.body.pipeThrough(new TextDecoderStream()).getReader();
  let accumulated = '';
  let buffer = '';

  // Add a placeholder assistant message
  const current = store.state.chat;
  const placeholderIdx = current.messages.length;
  const placeholder = { role: 'assistant', content: '', ts: Date.now() };
  store.set({
    chat: { messages: [...current.messages, placeholder], streaming: true, conversationId: current.conversationId },
  });

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      // Buffer chunks and process complete SSE frames (terminated by \n\n)
      buffer += value;
      const frames = buffer.split('\n\n');
      // Last element is incomplete — keep it in the buffer
      buffer = frames.pop() || '';

      for (const frame of frames) {
        for (const line of frame.split('\n')) {
          if (line.startsWith('data: ')) {
            const payload = line.slice(6);
            if (payload === '[DONE]') continue;
            try {
              const parsed = JSON.parse(payload);
              accumulated += parsed.delta || parsed.content || parsed.text || '';
            } catch {
              // Plain text chunk
              accumulated += payload;
            }
          }
        }
      }

      // Update the placeholder message in-place
      const chatNow = store.state.chat;
      const msgs = [...chatNow.messages];
      if (msgs[placeholderIdx]) {
        msgs[placeholderIdx] = { ...msgs[placeholderIdx], content: accumulated };
        store.set({ chat: { messages: msgs, streaming: true, conversationId: chatNow.conversationId } });
      }
    }
  } finally {
    reader.releaseLock();
    const final = store.state.chat;
    store.set({ chat: { ...final, streaming: false } });
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatMessage(content) {
  if (!content) return '';
  // Basic formatting: escape then convert markdown-ish patterns
  let safe = esc(content);
  // Code blocks: ```...```
  safe = safe.replace(/```([\s\S]*?)```/g, '<pre class="chat-code">$1</pre>');
  // Inline code: `...`
  safe = safe.replace(/`([^`]+)`/g, '<code>$1</code>');
  // Bold: **...**
  safe = safe.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
  // Newlines
  safe = safe.replace(/\n/g, '<br>');
  return safe;
}

// ---------------------------------------------------------------------------
// Query history (audit log)
// ---------------------------------------------------------------------------

function loadAuditHistory(prefix) {
  const listEl = document.getElementById(prefix + '-audit-list');
  if (!listEl) return;

  function setEmpty(msg) {
    listEl.textContent = '';
    const d = document.createElement('div');
    d.className = 'chat-audit-empty';
    d.textContent = msg;
    listEl.appendChild(d);
  }

  apiFetch('/api/v1/openclaw/audit?limit=20')
    .then(r => {
      if (!r.ok) throw new Error('HTTP ' + r.status);
      return r.json();
    })
    .then(data => {
      const entries = data.entries || [];
      if (entries.length === 0) { setEmpty('No queries yet'); return; }
      listEl.textContent = '';
      for (const e of entries) {
        const row = document.createElement('div');
        row.className = 'chat-audit-entry';
        const ts = document.createElement('span');
        ts.className = 'chat-audit-ts';
        ts.textContent = new Date(e.ts).toLocaleString();
        const prompt = document.createElement('span');
        prompt.className = 'chat-audit-prompt';
        prompt.textContent = e.prompt || '';
        const resp = document.createElement('span');
        resp.className = 'chat-audit-resp';
        const respText = (e.response || '').substring(0, 120);
        resp.textContent = respText + (e.response && e.response.length > 120 ? '…' : '');
        row.appendChild(ts);
        row.appendChild(prompt);
        row.appendChild(resp);
        listEl.appendChild(row);
      }
    })
    .catch(() => { setEmpty('Query history unavailable'); });
}

function scrollToBottom(el) {
  if (el) {
    requestAnimationFrame(() => { el.scrollTop = el.scrollHeight; });
  }
}
