'use strict';

// セッションごとのファイラー状態を保持
const filerSessions = {};

function getFilerState(sessionName) {
  if (!filerSessions[sessionName]) {
    filerSessions[sessionName] = {
      rootPath: '',
      currentPath: '',
      history: [],
      viewing: 'list', // 'list' | 'file'
      active: false,
    };
  }
  return filerSessions[sessionName];
}

function initFilerForSession(sessionName, panePath) {
  const fs = getFilerState(sessionName);
  if (panePath && fs.rootPath !== panePath) {
    // pane が変わった (or 初回) → 現 pane の CWD にリセット
    fs.rootPath = panePath;
    fs.currentPath = panePath;
    fs.history = [];
    fs.viewing = 'list';
  }
}

function toggleFiler() {
  const sessionName = state.currentSession;
  if (!sessionName) return;

  const pane = findPane(sessionName, state.currentWindow, state.currentPane);

  initFilerForSession(sessionName, pane?.path || '');
  const fs = getFilerState(sessionName);
  fs.active = !fs.active;

  if (fs.active) {
    showFilerPanel(fs);
  } else {
    hideFilerPanel();
  }
  $('btn-filer').classList.toggle('active', fs.active);
}

function showFilerPanel(fs) {
  $('pane-content').hidden = true;
  $('pane-tabs').hidden = true;
  document.querySelector('.input-area').hidden = true;
  $('filer-panel').hidden = false;

  if (fs.viewing === 'list') {
    // DOM が前 session/pane のまま残ってるので毎回 reload する
    loadFilerDir(fs.currentPath || fs.rootPath);
  }
}

function hideFilerPanel() {
  $('filer-panel').hidden = true;
  $('pane-content').hidden = false;
  $('pane-tabs').hidden = false;
  document.querySelector('.input-area').hidden = false;
}

function currentFilerState() {
  if (!state.currentSession) return null;
  return getFilerState(state.currentSession);
}

// /api/filer/{raw,download} の URL 組み立てを1箇所に集約。
function filerUrl(endpoint, filePath, rootPath) {
  return `/api/filer/${endpoint}?path=${encodeURIComponent(filePath)}&root=${encodeURIComponent(rootPath)}&token=${encodeURIComponent(state.token)}`;
}

async function loadFilerDir(path) {
  const fs = currentFilerState();
  if (!fs) return;
  fs.viewing = 'list';
  try {
    const data = await apiFetch(`/api/filer/list?path=${encodeURIComponent(path)}&root=${encodeURIComponent(fs.rootPath)}`);
    fs.currentPath = data.path;
    renderFilerPath();
    renderFilerList(data.entries, data.path);
  } catch (e) {
    $('filer-content').textContent = `(読み込み失敗: ${e.message})`;
  }
}

