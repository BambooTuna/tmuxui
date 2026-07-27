'use strict';

// xterm.js表示レイヤー(唯一の描画エンジン)。
// vendor UMDビルドのグローバル名: xterm.js本体は window.Terminal (フラットexport)、
// addon-fitは window.FitAddon.FitAddon (factoryが{FitAddon:class}を返す実装のため入れ子)。
// パレットは render/palette.js の定義を参照する(classic/xterm共通)。

let terminal = null;
let fitAddon = null;

function termBuildTheme() {
  const name = paletteThemeName();
  const c = paletteBasicColors();
  return {
    background: PALETTE_BG[name], foreground: PALETTE_FG[name], cursor: PALETTE_FG[name],
    black: c[0], red: c[1], green: c[2], yellow: c[3],
    blue: c[4], magenta: c[5], cyan: c[6], white: c[7],
    brightBlack: c[8], brightRed: c[9], brightGreen: c[10], brightYellow: c[11],
    brightBlue: c[12], brightMagenta: c[13], brightCyan: c[14], brightWhite: c[15],
  };
}

function termInit() {
  if (terminal || typeof Terminal === 'undefined') return;
  const container = $('pane-content');
  if (!container) return;

  terminal = new Terminal({
    disableStdin: true,
    scrollback: 20000,
    fontSize: 12,
    convertEol: false,
    fontFamily: (getComputedStyle(document.documentElement).getPropertyValue('--mono') || 'monospace').trim(),
    theme: termBuildTheme(),
  });
  fitAddon = new FitAddon.FitAddon();
  terminal.loadAddon(fitAddon);
  terminal.open(container);

  // data-theme変更元(settings.js)のイベント登録順に依存しないようMutationObserverで追従する
  new MutationObserver(() => {
    if (terminal) terminal.options.theme = termBuildTheme();
  }).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });

  termSetupResizeWatchers(container.parentElement || container);
  termSetupTouchScroll(container);
}

// xterm.js 6.0はビューポートをネイティブスクロール要素として公開しない(カスタム
// スクロールバー実装で.xterm-viewportのscrollHeight==clientHeightになる。実測済み)ため、
// モバイルのスワイプではスクロールバックを遡れない。縦方向のタッチジェスチャをローカル
// スクロールへ変換して実現する。横方向のジェスチャは#pane-contentのoverflow-x:autoによる
// ネイティブ横パンに委ねるため一切触らない。
//
// 視覚モデル: [scrollback履歴 … terminal最上行 … terminal最下行] を1本の縦ストリームと
// みなし、縦ドラッグはその上を連続的に移動する。実ptyサイズ修正でterminalの行数が
// コンテナの表示行数より多くなったため(例: 51行 vs コンテナが表示できる範囲)、
// 「scrollback(terminal.scrollLines)」と「#pane-content自体の縦パン(container.scrollTop、
// terminal要素がコンテナより縦に大きい分の可動域)」という2つの独立したスクロール経路が
// でき、ここを1本のジェスチャで繋ぐ必要がある。境界はcontainer.scrollTop==0(これより古い
// 方向はscrollbackの領分)。スクロールバックが空のTUI(claude/vim等のalternate screen)では
// scrollLinesが実質no-opで副作用は無い(TUIを遡る手段は従来通りリモートスクロールボタン)。
function termSetupTouchScroll(container) {
  let startX = 0, startY = 0, lastY = 0;
  let axis = null; // null=未判定 / 'v'=縦(ローカルスクロール) / 'h'=横(ネイティブパンに委譲)
  let acc = 0; // scrollLines用のセル高未満の端数の持ち越し(container.scrollTopはpx単位なので端数不要)
  container.addEventListener('touchstart', e => {
    if (e.touches.length !== 1) return;
    startX = e.touches[0].clientX;
    startY = lastY = e.touches[0].clientY;
    axis = null;
    acc = 0;
  }, { passive: true });
  container.addEventListener('touchmove', e => {
    if (!terminal || e.touches.length !== 1) return;
    const x = e.touches[0].clientX;
    const y = e.touches[0].clientY;
    if (axis === null) {
      const dx = Math.abs(x - startX);
      const dy = Math.abs(y - startY);
      if (dx < 6 && dy < 6) return; // 誤爆防止のデッドゾーン
      axis = dy >= dx ? 'v' : 'h';
    }
    if (axis !== 'v') return;
    // style.cssの`#pane-content { touch-action: pan-x }`により縦ジェスチャはブラウザの
    // ネイティブスクロール候補から外れているため、ここでのpreventDefaultは常に効く
    // (デッドゾーン判定中もtouch-actionは効いているので、判定完了前の6px未満の移動で
    // ジェスチャがネイティブスクロールに確定してしまう心配はない)。
    e.preventDefault();
    const dyPx = lastY - y; // 指を上へ(+)=新しい方向(下へ進む)、指を下へ(-)=古い方向(過去へ遡る)
    lastY = y;

    if (dyPx < 0) {
      // 古い方向: まずコンテナ自体を上へパンし、scrollTopが0に達した残り分だけ
      // scrollback(scrollLines)へ渡す。scrollTopはpx単位で正確に分割できる。
      if (container.scrollTop > 0) {
        const consumed = Math.max(dyPx, -container.scrollTop);
        container.scrollTop += consumed;
        acc += dyPx - consumed;
      } else {
        acc += dyPx;
      }
    } else if (dyPx > 0) {
      // 新しい方向: scrollbackが過去位置にいる間(viewportY < baseY)はそちらを優先して
      // 現在へ戻す。既に最下部(baseYに追いついている)ならコンテナを下へパンする。
      // 境界跨ぎはtouchmoveが高頻度で発火するため次イベント以降で自然に切り替わる。
      const buf = terminal.buffer.active;
      if (buf.viewportY < buf.baseY) {
        acc += dyPx;
      } else {
        container.scrollTop += dyPx;
      }
    }

    const screen = terminal.element && terminal.element.querySelector('.xterm-screen');
    const cell = screen && terminal.rows > 0 ? screen.clientHeight / terminal.rows : 0;
    if (cell > 0) {
      const lines = Math.trunc(acc / cell);
      if (lines !== 0) {
        terminal.scrollLines(lines);
        acc -= lines * cell;
      }
    }
  }, { passive: false });
}

