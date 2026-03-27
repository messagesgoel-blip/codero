// renderers/chat.js — Chat page (full + floating mode) renderer.

import store from '../store.js';
import { apiFetch, apiPost } from '../api.js';
import { esc, $, setHtml, html, debounce } from '../utils.js';
import { glassCard, skeleton, toast } from '../components.js';

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
      const floatEl = $('floating-chat');
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
      const cls = msg.role === 'user' ? 'chat-msg-user' : 'chat-msg-assistant';
      const label = msg.role === 'user' ? 'You' : 'Codero';
      messagesHtml += `<div class="chat-msg ${cls}">
        <div class="chat-msg-label">${esc(label)}</div>
        <div class="chat-msg-body">${formatMessage(msg.content)}</div>
      </div>`;
    }
  }

  const quickBtns = QUICK_QUERIES.map(q =>
    `<button class="btn btn-secondary btn-sm quick-query" data-prompt="${esc(q.prompt)}">${esc(q.label)}</button>`
  ).join('');

  const streaming = chat.streaming;
  const disabled = streaming ? 'disabled' : '';
  const btnLabel = streaming ? 'Sending...' : 'Send';

  return `
    <div class="chat-container">
      <div class="chat-messages">${messagesHtml}</div>
      <div class="chat-quick-queries">${quickBtns}</div>
      <div class="chat-input-row">
        <textarea id="${esc(prefix)}-input" class="chat-input" placeholder="Ask something..." rows="1" ${disabled}></textarea>
        <button id="${esc(prefix)}-send" class="btn btn-primary chat-send" ${disabled}>${btnLabel}</button>
      </div>
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
      if (e.key === 'Enter' && !e.shiftKey) {
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
  const quickBtns = container.querySelectorAll('.quick-query');
  for (const btn of quickBtns) {
    btn.addEventListener('click', () => {
      const prompt = btn.dataset.prompt;
      if (input) input.value = prompt;
      sendMessage(input);
    });
  }
}

// ---------------------------------------------------------------------------
// Send message
// ---------------------------------------------------------------------------

async function sendMessage(inputEl) {
  if (!inputEl) return;
  const prompt = inputEl.value.trim();
  if (!prompt) return;

  const chat = store.state.chat;

  // Append user message
  const userMsg = { role: 'user', content: prompt, ts: Date.now() };
  const updatedMessages = [...chat.messages, userMsg];
  store.set({ chat: { messages: updatedMessages, streaming: true } });

  inputEl.value = '';
  inputEl.style.height = 'auto';

  try {
    const resp = await apiFetch('chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        prompt,
        conversation_id: chat.conversationId || null,
      }),
      raw: true,
    });

    // Check if the response supports streaming
    if (resp.body && resp.headers.get('content-type')?.includes('text/event-stream')) {
      await handleStreamingResponse(resp);
    } else {
      // Standard JSON response
      const data = await resp.json();
      const convId = data.conversation_id || chat.conversationId;
      const assistantMsg = { role: 'assistant', content: data.reply || data.message || '', ts: Date.now() };
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
    const errMsg = { role: 'assistant', content: `Error: ${err.message}`, ts: Date.now() };
    const current = store.state.chat;
    store.set({
      chat: {
        messages: [...current.messages, errMsg],
        streaming: false,
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

  // Add a placeholder assistant message
  const current = store.state.chat;
  const placeholderIdx = current.messages.length;
  const placeholder = { role: 'assistant', content: '', ts: Date.now() };
  store.set({
    chat: { messages: [...current.messages, placeholder], streaming: true },
  });

  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      // Parse SSE-style chunks: "data: ...\n\n"
      const lines = value.split('\n');
      for (const line of lines) {
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

      // Update the placeholder message in-place
      const msgs = [...store.state.chat.messages];
      if (msgs[placeholderIdx]) {
        msgs[placeholderIdx] = { ...msgs[placeholderIdx], content: accumulated };
        store.set({ chat: { messages: msgs, streaming: true } });
      }
    }
  } finally {
    reader.releaseLock();
    store.set({ chat: { streaming: false } });
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

function scrollToBottom(el) {
  if (el) {
    requestAnimationFrame(() => { el.scrollTop = el.scrollHeight; });
  }
}
