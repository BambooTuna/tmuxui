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
  termSyncScrollLock();
}

function termWrite(bytes) {
  if (!terminal) return;
  terminal.write(bytes);
  termSyncScrollLock();
}

// termBufferType()==='alternate'(スクロールバックが無く、input.jsのinitScrollForward()が
// スワイプ/ホイールをPgUp/PgDn転送に回す状態)の間は、xterm.js自身の.xterm-viewportの
// ネイティブスクロールを止める。止めないと「PC側へのページ送り」とxterm.js側の見た目上の
// スクロールが同時に起きる二重スクロールになる(転送先には何も表示が追従しないローカルの
// スクロールだけが残ってズレる)。通常のスクロールバックありペインでは何もしない。
// .xterm-viewportだけでなく#pane-content自体もロック対象にする: ペインの実サイズ(サーバー側の
// 行数)がコンテナの表示可能高さを超えるとき、#pane-content自体がoverflow-y:autoで独自に
// スクロール可能になっており、.xterm-viewportだけ止めてもこちらが「ちょっとだけ」動いてしまう。
// ロックはscrollTopを0(先頭)に固定するのではなく末尾へ寄せる: 固定前にscrollTop=0のままだと
// はみ出た分(ペインの実サイズ-コンテナ高さ)だけ末尾(最新行)が常に見切れてしまうため。
function termSyncScrollLock() {
  if (!terminal || !terminal.element) return;
  const viewport = terminal.element.querySelector('.xterm-viewport');
  const container = document.getElementById('pane-content');
  const locked = termBufferType() === 'alternate';
  if (viewport) {
    viewport.classList.toggle('term-scroll-locked', locked);
    if (locked) viewport.scrollTop = viewport.scrollHeight;
  }
  if (container) {
    container.classList.toggle('term-scroll-locked', locked);
    if (locked) container.scrollTop = container.scrollHeight;
  }
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

// input.jsのinitScrollForward()がローカルスクロール可否の判定にのみ使う。alternate画面
// バッファ(フルスクリーンTUI)はxterm.jsが自力で検知できるが、herdrバックエンド経由では
// pane.readがモード切替シーケンス自体を運ばない(画面クリア+全量再描画で都度書き込むだけの
// ため、\x1b[?1049h相当が来ない)ためbuffer.active.typeが常に'normal'のままになり、Claude Code
// 等のフルスクリーンTUI(herdr側にもスクロールバックが存在しない: 実機確認済み)でスワイプしても
// ローカルにスクロールする内容が無く「見えている範囲から出られない」。baseY===0(遡れる行が無い)
// の場合もalternate相当として扱い、initScrollForward()側でPgUp/PgDn転送(TUI側のページ送り)に
// フォールバックできるようにする。
function termBufferType() {
  if (!terminal) return 'normal';
  const buf = terminal.buffer.active;
  if (buf.type === 'alternate' || buf.baseY === 0) return 'alternate';
  return 'normal';
}
