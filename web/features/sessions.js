'use strict';

// ===== Session Management =====
// セッション名(プレフィックスなし、表示用)からAPI呼び出し用の"backend:name"識別子を組み立てる
function sessionFullId(name) {
  const session = findSession(name);
  return session ? `${session.backend}:${name}` : name;
}

async function createSession() {
  const result = await showModal({
    message: 'セッション名',
    input: true,
    input2: true,
    input2Placeholder: 'ディレクトリ (任意)',
    okLabel: '作成',
  });
  if (!result || !result.value) return;
  await apiFetch('/api/sessions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: result.value, dir: result.value2 || undefined }),
  });
  loadSessions();
}

async function deleteSession(name) {
  const ok = await showModal({ message: `"${name}" を終了しますか？`, okLabel: '終了', okDanger: true });
  if (!ok) return;
  await apiFetch(`/api/sessions/${encodeURIComponent(sessionFullId(name))}`, { method: 'DELETE' });
  loadSessions();
}

async function renameSession(oldName) {
  const newName = await showModal({ message: '新しい名前', input: true, inputValue: oldName, okLabel: '変更' });
  if (!newName || newName === oldName) return;
  await apiFetch(`/api/sessions/${encodeURIComponent(sessionFullId(oldName))}/rename`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: newName }),
  });
  loadSessions();
}

// ===== Window Management =====
async function createWindow() {
  const name = await showModal({ message: 'Window名（任意）', input: true, okLabel: '作成' });
  if (name === null) return;
  await apiFetch(`/api/sessions/${encodeURIComponent(sessionFullId(state.currentSession))}/windows`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: typeof name === 'string' ? name : '' }),
  });
  loadSessions();
}

// セッション一覧のカードメニューから呼ぶ版。state.currentSession(詳細画面のコンテキスト)に
// 依存せず、指定されたsessionName直下にタブを作成する点がcreateWindow()と異なる。
async function addTabToSession(sessionName) {
  const name = await showModal({ message: 'タブ名（任意）', input: true, okLabel: '作成' });
  if (name === null) return;
  await apiFetch(`/api/sessions/${encodeURIComponent(sessionFullId(sessionName))}/windows`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: typeof name === 'string' ? name : '' }),
  });
  loadSessions();
}

// herdrセッション配下にworktreeを新規作成する(新しいworkspaceとして生える)。
// 作成後は2秒周期のトポロジーポーリングで自然に一覧へ反映されるが、体感を早めるため
// 一度loadSessions()も呼んでおく。
async function createWorktree(sessionName) {
  const result = await showModal({ message: 'ブランチ名', input: true, okLabel: '作成' });
  if (result === null) return;
  const branch = typeof result === 'string' ? result.trim() : '';
  if (!branch) {
    alert('ブランチ名を入力してください');
    return;
  }
  try {
    await apiFetch(`/api/sessions/${encodeURIComponent(sessionFullId(sessionName))}/worktrees`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ branch }),
    });
    loadSessions();
  } catch (e) {
    alert(`worktreeの作成に失敗しました: ${e.message}`);
  }
}

async function deleteWindow(sessionName, windowIndex) {
  const ok = await showModal({ message: 'このWindowを閉じますか？', okLabel: '閉じる', okDanger: true });
  if (!ok) return;
  await apiFetch(`/api/sessions/${encodeURIComponent(sessionFullId(sessionName))}/windows/${windowIndex}`, { method: 'DELETE' });
  await loadSessions();
  const session = findSession(sessionName);
  if (!session || session.windows.length === 0) {
    showSessionList();
  } else if (state.currentSession === sessionName && state.currentWindow === windowIndex) {
    showWindowDetail(sessionName, session.windows[0].index);
  }
}

async function renameWindow(sessionName, windowIndex) {
  const win = findWindow(sessionName, windowIndex);
  const newName = await showModal({ message: '新しいWindow名', input: true, inputValue: win?.name || '', okLabel: '変更' });
  if (!newName) return;
  await apiFetch(`/api/sessions/${encodeURIComponent(sessionFullId(sessionName))}/windows/${windowIndex}/rename`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: newName }),
  });
  loadSessions();
}

