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
    apiFetch('/api/preferences', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ theme: theme }),
    }).catch(() => {});
  }
}

function updateThemeButtons() {
  const current = getCachedTheme();
  const btns = document.querySelectorAll('.theme-switch-btn');
  for (const btn of btns) {
    btn.classList.toggle('active', btn.dataset.themeValue === current);
  }
}

// 描画エンジン切替(xterm.js / 従来)。切替はパイプラインごと入れ替わるためリロードで反映する
function updateRendererButtons() {
  const current = localStorage.getItem('tmuxuiXterm') === '0' ? 'classic' : 'xterm';
  for (const btn of document.querySelectorAll('.renderer-switch-btn')) {
    btn.classList.toggle('active', btn.dataset.rendererValue === current);
  }
}

function applyRenderer(value) {
  localStorage.setItem('tmuxuiXterm', value === 'classic' ? '0' : '1');
  location.reload();
}

function showSettings() {
  state.settingsReturnView = document.getElementById('view-detail').classList.contains('active') ? 'detail' : 'sessions';
  document.getElementById('view-detail').classList.remove('active');
  document.getElementById('view-sessions').classList.remove('active');
  document.getElementById('view-settings').classList.add('active');
  updateThemeButtons();
  updateRendererButtons();
}

// NOTE: state は app.js で定義されるグローバル変数（DOMContentLoaded 後に参照）
function hideSettings() {
  document.getElementById('view-settings').classList.remove('active');
  var returnTo = (typeof state !== 'undefined' && state.settingsReturnView) || 'sessions';
  document.getElementById(returnTo === 'detail' ? 'view-detail' : 'view-sessions').classList.add('active');
}

// ページロード時はキャッシュからテーマを即適用（フラッシュ防止）
applyTheme(getCachedTheme(), false);

document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('btn-settings').addEventListener('click', showSettings);
  document.getElementById('btn-settings-back').addEventListener('click', hideSettings);

  for (const btn of document.querySelectorAll('.theme-switch-btn')) {
    btn.addEventListener('click', () => applyTheme(btn.dataset.themeValue));
  }
  for (const btn of document.querySelectorAll('.renderer-switch-btn')) {
    btn.addEventListener('click', () => applyRenderer(btn.dataset.rendererValue));
  }
});
