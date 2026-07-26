'use strict';

// テーマのフラッシュ防止: localStorage をキャッシュとして使用（API が正）
const THEME_CACHE_KEY = 'tmuxui-theme-cache';

function getCachedTheme() {
  return localStorage.getItem(THEME_CACHE_KEY) || 'dark';
}

function applyTheme(theme, save) {
  if (theme === 'dark') {
    document.documentElement.removeAttribute('data-theme');
  } else {
    document.documentElement.setAttribute('data-theme', theme);
  }
  localStorage.setItem(THEME_CACHE_KEY, theme);

  // PWA/モバイルブラウザのステータスバー色を同期
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta) {
    meta.setAttribute('content', theme === 'pastel' ? '#faf6f0' : '#1a1a2e');
  }

  updateThemeButtons();

  if (save !== false) {
    savePreferences({ theme: theme });
  }
}

function updateThemeButtons() {
  const current = getCachedTheme();
  const btns = document.querySelectorAll('.theme-switch-btn');
  for (const btn of btns) {
    btn.classList.toggle('active', btn.dataset.themeValue === current);
  }
}

function showSettings() {
  state.settingsReturnView = document.getElementById('view-detail').classList.contains('active') ? 'detail' : 'sessions';
  document.getElementById('view-detail').classList.remove('active');
  document.getElementById('view-sessions').classList.remove('active');
  document.getElementById('view-settings').classList.add('active');
  updateThemeButtons();
  loadUpdateStatus();
}

// ===== アップデート =====
// currentUpdateStatus / renderUpdateSection は transport/ws.js からの push 再描画でも使うため
// グローバルに置く(bus経由の 'ws:update_status' イベントで呼ばれる)。
let currentUpdateStatus = null;
let currentAutoUpdatePrefs = { enabled: true, autoApply: false, intervalHours: 24 };

async function loadUpdateStatus() {
  const prefs = await loadPreferences();
  if (prefs.autoUpdate && typeof prefs.autoUpdate === 'object') {
    currentAutoUpdatePrefs = Object.assign({}, currentAutoUpdatePrefs, prefs.autoUpdate);
  }
  updateAutoUpdateInputs();

  try {
    const status = await apiFetch('/api/update/status');
    renderUpdateSection(status);
  } catch (e) {
    console.warn('アップデート状況の取得に失敗しました', e);
  }

  loadAvailableReleases();
}

async function checkUpdateNow() {
  const btn = $('update-check-btn');
  if (!btn || btn.disabled) return;
  btn.disabled = true;
  const originalText = btn.textContent;
  btn.textContent = '確認中…';
  try {
    const status = await apiFetch('/api/update/check', { method: 'POST' });
    renderUpdateSection(status);
  } catch {
    showUpdateError('アップデートの確認に失敗しました');
  } finally {
    btn.disabled = false;
    btn.textContent = originalText;
  }
}

async function applyUpdateNow() {
  const btn = $('update-apply-btn');
  if (!btn || btn.disabled) return;
  btn.disabled = true;
  btn.dataset.applying = '1';
  btn.textContent = '適用中…';
  hideUpdateError();
  try {
    await apiFetch('/api/update/apply', { method: 'POST' });
    // レスポンス後、自プロセスは syscall.Exec で再起動する。WebSocket は自動再接続する
    // (transport/ws.js)ので、ここではメッセージ表示のみ行い、ボタンは無効のままにしておく
    btn.textContent = 'アップデート完了・再接続中…';
  } catch {
    showUpdateError('アップデートの適用に失敗しました');
    delete btn.dataset.applying;
    btn.disabled = !(currentUpdateStatus && currentUpdateStatus.hasUpdate);
    btn.textContent = '今すぐアップデート';
  }
}

function renderUpdateSection(status) {
  currentUpdateStatus = status;
  const currentEl = $('update-current');
  const latestEl = $('update-latest');
  const checkedEl = $('update-last-checked');
  const applyBtn = $('update-apply-btn');

  if (currentEl) currentEl.textContent = status.current || '—';
  if (latestEl) latestEl.textContent = status.latest || '—';
  if (checkedEl) {
    checkedEl.textContent = status.lastCheckedAt && status.lastCheckedAt.slice(0, 4) !== '0001'
      ? new Date(status.lastCheckedAt).toLocaleString('ja-JP')
      : '—';
  }
  // 適用中(dataset.applying)にWS pushが来ても「適用中…」表示を上書きしない
  if (applyBtn && !applyBtn.dataset.applying) {
    applyBtn.disabled = !status.hasUpdate || !!status.applied;
    applyBtn.textContent = status.applied ? '適用済み・再起動待ち' : '今すぐアップデート';
  }

  if (status.lastError) {
    showUpdateError(status.lastError);
  } else {
    hideUpdateError();
  }
}

function showUpdateError(msg) {
  const el = $('update-error');
  if (!el) return;
  el.textContent = msg;
  el.hidden = false;
}

function hideUpdateError() {
  const el = $('update-error');
  if (!el) return;
  el.hidden = true;
  el.textContent = '';
}