// ===== Pane Management =====
async function closePane(target) {
  const ok = await showModal({ message: 'このペインを閉じますか？', okLabel: '閉じる', okDanger: true });
  if (!ok) return;
  await apiFetch(`/api/panes/${encodeURIComponent(target)}`, { method: 'DELETE' });
  await loadSessions();
  const win = findWindow(state.currentSession, state.currentWindow);
  if (!win || win.panes.length === 0) {
    showSessionList();
  } else if (state.currentPane === target) {
    showWindowDetail(state.currentSession, state.currentWindow);
  } else {
    renderPaneTabs();
  }
}

async function addPane() {
  if (!state.currentPane) return;
  await apiFetch(`/api/panes/${encodeURIComponent(state.currentPane)}/split`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ horizontal: false }),
  });
  await loadSessions();
  if (state.currentSession && state.currentWindow !== null) {
    showWindowDetail(state.currentSession, state.currentWindow);
  }
}

// ===== Card Menu =====
let cardMenuTarget = null;

const cardMenuOverlay = createOverlay('card-menu-overlay', {
  onHide: () => { cardMenuTarget = null; },
});

function openCardMenu(sessionName) {
  cardMenuTarget = sessionName;
  const pinBtn = $('card-menu-pin');
  if (pinBtn) pinBtn.textContent = isPinned(sessionName) ? 'ピン解除' : 'ピン留め';
  // 「タブ追加」「worktreeを生やす」はherdrセッション限定(tmuxセッションには無い概念のため)。
  const isHerdr = findSession(sessionName)?.backend === 'herdr';
  const addTabBtn = $('card-menu-add-tab');
  if (addTabBtn) addTabBtn.hidden = !isHerdr;
  const addWorktreeBtn = $('card-menu-add-worktree');
  if (addWorktreeBtn) addWorktreeBtn.hidden = !isHerdr;
  cardMenuOverlay.show();
}

function closeCardMenu() {
  cardMenuOverlay.hide();
}

// ===== Window Menu =====
let windowMenuTarget = null;

const windowMenuOverlay = createOverlay('window-menu-overlay', {
  onHide: () => { windowMenuTarget = null; },
});

function openWindowMenu(sessionName, windowIndex) {
  windowMenuTarget = { session: sessionName, index: windowIndex };
  windowMenuOverlay.show();
}

function closeWindowMenu() {
  windowMenuOverlay.hide();
}

// ===== Data Loading =====
async function loadSessions() {
  try {
    const data = await apiFetch('/api/sessions');
    state.sessions = data.sessions || [];
  } catch (e) {
    console.warn('セッション一覧の取得に失敗しました', e);
  }
  autoSelectTopTab();
  renderSessionList();
}

// 現在選択中のバックエンドタブに該当セッションが1件もなく、もう片方に1件以上ある場合は
// 自動的にそちらへ寄せる(空のタブを見せ続けない)。localStorageには保存しない(一時的な救済のみ)。
function autoSelectTopTab() {
  const herdrCount = state.sessions.filter(s => s.backend === 'herdr').length;
  const tmuxCount = state.sessions.length - herdrCount;
  if (state.currentTopTab === 'herdr' && herdrCount === 0 && tmuxCount > 0) {
    state.currentTopTab = 'tmux';
  } else if (state.currentTopTab === 'tmux' && tmuxCount === 0 && herdrCount > 0) {
    state.currentTopTab = 'herdr';
  }
}

// ===== Views =====
function showSessionList() {
  if (state.currentPane) {
    wsSend({ type: 'unsubscribe', target: state.currentPane });
  }
  state.currentSession = null;
  state.currentPane = null;
  state.currentWindow = null;

  $('view-detail').classList.remove('active');
  $('view-sessions').classList.add('active');
  loadSessions();
}

function showWindowDetail(sessionName, windowIndex) {
  state.currentSession = sessionName;
  state.currentWindow = windowIndex;
  updateBreadcrumb();

  syncFilerOnSessionSwitch();
  $('view-sessions').classList.remove('active');
  $('view-detail').classList.add('active');
  $('cmd-input').value = '';

  const win = findWindow(sessionName, windowIndex);
  if (win && win.panes.length > 0) {
    renderPaneTabs();
    switchPane(win.panes[0].target);
  } else {
    renderPaneTabs();
    termReset();
  }
}

