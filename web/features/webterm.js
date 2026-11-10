'use strict';

// terminal タブ: iPad等のブラウザからこのマシンを直接操作するターミナル。
// 専用WS(/ws/pty)の先でサーバが `tmux new-session -A -s webterm` をptyの中で走らせ、
// ptyのバイト列をxterm.jsと素通しする(ttyd/VS Codeと同じ標準構成)。
// 画面再描画・端末モード復元・リサイズはattachしたtmux自身が行うため、
// クライアント側に端末状態の再現ロジックは持たない。

const WEBTERM_SESSION = 'webterm';

let webtermTerm = null;
let webtermFit = null;
let webtermWs = null;        // /ws/pty 専用WebSocket(メインの/wsとは独立)
let webtermSwallowUntil = 0;    // Cmd系処理直後のIME確定ゴミ(「等)を捨てる期限(ms epoch)
let webtermSwallowAllUntil = 0; // 変換中Ctrl+英字の後、IMEが確定してくる変換中文字を丸ごと捨てる期限

// iPad実機の状態を数字で見るためのデバッグ表示(調査中につき常時ON。終わったら消す)。
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
  el.textContent = `term=${webtermTerm?.cols}x${webtermTerm?.rows} ws=${webtermWs?.readyState ?? '-'} ${extra || ''}\n` +
    `box=${Math.round(box?.width)}x${Math.round(box?.height)} screen=${Math.round(screen?.width)}x${Math.round(screen?.height)} cell=${cell}\n` +
    `font=${document.fonts.check('12px "JetBrainsMono NF"')} dpr=${devicePixelRatio}`;
}

// terminalタブが今実際に見えているか
function webtermVisible() {
  return state.currentTopTab === 'terminal' &&
    $('view-sessions').classList.contains('active');
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
  webtermLoadWebgl();
  // OSC 52対応(公式addon): herdrのコピーやtmuxのosc52-yankが発火するOSC 52を
  // xtermがパースし、providerのwriteTextに渡してくれる。デフォルトproviderは
  // navigator.clipboard直叩きでhttp配信では失敗するため差し替える。
  webtermTerm.loadAddon(new ClipboardAddon.ClipboardAddon(new ClipboardAddon.Base64(), {
    readText: async () => '', // クリップボード読み取り(OSC 52クエリ)は非対応でよい
    writeText: async (_sel, text) => { webtermCopyText(text); },
  }));
  // キー入力・IME確定文字・マウスレポートすべてxtermがエンコードした
  // バイト列としてonDataに出てくるので、そのままpty(tmux)へ流す。
  // 例外: Cmd系ショートカット処理直後は、日本語IMEがCmd+[等のkeydownを
  // preventDefaultしても勝手にcomposition確定する「」等を1回だけ捨てる(Safari)。
  webtermTerm.onData(d => {
    if (Date.now() < webtermSwallowAllUntil) return; // 変換中Ctrl+英字直後のIME確定分を捨てる
    if (Date.now() < webtermSwallowUntil && !/[\x00-\x7f]/.test(d)) {
      webtermSwallowUntil = 0;
      return;
    }
    webtermSend(d);
  });
  // xterm公式のショートカット横取りフック。falseを返したキーだけxtermが無視する。
  // Cmd系のみherdrシーケンス(prefix C-a + キー)に変換し、他は一切触らない。
  // 注意: C-aはtmux自身のprefixでもあるため、ptyに書くとattach中のtmuxが食ってしまう。
  // 既存のsend_keys API(tmux send-keys=paneへ直接注入、prefix解釈を迂回)で送る。
  // Cmd/Opt系の実処理はdocumentのkeydown capture(webtermShortcut)で行う。
  // フォーカスがxtermに無くても効かせるため(コピー直後等にフォーカスが外れていると
  // 「効いたり効かなかったり」になる)。ここではxtermへの流入だけ止める。
  webtermTerm.attachCustomKeyEventHandler(e => {
    if (e.type !== 'keydown') return true;
    // Cmd+C/Cmd+V はブラウザ標準のコピペに委ねる
    if (e.metaKey && (e.code === 'KeyC' || (e.code === 'KeyV' && !e.shiftKey))) return true;
    if (e.metaKey || (e.altKey && !e.ctrlKey && /^Key[HJKL]$/.test(e.code))) return false;
    return true;
  });
  webtermBindTap(container);
  webtermTerm.onResize(({ cols, rows }) => {
    if (webtermWs && webtermWs.readyState === WebSocket.OPEN) {
      webtermWs.send(JSON.stringify({ type: 'resize', cols, rows }));
    }
  });
  new MutationObserver(() => {
    if (webtermTerm) webtermTerm.options.theme = termBuildTheme();
  }).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
}

