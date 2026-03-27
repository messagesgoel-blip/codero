// sse.js — Server-Sent Events connection with event bus for partial updates.

import store from './store.js';
import { debounce } from './utils.js';

class EventBus extends EventTarget {
  emit(type, detail) {
    this.dispatchEvent(new CustomEvent(type, { detail }));
  }
  on(type, fn) {
    this.addEventListener(type, e => fn(e.detail));
  }
}

export const bus = new EventBus();

let eventSource = null;
let reconnectTimer = null;
let reconnectDelay = 1000;

export function connectSSE() {
  if (eventSource) return;
  try {
    eventSource = new EventSource('/api/v1/dashboard/events');

    eventSource.addEventListener('activity', e => {
      try {
        const event = JSON.parse(e.data);
        debouncedPatch(event);
        bus.emit('activity', event);
      } catch { /* ignore parse errors */ }
    });

    eventSource.onopen = () => {
      reconnectDelay = 1000;
      bus.emit('sse:connected');
    };

    eventSource.onerror = () => {
      eventSource.close();
      eventSource = null;
      bus.emit('sse:disconnected');
      scheduleReconnect();
    };
  } catch {
    scheduleReconnect();
  }
}

function scheduleReconnect() {
  if (reconnectTimer) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    reconnectDelay = Math.min(reconnectDelay * 2, 30000);
    connectSSE();
  }, reconnectDelay);
}

export function disconnectSSE() {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
}

const debouncedPatch = debounce(patchStoreFromEvent, 500);

function patchStoreFromEvent(event) {
  if (!event) return;
  const events = store.select('events');
  if (events && Array.isArray(events)) {
    const updated = [
      {
        seq: event.seq, repo: event.repo, branch: event.branch,
        type: event.event_type, payload: event.payload,
        createdAt: event.created_at, sessionId: event.session_id,
        assignmentId: event.assignment_id,
      },
      ...events,
    ].slice(0, 100);
    store.set({ events: updated });
  }
}
