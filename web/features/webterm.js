'use strict';

// terminal タブ: iPad等のブラウザからこのマシンを直接操作する自作ターミナル。
// 専用tmuxセッション(webterm)を作り、既存のsubscribe/send_raw基盤(WS)に相乗りする。
// キーボードはttyd/VS Codeと同じ標準構成: xterm.jsに完全に任せ(IME含む)、
// onDataで出てくるバイト列をそのままsend_rawで流す。ショートカットの横取りは
// xterm公式のattachCustomKeyEventHandlerでCmd系のみ行う。

const WEBTERM_SESSION = 'webterm';

let webtermTerm = null;
let webtermFit = null;
let webtermTarget = null;      // 例: "tmux:webterm:0.%5" (state.sessionsのprefix付きtarget)
let webtermSubscribed = false; // このクライアントのWS購読がwebtermを向いているか(の自己申告)
let webtermCreateRequested = false;
let webtermSwallowUntil = 0; // Cmd系処理直後のIME確定ゴミ(「等)を捨てる期限(ms epoch)

// iPad実機の状態を数字で見るためのデバッグ表示(調査中につき常時ON。終わったら消す)。
// term=xtermのグリッド / snap=サーバから来たsnapshot寸法 / box=コンテナpx / cell=1セルの実測幅
function webtermDebugUpdate(extra) {
  let el = document.getElementById('webterm-debug');
  if (!el) {
    el = document.createElement('div');
    el.id = 'webterm-debug';
    el.style.cssText = 'position:fixed;top:40px;right:4px;z-index:99;background:rgba(0,0,0,.8);color:#0f0;font:10px monospace;padding:2px 6px;pointer-events:none;white-space:pre';
    document.body.appendChild(el);
  }
  const box = $('webterm')?.getBoundingClientRect();
  const screen = document.querySelector('#webterm .xterm-screen')?.getBoundingClientRect();
  const cell = screen && webtermTerm ? (screen.width / webtermTerm.cols).toFixed(3) : '?';
  el.textContent = `term=${webtermTerm?.cols}x${webtermTerm?.rows} ${extra || ''}\n` +
    `box=${Math.round(box?.width)}x${Math.round(box?.height)} screen=${Math.round(screen?.width)}x${Math.round(screen?.height)} cell=${cell}\n` +
    `font=${document.fonts.check('12px "JetBrainsMono NF"')} dpr=${devicePixelRatio}`;
}

// terminalタブが今実際に見えているか
function webtermVisible() {
  return state.currentTopTab === 'terminal' &&
    $('view-sessions').classList.contains('active');
}

function webtermFindTarget() {
  const s = state.sessions.find(s => s.backend !== 'herdr' && s.name === WEBTERM_SESSION);
  return s?.windows?.[0]?.panes?.[0]?.target || null;
}

let webtermFontLoading = false;