// WebGLレンダラの装着。iPad Safariは背面化中にWebGL contextを破棄することがあり、
// 3秒以内に復元されないとaddonはonContextLossを投げて描画停止のまま止まる
// (=WS再接続してもbufferだけ進み画面が固まる。実測で確認済みの再発原因)。
// onContextLossで古いaddonをdisposeし、表示中なら作り直して全再描画する。
let webtermWebgl = null;

function webtermLoadWebgl() {
  if (!webtermTerm || webtermWebgl) return;
  let addon;
  try {
    addon = new WebglAddon.WebglAddon();
    addon.onContextLoss(() => {
      addon.dispose(); // xtermがDOMレンダラへ自動フォールバックする
      if (webtermWebgl === addon) webtermWebgl = null;
      // 前面ならWebGLを作り直す(裏ならDOMのまま。次のresyncで作り直す)
      if (document.visibilityState === 'visible') webtermRestoreRenderer();
    });
    webtermTerm.loadAddon(addon);
    webtermWebgl = addon;
  } catch (err) {
    console.warn('webterm: WebGL renderer unavailable, using DOM renderer', err);
    if (addon) { try { addon.dispose(); } catch (_) {} }
  }
}

// WebGL作り直し+全再描画。復帰時(webtermResync)からも呼ぶ。
// addonのonContextLossはlost後3秒待ってから発火するため、復帰直後は
// canvasのisContextLost()を直接見て、死んでいたら3秒待たず即交換する。
function webtermRestoreRenderer() {
  if (!webtermTerm) return;
  if (webtermWebgl) {
    const dead = [...document.querySelectorAll('#webterm canvas')]
      .some(c => c.getContext('webgl2')?.isContextLost());
    if (dead) {
      try { webtermWebgl.dispose(); } catch (_) {}
      webtermWebgl = null;
    }
  }
  webtermLoadWebgl();
  webtermTerm.refresh(0, webtermTerm.rows - 1);
  webtermDebugUpdate(`webgl=${webtermWebgl ? 'on' : 'off'}`);
}

// ptyへの送信。文字列はUTF-8バイナリで送る(TextMessageはresize等の制御用)
function webtermSend(s) {
  if (webtermWs && webtermWs.readyState === WebSocket.OPEN) {
    webtermWs.send(new TextEncoder().encode(s));
  }
}

// 見えていて未接続ならpty WSを張る。tmuxセッションはサーバ側が
// 無ければ作るので、クライアントからの作成APIは不要。
// 接続は世代番号で管理: 復帰イベントの連打や古いWSの遅延callbackが
// 現行接続を上書き・二重接続しないよう、各callbackで世代を検査する。
let webtermGen = 0;
let webtermReconnectTimer = null;
let webtermStale = false; // terminalタブ非表示中に復帰イベントが来た印(次の表示で強制再接続)

