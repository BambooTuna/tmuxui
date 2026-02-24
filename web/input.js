// ===== Claude Code Commands =====
const DEFAULT_CLAUDE_COMMANDS = [
  { cmd: '/bug',           desc: 'バグを報告' },
  { cmd: '/clear',         desc: '会話履歴をクリア' },
  { cmd: '/compact',       desc: '会話をコンパクト化' },
  { cmd: '/config',        desc: '設定を開く' },
  { cmd: '/context',       desc: 'コンテキスト使用状況を表示' },
  { cmd: '/copy',          desc: '最後の応答をコピー' },
  { cmd: '/cost',          desc: 'トークン使用統計を表示' },
  { cmd: '/debug',         desc: 'デバッグログ確認' },
  { cmd: '/desktop',       desc: 'Desktopアプリに引き継ぐ' },
  { cmd: '/doctor',        desc: 'ヘルスチェック' },
  { cmd: '/exit',          desc: 'REPL を終了' },
  { cmd: '/export',        desc: '会話をエクスポート' },
  { cmd: '/fast',          desc: 'Fastモード切替' },
  { cmd: '/help',          desc: 'ヘルプを表示' },
  { cmd: '/init',          desc: 'CLAUDE.md で初期化' },
  { cmd: '/login',         desc: 'ログイン' },
  { cmd: '/logout',        desc: 'ログアウト' },
  { cmd: '/mcp',           desc: 'MCP サーバー接続管理' },
  { cmd: '/memory',        desc: 'CLAUDE.md メモリ編集' },
  { cmd: '/model',         desc: 'AIモデルを変更' },
  { cmd: '/permissions',   desc: 'パーミッション設定' },
  { cmd: '/plan',          desc: 'プランモードに入る' },
  { cmd: '/pr-comments',   desc: 'PRコメントを表示' },
  { cmd: '/rename',        desc: 'セッション名を変更' },
  { cmd: '/resume',        desc: '会話を再開' },
  { cmd: '/review',        desc: 'コードレビュー' },
  { cmd: '/rewind',        desc: '会話を巻き戻し' },
  { cmd: '/stats',         desc: '使用状況を表示' },
  { cmd: '/status',        desc: 'バージョン・モデル情報' },
  { cmd: '/statusline',    desc: 'ステータスライン設定' },
  { cmd: '/tasks',         desc: 'バックグラウンドタスク管理' },
  { cmd: '/teleport',      desc: 'リモートセッション再開' },
  { cmd: '/terminal-setup', desc: 'ターミナル設定' },
  { cmd: '/theme',         desc: 'カラーテーマを変更' },
  { cmd: '/todos',         desc: 'TODO リスト表示' },
  { cmd: '/usage',         desc: 'プラン使用制限を表示' },
  { cmd: '/vim',           desc: 'Vimモード切替' },
];

async function fetchClaudeCommands() {
  try {
    const data = await apiFetch('/api/claude/commands');
    if (data.commands && data.commands.length > 0) {
      state.claudeCommands = data.commands.map(c => ({
        cmd: '/' + c.name, desc: c.description,
      }));
    }
  } catch {}
}

function getClaudeCommands() {
  return state.claudeCommands || DEFAULT_CLAUDE_COMMANDS;
}

function populateCommandSheet() {
  const cmdList = $('sheet-commands').querySelector('.sheet-cmd-list');
  cmdList.innerHTML = '';
  for (const item of getClaudeCommands()) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'sheet-cmd';
    btn.dataset.cmd = item.cmd;
    btn.textContent = item.cmd;
    btn.addEventListener('click', () => {
      $('cmd-input').value = item.cmd;
      $('cmd-input').focus();
      $('keys-sheet-overlay').hidden = true;
    });
    cmdList.appendChild(btn);
  }
}

