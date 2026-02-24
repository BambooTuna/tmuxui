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
      const size = calcTermSize();
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

function handleWSMessage(msg) {
  switch (msg.type) {
    case 'pane_content':
      if (msg.target === state.currentPane) {
        renderPaneContent(msg.content || '');
      }
      break;

    case 'pane_list':
      if (Array.isArray(msg.sessions)) {
        state.sessions = msg.sessions;
        if ($('view-sessions').classList.contains('active')) {
          renderSessionList();
        }
        if (state.currentSession) {
          renderPaneTabs();
        }
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
