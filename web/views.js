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
  await apiFetch(`/api/sessions/${encodeURIComponent(name)}`, { method: 'DELETE' });
  loadSessions();
}

async function renameSession(oldName) {
  const newName = await showModal({ message: '新しい名前', input: true, inputValue: oldName, okLabel: '変更' });
  if (!newName || newName === oldName) return;
  await apiFetch(`/api/sessions/${encodeURIComponent(oldName)}/rename`, {
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
  await apiFetch(`/api/sessions/${encodeURIComponent(state.currentSession)}/windows`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: typeof name === 'string' ? name : '' }),
  });
  loadSessions();
}

async function deleteWindow(sessionName, windowIndex) {
  const ok = await showModal({ message: 'このWindowを閉じますか？', okLabel: '閉じる', okDanger: true });
  if (!ok) return;
  await apiFetch(`/api/sessions/${encodeURIComponent(sessionName)}/windows/${windowIndex}`, { method: 'DELETE' });
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
  await apiFetch(`/api/sessions/${encodeURIComponent(sessionName)}/windows/${windowIndex}/rename`, {
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
  renderSessionList();
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
    $('pane-content').textContent = 'ペインがありません';
  }
}

function updateBreadcrumb() {
  const session = state.sessions.find(s => s.name === state.currentSession);
  $('breadcrumb-session').textContent = state.currentSession || '';
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
  $('pane-content').textContent = '読み込み中...';

  const size = calcTermSize();
  wsSend({ type: 'subscribe', target, ...(size || {}) });
  loadPaneContent(target);
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

  $('window-sheet-header').textContent = session.name + ' の Windows';
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
    btn.textContent = `${win.index}:${win.name}`;
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

// ===== Rendering =====
function renderSessionList() {
  const el = $('session-list');
  el.innerHTML = '';

  if (!state.sessions.length) {
    const p = document.createElement('p');
    p.className = 'empty-state';
    p.textContent = 'セッションがありません';
    el.appendChild(p);
    return;
  }

  for (const session of state.sessions) {
    if (session.windows.length <= 1) {
      el.appendChild(createSessionCard(session));
    } else {
      el.appendChild(createSessionGroup(session));
    }
  }
}

function createSessionCard(session) {
  const win = session.windows[0];
  const paneCount = win ? win.panes.length : 0;
  const cmd = win && win.panes.length > 0 ? win.panes[0].cmd : '';
  const card = document.createElement('div');
  card.className = 'session-card';
  card.innerHTML =
    `<span class="session-card-name">${esc(session.name)}</span>` +
    `<span class="session-card-meta">${esc(cmd)} · ${paneCount} panes</span>` +
    `<button class="btn-card-menu" aria-label="メニュー">⋯</button>`;
  card.querySelector('.btn-card-menu').addEventListener('click', e => {
    e.stopPropagation();
    openCardMenu(session.name);
  });
  card.addEventListener('click', () => {
    if (win) showWindowDetail(session.name, win.index);
  });
  return card;
}

function createSessionGroup(session) {
  const isExpanded = !!state.expandedSessions[session.name];
  const group = document.createElement('div');
  group.className = 'session-group';

  const header = document.createElement('div');
  header.className = 'session-group-header';
  header.innerHTML =
    `<span class="toggle">${isExpanded ? '▾' : '▸'}</span>` +
    `<span class="session-card-name" style="flex:1">${esc(session.name)} ` +
    `<span class="session-card-meta">(${session.windows.length}w)</span></span>` +
    `<button class="btn-card-menu" aria-label="メニュー">⋯</button>`;

  header.querySelector('.btn-card-menu').addEventListener('click', e => {
    e.stopPropagation();
    openCardMenu(session.name);
  });
  header.addEventListener('click', () => {
    state.expandedSessions[session.name] = !state.expandedSessions[session.name];
    renderSessionList();
  });
  group.appendChild(header);

  if (isExpanded) {
    const body = document.createElement('div');
    body.className = 'session-group-body';
    for (const win of session.windows) {
      const card = document.createElement('div');
      card.className = 'window-card';
      const cmd = win.panes.length > 0 ? win.panes[0].cmd : '';
      card.innerHTML =
        `<span class="window-card-name">${esc(win.index + ':' + win.name)}</span>` +
        `<span class="window-card-meta">${esc(cmd)} · ${win.panes.length} panes</span>`;
      card.addEventListener('click', () => showWindowDetail(session.name, win.index));
      body.appendChild(card);
    }
    group.appendChild(body);
  }

  return group;
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
    btn.textContent = labels[i];
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
    btn.innerHTML = `${esc(labels[i])}` +
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