// ===== Events =====
function bindEvents() {
  // Back
  $('btn-back').addEventListener('click', showSessionList);

  // Refresh
  $('btn-refresh').addEventListener('click', () => {
    if (!state.currentPane || state.refreshing) return;
    startRefreshing();
    wsSend({ type: 'refresh', target: state.currentPane });
    if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
      loadPaneContent(state.currentPane);
    }
    setTimeout(stopRefreshing, 5000);
  });

  // Cursor keys
  $('btn-up').addEventListener('click', () => {
    if (state.currentPane) sendKeys(state.currentPane, 'Up');
  });
  $('btn-down').addEventListener('click', () => {
    if (state.currentPane) sendKeys(state.currentPane, 'Down');
  });

  // Send text
  const doSend = () => {
    if (!state.currentPane) return;
    const text = $('cmd-input').value.trim();
    if (text) {
      sendKeys(state.currentPane, text + '\n');
      $('cmd-input').value = '';
    } else {
      sendKeys(state.currentPane, 'Enter');
    }
  };
  $('cmd-form').addEventListener('submit', e => {
    e.preventDefault();
    doSend();
  });

  // Enter button
  $('btn-enter').addEventListener('click', () => {
    if (state.currentPane) sendKeys(state.currentPane, 'Enter');
  });

  // New session
  $('btn-new-session').addEventListener('click', createSession);

  // Card menu actions
  $('card-menu-rename').addEventListener('click', () => {
    if (cardMenuTarget) renameSession(cardMenuTarget);
    closeCardMenu();
  });
  $('card-menu-delete').addEventListener('click', () => {
    if (cardMenuTarget) deleteSession(cardMenuTarget);
    closeCardMenu();
  });

  // Keys sheet
  function openSheet() {
    $('sheet-categories').hidden = false;
    $('sheet-ctrl').hidden = true;
    $('sheet-commands').hidden = true;
    $('keys-sheet-overlay').hidden = false;
  }
  function closeSheet() {
    $('keys-sheet-overlay').hidden = true;
  }
  $('btn-keys-more').addEventListener('click', openSheet);
  $('keys-sheet-overlay').addEventListener('click', e => {
    if (e.target === $('keys-sheet-overlay')) closeSheet();
  });
  for (const btn of $('keys-sheet-overlay').querySelectorAll('.sheet-category')) {
    btn.addEventListener('click', () => {
      $('sheet-categories').hidden = true;
      $('sheet-' + btn.dataset.submenu).hidden = false;
    });
  }
  for (const btn of $('keys-sheet-overlay').querySelectorAll('.sheet-back')) {
    btn.addEventListener('click', () => {
      btn.closest('.sheet-sub').hidden = true;
      $('sheet-categories').hidden = false;
    });
  }
  for (const btn of $('keys-sheet-overlay').querySelectorAll('.sheet-key')) {
    btn.addEventListener('click', () => {
      if (state.currentPane) sendKeys(state.currentPane, btn.dataset.keys);
      closeSheet();
    });
  }
  for (const btn of $('keys-sheet-overlay').querySelectorAll('.sheet-cmd')) {
    btn.addEventListener('click', () => {
      const inp = $('cmd-input');
      inp.value += btn.dataset.cmd;
      inp.focus();
      closeSheet();
    });
  }

  // Dynamically populate command sheet
  populateCommandSheet();
  fetchClaudeCommands().then(() => populateCommandSheet());

  // / trigger suggest
  const cmdInp = $('cmd-input');
  const suggestEl = $('cmd-suggest');

  function renderSuggest(val) {
    const cmds = getClaudeCommands();
    const q = val.slice(1).toLowerCase();
    const filtered = q === ''
      ? cmds
      : cmds.filter(c => c.cmd.slice(1).startsWith(q));
    if (!filtered.length) {
      suggestEl.hidden = true;
      return;
    }
    suggestEl.innerHTML = '';
    for (const item of filtered) {
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'cmd-suggest-item';
      const cmdSpan = document.createElement('span');
      cmdSpan.className = 'cmd-suggest-cmd';
      cmdSpan.textContent = item.cmd;
      const descSpan = document.createElement('span');
      descSpan.className = 'cmd-suggest-desc';
      descSpan.textContent = item.desc;
      btn.appendChild(cmdSpan);
      btn.appendChild(descSpan);
      btn.addEventListener('click', () => {
        cmdInp.value = item.cmd;
        suggestEl.hidden = true;
        cmdInp.focus();
      });
      suggestEl.appendChild(btn);
    }
    suggestEl.hidden = false;
  }

  cmdInp.addEventListener('input', () => {
    const val = cmdInp.value;
    if (val.startsWith('/')) {
      renderSuggest(val);
    } else {
      suggestEl.hidden = true;
    }
  });

  cmdInp.addEventListener('keydown', e => {
    if (e.key === 'Escape' && !suggestEl.hidden) {
      suggestEl.hidden = true;
    }
  });

  cmdInp.addEventListener('blur', () => {
    setTimeout(() => { suggestEl.hidden = true; }, 200);
  });

  // Snippet popup
  $('btn-snippet').addEventListener('click', toggleSnippetMenu);

  // Card menu overlay
  $('card-menu-overlay').addEventListener('click', e => {
    if (e.target === $('card-menu-overlay')) closeCardMenu();
  });
  $('card-menu-cancel').addEventListener('click', closeCardMenu);

  // Snippet menu overlay
  $('snippet-menu-overlay').addEventListener('click', e => {
    if (e.target === $('snippet-menu-overlay')) closeSnippetMenu();
  });

  // Snippet management sheet
  initSnippetSheet();

  // Drawer
  $('btn-menu').addEventListener('click', openDrawer);
  $('drawer-overlay').addEventListener('click', closeDrawer);

  // Auto approve toggle
  $('btn-auto-approve').addEventListener('click', () => {
    state.autoApprove = !state.autoApprove;
    $('btn-auto-approve').classList.toggle('active', state.autoApprove);
  });

  // Permission dismiss
  $('btn-perm-dismiss').addEventListener('click', hidePermissionBanner);

  // Window sheet
  $('breadcrumb-window').addEventListener('click', openWindowSheet);
  $('window-sheet-overlay').addEventListener('click', e => {
    if (e.target === $('window-sheet-overlay')) closeWindowSheet();
  });

  // Window menu
  $('window-menu-overlay').addEventListener('click', e => {
    if (e.target === $('window-menu-overlay')) closeWindowMenu();
  });
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

