function connectWS() {
  clearTimeout(state.reconnectTimer);

  if (state.ws) {
    state.ws.onclose = null;
    state.ws.close();
    state.ws = null;
  }

  setWsStatus('connecting');

  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const url = `${proto}//${location.host}/ws?token=${encodeURIComponent(state.token)}`;

  try {
    state.ws = new WebSocket(url);
  } catch {
    setWsStatus('disconnected');
    state.reconnectTimer = setTimeout(connectWS, 5000);
    return;
  }

  state.ws.onopen = () => {
    setWsStatus('connected');
    if (state.currentPane) {
      const size = getSubscribeSize();
      wsSend({ type: 'subscribe', target: state.currentPane, ...(size || {}) });
    }
  };

  state.ws.onmessage = e => {
    try { handleWSMessage(JSON.parse(e.data)); } catch {}
  };

  state.ws.onclose = () => {
    setWsStatus('disconnected');
    state.reconnectTimer = setTimeout(connectWS, 3000);
  };

  state.ws.onerror = () => {};
}

function wsSend(msg) {
  if (state.ws && state.ws.readyState === WebSocket.OPEN) {
    state.ws.send(JSON.stringify(msg));
    return true;
  }
  return false;
}

// 設定画面の「描画エンジン」で切替可能。'0'(従来)を明示セットした場合のみ旧pane_content描画
function xtermEnabled() {
  return localStorage.getItem('tmuxuiXterm') !== '0';
}

function base64ToBytes(b64) {
  return Uint8Array.from(atob(b64), c => c.charCodeAt(0));
}

function getSubscribeSize() {
  // 古いindex.htmlキャッシュ等でterm.jsが未ロードでもsubscribeを止めない
  if (xtermEnabled() && typeof termFit === 'function') return termFit();
  return calcTermSize();
}

// ペインの実サイズが正: pane_listで報告されたサイズとterminalの現在サイズがずれていたら
// (他クライアントによるリサイズや、resize要求が実クライアント優先で無視された場合など)
// subscribeを再送してsnapshotから取り直す。サイズが一致すればそこで収束して止まる。
let lastSizeResyncAt = 0;
function checkPaneSizeSync() {
  if (!xtermEnabled() || !state.currentPane || typeof termGetSize !== 'function') return;
  const session = state.sessions.find(s => s.name === state.currentSession);
  const win = session?.windows.find(w => w.index === state.currentWindow);
  const pane = win?.panes.find(p => p.target === state.currentPane);
  const m = pane && /^(\d+)x(\d+)$/.exec(pane.size);
  if (!m) return;
  const paneCols = parseInt(m[1], 10);
  const paneRows = parseInt(m[2], 10);
  const cur = termGetSize();
  if (!cur || (cur.cols === paneCols && cur.rows === paneRows)) return;
  const now = Date.now();
  if (now - lastSizeResyncAt < 1000) return;
  lastSizeResyncAt = now;
  const size = getSubscribeSize();
  wsSend({ type: 'subscribe', target: state.currentPane, ...(size || {}) });
}

function handleWSMessage(msg) {
  switch (msg.type) {
    case 'pane_content':
      if (!xtermEnabled() && msg.target === state.currentPane) {
        renderPaneContent(msg.content || '');
      }
      break;

    case 'pane_snapshot':
      if (xtermEnabled() && msg.target === state.currentPane) {
        // ペインの実サイズが正: 書き込む前にterminalをそのサイズへ追従させる
        if (msg.cols > 0 && msg.rows > 0) termSetSize(msg.cols, msg.rows);
        termWriteSnapshot(base64ToBytes(msg.data));
        stopRefreshing();
      }
      break;

    case 'pane_output':
      if (xtermEnabled() && msg.target === state.currentPane) {
        termWrite(base64ToBytes(msg.data));
      }
      break;

    case 'pane_list':
      if (Array.isArray(msg.sessions)) {
        const prevJson = JSON.stringify(state.sessions);
        state.sessions = msg.sessions;
        if ($('view-sessions').classList.contains('active') && JSON.stringify(msg.sessions) !== prevJson) {
          renderSessionList();
        }
        if (state.currentSession) {
          renderPaneTabs();
        }
        checkPaneSizeSync();
      }
      break;

    case 'permission_detected':
      showPermissionBanner(msg);
      break;
  }
}


function setWsStatus(status) {
  state.wsStatus = status;
  $('ws-dot').className = `ws-dot ${status}`;
}

function calcTermSize() {
  const el = $('pane-content');
  if (!el) return null;
  const probe = document.createElement('span');
  probe.style.cssText = 'position:absolute;visibility:hidden;font-family:' +
    getComputedStyle(el).fontFamily + ';font-size:' +
    getComputedStyle(el).fontSize + ';white-space:pre';
  probe.textContent = 'M'.repeat(10);
  document.body.appendChild(probe);
  const charW = probe.offsetWidth / 10;
  const lineH = parseFloat(getComputedStyle(el).lineHeight) || parseFloat(getComputedStyle(el).fontSize) * 1.4;
  document.body.removeChild(probe);

  const style = getComputedStyle(el);
  const padL = parseFloat(style.paddingLeft);
  const padR = parseFloat(style.paddingRight);
  const padT = parseFloat(style.paddingTop);
  const padB = parseFloat(style.paddingBottom);

  const cols = Math.floor((el.clientWidth - padL - padR) / charW);
  const rows = Math.floor((el.clientHeight - padT - padB) / lineH);
  return { cols: Math.max(cols, 20), rows: Math.max(rows, 5) };
}

let resizeTimer = null;
window.addEventListener('resize', () => {
  if (xtermEnabled()) return; // xterm有効時はterm.js側のResizeObserver/visualViewportが処理する
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => {
    if (state.currentPane) {
      const size = calcTermSize();
      if (size) wsSend({ type: 'resize', target: state.currentPane, ...size });
    }
  }, 300);
});

function sendKeys(target, keys) {
  if (!wsSend({ type: 'send_keys', target, keys })) {
    apiFetch(`/api/panes/${encodeURIComponent(target)}/keys`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ keys }),
    }).catch(() => {});
  }
}
