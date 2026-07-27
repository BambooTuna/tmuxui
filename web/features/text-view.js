'use strict';

// テキストビュー(コピー用オーバーレイ)。xterm.jsはタッチでの範囲選択を実質サポートして
// いないため、コピーしたい時だけxtermバッファ全文(termGetBufferText)をプレーンテキストの
// 全画面オーバーレイに展開する。素のDOMテキストなのでOSネイティブの長押し選択・コピーが
// そのまま使える。開いた瞬間のスナップショットであり、pane更新への追従はしない
// (選択中に内容が動くと操作を壊すため。最新を見たければ開き直す)。

function openTextView() {
  const overlay = $('text-view-overlay');
  const body = $('text-view-body');
  const text = typeof termGetBufferText === 'function' ? termGetBufferText() : '';
  body.textContent = text || '(内容がありません)';
  overlay.hidden = false;
  // 最新出力(末尾)から見たい場面が多いので下端から開始
  body.scrollTop = body.scrollHeight;
}

function closeTextView() {
  $('text-view-overlay').hidden = true;
  $('text-view-body').textContent = '';
}

async function copyTextViewAll() {
  const text = $('text-view-body').textContent;
  const btn = $('text-view-copy-all');
  try {
    await navigator.clipboard.writeText(text);
    btn.textContent = 'コピーしました';
  } catch {
    // clipboard API不可(非https等)時のフォールバック: 全選択して手動コピーを促す
    const range = document.createRange();
    range.selectNodeContents($('text-view-body'));
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
    btn.textContent = '長押しでコピー';
  }
  setTimeout(() => { btn.textContent = '全文コピー'; }, 1500);
}

function initTextView() {
  $('btn-text-view').addEventListener('click', () => {
    // ドロワー(☰)最上段からの起動: 先にドロワーを閉じてからオーバーレイ表示
    if (typeof closeDrawer === 'function') closeDrawer();
    openTextView();
  });
  $('text-view-close').addEventListener('click', closeTextView);
  $('text-view-copy-all').addEventListener('click', copyTextViewAll);
}