function webtermInitTerm() {
  if (webtermTerm || typeof Terminal === 'undefined') return;
  const container = $('webterm');
  if (!container) return;
  // xtermは初期化時にフォント幅を計測するため、同梱Nerd Fontのロード完了を待つ。
  // 未ロードで開くとfallbackフォントの幅でグリッドが引かれ表示がズレる。
  if (!document.fonts.check('12px "JetBrainsMono NF"') ||
      !document.fonts.check('bold 12px "JetBrainsMono NF"')) {
    if (!webtermFontLoading) {
      webtermFontLoading = true;
      Promise.all([
        document.fonts.load('12px "JetBrainsMono NF"'),
        document.fonts.load('bold 12px "JetBrainsMono NF"'),
      ]).finally(() => webtermMaybeAttach());
    }
    return;
  }
  webtermTerm = new Terminal({
    disableStdin: false,
    scrollback: 5000,
    fontSize: 12,
    convertEol: false,
    fontFamily: (getComputedStyle(document.documentElement).getPropertyValue('--mono') || 'monospace').trim(),
    theme: termBuildTheme(),
  });
  webtermFit = new FitAddon.FitAddon();
  webtermTerm.loadAddon(webtermFit);
  webtermTerm.open(container);
  // Safari(iPad)のDOMレンダラは文字間隔を物理pxに丸め、端数が全列に累積して
  // 右端の列が描画領域外へはみ出す(実機で確認)。WebGLはグリフを正確な座標へ
  // 直接描くためズレない。失敗時(WebGL非対応等)はDOMレンダラのまま続行。
  try {
    webtermTerm.loadAddon(new WebglAddon.WebglAddon());
  } catch (err) {
    console.warn('webterm: WebGL renderer unavailable, using DOM renderer', err);
  }
  // OSC 52対応(公式addon): herdrのコピーやtmuxのosc52-yankが発火するOSC 52を
  // xtermがパースし、providerのwriteTextに渡してくれる。デフォルトproviderは
  // navigator.clipboard直叩きでhttp配信では失敗するため、execCommandフォール
  // バック持ちのwebtermCopyTextに差し替える。
  webtermTerm.loadAddon(new ClipboardAddon.ClipboardAddon(new ClipboardAddon.Base64(), {
    readText: async () => '', // クリップボード読み取り(OSC 52クエリ)は非対応でよい
    writeText: async (_sel, text) => { webtermCopyText(text); },
  }));
  // キー入力・IME確定文字・マウスレポートすべてxtermがエンコードした
  // バイト列としてonDataに出てくるので、そのままherdr(pty)へ流す。
  // これがttyd/VS Code等と同じ標準構成。自前のキー変換はしない。
  // 例外: Cmd系ショートカット処理直後は、日本語IMEがCmd+[等のkeydownを
  // preventDefaultしても勝手にcomposition確定する「」等を1回だけ捨てる(Safari)。
  webtermTerm.onData(d => {
    if (!webtermTarget) return;
    if (Date.now() < webtermSwallowUntil && !/[\x00-\x7f]/.test(d)) {
      webtermSwallowUntil = 0;
      return;
    }
    sendRaw(webtermTarget, d);
  });
  // xterm公式のショートカット横取りフック。falseを返したキーだけxtermが無視する。
  // Cmd系のみherdrシーケンスに変換し、他は一切触らない(IME・修飾キー含め全てxtermに任せる)。
  webtermTerm.attachCustomKeyEventHandler(e => {
    if (e.type !== 'keydown' || !e.metaKey) return true;
    // Cmd+C/Cmd+V はブラウザ標準のコピペに委ねる
    if (e.code === 'KeyC' || (e.code === 'KeyV' && !e.shiftKey)) return true;
    const seq = webtermHerdrSeq(e);
    if (seq && webtermTarget) {
      e.preventDefault();
      webtermSwallowUntil = Date.now() + 300;
      sendKeys(webtermTarget, 'C-a');
      sendKeys(webtermTarget, seq);
    }
    return false;
  });
  webtermBindTap(container);
  new MutationObserver(() => {
    if (webtermTerm) webtermTerm.options.theme = termBuildTheme();
  }).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
}

// 見えていて未購読なら購読する。webtermセッションが無ければ一度だけ作成を投げ、
// pane_list更新で拾えるようになった時に再度ここへ来て購読する。
function webtermMaybeAttach() {
  if (!webtermVisible()) return;
  const target = webtermFindTarget();
  if (!target) {
    if (!webtermCreateRequested) {
      webtermCreateRequested = true;
      apiFetchQuiet('/api/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: WEBTERM_SESSION, dir: '' }),
      });
    }
    return;
  }
  webtermInitTerm();
  if (!webtermTerm) return;
  if (webtermSubscribed && webtermTarget === target) return;
  webtermTarget = target;
  const p = webtermFit.proposeDimensions();
  const size = p && p.cols && p.rows ? { cols: p.cols, rows: p.rows } : {};
  if (wsSend({ type: 'subscribe', target, ...size })) {
    webtermSubscribed = true;
  }
  // キー入力は基本xtermのtextarea経由(IME対応)なので、表示時に必ずフォーカスしておく
  webtermTerm.focus();
}