function webtermMaybeAttach() {
  if (!webtermVisible()) return;
  webtermInitTerm();
  if (!webtermTerm) return;
  // 復帰イベントがterminalタブ非表示中に来ていた場合、既存WSは半死にの可能性が
  // あるので信用せず張り直す(readyState=OPENのまま死ぬiPadOSの既知挙動)。
  if (webtermStale) {
    webtermStale = false;
    webtermGen++;
    if (webtermWs) { try { webtermWs.close(); } catch (_) {} webtermWs = null; }
    webtermRestoreRenderer();
  }
  // CONNECTING中もwebtermWsに入っている(生成直後に代入)ので、これで多重接続を防げる
  if (webtermWs && (webtermWs.readyState === WebSocket.CONNECTING || webtermWs.readyState === WebSocket.OPEN)) {
    webtermTerm.focus();
    return;
  }
  clearTimeout(webtermReconnectTimer);
  const gen = ++webtermGen;
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const ws = new WebSocket(`${proto}//${location.host}/ws/pty?session=${WEBTERM_SESSION}&token=${encodeURIComponent(state.token)}`);
  ws.binaryType = 'arraybuffer';
  webtermWs = ws;
  ws.onopen = () => {
    if (gen !== webtermGen) { ws.close(); return; } // 旧世代の遅延openは破棄
    // attach直後にサイズを合わせる(tmuxが再描画してくれる)
    webtermFit.fit();
    const { cols, rows } = webtermTerm;
    ws.send(JSON.stringify({ type: 'resize', cols, rows }));
    webtermTerm.focus();
    webtermDebugUpdate('attached');
  };
  ws.onmessage = e => {
    if (gen === webtermGen && webtermTerm) webtermTerm.write(new Uint8Array(e.data));
  };
  ws.onclose = () => {
    if (gen !== webtermGen) return; // 旧世代のcloseは無視
    webtermWs = null;
    webtermDebugUpdate('closed');
    // タブ表示中なら自動再接続(裏に回っている間は再接続しない)
    clearTimeout(webtermReconnectTimer);
    if (webtermVisible()) webtermReconnectTimer = setTimeout(webtermMaybeAttach, 1500);
  };
  ws.onerror = () => {};
}

// iPadのタッチ操作。xterm.jsはtouchをスクロール専用に消費するため自前で扱う:
//  - タップ: クリック(SGRマウスpress+release)としてherdrへ転送
//  - 長押し→ドラッグ: クライアント側で矩形選択。オーバーレイで見たまま=コピー内容。
//    指を離した瞬間(=ユーザー操作イベント内)にxtermバッファから取り出して自動コピー。
//    ※herdrへドラッグ転送してOSC52を待つ方式は、OSC52が非同期到達でジェスチャ外に
//      なりiOS Safariがクリップボード書き込みを拒否する(自動コピー不能)ため採らない。
//      矩形選択は列範囲を跨いだ行も左右列で切るので、隣ペイン(space一覧等)を巻き込まない。
function webtermBindTap(container) {
  let start = null;      // {x,y} touchstart座標
  let anchor = null;     // 選択の起点セル {col,row}(1-based, ビューポート)
  let rect = null;       // 現在の矩形 {top,bottom,left,right}(0-based絶対行/列)
  let selecting = false;
  let lpTimer = null;

  const cellAt = (x, y) => {
    const r = container.querySelector('.xterm-screen')?.getBoundingClientRect();
    if (!r) return null;
    const col = Math.min(webtermTerm.cols, Math.max(1, Math.floor((x - r.left) / (r.width / webtermTerm.cols)) + 1));
    const row = Math.min(webtermTerm.rows, Math.max(1, Math.floor((y - r.top) / (r.height / webtermTerm.rows)) + 1));
    return { col, row };
  };
  const mouseOn = () => webtermTerm.modes.mouseTrackingMode !== 'none';
  const updateRect = c => {
    const vy = webtermTerm.buffer.active.viewportY;
    const ar = vy + anchor.row - 1, cr = vy + c.row - 1;
    rect = {
      top: Math.min(ar, cr), bottom: Math.max(ar, cr),
      left: Math.min(anchor.col, c.col) - 1, right: Math.max(anchor.col, c.col) - 1,
    };
    webtermDrawRect(container, rect);
  };

  container.addEventListener('touchstart', e => {
    clearTimeout(lpTimer);
    selecting = false;
    if (e.touches.length !== 1) { start = null; return; }
    const t = e.touches[0];
    start = { x: t.clientX, y: t.clientY };
    lpTimer = setTimeout(() => {
      if (!start || !webtermTerm) return;
      anchor = cellAt(start.x, start.y);
      if (!anchor) return;
      selecting = true;
      updateRect(anchor);
    }, 400);
  }, { passive: true });

  container.addEventListener('touchmove', e => {
    if (!start) return;
    const t = e.touches[0];
    if (selecting) {
      e.preventDefault(); // 選択中はスクロールさせない
      const c = cellAt(t.clientX, t.clientY);
      if (c) updateRect(c);
      return;
    }
    if (Math.hypot(t.clientX - start.x, t.clientY - start.y) > 10) {
      clearTimeout(lpTimer); // 動いた=スクロール。長押し不成立
    }
  }, { passive: false });

  container.addEventListener('touchend', e => {
    clearTimeout(lpTimer);
    if (!start || !webtermTerm) { selecting = false; start = null; return; }
    const s = start; start = null;
    if (selecting) {
      selecting = false;
      webtermDrawRect(container, null);
      if (rect) webtermCopyRect(rect); // ここはtouchend=ユーザー操作イベント内なので確実にコピーできる
      rect = null;
      return;
    }
    const t = e.changedTouches[0];
    if (Math.hypot(t.clientX - s.x, t.clientY - s.y) > 10) return; // スクロールは対象外
    if (!mouseOn()) return;
    const c = cellAt(t.clientX, t.clientY);
    if (!c) return;
    webtermSend(`\x1b[<0;${c.col};${c.row}M\x1b[<0;${c.col};${c.row}m`);
  }, { passive: true });
}