async function loadFilerFile(filePath) {
  const fs = currentFilerState();
  if (!fs) return;
  fs.viewing = 'file';
  renderFilerPath();

  const el = $('filer-content');
  el.innerHTML = '';

  const mediaType = filerMediaType(filePath);

  if (mediaType === 'image') {
    const rawUrl = filerUrl('raw', filePath, fs.rootPath);
    const img = document.createElement('img');
    img.className = 'filer-image-preview';
    img.alt = filePath.split('/').pop();
    img.loading = 'lazy';
    img.decoding = 'async';
    img.src = rawUrl;
    img.addEventListener('click', () => { window.location.href = rawUrl; });
    img.onerror = () => {
      el.innerHTML = '';
      const msg = document.createElement('div');
      msg.className = 'filer-binary-msg';
      msg.textContent = '画像を読み込めませんでした';
      el.appendChild(msg);
    };
    el.appendChild(img);
    el.appendChild(filerDownloadBtn(filePath));
    return;
  }

  if (mediaType === 'video') {
    const rawUrl = filerUrl('raw', filePath, fs.rootPath);
    const video = document.createElement('video');
    video.className = 'filer-video-preview';
    video.controls = true;
    video.playsInline = true;
    video.preload = 'metadata';
    video.src = rawUrl;
    el.appendChild(video);
    el.appendChild(filerDownloadBtn(filePath));
    return;
  }

  if (mediaType === 'audio') {
    const rawUrl = filerUrl('raw', filePath, fs.rootPath);
    const wrap = document.createElement('div');
    wrap.className = 'filer-audio-wrap';
    const icon = document.createElement('div');
    icon.className = 'filer-audio-icon';
    icon.innerHTML = '&#127925;';
    const name = document.createElement('div');
    name.className = 'filer-pdf-name';
    name.textContent = filePath.split('/').pop();
    const audio = document.createElement('audio');
    audio.controls = true;
    audio.preload = 'metadata';
    audio.src = rawUrl;
    wrap.appendChild(icon);
    wrap.appendChild(name);
    wrap.appendChild(audio);
    el.appendChild(wrap);
    el.appendChild(filerDownloadBtn(filePath));
    return;
  }

  if (mediaType === 'pdf') {
    const pdfUrl = filerUrl('raw', filePath, fs.rootPath);
    const wrap = document.createElement('div');
    wrap.className = 'filer-pdf-fallback';
    wrap.innerHTML =
      `<div class="filer-pdf-icon">&#128220;</div>` +
      `<div class="filer-pdf-name">${esc(filePath.split('/').pop())}</div>` +
      `<button class="filer-pdf-open">PDFを開く</button>`;
    wrap.querySelector('.filer-pdf-open').addEventListener('click', () => {
      window.location.href = pdfUrl;
    });
    el.appendChild(wrap);
    el.appendChild(filerDownloadBtn(filePath));
    return;
  }

  el.textContent = '読み込み中...';
  try {
    const data = await apiFetch(`/api/filer/read?path=${encodeURIComponent(filePath)}&root=${encodeURIComponent(fs.rootPath)}`);
    el.innerHTML = '';
    if (data.binary) {
      const msg = document.createElement('div');
      msg.className = 'filer-binary-msg';
      msg.textContent = data.reason === 'file too large'
        ? `ファイルが大きすぎます (${filerFormatSize(data.size)})`
        : `バイナリファイルです (${filerFormatSize(data.size)})`;
      el.appendChild(msg);
    } else {
      const pre = document.createElement('div');
      pre.className = 'filer-file-content';
      pre.textContent = data.content;
      el.appendChild(pre);
    }
    // アクションボタン群 (html は「プレビュー」を追加)
    const actions = document.createElement('div');
    actions.className = 'filer-file-actions';
    const ext = (filePath.match(/\.[^.]+$/) || [''])[0].toLowerCase();
    if (ext === '.html' || ext === '.htm') {
      const previewUrl = filerUrl('raw', filePath, fs.rootPath);
      const previewBtn = document.createElement('a');
      previewBtn.href = previewUrl;
      previewBtn.target = '_blank';
      previewBtn.rel = 'noopener noreferrer';
      previewBtn.className = 'filer-preview-btn';
      previewBtn.textContent = 'ブラウザでプレビュー';
      actions.appendChild(previewBtn);
    }
    actions.appendChild(filerDownloadBtn(filePath));
    el.appendChild(actions);
  } catch (e) {
    el.textContent = `(読み込み失敗: ${e.message})`;
  }
}

function renderFilerPath() {
  const fs = currentFilerState();
  if (!fs) return;
  const display = fs.currentPath || '/';
  // ルートパスからの相対表示
  if (fs.rootPath && display.startsWith(fs.rootPath)) {
    const rel = display.slice(fs.rootPath.length);
    $('filer-path').textContent = rel || '/';
  } else {
    $('filer-path').textContent = display;
  }
  // ファイル表示中 or ルートより深い階層なら戻れる
  const canGoUp = fs.viewing === 'file' || (fs.currentPath && fs.currentPath !== fs.rootPath);
  $('btn-filer-up').disabled = !canGoUp;
}

function renderFilerList(entries, dirPath) {
  const el = $('filer-content');
  el.innerHTML = '';

  for (const entry of entries) {
    const row = document.createElement('div');
    row.className = 'filer-entry';
    const icon = entry.isDir ? '&#9656;' : filerFileIcon(entry.name);
    const nameClass = entry.isDir ? 'filer-entry-name dir' : 'filer-entry-name';
    const sizeText = entry.isDir ? '' : filerFormatSize(entry.size);
    const gitBadge = filerGitBadge(entry.gitStatus);
    row.innerHTML =
      `<span class="filer-entry-icon">${icon}</span>` +
      `<span class="${nameClass}">${esc(entry.name)}</span>` +
      gitBadge +
      (sizeText ? `<span class="filer-entry-size">${sizeText}</span>` : '');

    if (entry.isDir) {
      row.addEventListener('click', () => {
        const fs = currentFilerState();
        if (fs) fs.history.push(dirPath);
        loadFilerDir(dirPath + '/' + entry.name);
      });
    } else {
      row.addEventListener('click', () => {
        const fs = currentFilerState();
        if (fs) fs.history.push(dirPath);
        loadFilerFile(dirPath + '/' + entry.name);
      });
    }
    el.appendChild(row);
  }

  if (!entries.length) {
    const empty = document.createElement('p');
    empty.className = 'empty-state';
    empty.textContent = '空のディレクトリ';
    el.appendChild(empty);
  }
}