// iPadのタッチ操作。xterm.jsはtouchをスクロール専用に消費しマウスレポート化
// しないため、ここでマウスイベントに変換してアプリへ転送する:
//  - タップ: クリック(press+release)
//  - 長押し→ドラッグ: マウスドラッグ(press → motion... → release)として転送。
//    herdrはPCのドラッグと同じくペイン内選択と自動コピー(OSC 52)を自前で行うので、
//    選択の見た目も範囲もPCと同一になる。クライアント側では選択を実装しない。
// マウス非対応アプリ(素のシェル等)ではドラッグ転送先が無いため何もしない
// (SGRシーケンスを送るとゴミ文字として入力される)。
function webtermBindTap(container) {
  let start = null;
  let dragging = false;
  let lastCell = null;
  let lpTimer = null;

  const cellAt = (x, y) => {
    const r = container.querySelector('.xterm-screen')?.getBoundingClientRect();
    if (!r) return null;
    const col = Math.min(webtermTerm.cols, Math.max(1, Math.floor((x - r.left) / (r.width / webtermTerm.cols)) + 1));
    const row = Math.min(webtermTerm.rows, Math.max(1, Math.floor((y - r.top) / (r.height / webtermTerm.rows)) + 1));
    return { col, row };
  };
  const mouseOn = () => webtermTerm.modes.mouseTrackingMode !== 'none';

  container.addEventListener('touchstart', e => {
    clearTimeout(lpTimer);
    dragging = false;
    if (e.touches.length !== 1) { start = null; return; }
    const t = e.touches[0];
    start = { x: t.clientX, y: t.clientY };
    lpTimer = setTimeout(() => {
      // 長押し成立: マウスボタン押下として転送しドラッグ開始
      if (!start || !webtermTerm || !webtermTarget || !mouseOn()) return;
      const c = cellAt(start.x, start.y);
      if (!c) return;
      dragging = true;
      lastCell = c;
      sendRaw(webtermTarget, `\x1b[<0;${c.col};${c.row}M`);
    }, 400);
  }, { passive: true });

  container.addEventListener('touchmove', e => {
    if (!start) return;
    const t = e.touches[0];
    if (dragging) {
      e.preventDefault(); // ドラッグ中はスクロールさせない
      const c = cellAt(t.clientX, t.clientY);
      if (c && (c.col !== lastCell.col || c.row !== lastCell.row)) {
        lastCell = c;
        sendRaw(webtermTarget, `\x1b[<32;${c.col};${c.row}M`); // button1 motion
      }
      return;
    }
    if (Math.hypot(t.clientX - start.x, t.clientY - start.y) > 10) {
      clearTimeout(lpTimer); // 動いた=スクロール。長押し不成立
    }
  }, { passive: false });

  container.addEventListener('touchend', e => {
    clearTimeout(lpTimer);
    if (!start || !webtermTerm) { dragging = false; start = null; return; }
    const s = start; start = null;
    if (dragging) {
      dragging = false;
      if (lastCell) sendRaw(webtermTarget, `\x1b[<0;${lastCell.col};${lastCell.row}m`); // release=選択確定
      return;
    }
    const t = e.changedTouches[0];
    if (Math.hypot(t.clientX - s.x, t.clientY - s.y) > 10) return; // スクロールは対象外
    if (!webtermTarget || !mouseOn()) return;
    const c = cellAt(t.clientX, t.clientY);
    if (!c) return;
    sendRaw(webtermTarget, `\x1b[<0;${c.col};${c.row}M\x1b[<0;${c.col};${c.row}m`);
  }, { passive: true });
}

// クリップボードへの書き込み。ブラウザの制約が2段ある:
//  - http配信(非secure context)ではnavigator.clipboardが存在しない → execCommand('copy')
//  - Safariはどちらの経路もユーザー操作(タップ等)のイベント内でしか許可しない
// そのためOSC 52のような非同期到達のコピーは即時には書けない。これは既知の制約で、
// Web系ターミナルの定石は「タップで確定するコピーUIを出す」方式。ここでもそれに倣い、
// 即時コピーを試して失敗したら画面下に「タップでコピー」トーストを出す。
function webtermCopyText(text) {
  if (webtermTryCopy(text)) {
    webtermDebugUpdate(`copy=${text.slice(0, 24)}`);
    return;
  }
  if (navigator.clipboard && window.isSecureContext) {
    // secure contextならasync APIも試す(Safari以外はジェスチャ外でも通ることがある)
    navigator.clipboard.writeText(text).then(
      () => webtermDebugUpdate(`copy=${text.slice(0, 24)}`),
      () => webtermCopyToast(text));
    return;
  }
  webtermCopyToast(text);
}