// 矩形選択のハイライトを1枚のオーバーレイdivで描く。rect=nullで消す
function webtermDrawRect(container, rect) {
  let el = document.getElementById('webterm-select-rect');
  if (!rect) { el?.remove(); return; }
  const screen = container.querySelector('.xterm-screen');
  if (!screen) return;
  const r = screen.getBoundingClientRect();
  const cw = r.width / webtermTerm.cols;
  const ch = r.height / webtermTerm.rows;
  const vy = webtermTerm.buffer.active.viewportY;
  if (!el) {
    el = document.createElement('div');
    el.id = 'webterm-select-rect';
    el.style.cssText = 'position:fixed;z-index:50;background:rgba(80,140,255,.35);border:1px solid rgba(80,140,255,.8);pointer-events:none';
    document.body.appendChild(el);
  }
  el.style.left = (r.left + rect.left * cw) + 'px';
  el.style.top = (r.top + (rect.top - vy) * ch) + 'px';
  el.style.width = ((rect.right - rect.left + 1) * cw) + 'px';
  el.style.height = ((rect.bottom - rect.top + 1) * ch) + 'px';
}

// 矩形内のテキストを行ごとに切り出してコピー(各行の右端空白は落とす)
function webtermCopyRect(rect) {
  const buf = webtermTerm.buffer.active;
  const lines = [];
  for (let row = rect.top; row <= rect.bottom; row++) {
    const line = buf.getLine(row);
    lines.push(line ? line.translateToString(false, rect.left, rect.right + 1).replace(/\s+$/, '') : '');
  }
  const text = lines.join('\n').replace(/\n+$/, '');
  if (text) webtermCopyText(text);
}

// クリップボードへの書き込み。OSC 52はドラッグを離した直後に非同期で届くが、
// Safariのtransient user activation(直前のタッチ操作)が数秒残っているため、
// その場でexecCommand('copy')が通ることが多い。通ればトースト無しで完了。
// 通らなかった時だけ専用ボタンを出す(他のタップは奪わない=IMEを壊さない)。
function webtermCopyText(text) {
  if (webtermTryCopy(text)) {
    webtermDebugUpdate(`copy=${text.slice(0, 24)}`);
    document.getElementById('webterm-copy-btn')?.remove();
    return;
  }
  webtermCopyButton(text);
}

// execCommandによる同期コピー。iOSではtextareaのselect()だけでは選択されないため
// setSelectionRangeを使う。IMEを壊さないよう、実行後は選択を消しxtermへfocusを戻す。
function webtermTryCopy(text) {
  const active = document.activeElement; // 現在のfocus(xterm textarea等)を後で復元
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.readOnly = true;
  ta.style.cssText = 'position:fixed;left:-9999px;top:0';
  document.body.appendChild(ta);
  ta.focus();
  ta.setSelectionRange(0, text.length);
  let ok = false;
  try { ok = document.execCommand('copy'); } catch (_) {}
  ta.remove();
  // focusを元へ戻す。IME中にコピーが割り込んでも入力が途切れないように。
  if (active && active.focus) active.focus();
  else if (webtermTerm) webtermTerm.focus();
  return ok;
}

