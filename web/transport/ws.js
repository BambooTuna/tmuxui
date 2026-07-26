'use strict';

// WebSocket接続・送受信のみを担う搬送層。描画関数は直接呼ばず、CustomEvent
// ('ws:pane_snapshot'/'ws:pane_output'/'ws:pane_list'/'ws:permission'/
// 'ws:update_status') を dispatch して features/render 側に通知する。

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
      wsSend(subscribePayload(state.currentPane, size));
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

function base64ToBytes(b64) {
  return Uint8Array.from(atob(b64), c => c.charCodeAt(0));
}

function getSubscribeSize() {
  // 古いindex.htmlキャッシュ等でterm.jsが未ロードでもsubscribeを止めない
  if (typeof termFit === 'function') return termFit();
  return null;
}

function subscribePayload(target, size) {
  return { type: 'subscribe', target, ...(size || {}) };
}

// ペインの実サイズが正: pane_listで報告されたサイズとterminalの現在サイズがずれていたら
// (他クライアントによるリサイズや、resize要求が実クライアント優先で無視された場合など)
// subscribeを再送してsnapshotから取り直す。サイズが一致すればそこで収束して止まる。
let lastSizeResyncAt = 0;
function checkPaneSizeSync() {
  if (!state.currentPane || typeof termGetSize !== 'function') return;
  const pane = findPane(state.currentSession, state.currentWindow, state.currentPane);
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
  wsSend(subscribePayload(state.currentPane, size));
}

function handleWSMessage(msg) {
  switch (msg.type) {
    case 'pane_snapshot':
      bus.emit('ws:pane_snapshot', { target: msg.target, cols: msg.cols, rows: msg.rows, data: msg.data });
      break;

    case 'pane_output':
      bus.emit('ws:pane_output', { target: msg.target, data: msg.data });
      break;

    case 'pane_list':
      if (Array.isArray(msg.sessions)) {
        const prevJson = JSON.stringify(state.sessions);
        state.sessions = msg.sessions;
        const changed = JSON.stringify(msg.sessions) !== prevJson;
        bus.emit('ws:pane_list', { sessions: msg.sessions, changed });
        checkPaneSizeSync();
      }
      break;

    case 'permission_detected':
      bus.emit('ws:permission', msg);
      break;

    case 'update_status':
      if (msg.updateStatus) {
        bus.emit('ws:update_status', { updateStatus: msg.updateStatus });
      }
      break;
  }
}


function setWsStatus(status) {
  state.wsStatus = status;
  $('ws-dot').className = `ws-dot ${status}`;
}

function sendKeys(target, keys) {
  if (!wsSend({ type: 'send_keys', target, keys })) {
    apiFetchQuiet(`/api/panes/${encodeURIComponent(target)}/keys`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ keys }),
    });
  }
}
