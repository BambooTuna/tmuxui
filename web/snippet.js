// ===== Snippet Management Sheet =====
let snippetEditTarget = null;
let snippetMenuTarget = null;

function initSnippetSheet() {
  $('snippet-sheet-overlay').addEventListener('click', e => {
    if (e.target === $('snippet-sheet-overlay')) closeSnippetSheet();
  });
  $('btn-snippet-create').addEventListener('click', startCreateSnippet);
  $('snippet-edit-back').addEventListener('click', backToSnippetList);
  $('snippet-edit-cancel').addEventListener('click', backToSnippetList);
  $('snippet-edit-save').addEventListener('click', saveSnippet);

  // Snippet item menu (edit/delete/cancel)
  $('snippet-item-menu-overlay').addEventListener('click', e => {
    if (e.target === $('snippet-item-menu-overlay')) closeSnippetItemMenu();
  });
  $('snippet-item-edit').addEventListener('click', () => {
    if (snippetMenuTarget) editSnippet(snippetMenuTarget.name);
    closeSnippetItemMenu();
  });
  $('snippet-item-delete').addEventListener('click', () => {
    if (snippetMenuTarget) deleteSnippetItem(snippetMenuTarget.name, snippetMenuTarget.label);
    closeSnippetItemMenu();
  });
  $('snippet-item-cancel').addEventListener('click', closeSnippetItemMenu);
}

function backToSnippetList() {
  $('snippet-sheet-edit').hidden = true;
  $('snippet-sheet-list').hidden = false;
}

function openSnippetSheet() {
  $('snippet-sheet-list').hidden = false;
  $('snippet-sheet-edit').hidden = true;
  $('snippet-sheet-overlay').hidden = false;
  renderSnippetSheetItems();
}

function closeSnippetSheet() {
  $('snippet-sheet-overlay').hidden = true;
}

async function renderSnippetSheetItems() {
  const el = $('snippet-sheet-items');
  el.innerHTML = '';
  try {
    const data = await apiFetch('/api/snippets');
    const snippets = data.snippets || [];
    if (!snippets.length) {
      el.innerHTML = '<div class="empty-state" style="padding:24px 0">スニペットがありません</div>';
      return;
    }
    for (const snip of snippets) {
      const row = document.createElement('div');
      row.className = 'snippet-sheet-row';
      row.innerHTML =
        `<span class="snippet-sheet-name">${esc(snip.label)}</span>` +
        `<button class="btn-card-menu" aria-label="メニュー">⋯</button>`;
      row.querySelector('.btn-card-menu').addEventListener('click', e => {
        e.stopPropagation();
        openSnippetItemMenu(snip);
      });
      el.appendChild(row);
    }
  } catch {}
}

function openSnippetItemMenu(snip) {
  snippetMenuTarget = snip;
  $('snippet-item-menu-overlay').hidden = false;
}

function closeSnippetItemMenu() {
  $('snippet-item-menu-overlay').hidden = true;
  snippetMenuTarget = null;
}

async function editSnippet(name) {
  snippetEditTarget = name;
  try {
    const data = await apiFetch(`/api/snippets/${encodeURIComponent(name)}`);
    $('snippet-edit-name').value = name;
    $('snippet-edit-content').value = data.content || '';
  } catch {
    $('snippet-edit-name').value = name;
    $('snippet-edit-content').value = '';
  }
  $('snippet-sheet-list').hidden = true;
  $('snippet-sheet-edit').hidden = false;
}

async function saveSnippet() {
  const name = $('snippet-edit-name').value.trim();
  const content = $('snippet-edit-content').value;
  if (!name) return;
  try {
    if (snippetEditTarget) {
      await apiFetch(`/api/snippets/${encodeURIComponent(snippetEditTarget)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, content }),
      });
    } else {
      await apiFetch('/api/snippets', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, content }),
      });
    }
  } catch {}
  backToSnippetList();
  renderSnippetSheetItems();
}

async function deleteSnippetItem(name, label) {
  const ok = await showModal({
    message: `"${label}" を削除しますか？`,
    okLabel: '削除',
    okDanger: true,
  });
  if (!ok) return;
  try {
    await apiFetch(`/api/snippets/${encodeURIComponent(name)}`, { method: 'DELETE' });
  } catch {}
  renderSnippetSheetItems();
}

function startCreateSnippet() {
  snippetEditTarget = null;
  $('snippet-edit-name').value = '';
  $('snippet-edit-content').value = '';
  $('snippet-sheet-list').hidden = true;
  $('snippet-sheet-edit').hidden = false;
  $('snippet-edit-name').focus();
}