// execCommandによる同期コピー。iOSではtextareaのselect()だけでは選択されないため
// contentEditable+Range+setSelectionRangeの定石を使う。readOnlyでキーボード抑止。
function webtermTryCopy(text) {
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.readOnly = true;
  ta.contentEditable = 'true';
  ta.style.cssText = 'position:fixed;left:-9999px;top:0';
  document.body.appendChild(ta);
  const range = document.createRange();
  range.selectNodeContents(ta);
  const sel = window.getSelection();
  sel.removeAllRanges();
  sel.addRange(range);
  ta.setSelectionRange(0, text.length);
  let ok = false;
  try { ok = document.execCommand('copy'); } catch (_) {}
  // DOM selectionを残すとSafariが選択UI(ハンドル)を出し続け、以後のIME入力も壊れる
  sel.removeAllRanges();
  ta.remove();
  if (webtermTerm) webtermTerm.focus();
  return ok;
}

// ジェスチャ外で届いたコピー(OSC 52等)はSafariが即時書き込みを拒否するため、
// pendingに保持して「次に画面のどこかに触れた瞬間」(=ジェスチャ内)に自動確定する。
// ドラッグコピー後はどうせ画面に触るので、体感はほぼ自動コピーになる。
let webtermPendingCopy = null;
function webtermCopyToast(text) {
  webtermPendingCopy = text;
  let el = document.getElementById('webterm-copy-toast');
  if (!el) {
    el = document.createElement('div');
    el.id = 'webterm-copy-toast';
    el.style.cssText = 'position:fixed;bottom:12px;left:50%;transform:translateX(-50%);z-index:100;' +
      'background:rgba(45,108,223,.9);color:#fff;border-radius:8px;padding:6px 12px;' +
      'font:12px -apple-system,sans-serif;max-width:80%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;pointer-events:none';
    document.body.appendChild(el);
  }
  el.textContent = `コピー待ち(画面タップで確定): ${text.slice(0, 40)}`;
  clearTimeout(el._hideTimer);
  el._hideTimer = setTimeout(() => { el.remove(); webtermPendingCopy = null; }, 15000);
}

// pendingコピーの確定。touchend(指を離した瞬間)はuser activationが生きているので
// execCommandが通る。captureで拾い、あらゆるタッチ操作のついでに確定させる。
document.addEventListener('touchend', () => {
  if (!webtermPendingCopy) return;
  const text = webtermPendingCopy;
  webtermPendingCopy = null;
  const ok = webtermTryCopy(text);
  webtermDebugUpdate(ok ? `copy=${text.slice(0, 24)}` : 'copy=failed');
  document.getElementById('webterm-copy-toast')?.remove();
}, true);

// ===== WS通知 =====
bus.on('ws:pane_snapshot', e => {
  if (e.detail.target === webtermTarget && webtermTerm) {
    if (e.detail.cols > 0 && e.detail.rows > 0 &&
        (webtermTerm.cols !== e.detail.cols || webtermTerm.rows !== e.detail.rows)) {
      webtermTerm.resize(e.detail.cols, e.detail.rows);
    }
    webtermTerm.reset();
    webtermTerm.write(base64ToBytes(e.detail.data));
    webtermDebugUpdate(`snap=${e.detail.cols}x${e.detail.rows}`);
  } else if (webtermTarget) {
    // 別ペインのsnapshotが来た=このクライアントの購読が奪われた(pane詳細を開いた等)
    webtermSubscribed = false;
  }
});

bus.on('ws:pane_output', e => {
  if (e.detail.target === webtermTarget && webtermTerm) {
    webtermTerm.write(base64ToBytes(e.detail.data));
  }
});

// pane_listはHubが定期broadcastするので、タブ表示中の(再)購読トリガに使う
bus.on('ws:pane_list', () => webtermMaybeAttach());

