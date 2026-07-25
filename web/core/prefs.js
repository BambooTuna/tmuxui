'use strict';

// ===== /api/preferences の一元キャッシュ =====
// theme / pinnedSessions / autoUpdate の個別PUTと、複数箇所からの重複GETを
// ここに集約する。save() は既存キャッシュにマージしてからPUTするので、
// 1項目だけ送っても他のキーを消さない。

let prefsCache = null;
let prefsLoading = null;

async function loadPreferences(force) {
  if (!force && prefsCache) return prefsCache;
  if (!prefsLoading) {
    prefsLoading = apiFetch('/api/preferences')
      .then(data => { prefsCache = data || {}; return prefsCache; })
      .catch(e => {
        console.warn('preferences の取得に失敗しました', e);
        prefsCache = prefsCache || {};
        return prefsCache;
      })
      .finally(() => { prefsLoading = null; });
  }
  return prefsLoading;
}

function getPreferences() {
  return prefsCache || {};
}

async function savePreferences(patch) {
  prefsCache = Object.assign({}, prefsCache || {}, patch);
  try {
    await apiFetch('/api/preferences', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    });
  } catch (e) {
    console.warn('preferences の保存に失敗しました', e);
  }
}
