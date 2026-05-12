// ─── State ───────────────────────────────────────────────────────────────────

const state = {
  loggedIn: false,
  hasUser: false,
  agentRunning: false,
  cancellingTask: false,
  askActive: false,
  hasQueued: false,
  isStreaming: false,
  streamingText: '',
  lastToolName: '',
  messages: [],
  toolGroupsOpen: {},
  sse: null,
  projectId: 1,
};

// ─── Boot ────────────────────────────────────────────────────────────────────

async function init() {
  try {
    const controller = new AbortController();
    const tid = setTimeout(() => controller.abort(), 5000);
    let s = {};
    try {
      s = await fetch('/api/auth/status', {signal: controller.signal}).then(r => r.json());
    } finally {
      clearTimeout(tid);
    }
    state.hasUser  = s.hasUser  || false;
    state.loggedIn = s.loggedIn || false;
  } catch(_) {
    state.hasUser  = false;
    state.loggedIn = false;
  }

  if (state.loggedIn) {
    await loadHistory();
    await loadProjects();
    showView('app');
    connect();
  } else {
    syncAuthForm();
    showView('auth');
  }
}

// ─── Views ───────────────────────────────────────────────────────────────────

function showView(name) {
  document.getElementById('view-auth').style.display     = name === 'auth'     ? '' : 'none';
  document.getElementById('view-app').style.display      = name === 'app'      ? '' : 'none';
  document.getElementById('view-settings').style.display = name === 'settings' ? '' : 'none';
}

// ─── Auth ────────────────────────────────────────────────────────────────────

function syncAuthForm() {
  document.getElementById('auth-sub').textContent = state.hasUser ? 'sign in to continue.' : 'create your account.';
  document.getElementById('auth-btn').textContent = state.hasUser ? 'sign in' : 'create account';
}

function showAuthError(msg) {
  const el = document.getElementById('auth-error');
  el.textContent = msg;
  el.style.display = '';
}

function clearAuthError() {
  const el = document.getElementById('auth-error');
  el.textContent = '';
  el.style.display = 'none';
}

async function doLogin() {
  clearAuthError();
  const username = document.getElementById('auth-username').value;
  const password = document.getElementById('auth-password').value;
  const path = state.hasUser ? '/api/auth/login' : '/api/auth/setup';
  const r = await fetch(path, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({username, password}),
  });
  if (!r.ok) {
    const j = await r.json().catch(() => ({}));
    showAuthError(j.error || (state.hasUser ? 'invalid credentials' : 'failed'));
    return;
  }
  state.loggedIn = true;
  state.hasUser  = true;
  await loadHistory();
  showView('app');
  connect();
}

async function doLogout() {
  await fetch('/api/auth/logout', {method: 'POST'});
  state.loggedIn = false;
  if (state.sse) { state.sse.close(); state.sse = null; }
  state.messages = [];
  state.streamingText = '';
  state.isStreaming = false;
  syncAuthForm();
  showView('auth');
}

// ─── History ─────────────────────────────────────────────────────────────────

async function loadHistory() {
  try {
    const j = await fetch('/api/history').then(r => r.json());
    if (j.messages) {
      state.messages = j.messages;
      renderFeed();
    }
  } catch(_) {}
}

// ─── SSE ─────────────────────────────────────────────────────────────────────

