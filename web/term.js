// xterm.js表示レイヤー。xtermEnabled()がfalseの間は一切マウントしない(ロールバック用)。
// vendor UMDビルドのグローバル名: xterm.js本体は window.Terminal (フラットexport)、
// addon-fitは window.FitAddon.FitAddon (factoryが{FitAddon:class}を返す実装のため入れ子)。

// ansi.js内の非公開パレットをxterm.js用に複製(ansi.jsはwindow.ansiToHtmlしか公開していないため)
const TERM_DARK_COLORS = [
  '#555555', '#cc0000', '#4e9a06', '#c4a000',
  '#3465a4', '#75507b', '#06989a', '#d3d7cf',
  '#555753', '#ef2929', '#8ae234', '#fce94f',
  '#729fcf', '#ad7fa8', '#34e2e2', '#eeeeec',
];
const TERM_PASTEL_COLORS = [
  '#6e6e6e', '#a82020', '#2e7d32', '#8a6d00',
  '#1565c0', '#7b1fa2', '#00838f', '#4a4a4a',
  '#808080', '#c62828', '#388e3c', '#f9a825',
  '#1976d2', '#8e24aa', '#00acc1', '#333333',
];
const TERM_BG = { dark: '#1a1a1a', pastel: '#faf6f0' };
const TERM_FG = { dark: '#e0e0e0', pastel: '#4a3f35' };

let terminal = null;
let fitAddon = null;

function termThemeName() {
  return document.documentElement.getAttribute('data-theme') === 'pastel' ? 'pastel' : 'dark';
}

function termBuildTheme() {
  const name = termThemeName();
  const c = name === 'pastel' ? TERM_PASTEL_COLORS : TERM_DARK_COLORS;
  return {
    background: TERM_BG[name], foreground: TERM_FG[name], cursor: TERM_FG[name],
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

// ===== コピー用オーバーレイ =====
// xterm.jsのcanvasセレクションはモバイルの長押しUXが不安定なので、バッファをプレーンテキスト化して
// OSネイティブに選択・コピーできる<pre>に流し込む。ボタン一発の一括コピーも用意。
function termGetBufferText(includeScrollback) {
  if (!terminal) return '';
  const buf = terminal.buffer.active;
  const start = includeScrollback ? 0 : buf.viewportY;
  const end = includeScrollback ? (buf.length) : (buf.viewportY + terminal.rows);
  const lines = [];
  for (let y = start; y < end; y++) {
    const line = buf.getLine(y);
    if (!line) continue;
    lines.push(line.translateToString(true));
  }
  // 末尾の空行を刈る
  while (lines.length && lines[lines.length - 1] === '') lines.pop();
  return lines.join('\n');
}

async function writeClipboard(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {}
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    return ok;
  } catch {
    return false;
  }
}

function refreshPaneCopyText() {
  const pre = document.getElementById('pane-copy-text');
  const cb = document.getElementById('pane-copy-include-scrollback');
  if (!pre) return;
  pre.textContent = termGetBufferText(!!(cb && cb.checked));
}

function openPaneCopyOverlay() {
  const overlay = document.getElementById('pane-copy-overlay');
  if (!overlay || !terminal) return;
  refreshPaneCopyText();
  overlay.hidden = false;
}

function closePaneCopyOverlay() {
  const overlay = document.getElementById('pane-copy-overlay');
  if (!overlay) return;
  overlay.hidden = true;
  const toast = document.getElementById('pane-copy-toast');
  if (toast) toast.hidden = true;
}

function showPaneCopyToast(msg) {
  const toast = document.getElementById('pane-copy-toast');
  if (!toast) return;
  toast.textContent = msg || 'コピーしました';
  toast.hidden = false;
  clearTimeout(showPaneCopyToast._t);
  showPaneCopyToast._t = setTimeout(() => { toast.hidden = true; }, 1500);
}

function initPaneCopyUI() {
  const btn = document.getElementById('btn-pane-copy');
  const overlay = document.getElementById('pane-copy-overlay');
  const closeBtn = document.getElementById('pane-copy-close');
  const allBtn = document.getElementById('pane-copy-all-btn');
  const cb = document.getElementById('pane-copy-include-scrollback');
  if (!btn || !overlay) return;

  btn.addEventListener('click', () => openPaneCopyOverlay());
  if (closeBtn) closeBtn.addEventListener('click', closePaneCopyOverlay);
  overlay.addEventListener('click', e => {
    if (e.target === overlay) closePaneCopyOverlay();
  });
  if (cb) cb.addEventListener('change', refreshPaneCopyText);
  if (allBtn) allBtn.addEventListener('click', async () => {
    const text = termGetBufferText(!!(cb && cb.checked));
    const ok = await writeClipboard(text);
    showPaneCopyToast(ok ? 'コピーしました' : 'コピーに失敗しました');
  });
}

document.addEventListener('DOMContentLoaded', initPaneCopyUI);

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

document.addEventListener('DOMContentLoaded', initRemoteScrollButtons);