// 即時コピーが拒否された時だけ出す確定ボタン。このボタン自体のタップのみコピーする
// (画面の他の場所のタップやキー入力は一切奪わない)。iOS Safariはボタンでも
// clickが発火しないことがあるためtouchendで確定し、確定後は必ず消す。
function webtermCopyButton(text) {
  document.getElementById('webterm-copy-btn')?.remove();
  const el = document.createElement('button');
  el.id = 'webterm-copy-btn';
  el.style.cssText = 'position:fixed;bottom:12px;left:50%;transform:translateX(-50%);z-index:100;' +
    'background:#2d6cdf;color:#fff;border:0;border-radius:8px;padding:10px 16px;' +
    'font:13px -apple-system,sans-serif;max-width:80%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap';
  el.textContent = `📋 タップしてコピー: ${text.slice(0, 36)}`;
  const confirm = ev => {
    ev.preventDefault();
    ev.stopPropagation();
    const ok = webtermTryCopy(text);
    webtermDebugUpdate(ok ? `copy=${text.slice(0, 24)}` : 'copy=failed');
    el.remove();
  };
  el.addEventListener('touchend', confirm);
  el.addEventListener('click', confirm); // マウス(PC)用フォールバック
  document.body.appendChild(el);
  clearTimeout(webtermCopyButton._t);
  webtermCopyButton._t = setTimeout(() => el.remove(), 12000);
}

// webtermセッションのpane target(send_keys API用)。メインWS(/ws)のpane_listから引く
function webtermFindTarget() {
  const s = state.sessions.find(s => s.backend !== 'herdr' && s.name === WEBTERM_SESSION);
  return s?.windows?.[0]?.panes?.[0]?.target || null;
}

// herdrのprefix(C-a)+キーをwebterm paneへ送る。pane_list未着でtargetが引けない時は
// tmuxのセッション名targetにフォールバック(先頭windowのactive paneに届く=webtermは単一pane)。
// 「Cmd+Wが効いたり効かない」の一因: メイン/wsが半死にでstate.sessionsが空だと
// targetがnullになり黙って何もしなかった。
function webtermSendHerdr(seq) {
  const target = webtermFindTarget() || `${WEBTERM_SESSION}:0`;
  sendKeys(target, 'C-a');
  sendKeys(target, seq);
  webtermDebugUpdate(`herdr=${seq} target=${target}`);
}

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

// Safariはcomposition中のkeydownでisComposingを正しく立てないことがある
// (確定を引き起こすキー自体はfalseで届く等)。textareaのcompositionイベントで
// 自前の変換中フラグを持ち、isComposingと併用する。
let webtermComposing = false;