function connect() {
  if (state.sse) state.sse.close();
  state.sse = new EventSource('/events');

  state.sse.addEventListener('error', () => {
    fetch('/api/auth/status').then(r => r.json()).then(s => {
      if (!s.loggedIn) {
        state.loggedIn = false;
        if (state.sse) { state.sse.close(); state.sse = null; }
        syncAuthForm();
        showView('auth');
      }
    }).catch(() => {});
  });

  state.sse.addEventListener('msg', e => {
    try { onMsg(JSON.parse(e.data)); } catch(_) {}
  });

  state.sse.addEventListener('state', e => {
    try {
      const s = JSON.parse(e.data);
      state.agentRunning = s.running;
      state.hasQueued    = s.queued || false;
      if (!s.running) { state.lastToolName = ''; state.cancellingTask = false; }
      syncAgentState();
    } catch(_) {}
  });

  state.sse.addEventListener('ask_state', e => {
    try {
      state.askActive = JSON.parse(e.data).active;
      syncAskState();
    } catch(_) {}
  });

  state.sse.addEventListener('token', e => {
    try {
      const d = JSON.parse(e.data);
      state.isStreaming   = true;
      state.streamingText += d.text;
      upsertStreamingBubble();
    } catch(_) {}
  });

  state.sse.addEventListener('stream_cancel', () => {
    state.streamingText = '';
    state.isStreaming   = false;
    removeStreamingBubble();
    syncAgentState();
  });

  state.sse.addEventListener('stream_done', () => {
    const feed = document.getElementById('feed');
    removeStreamingBubble();
    const dotsRow = document.getElementById('thinking-dots-row');
    if (dotsRow) dotsRow.remove();
    if (state.streamingText) {
      const msg = {
        type: 'message',
        text: state.streamingText,
        ts: new Date().toTimeString().slice(0, 8),
      };
      state.messages.push(msg);
      if (feed) feed.appendChild(buildBubbleRow(msg));
    }
    state.streamingText = '';
    state.isStreaming   = false;
    syncAgentState();
    scrollFeed();
  });
}

// ─── Message handling ─────────────────────────────────────────────────────────

function onMsg(m) {
  if (isToolCall(m)) {
    state.streamingText = '';
    state.isStreaming   = false;
  }
  state.messages.push(m);
  if (isToolCall(m)) {
    const parsed = parseToolCall(m.text);
    if (parsed.name) state.lastToolName = parsed.name;
  }
  appendToFeed(m);
}

function appendToFeed(msg) {
  const feed = document.getElementById('feed');
  if (!feed) return;

  // Remove empty state placeholder if present
  const empty = feed.querySelector('.empty-state');
  if (empty) empty.remove();

  // Ensure spacer exists
  if (!feed.querySelector('.feed-spacer')) {
    const spacer = document.createElement('div');
    spacer.className = 'feed-spacer';
    feed.insertBefore(spacer, feed.firstChild);
  }

  // Remove streaming bubble and thinking dots before appending real content
  removeStreamingBubble();
  const dotsRow = document.getElementById('thinking-dots-row');
  if (dotsRow) dotsRow.remove();

  if (isToolCall(msg)) {
    const idx     = state.messages.length - 1; // msg already pushed
    const prevMsg = state.messages[idx - 1];

    if (prevMsg && isToolCall(prevMsg)) {
      // Consecutive tool call — extend the existing last tool group in the DOM
      const lastGroupRow = findLastToolGroupRow(feed);
      if (lastGroupRow) {
        const gid    = lastGroupRow.querySelector('.tool-group').dataset.gid;
        const groups = groupMessages(state.messages);
        const group  = groups.find(g => g.gid === gid);
        if (group) {
          lastGroupRow.replaceWith(buildToolGroupRow(group.tools, gid, true));
        }
      }
    } else {
      // New tool group — de-activate running dot on the previous last group
      const prevLastRow = findLastToolGroupRow(feed);
      if (prevLastRow) {
        const dot = prevLastRow.querySelector('.tool-status-dot');
        if (dot) dot.classList.remove('running');
      }
      const gid = 'tg' + idx;
      feed.appendChild(buildToolGroupRow([msg], gid, true));
    }
  } else {
    feed.appendChild(buildBubbleRow(msg));
  }

  syncAgentState();
  scrollFeed();
}

function findLastToolGroupRow(feed) {
  const rows = feed.querySelectorAll('.bubble-row');
  for (let i = rows.length - 1; i >= 0; i--) {
    if (rows[i].querySelector('.tool-group')) return rows[i];
  }
  return null;
}

function isToolCall(m) {
  if (m.type !== 'message' || typeof m.text !== 'string') return false;
  return m.text.startsWith('\x60tool\x60');
}

