// ===== Modal =====
function showModal({ message, input, inputValue, input2, input2Placeholder, okLabel, okDanger }) {
  return new Promise(resolve => {
    $('modal-message').textContent = message;
    const inp = $('modal-input');
    const inp2 = $('modal-input2');
    const okBtn = $('modal-ok');
    if (input) {
      inp.hidden = false;
      inp.value = inputValue || '';
    } else {
      inp.hidden = true;
    }
    if (input2) {
      inp2.hidden = false;
      inp2.value = '';
      inp2.placeholder = input2Placeholder || '';
    } else {
      inp2.hidden = true;
    }
    okBtn.textContent = okLabel || 'OK';
    okBtn.style.background = okDanger ? 'var(--error)' : 'var(--accent)';
    $('modal-overlay').hidden = false;
    if (input) inp.focus();

    function cleanup() {
      $('modal-overlay').hidden = true;
      okBtn.removeEventListener('click', onOk);
      $('modal-cancel').removeEventListener('click', onCancel);
      $('modal-overlay').removeEventListener('click', onOverlay);
      inp.removeEventListener('keydown', onKey);
      inp2.removeEventListener('keydown', onKey);
    }
    function onOk() {
      cleanup();
      if (input2) {
        resolve({ value: inp.value.trim(), value2: inp2.value.trim() });
      } else {
        resolve(input ? inp.value.trim() : true);
      }
    }
    function onCancel() {
      cleanup();
      resolve(null);
    }
    function onOverlay(e) {
      if (e.target === $('modal-overlay')) { cleanup(); resolve(null); }
    }
    function onKey(e) {
      if (e.key === 'Enter') { e.preventDefault(); onOk(); }
    }
    okBtn.addEventListener('click', onOk);
    $('modal-cancel').addEventListener('click', onCancel);
    $('modal-overlay').addEventListener('click', onOverlay);
    if (input) inp.addEventListener('keydown', onKey);
    if (input2) inp2.addEventListener('keydown', onKey);
  });
}