function filerGitBadge(status) {
  if (!status) return '';
  const map = {
    'M': ['変更', 'warn'],
    '?': ['未追跡', 'error'],
    'A': ['追加', 'success'],
    'D': ['削除', 'error'],
    'MM': ['変更+', 'warn'],
    'R': ['名前変更', 'accent'],
  };
  const m = map[status] || [status, 'muted'];
  return `<span class="filer-git-badge filer-git-${m[1]}">${m[0]}</span>`;
}

function filerMediaType(filename) {
  const ext = (filename.match(/\.[^.]+$/) || [''])[0].toLowerCase();
  if (['.png', '.jpg', '.jpeg', '.gif', '.webp', '.bmp', '.ico', '.svg'].includes(ext)) return 'image';
  if (ext === '.pdf') return 'pdf';
  if (['.mp4', '.mov', '.webm', '.m4v'].includes(ext)) return 'video';
  if (['.mp3', '.wav', '.m4a', '.ogg', '.aac'].includes(ext)) return 'audio';
  return null;
}

function filerFileIcon(filename) {
  const ext = (filename.match(/\.[^.]+$/) || [''])[0].toLowerCase();
  // 色付きドット: ファイル種別ごとに色分け
  const colorMap = {
    '.js': '#f7df1e', '.ts': '#3178c6', '.jsx': '#61dafb', '.tsx': '#3178c6',
    '.go': '#00add8', '.py': '#3776ab', '.rb': '#cc342d', '.rs': '#dea584',
    '.c': '#555', '.cpp': '#f34b7d', '.h': '#555',
    '.java': '#b07219', '.kt': '#a97bff', '.swift': '#f05138',
    '.sh': '#89e051', '.bash': '#89e051', '.zsh': '#89e051',
    '.html': '#e34c26', '.css': '#563d7c', '.htm': '#e34c26', '.scss': '#c6538c',
    '.json': '#a1a1a1', '.yaml': '#cb171e', '.yml': '#cb171e',
    '.toml': '#9c4221', '.xml': '#0060ac', '.ini': '#a1a1a1',
    '.md': '#083fa1', '.txt': '#a1a1a1', '.log': '#a1a1a1',
    '.png': '#a473b6', '.jpg': '#a473b6', '.jpeg': '#a473b6',
    '.gif': '#a473b6', '.webp': '#a473b6', '.svg': '#ffb13b',
    '.zip': '#e6b800', '.tar': '#e6b800', '.gz': '#e6b800',
    '.mp3': '#fb5c74', '.wav': '#fb5c74', '.m4a': '#fb5c74',
    '.mp4': '#fb5c74', '.mov': '#fb5c74',
    '.pdf': '#ec1c24',
  };
  const color = colorMap[ext] || '#888';
  return `<span style="color:${color}">&#9679;</span>`;
}

function filerFormatSize(bytes) {
  if (bytes < 1024) return bytes + ' B';
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
}

function handleFilerUp() {
  const fs = currentFilerState();
  if (!fs) return;
  if (fs.viewing === 'file') {
    const prevDir = fs.history.pop() || fs.rootPath;
    loadFilerDir(prevDir);
    return;
  }
  if (fs.currentPath && fs.currentPath !== fs.rootPath) {
    const parentPath = fs.currentPath.replace(/\/[^/]*\/?$/, '') || '/';
    // ルートより上には行かない
    if (fs.rootPath && !parentPath.startsWith(fs.rootPath) && parentPath !== fs.rootPath) {
      return;
    }
    fs.history.push(fs.currentPath);
    loadFilerDir(parentPath);
  }
}

function filerDownloadBtn(filePath) {
  const fs = currentFilerState();
  const url = filerUrl('download', filePath, fs.rootPath);
  const btn = document.createElement('a');
  btn.href = url;
  btn.className = 'filer-download-btn';
  btn.textContent = 'ダウンロード';
  btn.setAttribute('download', '');
  return btn;
}

