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

// ===== Card Menu =====
let cardMenuTarget = null;

function openCardMenu(sessionName, anchorEl) {
  cardMenuTarget = sessionName;
  const menu = $('card-menu');
  const rect = anchorEl.getBoundingClientRect();
  menu.style.top = rect.bottom + 4 + 'px';
  menu.style.right = (window.innerWidth - rect.right) + 'px';
  menu.style.left = 'auto';
  menu.hidden = false;
}

function closeCardMenu() {
  $('card-menu').hidden = true;
  cardMenuTarget = null;
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

  $('view-detail').classList.remove('active');
  $('view-sessions').classList.add('active');
  loadSessions();
}

function showSessionDetail(sessionName) {
  state.currentSession = sessionName;
  $('session-title').textContent = sessionName;

  $('view-sessions').classList.remove('active');
  $('view-detail').classList.add('active');
  $('cmd-input').value = '';

  const session = state.sessions.find(s => s.name === sessionName);
  if (session && session.panes.length > 0) {
    renderPaneTabs();
    switchPane(session.panes[0].target);
  } else {
    renderPaneTabs();
    $('pane-content').textContent = 'ペインがありません';
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
    const card = document.createElement('div');
    card.className = 'session-card';
    card.innerHTML =
      `<span class="session-card-name">${esc(session.name)}</span>` +
      `<span class="session-card-meta">${session.panes.length} panes</span>` +
      `<button class="btn-card-menu" aria-label="メニュー">⋯</button>`;
    const menuBtn = card.querySelector('.btn-card-menu');
    menuBtn.addEventListener('click', e => {
      e.stopPropagation();
      openCardMenu(session.name, menuBtn);
    });
    card.addEventListener('click', () => showSessionDetail(session.name));
    el.appendChild(card);
  }
}

function renderPaneTabs() {
  const el = $('pane-tabs');
  el.innerHTML = '';

  const session = state.sessions.find(s => s.name === state.currentSession);
  if (!session) return;

  for (const pane of session.panes) {
    const paneId = pane.target.split(':')[1] || pane.target;
    const btn = document.createElement('button');
    btn.className = 'pane-tab';
    if (pane.target === state.currentPane) btn.classList.add('active');
    btn.textContent = `${paneId} ${pane.cmd}`;
    btn.dataset.target = pane.target;
    btn.addEventListener('click', () => switchPane(pane.target));
    el.appendChild(btn);
  }
  renderDrawerPanes();
}

function renderDrawerPanes() {
  const el = $('drawer-pane-list');
  el.innerHTML = '';

  const session = state.sessions.find(s => s.name === state.currentSession);
  if (!session) return;

  for (const pane of session.panes) {
    const paneId = pane.target.split(':')[1] || pane.target;
    const btn = document.createElement('button');
    btn.className = 'drawer-item';
    if (pane.target === state.currentPane) btn.classList.add('active');
    btn.innerHTML = `${esc(paneId)}<span class="drawer-item-cmd">${esc(pane.cmd)}</span>` +
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
    el.appendChild(btn);
  }
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
