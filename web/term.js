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
    scrollback: 5000,
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