function parseToolCall(text) {
  const m = text.match(/^\x60tool\x60\s+\*\*([^*]+)\*\*\s*([\s\S]*)/);
  if (m) return {name: m[1].trim(), args: m[2].trim()};
  return {name: text, args: ''};
}

function groupMessages(messages) {
  const groups = [];
  let i = 0;
  while (i < messages.length) {
    const m = messages[i];
    if (isToolCall(m)) {
      const startIdx = i;
      const tools = [];
      while (i < messages.length && isToolCall(messages[i])) {
        tools.push(messages[i]);
        i++;
      }
      groups.push({kind: 'toolgroup', tools, gid: 'tg' + startIdx});
    } else {
      groups.push({kind: 'message', msg: m, gid: 'msg' + i});
      i++;
    }
  }
  return groups;
}

// ─── Feed rendering ───────────────────────────────────────────────────────────

function renderFeed() {
  const feed = document.getElementById('feed');
  feed.innerHTML = '';

  const groups     = groupMessages(state.messages);
  const hasContent = state.messages.length > 0 || state.isStreaming;

  if (!hasContent) {
    const empty = document.createElement('div');
    empty.className = 'empty-state';
    empty.innerHTML =
      '<svg width="40" height="40" viewBox="0 0 28 28" fill="none">' +
        '<circle cx="14" cy="8" r="3" stroke="#555" stroke-width="1.5"/>' +
        '<line x1="14" y1="11" x2="14" y2="22" stroke="#555" stroke-width="1.5" stroke-linecap="round"/>' +
        '<line x1="7" y1="16" x2="21" y2="16" stroke="#555" stroke-width="1.5" stroke-linecap="round"/>' +
        '<path d="M7 22 Q14 26 21 22" stroke="#555" stroke-width="1.5" fill="none" stroke-linecap="round"/>' +
      '</svg><p>send a message to start.</p>';
    feed.appendChild(empty);
    syncAgentState();
    return;
  }

  const spacer = document.createElement('div');
  spacer.className = 'feed-spacer';
  feed.appendChild(spacer);

  const lastToolGroupIdx = groups.reduce((acc, g, i) => g.kind === 'toolgroup' ? i : acc, -1);

  for (let i = 0; i < groups.length; i++) {
    const group = groups[i];
    if (group.kind === 'toolgroup') {
      feed.appendChild(buildToolGroupRow(group.tools, group.gid, i === lastToolGroupIdx));
    } else {
      feed.appendChild(buildBubbleRow(group.msg));
    }
  }

  if (state.isStreaming && state.streamingText) {
    feed.appendChild(buildStreamingBubble());
  }

  syncAgentState();
  scrollFeed();
}

function buildBubbleRow(msg) {
  const row = document.createElement('div');
  const isUser = msg.type === 'user' || msg.type === 'reply';
  row.className = 'bubble-row ' + (isUser ? 'user' : 'agent');

  const bubble   = document.createElement('div');
  const classMap = {user: 'bubble user-bubble', reply: 'bubble reply-bubble', ask: 'bubble ask-bubble'};
  bubble.className = classMap[msg.type] || 'bubble agent-bubble';

  const meta = document.createElement('div');
  const labelMap = {user: 'you', reply: 'you', ask: 'kaptaan asks'};
  meta.className   = 'bubble-meta' + (msg.type === 'user' ? ' user-meta' : '');
  meta.textContent = (labelMap[msg.type] || 'kaptaan') + ' · ' + (msg.ts || '');

  const content = document.createElement('div');
  content.className   = 'bubble-content';
  content.textContent = msg.text || '';

  bubble.appendChild(meta);
  bubble.appendChild(content);
  row.appendChild(bubble);
  return row;
}

