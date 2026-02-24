'use strict';

const state = {
  token: '',
  ws: null,
  wsStatus: 'disconnected',
  sessions: [],
  currentSession: null,
  currentPane: null,
  currentWindow: null,
  expandedSessions: {},
  pendingPermission: null,
  reconnectTimer: null,
  refreshing: false,
  autoApprove: false,
  claudeCommands: null,
};

const $ = id => document.getElementById(id);

function esc(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

async function apiFetch(path, options) {
  const sep = path.includes('?') ? '&' : '?';
  const url = `${path}${sep}token=${encodeURIComponent(state.token)}`;
  const res = await fetch(url, options);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  if (res.status === 204) return null;
  return res.json();
}

document.addEventListener('DOMContentLoaded', () => {
  state.token = new URLSearchParams(location.search).get('token') || '';
  bindEvents();
  connectWS();
  loadSessions();
});