// ===== Snippet Popup Menu =====
async function renderSnippetMenu() {
  const el = $('snippet-menu');
  el.innerHTML = '';
  try {
    const data = await apiFetch('/api/snippets');
    const snippets = data.snippets || [];
    if (!snippets.length) {
      const p = document.createElement('div');
      p.className = 'snippet-item';
      p.textContent = '(snippets/ にファイルを追加)';
      el.appendChild(p);
    }
    for (const snip of snippets) {
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'snippet-item';
      btn.textContent = snip.label;
      btn.addEventListener('click', async () => {
        try {
          const d = await apiFetch(`/api/snippets/${encodeURIComponent(snip.name)}`);
          if (d.content) {
            const inp = $('cmd-input');
            inp.value += d.content;
            inp.focus();
          }
        } catch {}
        closeSnippetMenu();
      });
      el.appendChild(btn);
    }
    const manage = document.createElement('button');
    manage.type = 'button';
    manage.className = 'snippet-item';
    manage.style.color = 'var(--accent)';
    manage.textContent = '管理...';
    manage.addEventListener('click', () => {
      closeSnippetMenu();
      openSnippetSheet();
    });
    el.appendChild(manage);
  } catch {}
}

function toggleSnippetMenu() {
  const el = $('snippet-menu-overlay');
  if (el.hidden) {
    renderSnippetMenu();
    el.hidden = false;
  } else {
    closeSnippetMenu();
  }
}

function closeSnippetMenu() {
  $('snippet-menu-overlay').hidden = true;
}