function buildToolGroupRow(tools, gid, isLast) {
  const isOpen = state.toolGroupsOpen[gid] !== false;

  const row = document.createElement('div');
  row.className = 'bubble-row agent';

  const tg = document.createElement('div');
  tg.className   = 'tool-group';
  tg.dataset.gid = gid;

  const header = document.createElement('div');
  header.className = 'tool-group-header';

  const dot = document.createElement('div');
  dot.className = 'tool-status-dot' + (isLast && state.agentRunning ? ' running' : '');

  const label = document.createElement('span');
  label.className   = 'tool-group-label';
  label.textContent = tools.length + (tools.length === 1 ? ' tool call' : ' tool calls');

  const toggle = document.createElement('span');
  toggle.className   = 'tool-group-toggle' + (isOpen ? ' open' : '');
  toggle.textContent = '▼';

  header.appendChild(dot);
  header.appendChild(label);
  header.appendChild(toggle);
  header.addEventListener('click', () => toggleToolGroup(gid));
  tg.appendChild(header);

  if (isOpen) {
    const toolRows = document.createElement('div');
    toolRows.className = 'tool-rows';
    for (const t of tools) {
      const parsed = parseToolCall(t.text);
      const tr   = document.createElement('div');
      tr.className = 'tool-row';

      const icon = document.createElement('span');
      icon.className   = 'tool-row-icon';
      icon.textContent = '⚡';

      const name = document.createElement('span');
      name.className   = 'tool-name';
      name.textContent = parsed.name;

      const args = document.createElement('span');
      args.className   = 'tool-args';
      args.textContent = parsed.args;

      tr.appendChild(icon);
      tr.appendChild(name);
      tr.appendChild(args);
      toolRows.appendChild(tr);
    }
    tg.appendChild(toolRows);
  }

  row.appendChild(tg);
  return row;
}

function buildStreamingBubble() {
  const row = document.createElement('div');
  row.className = 'bubble-row agent';
  row.id        = 'streaming-bubble';

  const bubble = document.createElement('div');
  bubble.className = 'bubble streaming-bubble';

  const meta = document.createElement('div');
  meta.className   = 'bubble-meta';
  meta.textContent = 'kaptaan · ' + new Date().toTimeString().slice(0, 8);

  const content = document.createElement('div');
  content.className   = 'bubble-content';
  content.id          = 'streaming-content';
  content.textContent = state.streamingText;

  bubble.appendChild(meta);
  bubble.appendChild(content);
  row.appendChild(bubble);
  return row;
}

function upsertStreamingBubble() {
  const feed    = document.getElementById('feed');
  let   bubble  = document.getElementById('streaming-bubble');
  let   content = document.getElementById('streaming-content');

  if (!bubble) {
    // Remove empty state if present
    const empty = feed.querySelector('.empty-state');
    if (empty) empty.remove();
    // Add spacer if missing
    if (!feed.querySelector('.feed-spacer')) {
      const spacer = document.createElement('div');
      spacer.className = 'feed-spacer';
      feed.insertBefore(spacer, feed.firstChild);
    }
    // Remove thinking dots
    const dots = document.getElementById('thinking-dots-row');
    if (dots) dots.remove();

    bubble = buildStreamingBubble();
    feed.appendChild(bubble);
    content = document.getElementById('streaming-content');
  }

  if (content) content.textContent = state.streamingText;
  scrollFeed();
}

function removeStreamingBubble() {
  const el = document.getElementById('streaming-bubble');
  if (el) el.remove();
}

function toggleToolGroup(gid) {
  state.toolGroupsOpen[gid] = state.toolGroupsOpen[gid] === false ? true : false;
  renderFeed();
}

// ─── Agent / ask state sync ───────────────────────────────────────────────────