function updateBreadcrumb() {
  const session = findSession(state.currentSession);
  $('breadcrumb-session').textContent = session ? sessionDisplayName(session) : (state.currentSession || '');
  if (session && session.windows.length > 1) {
    const win = findWindow(state.currentSession, state.currentWindow);
    $('breadcrumb-sep').hidden = false;
    $('breadcrumb-window').hidden = false;
    $('breadcrumb-window').textContent = win ? win.name || `${win.index}` : '';
  } else {
    $('breadcrumb-sep').hidden = true;
    $('breadcrumb-window').hidden = true;
  }
}

function switchPane(target) {
  if (state.currentPane === target) return;

  if (state.currentPane) {
    wsSend({ type: 'unsubscribe', target: state.currentPane });
  }

  state.currentPane = target;
  updateActiveTab();

  termReset();

  const size = getSubscribeSize();
  wsSend(subscribePayload(target, size));
}


function switchWindow(windowIndex) {
  if (state.currentWindow === windowIndex) return;
  if (state.currentPane) {
    wsSend({ type: 'unsubscribe', target: state.currentPane });
  }
  state.currentWindow = windowIndex;
  updateBreadcrumb();

  const win = findWindow(state.currentSession, windowIndex);
  if (win && win.panes.length > 0) {
    renderPaneTabs();
    switchPane(win.panes[0].target);
  }
}

const windowSheetOverlay = createOverlay('window-sheet-overlay');

function openWindowSheet() {
  const session = findSession(state.currentSession);
  if (!session) return;

  $('window-sheet-header').textContent = sessionDisplayName(session) + ' の Windows';
  const el = $('window-sheet-list');
  el.innerHTML = '';

  for (const win of session.windows) {
    const row = document.createElement('div');
    row.style.cssText = 'display:flex;align-items:center;gap:6px;margin-bottom:6px';

    const btn = document.createElement('button');
    btn.className = 'window-sheet-item';
    btn.style.flex = '1';
    btn.style.margin = '0';
    if (win.index === state.currentWindow) btn.classList.add('active');
    btn.innerHTML = `${agentDotHtml(win.agent_status)}${esc(win.index + ':' + win.name)}`;
    btn.addEventListener('click', () => {
      closeWindowSheet();
      switchWindow(win.index);
    });

    const menuBtn = document.createElement('button');
    menuBtn.className = 'btn-card-menu';
    menuBtn.textContent = '⋯';
    menuBtn.addEventListener('click', e => {
      e.stopPropagation();
      closeWindowSheet();
      openWindowMenu(session.name, win.index);
    });

    row.appendChild(btn);
    row.appendChild(menuBtn);
    el.appendChild(row);
  }

  const addBtn = document.createElement('button');
  addBtn.className = 'window-sheet-item window-sheet-add';
  addBtn.textContent = '＋ 新規Window';
  addBtn.addEventListener('click', () => {
    closeWindowSheet();
    createWindow();
  });
  el.appendChild(addBtn);

  windowSheetOverlay.show();
}

function closeWindowSheet() {
  windowSheetOverlay.hide();
}

// ===== Pane Label =====
function paneLabel(pane) {
  if (pane.title && !pane.title.includes('.local') && pane.title !== pane.cmd) {
    return pane.title;
  }
  return pane.cmd;
}

function paneLabels(panes) {
  const labels = panes.map(p => paneLabel(p));
  const count = {};
  for (const l of labels) count[l] = (count[l] || 0) + 1;
  const idx = {};
  return labels.map(l => {
    if (count[l] > 1) {
      idx[l] = (idx[l] || 0) + 1;
      return `${l} #${idx[l]}`;
    }
    return l;
  });
}

// ===== Agent Status =====
// herdrバックエンドのagent_status(idle/working/blocked/done/unknown)の表示用ラベル。
// unknown(agentが紐付いていないペイン/ウィンドウ/セッション)はバッジを出さない。
const AGENT_STATUS_LABELS = { blocked: '⚠ 要対応', working: '作業中', idle: '待機', done: '完了' };

