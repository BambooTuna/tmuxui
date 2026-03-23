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
  if (!fs.rootPath && panePath) {
    fs.rootPath = panePath;
    fs.currentPath = panePath;
  }
}

function toggleFiler() {
  const sessionName = state.currentSession;
  if (!sessionName) return;

  const session = state.sessions.find(s => s.name === sessionName);
  const win = session?.windows.find(w => w.index === state.currentWindow);
  const pane = win?.panes.find(p => p.target === state.currentPane);

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

  if (fs.viewing === 'list' && !$('filer-content').innerHTML) {
    loadFilerDir(fs.currentPath || fs.rootPath);
  } else if (fs.viewing === 'list') {
    renderFilerPath();
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
    const rawUrl = `/api/filer/raw?path=${encodeURIComponent(filePath)}&root=${encodeURIComponent(fs.rootPath)}&token=${encodeURIComponent(state.token)}`;
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
    return;
  }

  if (mediaType === 'video') {
    const rawUrl = `/api/filer/raw?path=${encodeURIComponent(filePath)}&root=${encodeURIComponent(fs.rootPath)}&token=${encodeURIComponent(state.token)}`;
    const video = document.createElement('video');
    video.className = 'filer-video-preview';
    video.controls = true;
    video.playsInline = true;
    video.preload = 'metadata';
    video.src = rawUrl;
    el.appendChild(video);
    return;
  }

  if (mediaType === 'audio') {
    const rawUrl = `/api/filer/raw?path=${encodeURIComponent(filePath)}&root=${encodeURIComponent(fs.rootPath)}&token=${encodeURIComponent(state.token)}`;
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
    return;
  }

  if (mediaType === 'pdf') {
    const pdfUrl = `/api/filer/raw?path=${encodeURIComponent(filePath)}&root=${encodeURIComponent(fs.rootPath)}&token=${encodeURIComponent(state.token)}`;
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
    const icon = entry.isDir ? '&#128194;' : filerFileIcon(entry.name);
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
  const map = {
    '.pdf': '&#128220;',
    '.png': '&#127912;', '.jpg': '&#127912;', '.jpeg': '&#127912;',
    '.gif': '&#127912;', '.webp': '&#127912;', '.bmp': '&#127912;',
    '.svg': '&#127912;', '.ico': '&#127912;',
    '.js': '&#128296;', '.ts': '&#128296;', '.go': '&#128296;',
    '.py': '&#128296;', '.rb': '&#128296;', '.rs': '&#128296;',
    '.c': '&#128296;', '.cpp': '&#128296;', '.h': '&#128296;',
    '.java': '&#128296;', '.kt': '&#128296;', '.swift': '&#128296;',
    '.sh': '&#128296;', '.bash': '&#128296;', '.zsh': '&#128296;',
    '.html': '&#127760;', '.css': '&#127760;', '.htm': '&#127760;',
    '.json': '&#128203;', '.yaml': '&#128203;', '.yml': '&#128203;',
    '.toml': '&#128203;', '.xml': '&#128203;', '.ini': '&#128203;',
    '.md': '&#128221;', '.txt': '&#128221;', '.log': '&#128221;',
    '.zip': '&#128230;', '.tar': '&#128230;', '.gz': '&#128230;',
    '.mp3': '&#127925;', '.wav': '&#127925;', '.m4a': '&#127925;',
    '.mp4': '&#127916;', '.mov': '&#127916;', '.avi': '&#127916;',
  };
  return map[ext] || '&#128196;';
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