async function filerCreateMd() {
  const fs = currentFilerState();
  if (!fs) return;

  const el = $('filer-content');
  el.innerHTML = '';
  fs.viewing = 'file';
  renderFilerPath();

  const form = document.createElement('div');
  form.className = 'filer-create-form';
  form.innerHTML =
    `<input class="filer-create-input" id="filer-create-name" placeholder="ファイル名 (.md)" autocomplete="off">` +
    `<textarea class="filer-create-textarea" id="filer-create-content" rows="12" placeholder="内容を入力..."></textarea>` +
    `<div class="filer-create-actions">` +
      `<button type="button" class="filer-create-cancel">キャンセル</button>` +
      `<button type="button" class="filer-create-save">作成</button>` +
    `</div>`;
  el.appendChild(form);

  form.querySelector('.filer-create-cancel').addEventListener('click', () => {
    loadFilerDir(fs.currentPath || fs.rootPath);
  });

  form.querySelector('.filer-create-save').addEventListener('click', async () => {
    let name = form.querySelector('#filer-create-name').value.trim();
    const content = form.querySelector('#filer-create-content').value;
    if (!name) return;
    if (!name.endsWith('.md')) name += '.md';

    try {
      await apiFetch('/api/filer/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          dir: fs.currentPath || fs.rootPath,
          root: fs.rootPath,
          filename: name,
          content: content,
        }),
      });
      loadFilerDir(fs.currentPath || fs.rootPath);
    } catch (e) {
      const msg = e.message.includes('409') ? '同名ファイルが既に存在します' : `作成失敗: ${e.message}`;
      alert(msg);
    }
  });

  form.querySelector('#filer-create-name').focus();
}

// ===== File Upload =====
function initFilerUpload() {
  const uploadInput = $('filer-upload-input');
  $('btn-filer-upload').addEventListener('click', () => {
    uploadInput.value = '';
    uploadInput.click();
  });
  uploadInput.addEventListener('change', () => {
    if (uploadInput.files.length > 0) {
      uploadFiles(uploadInput.files);
    }
  });

  // ドラッグ&ドロップ
  const panel = $('filer-panel');
  panel.addEventListener('dragover', e => {
    e.preventDefault();
    panel.classList.add('filer-dragover');
  });
  panel.addEventListener('dragleave', e => {
    e.preventDefault();
    panel.classList.remove('filer-dragover');
  });
  panel.addEventListener('drop', e => {
    e.preventDefault();
    panel.classList.remove('filer-dragover');
    if (e.dataTransfer.files.length > 0) {
      uploadFiles(e.dataTransfer.files);
    }
  });
}

// クリップボードコピー: navigator.clipboard は HTTPS/localhost 限定なので
// 使えない場合は textarea + execCommand にフォールバックする
function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text);
  }
  return new Promise((resolve, reject) => {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (e) { ok = false; }
    ta.remove();
    if (ok) resolve(); else reject(new Error('copy failed'));
  });
}

function fmtMB(bytes) {
  return (bytes / 1048576).toFixed(1);
}

function fmtEta(sec) {
  if (!isFinite(sec) || sec < 0) return '';
  if (sec < 90) return Math.round(sec) + 's';
  return Math.round(sec / 60) + 'm';
}

// fetch は送信進捗を取れないので XHR を使う
function filerXhrUpload(file, rootPath, onProgress) {
  return new Promise((resolve, reject) => {
    const fd = new FormData();
    fd.append('file', file);
    fd.append('root', rootPath);
    const xhr = new XMLHttpRequest();
    const sep = '/api/filer/upload'.includes('?') ? '&' : '?';
    xhr.open('POST', `/api/filer/upload${sep}token=${encodeURIComponent(state.token)}`);
    xhr.upload.onprogress = e => {
      if (e.lengthComputable) onProgress(e.loaded, e.total);
    };
    xhr.onload = () => {
      let data = {};
      try { data = JSON.parse(xhr.responseText); } catch (e) {}
      if (xhr.status >= 200 && xhr.status < 300) resolve(data);
      else reject(new Error(data.error || `HTTP ${xhr.status}`));
    };
    xhr.onerror = () => reject(new Error('接続に失敗しました'));
    xhr.onabort = () => reject(new Error('中断されました'));
    xhr.send(fd);
  });
}

let filerUploadToastTimer = null;

function getFilerUploadToast() {
  let el = document.querySelector('.filer-upload-toast');
  if (el) return el;

  el = document.createElement('div');
  el.className = 'filer-upload-toast';

  const closeBtn = document.createElement('button');
  closeBtn.type = 'button';
  closeBtn.className = 'filer-upload-toast-close';
  closeBtn.textContent = '×';
  closeBtn.addEventListener('click', () => el.remove());
  el.appendChild(closeBtn);

  document.body.appendChild(el);
  return el;
}

