'use strict';

// DOMContentLoaded で各 feature の init() を呼ぶだけの起動処理。
// 個々の DOM イベント登録は各 features/*.js の init 関数に閉じている。

async function applyStoredPreferences() {
  const prefs = await loadPreferences();
  if (prefs.theme) {
    applyTheme(prefs.theme, false);
  }
  if (Array.isArray(prefs.pinnedSessions)) {
    state.pinnedSessions = prefs.pinnedSessions.filter(n => typeof n === 'string');
  }
}

document.addEventListener('DOMContentLoaded', () => {
  state.token = new URLSearchParams(location.search).get('token') || '';

  // PWA: 初期履歴を上書きして「白い画面」への戻りを防止
  if (window.navigator.standalone) {
    history.replaceState({ tmuxui: true }, '');
    window.addEventListener('popstate', e => {
      if (!e.state || !e.state.tmuxui) {
        history.pushState({ tmuxui: true }, '');
      }
    });
  }

  initSettings();
  initSessions();
  initInputBar();
  initFiler();
  initSnippetSheet();
  initRemoteScrollButtons();

  connectWS();
  applyStoredPreferences();
  loadSessions();
});