// Cmd(Meta)系を herdr の prefix(Ctrl+A)後キーに変換する。
// ~/takenv/dotfiles/.config/wezterm/wezterm.lua の herdr keybind を移植したもの
// (普段 wezterm がこの変換をして herdr に送っている)。戻り値は prefix後に送るtmuxキー名。
// nullは「該当なし=送らない」。物理キー位置(e.code)で判定しキー配列非依存。
function webtermHerdrSeq(e) {
  const shift = e.shiftKey;
  switch (e.code) {
    case 'BracketLeft':  return shift ? 'Up' : 'p';   // Cmd+[ タブ前 / Cmd+Shift+[ スペース前
    case 'BracketRight': return shift ? 'Down' : 'n'; // Cmd+] タブ次 / Cmd+Shift+] スペース次
    case 'KeyT': return shift ? 'N' : 'c';            // Cmd+T 新タブ / Cmd+Shift+T スペース追加
    case 'KeyP': return shift ? null : 'g';           // Cmd+P session navigator
    case 'KeyG': return shift ? 'G' : null;           // Cmd+Shift+G 新worktree
    case 'KeyO': return shift ? 'O' : null;           // Cmd+Shift+O worktree開く
    case 'KeyV': return shift ? '-' : null;           // Cmd+Shift+V ペイン上下分割(Cmd+Vはペースト)
    case 'KeyH': return shift ? 'v' : 'h';            // Cmd+Shift+H 左右分割 / Cmd+H フォーカス左
    case 'KeyW': return shift ? null : 'x';           // Cmd+W ペイン閉じる
    case 'KeyJ': return shift ? null : 'j';
    case 'KeyK': return shift ? null : 'k';
    case 'KeyL': return shift ? null : 'l';
    default: return null;
  }
}

function initWebterm() {
  // フォーカスがxtermから外れた状態での打鍵は、フォーカスを戻すだけにする。
  // その1打は失われるが、キー変換の二重実装を持たないことを優先。
  // ただし設定モーダル等の他の入力欄にフォーカスがある間は奪わない。
  document.addEventListener('keydown', e => {
    if (!webtermVisible() || !webtermTerm || !webtermTarget) return;
    // 日本語IME変換中のCtrl+英字は、IMEが「変換確定」として食ってしまい
    // 端末に制御バイトが届かない(Ctrl+Cが中断にならず確定=Enter風に見える)。
    // 変換中でもCtrl+英字は端末制御と解釈し、IMEより先に直接送る。
    if (e.isComposing && e.ctrlKey && !e.metaKey && !e.altKey && /^Key[A-Z]$/.test(e.code)) {
      e.preventDefault();
      e.stopPropagation();
      webtermSwallowUntil = Date.now() + 300; // IMEが道連れに確定する変換中文字は捨てる
      sendRaw(webtermTarget, String.fromCharCode(e.code.charCodeAt(3) - 64)); // KeyA→\x01 .. KeyZ→\x1a
      return;
    }
    if (e.isComposing) return;
    const ae = document.activeElement;
    if (ae && (ae.tagName === 'INPUT' || ae.tagName === 'TEXTAREA' ||
        ae.tagName === 'SELECT' || ae.isContentEditable)) return;
    webtermTerm.focus();
  }, true);

  // WS再接続時は購読が消えているので張り直す
  bus.on('ws:open', () => { webtermSubscribed = false; webtermMaybeAttach(); });

  // 画面サイズ変化(iPad回転・別クライアントによるtmuxリサイズ後の復帰)時は
  // 新しい寸法でsubscribeし直してsnapshotから描き直す
  let resizeTimer = null;
  window.addEventListener('resize', () => {
    if (!webtermVisible() || !webtermTerm) return;
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => { webtermSubscribed = false; webtermMaybeAttach(); }, 300);
  });

  // iPadOS/SafariはPWAが裏に回るとWS受信を止め、その間の差分が欠落して
  // 復帰後の画面が壊れたままになる。前面復帰時はsnapshotから強制再同期する。
  const webtermResync = () => {
    if (!webtermVisible() || !webtermTerm) return;
    webtermSubscribed = false;
    webtermMaybeAttach();
  };
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') webtermResync();
  });
  window.addEventListener('pageshow', webtermResync);
  window.addEventListener('focus', webtermResync);

  // リロード直後はフォント適用・レイアウト確定前の寸法でsubscribeしてしまうことがあり、
  // 一発目の描画がズレる(ホーム→復帰の再同期で直るのと同じ理屈)。全フォント確定後、
  // WebGLのグリフキャッシュ(fallbackフォントで描いた字が残る)を破棄して再同期する。
  document.fonts.ready.then(() => setTimeout(() => {
    if (webtermTerm) webtermTerm.clearTextureAtlas();
    webtermResync();
  }, 300));
}

// sessions.jsのタブ切替(renderSessionList)から呼ばれる表示フック
function webtermOnTabChange() {
  webtermMaybeAttach();
}