// herdrのdisplay_name(workspace.label)は未設定だと空/数字のみのことがあり、
// そのまま出すと読みにくい。その場合はUUIDそのまま(session.name)ではなく、
// 末尾6文字だけを使った"Workspace xxxxxx"にフォールバックする。
// tmuxセッション(backend!=='herdr')はdisplay_name自体を持たないため従来通りsession.nameを使う。
function sessionDisplayName(session) {
  if (!session) return '';
  const name = session.display_name;
  const meaningful = name && String(name).trim() !== '' && !/^\d+$/.test(String(name).trim());
  if (meaningful) return name;
  if (session.backend === 'herdr') {
    return `Workspace ${String(session.name).slice(-6)}`;
  }
  return session.name || '';
}

// 一覧の行/タブ用の省スペースなドット表示。
function agentDotHtml(status) {
  if (!AGENT_STATUS_LABELS[status]) return '';
  return `<span class="agent-dot agent-dot--${status}" title="${esc(AGENT_STATUS_LABELS[status])}"></span>`;
}

// セッション自身のagent_statusに加え、配下のwindow/paneにblockedがあれば
// セッションをblocked扱いに昇格させる。「返事待ち一覧」を見落とさないためのソート/フィルタ/
// バッジ表示の判定基準として共通で使う。
function effectiveAgentStatus(session) {
  if (session.agent_status === 'blocked') return 'blocked';
  for (const win of session.windows || []) {
    if (win.agent_status === 'blocked') return 'blocked';
    for (const pane of win.panes || []) {
      if (pane.agent_status === 'blocked') return 'blocked';
    }
  }
  return session.agent_status || '';
}

// ===== Rendering =====
function isPinned(name) {
  return state.pinnedSessions.includes(name);
}

// ピン留め > agent statusの重要度(blocked > working > idle > done > なし)の順。
// 同じ重要度内はArray.prototype.sort()の安定ソート特性により従来の並び順を維持する。
const AGENT_STATUS_SORT_PRIORITY = { blocked: 0, working: 1, idle: 2, done: 3 };
function agentStatusSortRank(status) {
  return status in AGENT_STATUS_SORT_PRIORITY ? AGENT_STATUS_SORT_PRIORITY[status] : 4;
}

function sortedSessions(sessions) {
  const list = sessions || state.sessions;
  const pinOrder = new Map();
  state.pinnedSessions.forEach((n, i) => pinOrder.set(n, i));
  const pinned = [];
  const others = [];
  for (const s of list) {
    if (pinOrder.has(s.name)) pinned.push(s);
    else others.push(s);
  }
  pinned.sort((a, b) => pinOrder.get(a.name) - pinOrder.get(b.name));
  others.sort((a, b) => agentStatusSortRank(effectiveAgentStatus(a)) - agentStatusSortRank(effectiveAgentStatus(b)));
  return [...pinned, ...others];
}

function emptyState(el, text) {
  const p = document.createElement('p');
  p.className = 'empty-state';
  p.textContent = text;
  el.appendChild(p);
}

// セッション配下の全paneのうちagent_status===blockedの件数。Spaceカードの⚠バッジ件数と
// Tabカードの左アクセントバー要否判定の両方に使う共通集計。
function countBlockedPanes(session) {
  let n = 0;
  for (const win of session.windows || []) {
    for (const pane of win.panes || []) {
      if (pane.agent_status === 'blocked') n++;
    }
  }
  return n;
}

// ===== Top Tabs (herdr / tmux) =====
function setTopTab(tab) {
  if (tab !== 'herdr' && tab !== 'tmux') return;
  if (state.currentTopTab === tab) return;
  state.currentTopTab = tab;
  try { localStorage.setItem('tmuxui.topTab', tab); } catch {}
  renderSessionList();
}

function renderTopTabs() {
  const wrap = $('top-tabs');
  if (!wrap) return;
  wrap.dataset.active = state.currentTopTab;
  for (const btn of wrap.querySelectorAll('.top-tab-btn')) {
    btn.classList.toggle('active', btn.dataset.tab === state.currentTopTab);
  }
}