// ===== Session Management =====
// セッション名(プレフィックスなし、表示用)からAPI呼び出し用の"backend:name"識別子を組み立てる
function sessionFullId(name) {
  const session = state.sessions.find(s => s.name === name);
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

async function deleteWindow(sessionName, windowIndex) {
  const ok = await showModal({ message: 'このWindowを閉じますか？', okLabel: '閉じる', okDanger: true });
  if (!ok) return;
  await apiFetch(`/api/sessions/${encodeURIComponent(sessionFullId(sessionName))}/windows/${windowIndex}`, { method: 'DELETE' });
  await loadSessions();
  const session = state.sessions.find(s => s.name === sessionName);
  if (!session || session.windows.length === 0) {
    showSessionList();
  } else if (state.currentSession === sessionName && state.currentWindow === windowIndex) {
    showWindowDetail(sessionName, session.windows[0].index);
  }
}

async function renameWindow(sessionName, windowIndex) {
  const session = state.sessions.find(s => s.name === sessionName);
  const win = session?.windows.find(w => w.index === windowIndex);
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
  const session = state.sessions.find(s => s.name === state.currentSession);
  const win = session?.windows.find(w => w.index === state.currentWindow);
  if (!session || !win || win.panes.length === 0) {
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

function openCardMenu(sessionName) {
  cardMenuTarget = sessionName;
  const pinBtn = $('card-menu-pin');
  if (pinBtn) pinBtn.textContent = isPinned(sessionName) ? 'ピン解除' : 'ピン留め';
  $('card-menu-overlay').hidden = false;
}

function closeCardMenu() {
  $('card-menu-overlay').hidden = true;
  cardMenuTarget = null;
}

// ===== Window Menu =====
let windowMenuTarget = null;

function openWindowMenu(sessionName, windowIndex) {
  windowMenuTarget = { session: sessionName, index: windowIndex };
  $('window-menu-overlay').hidden = false;
}

function closeWindowMenu() {
  $('window-menu-overlay').hidden = true;
  windowMenuTarget = null;
}

// ===== Data Loading =====
async function loadSessions() {
  try {
    const data = await apiFetch('/api/sessions');
    state.sessions = data.sessions || [];
  } catch {}
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

async function loadPaneContent(target) {
  try {
    const data = await apiFetch(`/api/panes/${encodeURIComponent(target)}/content`);
    renderPaneContent(data.content || '');
  } catch (e) {
    renderPaneContent(`(取得失敗: ${e.message})`);
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

  const session = state.sessions.find(s => s.name === sessionName);
  const win = session?.windows.find(w => w.index === windowIndex);
  if (win && win.panes.length > 0) {
    renderPaneTabs();
    switchPane(win.panes[0].target);
  } else {
    renderPaneTabs();
    if (xtermEnabled()) {
      termReset();
    } else {
      $('pane-content').textContent = 'ペインがありません';
    }
  }
}

function updateBreadcrumb() {
  const session = state.sessions.find(s => s.name === state.currentSession);
  $('breadcrumb-session').textContent = session ? sessionDisplayName(session) : (state.currentSession || '');
  if (session && session.windows.length > 1) {
    const win = session.windows.find(w => w.index === state.currentWindow);
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

  if (xtermEnabled()) {
    termReset();
  } else {
    $('pane-content').textContent = '読み込み中...';
  }

  const size = getSubscribeSize();
  wsSend({ type: 'subscribe', target, ...(size || {}) });

  if (!xtermEnabled()) {
    loadPaneContent(target);
  }
}


function switchWindow(windowIndex) {
  if (state.currentWindow === windowIndex) return;
  if (state.currentPane) {
    wsSend({ type: 'unsubscribe', target: state.currentPane });
  }
  state.currentWindow = windowIndex;
  updateBreadcrumb();

  const session = state.sessions.find(s => s.name === state.currentSession);
  const win = session?.windows.find(w => w.index === windowIndex);
  if (win && win.panes.length > 0) {
    renderPaneTabs();
    switchPane(win.panes[0].target);
  }
}

function openWindowSheet() {
  const session = state.sessions.find(s => s.name === state.currentSession);
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

  $('window-sheet-overlay').hidden = false;
}

function closeWindowSheet() {
  $('window-sheet-overlay').hidden = true;
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
    state.currentPane = pane.target;
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
  apiFetch('/api/preferences', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ pinnedSessions: state.pinnedSessions }),
  }).catch(() => {});
}

function renderPaneTabs() {
  updateBreadcrumb();
  const el = $('pane-tabs');
  el.innerHTML = '';

  const session = state.sessions.find(s => s.name === state.currentSession);
  const win = session?.windows.find(w => w.index === state.currentWindow);
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

  const session = state.sessions.find(s => s.name === state.currentSession);
  const win = session?.windows.find(w => w.index === state.currentWindow);
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
        }).catch(() => {});
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

function openDrawer() {
  $('drawer-overlay').hidden = false;
  $('drawer').classList.add('open');
}

function closeDrawer() {
  $('drawer-overlay').hidden = true;
  $('drawer').classList.remove('open');
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

function renderPaneContent(content) {
  const el = $('pane-content');
  const atBottom = el.scrollHeight - el.scrollTop <= el.clientHeight + 60;
  el.innerHTML = ansiToHtml(content);
  if (atBottom) el.scrollTop = el.scrollHeight;
  stopRefreshing();
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

// ===== Top Tabs (init) =====
// #input-area/input.jsのbindEvents()には手を加えないため、セグメンテッドタブの
// イベント登録はここで自前で行う(settings.jsのbtn-settings系と同じパターン)。
document.addEventListener('DOMContentLoaded', () => {
  const wrap = $('top-tabs');
  if (!wrap) return;
  wrap.querySelectorAll('.top-tab-btn').forEach(btn => {
    btn.addEventListener('click', () => setTopTab(btn.dataset.tab));
  });
  renderTopTabs();
});

