// ブラウザSSH: xterm.js を単一画面で描画し、/ws/shell と生バイト双方向でつなぐ。
// セッション永続化は herdr/tmux 側に任せる方針なので、切断時は再接続で新規シェル。
// 途中出力の復元はしない。
(function () {
  'use strict';

  const term = new Terminal({
    cursorBlink: true,
    fontSize: 14,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
    scrollback: 5000,
    theme: {
      background: '#000000',
      foreground: '#e6e6e6',
    },
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open(document.getElementById('term'));
  try { fit.fit(); } catch (_) {}
  term.focus();

  // 注意: document capture で preventDefault すると xterm.js の keydown ハンドラが
  // 「既に処理済み」と判定して素通しになり、Space や修飾キー付き入力が xterm に
  // 届かなくなる。よってここでは preventDefault しない。ブラウザネイティブ
  // ショートカット (Cmd+Shift+[等) は下の Keyboard Lock (fullscreen中のみ動作)
  // で吸収する。fullscreen外では諦める。

  // xterm.js は Cmd (Meta) 修飾キー付きの入力をデフォルトで送信しない。
  // ホスト terminal (Ghostty等) が Cmd+Shift+ を独自マッピングで送っている
  // バイト列と同じものを WS 経由で送出する。マッピングは xxd で採取した値:
  //   Cmd+Shift+[ → ^A + Up  (herdr previous-space)
  //   Cmd+Shift+] → ^A + Down (herdr next-space)
  //   Cmd+Shift+H → ^A + v   (herdr split)
  //   Cmd+Shift+V → ^A + -   (herdr split)
  // TODO: 将来的にはprefsで編集可能なマッピングに拡張する
  const CMD_SHIFT_MAP = {
    BracketLeft: '\x01\x1b[A',
    BracketRight: '\x01\x1b[B',
    KeyH: '\x01v',
    KeyV: '\x01-',
  };
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== 'keydown') return true;
    if (!e.metaKey || !e.shiftKey || e.ctrlKey || e.altKey) return true;
    const payload = CMD_SHIFT_MAP[e.code];
    if (!payload) return true;
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(enc.encode(payload));
    }
    e.preventDefault();
    return false;
  });
  // フォーカスが外れたらいつでも取り戻す。ページ内クリックで端末に集中させる。
  document.addEventListener('click', () => term.focus());

  // Chrome の Keyboard Lock API (Cmd+W 等も含む全キー吸収)。fullscreen中のみ動作。
  // 初回のuserGesture(click/keydown/touch)で自動的にフルスクリーン+lockを試みる。
  // ボタン経由でもトグル可。fullscreenを抜けたら再度triggerできるようリセットする。
  let lockActivated = false;
  async function activateLock() {
    if (lockActivated) return;
    lockActivated = true;
    try {
      await document.documentElement.requestFullscreen();
    } catch (_) {
      lockActivated = false;
      return;
    }
    if (navigator.keyboard && typeof navigator.keyboard.lock === 'function') {
      try { await navigator.keyboard.lock(); } catch (_) {}
    }
    term.focus();
  }
  ['click', 'keydown', 'touchstart'].forEach((ev) =>
    document.addEventListener(ev, activateLock, { once: true, capture: true })
  );
  document.addEventListener('fullscreenchange', () => {
    if (!document.fullscreenElement) lockActivated = false;
  });

  const lockBtn = document.createElement('button');
  lockBtn.type = 'button';
  lockBtn.textContent = '⛶';
  lockBtn.title = 'フルスクリーン & 全キー吸収';
  lockBtn.style.cssText = [
    'position:fixed',
    'top:calc(env(safe-area-inset-top) + 4px)',
    'left:calc(env(safe-area-inset-left) + 8px)',
    'z-index:10',
    'padding:2px 8px',
    'font-size:14px',
    'line-height:1',
    'background:rgba(0,0,0,0.5)',
    'color:#fff',
    'border:0',
    'border-radius:10px',
    'cursor:pointer',
  ].join(';');
  lockBtn.addEventListener('click', async (e) => {
    e.stopPropagation();
    lockActivated = false;
    await activateLock();
  });
  document.body.appendChild(lockBtn);

  const statusEl = document.getElementById('status');
  let statusTimer = 0;
  function showStatus(text, opts) {
    if (!statusEl) return;
    statusEl.textContent = text;
    statusEl.classList.toggle('err', !!(opts && opts.err));
    statusEl.classList.add('show');
    clearTimeout(statusTimer);
    if (opts && opts.sticky) return;
    statusTimer = setTimeout(() => statusEl.classList.remove('show'), 1500);
  }

  let ws = null;
  let reconnectDelay = 500;
  const MAX_DELAY = 5000;
  let closedByUser = false;

  function wsURL() {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const src = new URLSearchParams(location.search);
    const q = new URLSearchParams();
    const tok = src.get('token');
    if (tok) q.set('token', tok);
    // u/s は index.js 側から /terminal?u=&s= で渡ってくる想定。
    // /terminal を token だけで開いた場合は指定なしのfallback起動になる。
    const u = src.get('u');
    const s = src.get('s');
    if (u) q.set('user', u);
    if (s) q.set('shell', s);
    const qs = q.toString();
    return proto + '//' + location.host + '/ws/shell' + (qs ? '?' + qs : '');
  }

  function sendResize() {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    try {
      ws.send(JSON.stringify({ t: 'resize', c: term.cols, r: term.rows }));
    } catch (_) {}
  }

  function connect() {
    showStatus('接続中…', { sticky: true });
    ws = new WebSocket(wsURL());
    ws.binaryType = 'arraybuffer';

    ws.onopen = () => {
      reconnectDelay = 500;
      showStatus('接続');
      try { fit.fit(); } catch (_) {}
      sendResize();
    };

    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string') {
        // 制御メッセージ (エラー通知等)
        try {
          const m = JSON.parse(ev.data);
          if (m && m.t === 'err') {
            term.write('\r\n\x1b[31m[shell error] ' + (m.d || '') + '\x1b[0m\r\n');
          }
        } catch (_) {}
        return;
      }
      term.write(new Uint8Array(ev.data));
    };

    ws.onclose = () => {
      if (closedByUser) return;
      showStatus('切断: 再接続待ち', { err: true, sticky: true });
      term.write('\r\n\x1b[33m[disconnected]\x1b[0m\r\n');
      setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(MAX_DELAY, reconnectDelay * 2);
    };

    ws.onerror = () => {
      // onclose に任せる
    };
  }

  const enc = new TextEncoder();
  term.onData((data) => {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    try {
      ws.send(enc.encode(data));
    } catch (_) {}
  });

  // リサイズ: window resize + visualViewport 変化 (モバイルの入力欄出し入れ対応)
  let resizeTimer = 0;
  function scheduleResize() {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => {
      try { fit.fit(); } catch (_) {}
      sendResize();
    }, 100);
  }
  window.addEventListener('resize', scheduleResize);
  if (window.visualViewport) {
    window.visualViewport.addEventListener('resize', scheduleResize);
  }
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) scheduleResize();
  });

  window.addEventListener('beforeunload', () => {
    closedByUser = true;
    try { ws && ws.close(); } catch (_) {}
  });

  connect();
})();