function renderSessionList() {
  renderTopTabs();
  const el = $('session-list');
  el.innerHTML = '';

  if (!state.sessions.length) {
    emptyState(el, 'セッションがありません');
    return;
  }

  const list = state.sessions.filter(s => (state.currentTopTab === 'herdr') === (s.backend === 'herdr'));
  if (!list.length) {
    emptyState(el, state.currentTopTab === 'herdr' ? 'herdr セッションがありません' : 'tmux セッションがありません');
    return;
  }

  for (const session of sortedSessions(list)) {
    el.appendChild(createSpaceCard(session));
  }
}

// ===== Space Card (herdr worktree / tmuxセッション単位) =====
function createSpaceCard(session) {
  const blockedCount = countBlockedPanes(session);
  const card = document.createElement('div');
  card.className = 'space-card' +
    (isPinned(session.name) ? ' space-card--pinned' : '') +
    (blockedCount > 0 ? ' space-card--blocked' : '');

  const header = document.createElement('div');
  header.className = 'space-card-header';
  header.innerHTML =
    `<span class="space-card-title">` +
      agentDotHtml(effectiveAgentStatus(session)) +
      `<span class="space-card-name">${esc(sessionDisplayName(session))}</span>` +
      (isPinned(session.name) ? `<span class="space-pin-icon" aria-label="ピン留め">&#128204;</span>` : '') +
    `</span>` +
    (blockedCount > 0 ? `<span class="space-blocked-badge">&#9888; ${blockedCount}</span>` : '') +
    `<button type="button" class="btn-card-menu" aria-label="メニュー">⋯</button>`;
  header.querySelector('.btn-card-menu').addEventListener('click', e => {
    e.stopPropagation();
    openCardMenu(session.name);
  });
  card.appendChild(header);

  if (session.worktree_label) {
    const wt = document.createElement('div');
    wt.className = 'space-card-worktree';
    wt.textContent = session.worktree_label;
    card.appendChild(wt);
  }

  const tabs = document.createElement('div');
  tabs.className = 'space-card-tabs';
  for (const win of session.windows || []) {
    tabs.appendChild(createTabCard(session, win));
  }
  card.appendChild(tabs);

  return card;
}

// ===== Tab Card (window単位、常時開示) =====
// win.name(herdrのtab.Label)はユーザー未設定だと空文字/数字のみのことが多く、
// "17:2"のような意味不明な表示になってしまう。名前が実質的な意味を持つ場合だけ
// それを主役にし、そうでなければ"Tab {index}"を主役にしてindex由来のsubラベルは省略する。
function createTabCard(session, win) {
  const blocked = (win.panes || []).some(p => p.agent_status === 'blocked');
  const card = document.createElement('div');
  card.className = 'tab-card' + (blocked ? ' tab-card--blocked' : '');

  const rawName = String(win.name || '').trim();
  const hasMeaningfulName = rawName !== '' && !/^\d+$/.test(rawName) && rawName !== String(win.index);
  const titleText = hasMeaningfulName ? rawName : `Tab ${win.index}`;
  const subText = hasMeaningfulName ? `Tab ${win.index}` : '';

  const header = document.createElement('div');
  header.className = 'tab-card-header';
  header.innerHTML =
    `<span class="tab-card-title">${esc(titleText)}</span>` +
    (subText ? `<span class="tab-card-sub">${esc(subText)}</span>` : '') +
    (blocked ? `<span class="tab-card-warn" aria-hidden="true">&#9888;</span>` : '') +
    `<span class="tab-card-meta">${(win.panes || []).length} panes</span>`;
  header.addEventListener('click', () => showWindowDetail(session.name, win.index));
  card.appendChild(header);

  const body = document.createElement('div');
  body.className = 'tab-card-body';
  for (const pane of win.panes || []) {
    body.appendChild(createPaneRow(session, win, pane));
  }
  card.appendChild(body);

  return card;
}

// ===== Pane Row =====
function createPaneRow(session, win, pane) {
  const blocked = pane.agent_status === 'blocked';
  const label = AGENT_STATUS_LABELS[pane.agent_status] || '';
  const row = document.createElement('div');
  row.className = 'pane-row' + (blocked ? ' pane-row--blocked' : '');
  row.innerHTML =
    agentDotHtml(pane.agent_status) +
    `<span class="pane-row-cmd">${esc(paneLabel(pane))}</span>` +
    (label ? `<span class="pane-row-status">${esc(label)}</span>` : '');
  row.addEventListener('click', () => {
    showWindowDetail(session.name, win.index);
  });
  return row;
}

