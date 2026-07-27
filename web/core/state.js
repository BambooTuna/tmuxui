'use strict';

// ===== グローバル状態 =====
const state = {
  token: '',
  ws: null,
  wsStatus: 'disconnected',
  sessions: [],
  currentSession: null,
  currentPane: null,
  currentWindow: null,
  currentTopTab: (() => {
    try { return localStorage.getItem('tmuxui.topTab') || 'herdr'; } catch { return 'herdr'; }
  })(),
  pendingPermission: null,
  reconnectTimer: null,
  refreshing: false,
  claudeCommands: null,
  pinnedSessions: [],
  settingsReturnView: 'sessions',
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

// 結果を待たない fire-and-forget な apiFetch 呼び出し用。エラーを完全に握りつぶさず
// 最低限 console.warn に残す(UIトーストまでは出さず挙動は変えない)。
function apiFetchQuiet(path, options) {
  return apiFetch(path, options).catch(e => {
    console.warn(`apiFetch failed: ${path}`, e);
  });
}

// ===== 簡易 pub-sub (CustomEvent の薄いラッパー) =====
// transport/ws.js が dispatch する 'ws:*' イベントや、render層からの通知など、
// モジュール間を疎結合にするための共通の仕組み。
const bus = {
  on(type, handler) { document.addEventListener(type, handler); },
  off(type, handler) { document.removeEventListener(type, handler); },
  emit(type, detail) { document.dispatchEvent(new CustomEvent(type, { detail })); },
};

// ===== session/window/pane ルックアップ共通ヘルパー =====
// state.sessions.find(...) → windows.find(...) → panes.find(...) という
// チェーンが各所に重複していたのをここに集約する。
function findSession(sessionName) {
  return state.sessions.find(s => s.name === sessionName);
}

function findWindow(sessionName, windowIndex) {
  const session = findSession(sessionName);
  return session?.windows.find(w => w.index === windowIndex);
}

function findPane(sessionName, windowIndex, paneTarget) {
  const win = findWindow(sessionName, windowIndex);
  return win?.panes.find(p => p.target === paneTarget);
}
