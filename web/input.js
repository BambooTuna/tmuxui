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
      if (state.currentPane) sendKeys(state.currentPane, btn.dataset.cmd + '\n');
      closeSheet();
    });
  }

  // Snippet
  $('btn-snippet').addEventListener('click', toggleSnippetMenu);
  document.addEventListener('click', e => {
    if (!e.target.closest('.snippet-wrap')) closeSnippetMenu();
    if (!e.target.closest('.card-menu') && !e.target.closest('.btn-card-menu')) closeCardMenu();
  });

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

// ===== Snippets =====
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
      return;
    }
    for (const snip of snippets) {
      const btn = document.createElement('button');
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
  } catch {}
}

function toggleSnippetMenu() {
  const el = $('snippet-menu');
  if (el.hidden) {
    renderSnippetMenu();
    el.hidden = false;
  } else {
    closeSnippetMenu();
  }
}

function closeSnippetMenu() {
  $('snippet-menu').hidden = true;
}