function togglePinSession(name) {
  if (!name) return;
  if (isPinned(name)) {
    state.pinnedSessions = state.pinnedSessions.filter(n => n !== name);
  } else {
    state.pinnedSessions = [...state.pinnedSessions, name];
  }
  renderSessionList();
  savePreferences({ pinnedSessions: state.pinnedSessions });
}

function renderPaneTabs() {
  updateBreadcrumb();
  const el = $('pane-tabs');
  el.innerHTML = '';

  const win = findWindow(state.currentSession, state.currentWindow);
  if (!win) return;

  const labels = paneLabels(win.panes);
  win.panes.forEach((pane, i) => {
    const btn = document.createElement('button');
    btn.className = 'pane-tab';
    if (pane.target === state.currentPane) btn.classList.add('active');
    btn.innerHTML = `${agentDotHtml(pane.agent_status)}${esc(labels[i])}`;
    btn.dataset.target = pane.target;
    btn.addEventListener('click', () => switchPane(pane.target));
    el.appendChild(btn);
  });
  renderDrawerPanes();
}

function renderDrawerPanes() {
  const el = $('drawer-pane-list');
  el.innerHTML = '';

  const win = findWindow(state.currentSession, state.currentWindow);
  if (!win) return;

  const labels = paneLabels(win.panes);
  win.panes.forEach((pane, i) => {
    const row = document.createElement('div');
    row.className = 'drawer-item-row';

    const btn = document.createElement('button');
    btn.className = 'drawer-item';
    if (pane.target === state.currentPane) btn.classList.add('active');
    btn.innerHTML = `${agentDotHtml(pane.agent_status)}${esc(labels[i])}` +
      (pane.path ? `<span class="drawer-item-path" data-path="${esc(pane.path)}">${esc(pane.path)}</span>` : '');
    btn.dataset.target = pane.target;
    btn.addEventListener('click', () => {
      switchPane(pane.target);
      closeDrawer();
    });
    const pathEl = btn.querySelector('.drawer-item-path');
    if (pathEl) {
      pathEl.addEventListener('click', e => {
        e.stopPropagation();
        navigator.clipboard.writeText(pathEl.dataset.path).then(() => {
          const orig = pathEl.textContent;
          pathEl.textContent = 'コピー済';
          setTimeout(() => { pathEl.textContent = orig; }, 1000);
        }).catch(e => console.warn('パスのコピーに失敗しました', e));
      });
    }
    row.appendChild(btn);

    const closeBtn = document.createElement('button');
    closeBtn.className = 'drawer-item-close';
    closeBtn.textContent = '×';
    closeBtn.ariaLabel = 'ペインを閉じる';
    closeBtn.addEventListener('click', e => {
      e.stopPropagation();
      closeDrawer();
      closePane(pane.target);
    });
    row.appendChild(closeBtn);

    el.appendChild(row);
  });

  const addBtn = document.createElement('button');
  addBtn.className = 'drawer-item drawer-item-add';
  addBtn.textContent = '＋ ペイン追加';
  addBtn.addEventListener('click', () => {
    closeDrawer();
    addPane();
  });
  el.appendChild(addBtn);
}

const paneDrawer = createDrawer('drawer-overlay', 'drawer');

function openDrawer() {
  paneDrawer.show();
}

function closeDrawer() {
  paneDrawer.hide();
}

function updateActiveTab() {
  const tabs = $('pane-tabs').querySelectorAll('.pane-tab');
  for (const tab of tabs) {
    tab.classList.toggle('active', tab.dataset.target === state.currentPane);
  }
  const items = $('drawer-pane-list').querySelectorAll('.drawer-item');
  for (const item of items) {
    item.classList.toggle('active', item.dataset.target === state.currentPane);
  }
}

// ===== Refresh =====
function startRefreshing() {
  state.refreshing = true;
  $('btn-refresh').classList.add('spinning');
}