// ペインの実サイズが正: ここではterminal.resizeせず、提案サイズが変わった時だけ
// resize要求を送る(実クライアントがいるセッションではサーバ側で無視される)。
function termSetupResizeWatchers(watchEl) {
  let timer = null;
  const doFit = () => {
    if (!terminal || !fitAddon || !state.currentPane) return;
    const proposed = fitAddon.proposeDimensions();
    if (!proposed || !proposed.cols || !proposed.rows) return;
    if (proposed.cols === terminal.cols && proposed.rows === terminal.rows) return;
    wsSend({ type: 'resize', target: state.currentPane, cols: proposed.cols, rows: proposed.rows });
  };
  const trigger = () => {
    clearTimeout(timer);
    timer = setTimeout(doFit, 300);
  };
  new ResizeObserver(trigger).observe(watchEl);
  if (window.visualViewport) {
    window.visualViewport.addEventListener('resize', trigger);
  }
}

function termReset() {
  if (terminal) terminal.reset();
}

function termWriteSnapshot(bytes) {
  termInit();
  if (!terminal) return;
  terminal.reset();
  terminal.write(bytes);
}

function termWrite(bytes) {
  if (terminal) terminal.write(bytes);
}

// ペインの実サイズが正のため、ここではterminalに適用せず「画面に収まる希望サイズ」を
// 提案するだけ(subscribe/resize要求の希望値として使う)。適用はtermSetSizeが行う。
function termFit() {
  // 非表示コンテナへのマウントは文字サイズ計測が壊れるため、初回利用時(=ペイン表示後)に遅延初期化する
  termInit();
  if (!terminal || !fitAddon) return null;
  const proposed = fitAddon.proposeDimensions();
  if (!proposed || !proposed.cols || !proposed.rows) return null;
  return { cols: proposed.cols, rows: proposed.rows };
}

// サーバから伝えられたペインの実サイズにterminalを追従させる(強制フィットはしない)
function termSetSize(cols, rows) {
  if (!terminal || !cols || !rows) return;
  if (terminal.cols === cols && terminal.rows === rows) return;
  terminal.resize(cols, rows);
}

function termGetSize() {
  return terminal ? { cols: terminal.cols, rows: terminal.rows } : null;
}

function termBufferType() {
  return terminal ? terminal.buffer.active.type : 'normal';
}

// バッファ全行(スクロールバック込み)をプレーンテキスト化して返す。
// features/text-view.jsのコピー用オーバーレイが使う。trimRight=trueで各行の
// 右端の空白セルを除去(translateToStringの第1引数)。末尾の連続空行は落とす。
function termGetBufferText() {
  if (!terminal) return '';
  const buf = terminal.buffer.active;
  const lines = [];
  for (let i = 0; i < buf.length; i++) {
    const line = buf.getLine(i);
    lines.push(line ? line.translateToString(true) : '');
  }
  while (lines.length > 0 && lines[lines.length - 1] === '') lines.pop();
  return lines.join('\n');
}

// transport/ws.js からの通知
bus.on('ws:pane_snapshot', e => {
  if (e.detail.target === state.currentPane) {
    // ペインの実サイズが正: 書き込む前にterminalをそのサイズへ追従させる
    if (e.detail.cols > 0 && e.detail.rows > 0) termSetSize(e.detail.cols, e.detail.rows);
    termWriteSnapshot(base64ToBytes(e.detail.data));
    bus.emit('render:pane-snapshot-applied');
  }
});

