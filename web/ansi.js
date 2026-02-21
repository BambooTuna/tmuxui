// ANSI SGR to HTML converter
// XSS対策: ANSIシーケンス以外のテキストはすべてHTMLエスケープする

(function () {
  // 基本8色 + 明るい8色 (fg: 30-37, 90-97 / bg: 40-47, 100-107)
  // 注: index 0 の黒はダークテーマ(#1a1a1a)で見えるよう #555 にしている
  const BASIC_COLORS = [
    '#555555', '#cc0000', '#4e9a06', '#c4a000',
    '#3465a4', '#75507b', '#06989a', '#d3d7cf',
    '#555753', '#ef2929', '#8ae234', '#fce94f',
    '#729fcf', '#ad7fa8', '#34e2e2', '#eeeeec',
  ];

  // 256色パレット生成
  const PALETTE_256 = (() => {
    const p = new Array(256);
    // 0-15: 基本色
    for (let i = 0; i < 16; i++) p[i] = BASIC_COLORS[i];
    // 16-231: 6x6x6 カラーキューブ
    for (let i = 0; i < 216; i++) {
      const r = Math.floor(i / 36);
      const g = Math.floor((i % 36) / 6);
      const b = i % 6;
      const toV = v => v === 0 ? 0 : v * 40 + 55;
      p[i + 16] = `#${toHex(toV(r))}${toHex(toV(g))}${toHex(toV(b))}`;
    }
    // 232-255: グレースケール
    for (let i = 0; i < 24; i++) {
      const v = i * 10 + 8;
      p[i + 232] = `#${toHex(v)}${toHex(v)}${toHex(v)}`;
    }
    return p;
  })();

  function toHex(n) {
    return n.toString(16).padStart(2, '0');
  }

  function htmlEscape(str) {
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  // 現在のスタイル状態から style 文字列を生成
  function buildStyle(state) {
    const parts = [];
    if (state.fg) {
      parts.push(`color:${state.fg}`);
    }
    // dim: opacity だと背景やスタッキングコンテキストに副作用があるため
    // 色の明度を下げる代わりに filter で対応
    if (state.dim && !state.bold) {
      parts.push('filter:brightness(0.5)');
    }
    if (state.bg) parts.push(`background:${state.bg}`);
    if (state.bold) parts.push('font-weight:bold');
    if (state.italic) parts.push('font-style:italic');
    const dec = [];
    if (state.underline) dec.push('underline');
    if (state.strike) dec.push('line-through');
    if (dec.length) parts.push(`text-decoration:${dec.join(' ')}`);
    return parts.join(';');
  }

  // params配列から色を読み取り、消費した要素数を返す
  // mode=38/48のとき次の要素を確認して色を決定
  function readColor(params, idx) {
    const mode = params[idx + 1];
    if (mode === 5) {
      // 256色
      const n = params[idx + 2];
      if (n >= 0 && n <= 255) {
        return { color: PALETTE_256[n], skip: 2 };
      }
      return { color: null, skip: 2 };
    } else if (mode === 2) {
      // RGB
      const r = params[idx + 2];
      const g = params[idx + 3];
      const b = params[idx + 4];
      if (r >= 0 && r <= 255 && g >= 0 && g <= 255 && b >= 0 && b <= 255) {
        return { color: `#${toHex(r)}${toHex(g)}${toHex(b)}`, skip: 4 };
      }
      return { color: null, skip: 4 };
    }
    return { color: null, skip: 0 };
  }

  function applyParams(params, state) {
    let i = 0;
    while (i < params.length) {
      const p = params[i];
      if (p === 0) {
        state.fg = null; state.bg = null;
        state.bold = false; state.dim = false;
        state.italic = false; state.underline = false; state.strike = false;
      } else if (p === 1) { state.bold = true; }
      else if (p === 2) { state.dim = true; }
      else if (p === 3) { state.italic = true; }
      else if (p === 4) { state.underline = true; }
      else if (p === 9) { state.strike = true; }
      else if (p >= 30 && p <= 37) { state.fg = BASIC_COLORS[p - 30]; }
      else if (p === 38) {
        const { color, skip } = readColor(params, i);
        if (color) state.fg = color;
        i += skip;
      }
      else if (p === 39) { state.fg = null; }
      else if (p >= 40 && p <= 47) { state.bg = BASIC_COLORS[p - 40]; }
      else if (p === 48) {
        const { color, skip } = readColor(params, i);
        if (color) state.bg = color;
        i += skip;
      }
      else if (p === 49) { state.bg = null; }
      else if (p >= 90 && p <= 97) { state.fg = BASIC_COLORS[p - 90 + 8]; }
      else if (p >= 100 && p <= 107) { state.bg = BASIC_COLORS[p - 100 + 8]; }
      i++;
    }
  }

  // SGRシーケンスの正規表現: \x1b[ で始まり m で終わるもの
  // それ以外のエスケープシーケンスも読み飛ばす
  const RE = /\x1b(?:\[([0-9;]*)m|\[[^a-zA-Z]*[a-zA-Z]|[^[])/g;

  function ansiToHtml(text) {
    const state = {
      fg: null, bg: null,
      bold: false, dim: false, italic: false, underline: false, strike: false,
    };

    let result = '';
    let openSpan = false;
    let lastIndex = 0;

    RE.lastIndex = 0;
    let match;

    while ((match = RE.exec(text)) !== null) {
      // マッチ前のプレーンテキストをエスケープして追加
      if (match.index > lastIndex) {
        result += htmlEscape(text.slice(lastIndex, match.index));
      }
      lastIndex = RE.lastIndex;

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

      applyParams(params, state);

      // スタイルがあれば新しいspanを開く
      const style = buildStyle(state);
      if (style) {
        result += `<span style="${style}">`;
        openSpan = true;
      }
    }

    // 残りのテキスト
    if (lastIndex < text.length) {
      result += htmlEscape(text.slice(lastIndex));
    }

    if (openSpan) result += '</span>';

    return result;
  }

  window.ansiToHtml = ansiToHtml;
})();