function stopRefreshing() {
  state.refreshing = false;
  $('btn-refresh').classList.remove('spinning');
}

// ===== Permission =====
function showPermissionBanner(msg) {
  if (state.autoApprove && msg.target) {
    sendKeys(msg.target, 'Enter');
    return;
  }
  state.pendingPermission = msg;
  $('permission-prompt-text').textContent = msg.prompt || '権限許可が必要です';
  $('permission-banner').hidden = false;
}

function hidePermissionBanner() {
  $('permission-banner').hidden = true;
  state.pendingPermission = null;
}

// ===== transport/ws.js からの通知 =====
bus.on('ws:pane_list', e => {
  if ($('view-sessions').classList.contains('active') && e.detail.changed) {
    renderSessionList();
  }
  if (state.currentSession) {
    renderPaneTabs();
  }
});

bus.on('ws:permission', e => showPermissionBanner(e.detail));

bus.on('render:pane-snapshot-applied', stopRefreshing);

// ===== 初期化 =====
function initSessions() {
  // Back
  $('btn-back').addEventListener('click', showSessionList);

  // Refresh
  $('btn-refresh').addEventListener('click', () => {
    if (!state.currentPane || state.refreshing) return;
    startRefreshing();
    // subscribeを再送するとバックエンド側がフルリシンク(snapshot再送)してくれる
    const size = getSubscribeSize();
    wsSend(subscribePayload(state.currentPane, size));
    setTimeout(stopRefreshing, 5000);
  });

  // New session
  $('btn-new-session').addEventListener('click', createSession);

  // Card menu actions
  $('card-menu-pin').addEventListener('click', () => {
    if (cardMenuTarget) togglePinSession(cardMenuTarget);
    closeCardMenu();
  });
  $('card-menu-rename').addEventListener('click', () => {
    if (cardMenuTarget) renameSession(cardMenuTarget);
    closeCardMenu();
  });
  $('card-menu-add-tab').addEventListener('click', () => {
    if (cardMenuTarget) addTabToSession(cardMenuTarget);
    closeCardMenu();
  });
  $('card-menu-add-worktree').addEventListener('click', () => {
    if (cardMenuTarget) createWorktree(cardMenuTarget);
    closeCardMenu();
  });
  $('card-menu-delete').addEventListener('click', () => {
    if (cardMenuTarget) deleteSession(cardMenuTarget);
    closeCardMenu();
  });
  $('card-menu-cancel').addEventListener('click', closeCardMenu);

  // Drawer
  $('btn-menu').addEventListener('click', openDrawer);

  // Auto approve toggle
  $('btn-auto-approve').addEventListener('click', () => {
    state.autoApprove = !state.autoApprove;
    $('btn-auto-approve').classList.toggle('active', state.autoApprove);
  });

  // Permission dismiss
  $('btn-perm-dismiss').addEventListener('click', hidePermissionBanner);

  // Window sheet
  $('breadcrumb-window').addEventListener('click', openWindowSheet);

  // Window menu
  $('window-menu-cancel').addEventListener('click', closeWindowMenu);
  $('window-menu-rename').addEventListener('click', () => {
    if (windowMenuTarget) renameWindow(windowMenuTarget.session, windowMenuTarget.index);
    closeWindowMenu();
  });
  $('window-menu-add').addEventListener('click', () => {
    closeWindowMenu();
    createWindow();
  });
  $('window-menu-close').addEventListener('click', () => {
    if (windowMenuTarget) deleteWindow(windowMenuTarget.session, windowMenuTarget.index);
    closeWindowMenu();
  });

  // Top Tabs
  const wrap = $('top-tabs');
  if (wrap) {
    wrap.querySelectorAll('.top-tab-btn').forEach(btn => {
      btn.addEventListener('click', () => setTopTab(btn.dataset.tab));
    });
    renderTopTabs();
  }

  // Android hardware back
  window.addEventListener('popstate', () => {
    if ($('drawer').classList.contains('open')) {
      closeDrawer();
      history.pushState(null, '');
      return;
    }
    if ($('view-detail').classList.contains('active')) {
      showSessionList();
      history.pushState(null, '');
    }
  });
  history.pushState(null, '');
}