bus.on('ws:pane_output', e => {
  if (e.detail.target === state.currentPane) {
    termWrite(base64ToBytes(e.detail.data));
  }
});

// ===== リモートスクロールボタン =====
// ローカルのスワイプ/ホイールは常にxterm.js自身のスクロールバックだけを動かす(#input-area/
// input.jsには一切触れない、独立した新規UI)。PC側(herdrペインの実体、特にscrollbackを
// 持たないalternate screenのフルスクリーンTUI)を過去に遡らせたいときは、画面右下の▲▼を
// タップ/ドラッグしてPgUp/PgDnを明示送信する。
function initRemoteScrollButtons() {
  const btnUp = document.getElementById('btn-remote-scroll-up');
  const btnDown = document.getElementById('btn-remote-scroll-down');
  if (!btnUp || !btnDown) return;

  const REMOTE_SCROLL_STEP_PX = 30;
  const DRAG_THRESHOLD_PX = 10;
  const HOLD_DELAY_MS = 400;
  const HOLD_REPEAT_MS = 180;

  // input-areaの実高(キーボード表示や複数行入力で変動する)にボタン位置を追従させる。
  // input-area自体は変更禁止のため、高さを外から観測してCSS変数に流すだけ。
  const inputArea = document.querySelector('#view-detail .input-area');
  if (inputArea && window.ResizeObserver) {
    new ResizeObserver(() => {
      document.documentElement.style.setProperty('--input-area-height', inputArea.offsetHeight + 'px');
    }).observe(inputArea);
  }

  const sendPage = down => {
    if (state.currentPane) sendKeys(state.currentPane, down ? 'NPage' : 'PPage');
  };

  // down: このボタンがPgDn(下方向)用ならtrue。
  // タップ=1回送信。押したまま動かさなければ長押しリピート(HOLD_DELAY_MS後からHOLD_REPEAT_MS間隔)。
  // DRAG_THRESHOLD_PXを超えて動かしたらドラッグモードに切り替え、リピートを止めて
  // ドラッグ量ベースの送信に一本化する(二重送信防止)。
  const bindButton = (btn, down) => {
    let pressing = false;
    let dragMode = false;
    let startY = 0;
    let sentSteps = 0;
    let holdTimer = 0;
    let repeatTimer = 0;

    const clearTimers = () => {
      if (holdTimer) { clearTimeout(holdTimer); holdTimer = 0; }
      if (repeatTimer) { clearInterval(repeatTimer); repeatTimer = 0; }
    };

    const onMove = clientY => {
      if (!pressing) return;
      const dy = clientY - startY;
      if (!dragMode && Math.abs(dy) >= DRAG_THRESHOLD_PX) {
        dragMode = true;
        clearTimers();
      }
      if (!dragMode) return;
      const progressDy = down ? dy : -dy;
      const steps = Math.max(0, Math.floor(progressDy / REMOTE_SCROLL_STEP_PX)) + 1; // +1はタップ分
      while (sentSteps < steps) {
        sendPage(down);
        sentSteps++;
      }
    };
    const start = clientY => {
      pressing = true;
      dragMode = false;
      startY = clientY;
      sentSteps = 1;
      sendPage(down); // 押した瞬間に1回送信(タップ相当)
      clearTimers();
      holdTimer = setTimeout(() => {
        holdTimer = 0;
        repeatTimer = setInterval(() => sendPage(down), HOLD_REPEAT_MS);
      }, HOLD_DELAY_MS);
      btn.classList.add('remote-scroll-btn-active');
    };
    const end = () => {
      pressing = false;
      dragMode = false;
      clearTimers();
      btn.classList.remove('remote-scroll-btn-active');
    };

    btn.addEventListener('touchstart', e => {
      e.preventDefault();
      start(e.touches[0].clientY);
    }, { passive: false });
    btn.addEventListener('touchmove', e => {
      e.preventDefault();
      onMove(e.touches[0].clientY);
    }, { passive: false });
    btn.addEventListener('touchend', e => {
      e.preventDefault();
      end();
    }, { passive: false });
    btn.addEventListener('touchcancel', end);
    btn.addEventListener('contextmenu', e => e.preventDefault());
    document.addEventListener('visibilitychange', () => {
      if (document.hidden) end();
    });

    // PCブラウザでの検証・操作用にmouseにも対応
    btn.addEventListener('mousedown', e => {
      e.preventDefault();
      start(e.clientY);
      const onMouseMove = ev => onMove(ev.clientY);
      const onMouseUp = () => {
        end();
        document.removeEventListener('mousemove', onMouseMove);
        document.removeEventListener('mouseup', onMouseUp);
      };
      document.addEventListener('mousemove', onMouseMove);
      document.addEventListener('mouseup', onMouseUp);
    });
  };

  bindButton(btnUp, false);
  bindButton(btnDown, true);
}
