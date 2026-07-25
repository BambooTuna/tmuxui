'use strict';

// ===== ANSI カラーパレット (classic / xterm 両エンジン共通の唯一の定義) =====
// 基本8色 + 明るい8色 (fg: 30-37, 90-97 / bg: 40-47, 100-107)
const PALETTE_DARK_COLORS = [
  '#555555', '#cc0000', '#4e9a06', '#c4a000',
  '#3465a4', '#75507b', '#06989a', '#d3d7cf',
  '#555753', '#ef2929', '#8ae234', '#fce94f',
  '#729fcf', '#ad7fa8', '#34e2e2', '#eeeeec',
];
const PALETTE_PASTEL_COLORS = [
  '#6e6e6e', '#a82020', '#2e7d32', '#8a6d00',
  '#1565c0', '#7b1fa2', '#00838f', '#4a4a4a',
  '#808080', '#c62828', '#388e3c', '#f9a825',
  '#1976d2', '#8e24aa', '#00acc1', '#333333',
];
// デフォルト前景/背景。classic モードも xterm.js 側と揃えるためこれを使う。
const PALETTE_BG = { dark: '#1a1a1a', pastel: '#faf6f0' };
const PALETTE_FG = { dark: '#e0e0e0', pastel: '#4a3f35' };

function paletteThemeName() {
  return document.documentElement.getAttribute('data-theme') === 'pastel' ? 'pastel' : 'dark';
}

function paletteBasicColors() {
  return paletteThemeName() === 'pastel' ? PALETTE_PASTEL_COLORS : PALETTE_DARK_COLORS;
}

function paletteHex(n) {
  return n.toString(16).padStart(2, '0');
}

let _cachedPaletteTheme = null;
let _cached256Palette = null;

// 256色パレット(0-15: 基本色, 16-231: 6x6x6キューブ, 232-255: グレースケール)
function paletteBuild256() {
  const theme = paletteThemeName();
  if (theme === _cachedPaletteTheme && _cached256Palette) return _cached256Palette;
  _cachedPaletteTheme = theme;
  const basic = paletteBasicColors();
  const p = new Array(256);
  for (let i = 0; i < 16; i++) p[i] = basic[i];
  for (let i = 0; i < 216; i++) {
    const r = Math.floor(i / 36);
    const g = Math.floor((i % 36) / 6);
    const b = i % 6;
    const toV = v => v === 0 ? 0 : v * 40 + 55;
    p[i + 16] = `#${paletteHex(toV(r))}${paletteHex(toV(g))}${paletteHex(toV(b))}`;
  }
  for (let i = 0; i < 24; i++) {
    const v = i * 10 + 8;
    p[i + 232] = `#${paletteHex(v)}${paletteHex(v)}${paletteHex(v)}`;
  }
  _cached256Palette = p;
  return p;
}

// xterm.js側と揃えるためのデフォルト色(ansi.js/term.js旧内のTERM_DEFAULT/TERM_BG/TERM_FGを統合)
const PALETTE_DEFAULT = {
  get fg() { return PALETTE_FG[paletteThemeName()]; },
  get bg() { return PALETTE_BG[paletteThemeName()]; },
};