function syncAgentState() {
  const dot      = document.getElementById('status-dot');
  const text     = document.getElementById('status-text');
  const stopBtn  = document.getElementById('stop-btn');
  const feed     = document.getElementById('feed');

  if (dot) {
    dot.className = 'status-dot ' + (state.agentRunning ? 'running' : 'idle');
  }
  if (text) {
    text.textContent = state.agentRunning
      ? ('running' + (state.lastToolName ? ' · ' + state.lastToolName : ''))
      : 'idle';
  }
  if (stopBtn) {
    stopBtn.style.display   = state.agentRunning ? '' : 'none';
    stopBtn.disabled        = state.cancellingTask;
  }

  // Thinking dots: show when running and not streaming
  let dotsRow = document.getElementById('thinking-dots-row');
  if (state.agentRunning && !state.isStreaming) {
    if (!document.getElementById('streaming-bubble') && !dotsRow) {
      dotsRow = document.createElement('div');
      dotsRow.className = 'bubble-row agent';
      dotsRow.id        = 'thinking-dots-row';
      const dots = document.createElement('div');
      dots.className = 'thinking-dots';
      dots.innerHTML = '<span></span><span></span><span></span>';
      dotsRow.appendChild(dots);
      if (feed) feed.appendChild(dotsRow);
      scrollFeed();
    }
  } else {
    if (dotsRow) dotsRow.remove();
  }

  // Queue banner
  const queuedBanner = document.getElementById('queued-banner');
  if (queuedBanner) {
    queuedBanner.style.display = (state.hasQueued && !state.askActive) ? '' : 'none';
  }
}

function syncAskState() {
  const askBanner     = document.getElementById('ask-banner');
  const composerInput = document.getElementById('composer-input');

  if (askBanner) askBanner.style.display = state.askActive ? '' : 'none';
  if (composerInput) {
    composerInput.placeholder = state.askActive ? 'type your reply…' : 'message kaptaan…';
  }
  syncAgentState();
}

// ─── Send ─────────────────────────────────────────────────────────────────────

async function doSend() {
  const input = document.getElementById('composer-input');
  const text  = input.value.trim();
  if (!text) return;
  input.value       = '';
  input.style.height = '';

  const path = state.askActive ? '/api/reply' : '/api/chat';
  const r    = await apiCall(path, {method: 'POST', body: JSON.stringify({text})});
  if (r && r.error) {
    onMsg({type: 'message', text: 'error: ' + r.error, ts: new Date().toTimeString().slice(0, 8)});
  }
}

// ─── Projects ───────────────────────────────────────────────────────────────

async function loadProjects() {
  const j = await apiCall('/api/projects');
  const projects = (j && j.projects) || [];
  const sel = document.getElementById('project-select');
  if (!sel) return;

  sel.innerHTML = '';
  for (const p of projects) {
    const opt = document.createElement('option');
    opt.value = p.id;
    opt.textContent = p.name;
    if (p.id === state.projectId) opt.selected = true;
    sel.appendChild(opt);
  }
}

function onProjectChange() {
  const sel = document.getElementById('project-select');
  if (!sel) return;
  const newId = parseInt(sel.value, 10);
  if (newId && newId !== state.projectId) {
    state.projectId = newId;
    state.messages = [];
    renderFeed();
    loadHistory();
  }
}

async function onCreateProject() {
  const name = prompt('New project name:');
  if (!name || !name.trim()) return;
  const r = await apiCall('/api/projects/create', {
    method: 'POST',
    body: JSON.stringify({name: name.trim()}),
  });
  if (r && r.id) {
    await loadProjects();
    const sel = document.getElementById('project-select');
    if (sel) {
      sel.value = r.id;
      onProjectChange();
    }
  }
}

// ─── Settings ─────────────────────────────────────────────────────────────────

function openSettings() {
  showView('settings');
  loadMemories();
  loadScratchpad();
  loadConfig();
}

async function loadMemories() {
  const j   = await apiCall('/api/memories');
  const list = document.getElementById('memories-list');
  if (!list) return;
  const mems = (j && j.memories) ? j.memories : [];
  if (mems.length === 0) {
    list.innerHTML = '<div class="empty-list">no memories saved</div>';
    return;
  }
  list.innerHTML = '';
  for (const m of mems) {
    const item = document.createElement('div');
    item.className = 'memory-item';

    const key = document.createElement('div');
    key.className   = 'memory-key';
    key.textContent = m.key;

    const content = document.createElement('div');
    content.className   = 'memory-content';
    content.textContent = m.content;

    const footer = document.createElement('div');
    footer.className = 'memory-footer';

    const time = document.createElement('span');
    time.className   = 'memory-time';
    time.textContent = m.updated_at;

    const delBtn = document.createElement('button');
    delBtn.className   = 'del-btn';
    delBtn.textContent = 'delete';
    delBtn.addEventListener('click', () => deleteMemory(m.key));

    footer.appendChild(time);
    footer.appendChild(delBtn);
    item.appendChild(key);
    item.appendChild(content);
    item.appendChild(footer);
    list.appendChild(item);
  }
}

