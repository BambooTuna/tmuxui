'use strict';

// classic(従来)描画エンジン。ANSI SGR を HTML に変換して #pane-content に書き込む。
// XSS対策: ANSIシーケンス以外のテキストはすべてHTMLエスケープする。
// パレットは render/palette.js の定義を参照する(classic/xterm共通)。

function ansiHtmlEscape(str) {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// 現在のスタイル状態から style 文字列を生成
// xtermのdrawBoldTextInBrightColors(既定true)相当: 基本色0-7がboldなら明色8-15に差し替える
function ansiResolveFg(styleState) {
  if (!styleState.fg) return null;
  if (!styleState.bold) return styleState.fg;
  const idx = styleState.basicFgIndex;
  if (typeof idx !== 'number' || idx < 0 || idx > 7) return styleState.fg;
  return paletteBasicColors()[idx + 8];
}

function ansiBuildStyle(styleState) {
  const parts = [];
  let fg = ansiResolveFg(styleState);
  let bg = styleState.bg;
  // reverse: 前景/背景を入れ替える(xtermと同じ)。未指定はデフォルト色を使う。
  if (styleState.reverse) {
    const defFg = PALETTE_DEFAULT.fg;
    const defBg = PALETTE_DEFAULT.bg;
    const f = fg || defFg;
    const b = bg || defBg;
    fg = b;
    bg = f;
  }
  if (fg) parts.push(`color:${fg}`);
  // dim: opacity だと背景やスタッキングコンテキストに副作用があるため
  // 色の明度を下げる代わりに filter で対応
  if (styleState.dim && !styleState.bold) {
    parts.push('filter:brightness(0.5)');
  }
  if (bg) parts.push(`background:${bg}`);
  if (styleState.bold) parts.push('font-weight:bold');
  if (styleState.italic) parts.push('font-style:italic');
  const dec = [];
  if (styleState.underline) dec.push('underline');
  if (styleState.strike) dec.push('line-through');
  if (dec.length) parts.push(`text-decoration:${dec.join(' ')}`);
  return parts.join(';');
}

// params配列から色を読み取り、消費した要素数を返す
// mode=38/48のとき次の要素を確認して色を決定
function ansiReadColor(params, idx) {
  const mode = params[idx + 1];
  if (mode === 5) {
    // 256色
    const n = params[idx + 2];
    if (n >= 0 && n <= 255) {
      return { color: paletteBuild256()[n], skip: 2 };
    }
    return { color: null, skip: 2 };
  } else if (mode === 2) {
    // RGB
    const r = params[idx + 2];
    const g = params[idx + 3];
    const b = params[idx + 4];
    if (r >= 0 && r <= 255 && g >= 0 && g <= 255 && b >= 0 && b <= 255) {
      return { color: `#${paletteHex(r)}${paletteHex(g)}${paletteHex(b)}`, skip: 4 };
    }
    return { color: null, skip: 4 };
  }
  return { color: null, skip: 0 };
}

function ansiApplyParams(params, styleState) {
  let i = 0;
  while (i < params.length) {
    const p = params[i];
    if (p === 0) {
      styleState.fg = null; styleState.bg = null; styleState.basicFgIndex = null;
      styleState.bold = false; styleState.dim = false;
      styleState.italic = false; styleState.underline = false; styleState.strike = false;
      styleState.reverse = false;
    } else if (p === 1) { styleState.bold = true; }
    else if (p === 2) { styleState.dim = true; }
    else if (p === 3) { styleState.italic = true; }
    else if (p === 4) { styleState.underline = true; }
    else if (p === 7) { styleState.reverse = true; }
    else if (p === 9) { styleState.strike = true; }
    else if (p === 22) { styleState.bold = false; styleState.dim = false; }
    else if (p === 23) { styleState.italic = false; }
    else if (p === 24) { styleState.underline = false; }
    else if (p === 27) { styleState.reverse = false; }
    else if (p === 29) { styleState.strike = false; }
    else if (p >= 30 && p <= 37) { styleState.fg = paletteBasicColors()[p - 30]; styleState.basicFgIndex = p - 30; }
    else if (p === 38) {
      const { color, skip } = ansiReadColor(params, i);
      if (color) { styleState.fg = color; styleState.basicFgIndex = null; }
      i += skip;
    }
    else if (p === 39) { styleState.fg = null; styleState.basicFgIndex = null; }
    else if (p >= 40 && p <= 47) { styleState.bg = paletteBasicColors()[p - 40]; }
    else if (p === 48) {
      const { color, skip } = ansiReadColor(params, i);
      if (color) styleState.bg = color;
      i += skip;
    }
    else if (p === 49) { styleState.bg = null; }
    else if (p >= 90 && p <= 97) { styleState.fg = paletteBasicColors()[p - 90 + 8]; styleState.basicFgIndex = null; }
    else if (p >= 100 && p <= 107) { styleState.bg = paletteBasicColors()[p - 100 + 8]; }
    i++;
  }
}

// SGRシーケンスの正規表現: \x1b[ で始まり m で終わるもの
// それ以外のエスケープシーケンスも読み飛ばす
const ANSI_SGR_RE = /\x1b(?:\[([0-9;]*)m|\[[^a-zA-Z]*[a-zA-Z]|[^[])/g;

function ansiToHtml(text) {
  const styleState = {
    fg: null, bg: null, basicFgIndex: null,
    bold: false, dim: false, italic: false, underline: false, strike: false,
    reverse: false,
  };

  let result = '';
  let openSpan = false;
  let lastIndex = 0;

  ANSI_SGR_RE.lastIndex = 0;
  let match;

  while ((match = ANSI_SGR_RE.exec(text)) !== null) {
    // マッチ前のプレーンテキストをエスケープして追加
    if (match.index > lastIndex) {
      result += ansiHtmlEscape(text.slice(lastIndex, match.index));
    }
    lastIndex = ANSI_SGR_RE.lastIndex;

    // SGR (\x1b[...m) 以外は無視
    if (match[1] === undefined) continue;

    // 現在のspanを閉じる
    if (openSpan) {
      result += '</span>';
      openSpan = false;
    }

    // パラメータを解析 (空文字列は [0] 扱い)
    const raw = match[1];
    const params = raw === ''
      ? [0]
      : raw.split(';').map(s => parseInt(s, 10) || 0);

    ansiApplyParams(params, styleState);

    // スタイルがあれば新しいspanを開く
    const style = ansiBuildStyle(styleState);
    if (style) {
      result += `<span style="${style}">`;
      openSpan = true;
    }
  }

  // 残りのテキスト
  if (lastIndex < text.length) {
    result += ansiHtmlEscape(text.slice(lastIndex));
  }

  if (openSpan) result += '</span>';

  return result;
}

// ===== #pane-content への描画 (classicモード) =====
// 取得したherdr履歴を .pane-content-inner でwrapし、内容が短い時は下寄せ・
// 長い時は自然に上へオーバーフローさせる(スクロール可能)。ターミナル的な
// 「最新出力が下、古いのが上」の見た目にモバイルでもなる。
function renderPaneContent(content) {
  const el = $('pane-content');
  el.classList.add('has-classic');   // xterm時にセットされる可能性のあるスタイル環境と分離するための明示マーカ
  const atBottom = el.scrollHeight - el.scrollTop <= el.clientHeight + 60;
  el.innerHTML = `<div class="pane-content-inner">${ansiToHtml(content)}</div>`;
  if (atBottom) el.scrollTop = el.scrollHeight;
  bus.emit('render:pane-content-applied');
}

// transport/ws.js からの通知。classicモード時のみ描画する(xterm時は無視)。
bus.on('ws:pane_content', e => {
  if (!xtermEnabled() && e.detail.target === state.currentPane) {
    renderPaneContent(e.detail.content || '');
  }
});