function scheduleFilerUploadToastDismiss(el) {
  if (filerUploadToastTimer) clearTimeout(filerUploadToastTimer);
  filerUploadToastTimer = setTimeout(() => {
    if (el.parentNode) el.remove();
  }, 15000);
}

function createFilerUploadRow(filename) {
  const row = document.createElement('div');
  row.className = 'filer-upload-result filer-upload-progress';
  row.innerHTML =
    `<div class="filer-upload-name">${esc(filename)}</div>` +
    `<div class="filer-upload-bar"><div class="filer-upload-bar-fill"></div></div>` +
    `<div class="filer-upload-status">準備中…</div>`;
  return {
    el: row,
    fill: row.querySelector('.filer-upload-bar-fill'),
    status: row.querySelector('.filer-upload-status'),
  };
}

function finishFilerUploadRow(row, path) {
  row.el.className = 'filer-upload-result';
  row.el.innerHTML =
    `<span class="filer-upload-path">${esc(path)}</span>` +
    `<div class="filer-upload-actions">` +
      `<button type="button" class="filer-upload-copy">コピー</button>` +
      `<button type="button" class="filer-upload-send">ターミナルに送信</button>` +
    `</div>`;
  const copyBtn = row.el.querySelector('.filer-upload-copy');
  copyBtn.addEventListener('click', () => {
    copyText(path).then(() => {
      copyBtn.textContent = 'コピー済';
      setTimeout(() => { copyBtn.textContent = 'コピー'; }, 1500);
    }).catch(() => {
      copyBtn.textContent = '失敗';
      setTimeout(() => { copyBtn.textContent = 'コピー'; }, 1500);
    });
  });
  row.el.querySelector('.filer-upload-send').addEventListener('click', () => {
    if (state.currentPane) {
      sendKeys(state.currentPane, path);
    }
  });
}

function errorFilerUploadRow(row, message) {
  row.status.classList.add('filer-upload-status-err');
  row.status.textContent = `失敗: ${message}`;
}

async function uploadFiles(fileList) {
  const fs = currentFilerState();
  if (!fs) return;

  const toast = getFilerUploadToast();

  for (const file of fileList) {
    const row = createFilerUploadRow(file.name);
    toast.insertBefore(row.el, toast.firstChild);

    // 直近サンプルから指数移動平均で速度を出す (瞬間値だとブレが大きい)
    let lastMs = performance.now();
    let lastLoaded = 0;
    let ema = 0;

    try {
      const data = await filerXhrUpload(file, fs.rootPath, (loaded, total) => {
        const now = performance.now();
        const dt = now - lastMs;
        if (dt >= 250) {
          const inst = (loaded - lastLoaded) / dt * 1000;
          ema = ema ? ema * 0.7 + inst * 0.3 : inst;
          lastMs = now;
          lastLoaded = loaded;
        }
        const pct = total ? (loaded / total * 100) : 0;
        row.fill.style.width = pct.toFixed(1) + '%';
        const rate = ema ? fmtMB(ema) + ' MB/s' : '…';
        const eta = ema && total ? fmtEta((total - loaded) / ema) : '';
        row.status.textContent =
          `${fmtMB(loaded)} / ${fmtMB(total)} MB (${pct.toFixed(1)}%) ${rate}` +
          (eta ? `  残り ${eta}` : '');
      });
      row.fill.style.width = '100%';
      finishFilerUploadRow(row, data.path);
    } catch (e) {
      errorFilerUploadRow(row, e.message);
    }
  }

  scheduleFilerUploadToastDismiss(toast);
  // ファイラーのリストを更新
  loadFilerDir(fs.currentPath || fs.rootPath);
}

// セッション切替時にファイラー状態を復元/リセット
function syncFilerOnSessionSwitch() {
  const sessionName = state.currentSession;
  if (!sessionName) return;
  const fs = getFilerState(sessionName);
  if (fs.active) {
    showFilerPanel(fs);
    $('btn-filer').classList.add('active');
  } else {
    hideFilerPanel();
    $('btn-filer').classList.remove('active');
  }
}

// ===== 初期化 =====
function initFiler() {
  $('btn-filer').addEventListener('click', toggleFiler);
  $('btn-filer-up').addEventListener('click', handleFilerUp);
  $('btn-filer-new-md').addEventListener('click', filerCreateMd);
  initFilerUpload();
}