async function deleteMemory(key) {
  await apiCall('/api/memories?key=' + encodeURIComponent(key), {method: 'DELETE'});
  loadMemories();
}

async function loadScratchpad() {
  const display = document.getElementById('scratchpad-display');
  const btn     = document.getElementById('refresh-scratchpad-btn');
  if (btn) { btn.disabled = true; btn.textContent = 'loading…'; }

  const j = await apiCall('/api/scratchpad');

  if (btn) { btn.disabled = false; btn.textContent = 'refresh'; }
  if (!display) return;

  if (!j || j.error) {
    display.className   = 'err-msg';
    display.textContent = (j && j.error) ? j.error : 'failed to load';
    return;
  }
  if (j.content) {
    const pre = document.createElement('pre');
    pre.className   = 'scratchpad-block';
    pre.textContent = j.content;
    display.replaceWith(pre);
    pre.id = 'scratchpad-display';
  } else {
    display.className   = 'empty-list';
    display.textContent = 'scratchpad is empty';
  }
}

async function checkCredits() {
  const display = document.getElementById('credits-display');
  const btn     = document.getElementById('check-credits-btn');
  if (btn) { btn.disabled = true; btn.textContent = 'checking…'; }

  const j = await fetch('/api/credits').then(r => r.json()).catch(() => ({error: 'network error'}));

  if (btn) { btn.disabled = false; btn.textContent = 'check credits'; }
  if (!display) return;

  if (j.error) {
    display.innerHTML = '';
    const err = document.createElement('div');
    err.className   = 'err-msg';
    err.textContent = j.error;
    display.appendChild(err);
    return;
  }

  display.innerHTML = '';
  const infos = (j.balance_infos) ? j.balance_infos : [];
  if (infos.length === 0) {
    display.innerHTML = '<div class="empty-list">no balance info</div>';
    return;
  }
  for (const b of infos) {
    const row = document.createElement('div');
    row.className = 'settings-row';

    const lbl = document.createElement('span');
    lbl.className   = 'settings-row-label';
    lbl.textContent = b.currency + ' balance';

    const val = document.createElement('span');
    val.className   = 'settings-row-value';
    val.textContent = b.total_balance;

    row.appendChild(lbl);
    row.appendChild(val);
    display.appendChild(row);
  }
}

async function loadConfig() {
  const j = await apiCall('/api/config');
  if (!j || !j.config) return;
  const c = j.config;
  const setVal = (id, key) => {
    const el = document.getElementById(id);
    if (el && c[key] !== undefined) el.value = c[key];
  };
  setVal('cfg-deepseek-key',    'deepseek_api_key');
  setVal('cfg-deepseek-model',  'deepseek_model');
  setVal('cfg-e2b-key',         'e2b_api_key');
  setVal('cfg-repo-url',        'repo_url');
  setVal('cfg-github-token',    'github_token');
  setVal('cfg-system-prompt',   'system_prompt');
  setVal('cfg-cf-token',     'cf_api_token');
  setVal('cfg-cf-zone',      'cf_zone_id');
  setVal('cfg-ssh-hosts',    'ssh_hosts');
}