function updateAutoUpdateInputs() {
  const onBtn = $('update-auto-on');
  const offBtn = $('update-auto-off');
  if (onBtn && offBtn) {
    onBtn.classList.toggle('active', !!currentAutoUpdatePrefs.enabled);
    offBtn.classList.toggle('active', !currentAutoUpdatePrefs.enabled);
  }
  const intervalEl = $('update-interval-hours');
  // 入力中に上書きしない
  if (intervalEl && document.activeElement !== intervalEl) {
    intervalEl.value = currentAutoUpdatePrefs.intervalHours || 24;
  }
}

function saveAutoUpdatePrefs() {
  savePreferences({ autoUpdate: currentAutoUpdatePrefs });
}

function setAutoUpdateEnabled(enabled) {
  currentAutoUpdatePrefs = Object.assign({}, currentAutoUpdatePrefs, { enabled });
  updateAutoUpdateInputs();
  saveAutoUpdatePrefs();
}

function setAutoUpdateInterval(hours) {
  const clamped = Math.min(168, Math.max(1, Math.round(hours) || 24));
  currentAutoUpdatePrefs = Object.assign({}, currentAutoUpdatePrefs, { intervalHours: clamped });
  updateAutoUpdateInputs();
  saveAutoUpdatePrefs();
}

// ===== バージョン指定切替 =====
let availableReleases = [];

async function loadAvailableReleases() {
  const select = $('update-version-select');
  const applyBtn = $('update-version-apply-btn');
  if (!select) return;
  select.innerHTML = '<option value="">読み込み中…</option>';
  if (applyBtn) applyBtn.disabled = true;
  try {
    const data = await apiFetch('/api/update/releases');
    availableReleases = data.releases || [];
    if (availableReleases.length === 0) {
      select.innerHTML = '<option value="">利用可能なバージョンがありません</option>';
      return;
    }
    select.innerHTML = '';
    for (const r of availableReleases) {
      const opt = document.createElement('option');
      opt.value = r.version;
      const label = r.version + (r.prerelease ? ' (pre)' : '');
      opt.textContent = label;
      if (currentUpdateStatus && ('v' + currentUpdateStatus.current) === r.version) {
        opt.textContent = label + ' — 現在';
        opt.selected = true;
      }
      select.appendChild(opt);
    }
    if (applyBtn) applyBtn.disabled = false;
  } catch (e) {
    select.innerHTML = '<option value="">取得に失敗しました</option>';
  }
}

async function applyVersionSwitch() {
  const select = $('update-version-select');
  const btn = $('update-version-apply-btn');
  if (!select || !btn || btn.disabled) return;
  const v = select.value;
  if (!v) return;
  if (!window.confirm(`${v} に切り替えます。自動アップデートは OFF になります。よろしいですか？`)) return;
  btn.disabled = true;
  btn.dataset.applying = '1';
  const originalText = btn.textContent;
  btn.textContent = '切替中…';
  hideUpdateError();
  try {
    await apiFetch('/api/update/apply', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ version: v }),
    });
    btn.textContent = '切替完了・再接続中…';
  } catch (e) {
    showUpdateError('バージョン切替に失敗しました');
    delete btn.dataset.applying;
    btn.disabled = false;
    btn.textContent = originalText;
  }
}

function hideSettings() {
  document.getElementById('view-settings').classList.remove('active');
  var returnTo = state.settingsReturnView || 'sessions';
  document.getElementById(returnTo === 'detail' ? 'view-detail' : 'view-sessions').classList.add('active');
}

// ページロード時はキャッシュからテーマを即適用（フラッシュ防止）。DOMContentLoaded を
// 待たずスクリプト読み込み時点で実行する必要があるため、initSettings() には含めない。
applyTheme(getCachedTheme(), false);

// ===== transport/ws.js からの通知 =====
bus.on('ws:update_status', e => renderUpdateSection(e.detail.updateStatus));

// ===== 初期化 =====
function initSettings() {
  document.getElementById('btn-settings').addEventListener('click', showSettings);
  document.getElementById('btn-settings-back').addEventListener('click', hideSettings);

  for (const btn of document.querySelectorAll('.theme-switch-btn')) {
    btn.addEventListener('click', () => applyTheme(btn.dataset.themeValue));
  }
  document.getElementById('update-check-btn').addEventListener('click', checkUpdateNow);
  document.getElementById('update-apply-btn').addEventListener('click', applyUpdateNow);
  document.getElementById('update-auto-on').addEventListener('click', () => setAutoUpdateEnabled(true));
  document.getElementById('update-auto-off').addEventListener('click', () => setAutoUpdateEnabled(false));
  const intervalEl = document.getElementById('update-interval-hours');
  intervalEl.addEventListener('change', () => setAutoUpdateInterval(parseInt(intervalEl.value, 10)));
  intervalEl.addEventListener('blur', () => setAutoUpdateInterval(parseInt(intervalEl.value, 10)));
  const versionRefreshBtn = document.getElementById('update-version-refresh-btn');
  const versionApplyBtn = document.getElementById('update-version-apply-btn');
  if (versionRefreshBtn) versionRefreshBtn.addEventListener('click', loadAvailableReleases);
  if (versionApplyBtn) versionApplyBtn.addEventListener('click', applyVersionSwitch);
}
