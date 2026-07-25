'use strict';

// ===== オーバーレイ共通ヘルパー =====
// 「hidden切替 + オーバーレイ自身のクリックで閉じる」という同じパターンが
// カードメニュー/Windowメニュー/各種シート/ドロワーなど9箇所に複製されていたのをここに集約する。

// オーバーレイ要素の外枠(自分自身)がクリックされたときだけ onClose を呼ぶ。
// unbind関数を返すので、showModal のように呼び出しごとに動的な close 処理を
// 差し替えたい場合はそのまま使える。
function bindOverlayOutsideClick(el, onClose) {
  const handler = e => { if (e.target === el) onClose(); };
  el.addEventListener('click', handler);
  return () => el.removeEventListener('click', handler);
}

// 静的な show/hide だけで完結するオーバーレイ(メニュー・シート類)用のファクトリ。
function createOverlay(overlayId, { onShow, onHide } = {}) {
  const el = $(overlayId);

  function hide() {
    el.hidden = true;
    if (onHide) onHide();
  }

  function show() {
    if (onShow) onShow();
    el.hidden = false;
  }

  bindOverlayOutsideClick(el, hide);

  return { el, show, hide };
}

// ドロワー(スライドイン式パネル)用。hidden切替に加えてパネル自身へのクラス付け外しが必要。
function createDrawer(overlayId, panelId, openClass = 'open') {
  const overlayEl = $(overlayId);
  const panelEl = $(panelId);

  function hide() {
    overlayEl.hidden = true;
    panelEl.classList.remove(openClass);
  }

  function show() {
    overlayEl.hidden = false;
    panelEl.classList.add(openClass);
  }

  bindOverlayOutsideClick(overlayEl, hide);

  return { overlayEl, panelEl, show, hide };
}

// ===== 汎用モーダル(確認/入力ダイアログ) =====
function showModal({ message, input, inputValue, input2, input2Placeholder, okLabel, okDanger }) {
  return new Promise(resolve => {
    $('modal-message').textContent = message;
    const inp = $('modal-input');
    const inp2 = $('modal-input2');
    const okBtn = $('modal-ok');
    if (input) {
      inp.hidden = false;
      inp.value = inputValue || '';
    } else {
      inp.hidden = true;
    }
    if (input2) {
      inp2.hidden = false;
      inp2.value = '';
      inp2.placeholder = input2Placeholder || '';
    } else {
      inp2.hidden = true;
    }
    okBtn.textContent = okLabel || 'OK';
    okBtn.style.background = okDanger ? 'var(--error)' : 'var(--accent)';
    $('modal-overlay').hidden = false;
    if (input) inp.focus();

    let unbindOverlay = () => {};

    function cleanup() {
      $('modal-overlay').hidden = true;
      okBtn.removeEventListener('click', onOk);
      $('modal-cancel').removeEventListener('click', onCancel);
      unbindOverlay();
      inp.removeEventListener('keydown', onKey);
      inp2.removeEventListener('keydown', onKey);
    }
    function onOk() {
      cleanup();
      if (input2) {
        resolve({ value: inp.value.trim(), value2: inp2.value.trim() });
      } else {
        resolve(input ? inp.value.trim() : true);
      }
    }
    function onCancel() {
      cleanup();
      resolve(null);
    }
    function onKey(e) {
      if (e.key === 'Enter') { e.preventDefault(); onOk(); }
    }
    okBtn.addEventListener('click', onOk);
    $('modal-cancel').addEventListener('click', onCancel);
    unbindOverlay = bindOverlayOutsideClick($('modal-overlay'), onCancel);
    if (input) inp.addEventListener('keydown', onKey);
    if (input2) inp2.addEventListener('keydown', onKey);
  });
}