async function saveConfig() {
  const btn = document.getElementById('cfg-save-btn');
  const err = document.getElementById('cfg-error');
  if (btn) { btn.disabled = true; btn.textContent = 'saving…'; }
  if (err) err.style.display = 'none';

  const fields = [
    {id: 'cfg-deepseek-key',   key: 'deepseek_api_key'},
    {id: 'cfg-deepseek-model', key: 'deepseek_model'},
    {id: 'cfg-e2b-key',        key: 'e2b_api_key'},
    {id: 'cfg-repo-url',       key: 'repo_url'},
    {id: 'cfg-github-token',   key: 'github_token'},
    {id: 'cfg-system-prompt',  key: 'system_prompt'},
    {id: 'cfg-cf-token',    key: 'cf_api_token'},
    {id: 'cfg-cf-zone',     key: 'cf_zone_id'},
    {id: 'cfg-ssh-hosts',   key: 'ssh_hosts'},
  ];

  for (const f of fields) {
    const el = document.getElementById(f.id);
    if (!el) continue;
    const r = await apiCall('/api/config', {method: 'POST', body: JSON.stringify({key: f.key, value: el.value})});
    if (r && r.error) {
      if (err) { err.textContent = r.error; err.style.display = ''; }
      if (btn) { btn.disabled = false; btn.textContent = 'save configuration'; }
      return;
    }
  }

  if (btn) {
    btn.disabled    = false;
    btn.textContent = '✓ saved';
    setTimeout(() => { btn.textContent = 'save configuration'; }, 2500);
  }
}

async function clearConvo() {
  await apiCall('/api/conversation/clear', {method: 'POST'});
  state.messages = [];
  renderFeed();
}

async function cancelTask() {
  if (state.cancellingTask) return;
  state.cancellingTask = true;
  if (document.getElementById('stop-btn')) document.getElementById('stop-btn').disabled = true;
  await apiCall('/api/task/cancel', {method: 'POST'});
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

function scrollFeed() {
  const feed = document.getElementById('feed');
  if (feed) feed.scrollTop = feed.scrollHeight;
}

function apiCall(path, opts = {}) {
  const sep = path.includes('?') ? '&' : '?';
  const url = path + sep + 'project_id=' + state.projectId;
  const headers = Object.assign({'Content-Type': 'application/json'}, opts.headers || {});
  return fetch(url, Object.assign({}, opts, {headers})).then(r => {
    if (r.status === 401) {
      state.loggedIn = false;
      if (state.sse) { state.sse.close(); state.sse = null; }
      syncAuthForm();
      showView('auth');
      return {error: 'session expired'};
    }
    return r.json();
  }).catch(() => ({}));
}

// ─── Event wiring ────────────────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => {
  // Auth
  document.getElementById('auth-btn').addEventListener('click', doLogin);
  document.getElementById('auth-password').addEventListener('keyup', e => {
    if (e.key === 'Enter') doLogin();
  });

  // Main app
  document.getElementById('stop-btn').addEventListener('click', cancelTask);
  document.getElementById('settings-btn').addEventListener('click', openSettings);

  // Project selector
  const projectSel = document.getElementById('project-select');
  if (projectSel) {
    projectSel.addEventListener('change', onProjectChange);
  }
  const projectNewBtn = document.getElementById('project-new-btn');
  if (projectNewBtn) {
    projectNewBtn.addEventListener('click', onCreateProject);
  }

  const composerInput = document.getElementById('composer-input');
  const sendBtn       = document.getElementById('send-btn');

  composerInput.addEventListener('input', e => {
    const el        = e.target;
    el.style.height = '';
    el.style.height = Math.min(el.scrollHeight, 120) + 'px';
    sendBtn.disabled = !el.value.trim();
  });
  composerInput.addEventListener('keydown', e => {
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') doSend();
  });
  sendBtn.addEventListener('click', doSend);

  // Settings
  document.getElementById('settings-back-btn').addEventListener('click', () => showView('app'));
  document.getElementById('logout-btn').addEventListener('click', doLogout);
  document.getElementById('clear-convo-btn').addEventListener('click', clearConvo);
  document.getElementById('check-credits-btn').addEventListener('click', checkCredits);
  document.getElementById('refresh-scratchpad-btn').addEventListener('click', loadScratchpad);
  document.getElementById('cfg-save-btn').addEventListener('click', saveConfig);

  // Show/hide toggles for secret fields
  document.querySelectorAll('.cfg-show-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const input = document.getElementById(btn.dataset.target);
      if (!input) return;
      if (input.type === 'password') {
        input.type      = 'text';
        btn.textContent = 'hide';
      } else {
        input.type      = 'password';
        btn.textContent = 'show';
      }
    });
  });

  init();
});