function initWebterm() {
  document.addEventListener('compositionstart', () => {
    webtermComposing = true;
    webtermDebugUpdate('comp=start');
  }, true);
  document.addEventListener('compositionend', () => {
    webtermComposing = false;
    webtermDebugUpdate('comp=end');
  }, true);

  document.addEventListener('keydown', e => {
    if (!webtermVisible() || !webtermTerm) return;
    // 【調査用】Ctrl系keydownが変換中でも発火するか、isComposingの値を実機で確認する
    if (e.ctrlKey && !e.metaKey) {
      webtermDebugUpdate(`kd ${e.code} isComp=${e.isComposing?1:0} myComp=${webtermComposing?1:0} kc=${e.keyCode}`);
    }
    // Cmd系herdrショートカットと、そのiPad代替のOpt+H/J/K/L(Cmd+HはOS予約で届かない)。
    // xtermのハンドラではなくここ(document capture)で処理する: コピー直後等で
    // フォーカスがxtermから外れていても効くように(「効いたり効かない」の原因だった)。
    // Opt+英字はMacで合成文字のkeydownになるがe.codeは物理キーなので判定できる。
    const isOptNav = e.altKey && !e.metaKey && !e.ctrlKey && /^Key[HJKL]$/.test(e.code);
    if (e.metaKey || isOptNav) {
      // Cmd+C/Cmd+V はブラウザ標準のコピペに委ねる
      if (e.metaKey && (e.code === 'KeyC' || (e.code === 'KeyV' && !e.shiftKey))) return;
      const seq = isOptNav ? e.code.charAt(3).toLowerCase() : webtermHerdrSeq(e);
      if (seq) {
        e.preventDefault();
        e.stopPropagation();
        webtermSwallowUntil = Date.now() + 300; // IME確定ゴミ/合成文字のonData流入を1回捨てる
        webtermSendHerdr(seq);
      }
      return;
    }
    // Ctrl+英字(Ctrl+C等)は常に自前で制御バイトを送り、xterm/IMEには渡さない。
    // Ctrl+英字はローマ字変換の構成キーになり得ないので横取りして問題なく、
    // 「変換中はkeydownでisComposingが立たない」等のSafari差異に依存せず確実に効く。
    // 変換中なら、この後IMEが確定してくる文字(ASCIIのことも非ASCIIのことも)を
    // 期限内は全て捨てて二重入力を防ぐ。
    if (e.ctrlKey && !e.metaKey && !e.altKey && /^Key[A-Z]$/.test(e.code)) {
      e.preventDefault();
      e.stopPropagation();
      if (e.isComposing || webtermComposing) webtermSwallowAllUntil = Date.now() + 300;
      webtermSend(String.fromCharCode(e.code.charCodeAt(3) - 64)); // KeyA→\x01 .. KeyZ→\x1a
      return;
    }
    if (e.isComposing) return;
    // フォーカスがxtermから外れた状態での打鍵はフォーカスを戻すだけ(1打は捨てる)。
    // ただし設定モーダル等の他の入力欄にフォーカスがある間は奪わない。
    const ae = document.activeElement;
    if (ae && (ae.tagName === 'INPUT' || ae.tagName === 'TEXTAREA' ||
        ae.tagName === 'SELECT' || ae.isContentEditable)) return;
    webtermTerm.focus();
  }, true);

  // 画面サイズ変化(iPad回転等)時はfitしてresizeをptyへ伝える(tmuxが再描画する)
  let resizeTimer = null;
  window.addEventListener('resize', () => {
    if (!webtermVisible() || !webtermTerm || !webtermFit) return;
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => webtermFit.fit(), 300);
  });

  // iPadOS/SafariはPWAが裏に回るとWSを黙って殺すが、oncloseが発火せず
  // readyStateがOPENを名乗り続ける「半死に」になる(入力が届かないのに正常に見える)。
  // 生死はクライアントからは判定できないため、前面復帰時は無条件に張り直す。
  // attachし直せばtmuxが全再描画するので、コストは一瞬のちらつきだけ。
  // visibilitychange/pageshow/focusは復帰時に連続発火するためdebounceして1回に。
  // IME変換中フラグも復帰時にリセット(compositionendが来ないまま裏に回った場合の残留対策)。
  let resyncTimer = null;
  const webtermResync = () => {
    // terminalタブ非表示中の復帰は今は張り直せないので「stale」の印だけ付け、
    // 次にterminalタブが表示された時(webtermMaybeAttach)に強制再接続する。
    if (!webtermVisible()) { webtermStale = true; return; }
    clearTimeout(resyncTimer);
    resyncTimer = setTimeout(() => {
      if (!webtermVisible()) { webtermStale = true; return; } // 150ms内に裏へ回った
      webtermComposing = false;
      webtermGen++; // 旧接続のcallbackを全て無効化
      if (webtermWs) { try { webtermWs.close(); } catch (_) {} webtermWs = null; }
      webtermStale = false;
      webtermRestoreRenderer(); // 背面化でWebGL contextが死んでいたら作り直す
      webtermMaybeAttach();
    }, 150);
  };
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') webtermResync();
  });
  window.addEventListener('pageshow', webtermResync);
  window.addEventListener('focus', webtermResync);

  // リロード直後はフォント適用前の寸法でattachしてしまうことがある。
  // 全フォント確定後にWebGLのグリフキャッシュを破棄してfitし直す。
  document.fonts.ready.then(() => setTimeout(() => {
    if (!webtermTerm) return;
    webtermTerm.clearTextureAtlas();
    if (webtermFit) webtermFit.fit();
  }, 300));
}

// sessions.jsのタブ切替(renderSessionList)から呼ばれる表示フック
function webtermOnTabChange() {
  webtermMaybeAttach();
}
