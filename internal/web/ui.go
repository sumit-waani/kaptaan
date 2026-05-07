package web

import "strings"

// indexHTML is the single-page UI built at init time so we can embed backticks.
var indexHTML string

func init() {
	indexHTML = strings.ReplaceAll(rawHTML, "BTICK", "`")
}

const rawHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover"/>
  <meta name="apple-mobile-web-app-capable" content="yes"/>
  <meta name="apple-mobile-web-app-status-bar-style" content="black-translucent"/>
  <meta name="theme-color" content="#0a0a0a"/>
  <title>Kaptaan</title>
  <script src="https://cdn.jsdelivr.net/npm/marked@12.0.0/marked.min.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/dompurify@3.1.6/dist/purify.min.js"></script>
  <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.14.1/dist/cdn.min.js"></script>
  <link rel="preconnect" href="https://fonts.googleapis.com"/>
  <link href="https://fonts.googleapis.com/css2?family=Geist+Mono:wght@400;500&family=Geist:wght@400;500;600;700&display=swap" rel="stylesheet"/>
  <style>
    :root {
      --bg:    #0a0a0a;
      --bg1:   #131313;
      --bg2:   #1c1c1c;
      --line:  #262626;
      --line2: #333;
      --text:  #f0f0f0;
      --text2: #a0a0a0;
      --text3: #666;
      --accent:#3b82f6;
      --ok:    #22c55e;
      --warn:  #f59e0b;
      --err:   #ef4444;
      --safe-top:    env(safe-area-inset-top, 0);
      --safe-bottom: env(safe-area-inset-bottom, 0);
      --sans:  'Geist', system-ui, -apple-system, sans-serif;
      --mono:  'Geist Mono', ui-monospace, monospace;
    }

    * { box-sizing: border-box; margin: 0; padding: 0; }
    html, body { height: 100%; overflow: hidden; }
    body {
      font-family: var(--sans);
      background: var(--bg);
      color: var(--text);
      font-size: 15px;
      line-height: 1.5;
      -webkit-font-smoothing: antialiased;
      overscroll-behavior: none;
    }
    button { font-family: inherit; font-size: inherit; cursor: pointer; border: none; background: none; color: inherit; }
    input, textarea { font-family: inherit; font-size: inherit; color: inherit; background: none; border: none; outline: none; }

    [x-cloak] { display: none !important; }

    /* ─── Auth screens ─────────────────────────────────────────────── */
    .auth-loading-wrap, .auth-wrap {
      min-height: 100dvh; display: flex; flex-direction: column; align-items: center; justify-content: center;
      padding: 24px; padding-top: calc(var(--safe-top) + 24px);
    }
    .spinner {
      width: 28px; height: 28px; border: 2px solid var(--line2); border-top-color: var(--text);
      border-radius: 50%; animation: spin 0.8s linear infinite;
    }
    @keyframes spin { to { transform: rotate(360deg); } }
    .auth-wordmark { font-family: var(--mono); font-size: 12px; letter-spacing: 4px; color: var(--text3); margin-bottom: 14px; text-transform: uppercase; }
    .auth-title-big { font-size: 28px; font-weight: 700; margin-bottom: 28px; }
    .auth-card { width: 100%; max-width: 380px; background: var(--bg1); border: 1px solid var(--line); border-radius: 14px; padding: 22px; }
    .auth-card-title { font-family: var(--mono); font-size: 11px; letter-spacing: 2px; color: var(--text3); margin-bottom: 16px; text-transform: uppercase; }
    .field { margin-bottom: 14px; }
    .field label { display: block; font-family: var(--mono); font-size: 11px; letter-spacing: 1.5px; color: var(--text3); margin-bottom: 6px; text-transform: uppercase; }
    .field input { display: block; width: 100%; padding: 11px 12px; background: var(--bg); border: 1px solid var(--line2); border-radius: 8px; }
    .field input:focus { border-color: var(--accent); }
    .auth-error { color: var(--err); font-size: 13px; margin: 8px 0 12px; }
    .auth-btn { display: block; width: 100%; padding: 12px; background: var(--text); color: var(--bg); border-radius: 8px; font-weight: 600; margin-top: 6px; }
    .auth-btn:disabled { opacity: 0.45; cursor: not-allowed; }

    /* ─── App shell ──────────────────────────────────────────────── */
    .app { height: 100dvh; display: flex; flex-direction: column; }

    .app-header {
      flex-shrink: 0;
      padding: calc(var(--safe-top) + 10px) 14px 10px;
      background: var(--bg);
      border-bottom: 1px solid var(--line);
      display: flex; align-items: center; gap: 10px;
    }
    .brand { font-weight: 700; font-size: 16px; letter-spacing: -0.2px; }
    .conn-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--text3); flex-shrink: 0; }
    .conn-dot.live { background: var(--ok); box-shadow: 0 0 0 3px rgba(34,197,94,0.15); }
    .state-pill {
      font-family: var(--mono); font-size: 10px; letter-spacing: 1px;
      padding: 3px 7px; border-radius: 4px; background: var(--bg2); color: var(--text2);
      text-transform: uppercase;
    }
    .state-pill.thinking { background: rgba(59,130,246,0.15); color: var(--accent); }
    .state-pill.paused   { background: rgba(245,158,11,0.15); color: var(--warn); }
    .state-pill.ready    { background: rgba(34,197,94,0.12);  color: var(--ok); }

    .nav-tabs { display: flex; gap: 2px; margin-left: auto; background: var(--bg1); border: 1px solid var(--line); border-radius: 8px; padding: 2px; }
    .nav-tab {
      padding: 6px 12px; border-radius: 6px; font-size: 13px; font-weight: 500; color: var(--text2);
    }
    .nav-tab.active { background: var(--bg2); color: var(--text); }

    .pause-btn {
      padding: 6px 10px; border-radius: 8px; font-size: 12px; font-weight: 500;
      background: var(--bg1); border: 1px solid var(--line2); color: var(--text);
    }
    .pause-btn.paused { background: var(--warn); border-color: var(--warn); color: #000; }

    /* ─── Chat view ──────────────────────────────────────────────── */
    .chat-view { flex: 1; display: flex; flex-direction: column; min-height: 0; }
    .messages {
      flex: 1; overflow-y: auto; padding: 12px 12px 6px;
      display: flex; flex-direction: column; gap: 8px;
      -webkit-overflow-scrolling: touch;
    }
    .msg-row { display: flex; }
    .msg-row.from-user { justify-content: flex-end; }
    .bubble {
      max-width: 82%; padding: 9px 13px; border-radius: 14px; line-height: 1.45;
      word-wrap: break-word; overflow-wrap: anywhere; font-size: 14.5px;
    }
    .bubble.agent { background: var(--bg1); border: 1px solid var(--line); border-bottom-left-radius: 4px; }
    .bubble.user  { background: var(--accent); color: #fff; border-bottom-right-radius: 4px; }
    .bubble.ask   { background: var(--bg2); border: 1px solid var(--accent); border-bottom-left-radius: 4px; }
    .bubble.system{ background: transparent; color: var(--text3); font-size: 12.5px; font-family: var(--mono); text-align: center; max-width: 100%; padding: 4px 8px; }
    .bubble p { margin: 0 0 0.5em; }
    .bubble p:last-child { margin: 0; }
    .bubble code { font-family: var(--mono); font-size: 12.5px; background: rgba(0,0,0,0.35); padding: 1px 5px; border-radius: 3px; }
    .bubble pre { background: var(--bg); padding: 10px 12px; border-radius: 8px; overflow-x: auto; margin: 6px 0; font-size: 12.5px; }
    .bubble pre code { background: none; padding: 0; }
    .bubble ul, .bubble ol { margin: 4px 0 4px 20px; }
    .bubble strong { font-weight: 600; }
    .ts { font-family: var(--mono); font-size: 10px; color: var(--text3); margin: 2px 6px 0; }
    .from-user .ts { text-align: right; }

    .composer {
      flex-shrink: 0;
      padding: 8px 10px calc(var(--safe-bottom) + 8px);
      background: var(--bg); border-top: 1px solid var(--line);
      display: flex; align-items: flex-end; gap: 8px;
    }
    .composer textarea {
      flex: 1; resize: none; padding: 10px 12px; background: var(--bg1);
      border: 1px solid var(--line2); border-radius: 18px; max-height: 120px; min-height: 40px;
      line-height: 1.4;
    }
    .composer textarea:focus { border-color: var(--accent); }
    .send-btn {
      width: 40px; height: 40px; border-radius: 50%; background: var(--accent); color: #fff;
      display: flex; align-items: center; justify-content: center; flex-shrink: 0;
    }
    .send-btn:disabled { background: var(--line2); cursor: not-allowed; }
    .send-btn svg { width: 18px; height: 18px; }

    /* ─── Settings view ────────────────────────────────────────── */
    .settings-view { flex: 1; overflow-y: auto; padding: 16px 14px calc(var(--safe-bottom) + 24px); }
    .settings-section { margin-bottom: 22px; }
    .settings-section h2 { font-size: 13px; font-family: var(--mono); letter-spacing: 1.5px; color: var(--text3); text-transform: uppercase; margin-bottom: 8px; }
    .settings-card { background: var(--bg1); border: 1px solid var(--line); border-radius: 12px; padding: 14px; }
    .settings-row { display: flex; justify-content: space-between; align-items: center; padding: 8px 0; border-bottom: 1px solid var(--line); font-size: 14px; }
    .settings-row:last-child { border-bottom: none; }
    .settings-row .meta { font-family: var(--mono); font-size: 11px; color: var(--text3); }
    .settings-action {
      padding: 9px 14px; border-radius: 8px; background: var(--bg2); border: 1px solid var(--line2);
      font-size: 13px; font-weight: 500;
    }
    .settings-action.primary { background: var(--text); color: var(--bg); border-color: var(--text); }
    .settings-action.danger  { color: var(--err); border-color: rgba(239,68,68,0.3); }
    .settings-action:disabled { opacity: 0.5; cursor: not-allowed; }
    .empty { color: var(--text3); font-size: 13px; text-align: center; padding: 16px; }
    pre.console {
      background: var(--bg); padding: 12px; border-radius: 8px; font-family: var(--mono); font-size: 11.5px;
      max-height: 240px; overflow: auto; white-space: pre-wrap; word-break: break-word; color: var(--text2);
      border: 1px solid var(--line);
    }
    .desc { color: var(--text2); font-size: 13px; margin-bottom: 10px; }
  </style>
</head>
<body x-data="kaptaan()" x-init="init()" x-cloak>

  <!-- Loading -->
  <div x-show="screen==='loading'" class="auth-loading-wrap"><div class="spinner"></div></div>

  <!-- Auth -->
  <div x-show="screen==='setup'||screen==='login'" class="auth-wrap">
    <div class="auth-wordmark">Kaptaan</div>
    <div class="auth-title-big" x-text="screen==='setup' ? 'Create account' : 'Welcome back'"></div>
    <div class="auth-card">
      <div class="auth-card-title" x-text="screen==='setup' ? 'Setup' : 'Sign in'"></div>
      <form @submit.prevent="submitAuth()">
        <div class="field">
          <label>Username</label>
          <input type="text" x-model="authUser" autocomplete="username" autocapitalize="off"
            autocorrect="off" spellcheck="false" placeholder="username" :disabled="authBusy"/>
        </div>
        <div class="field">
          <label>Password</label>
          <input type="password" x-model="authPass"
            :autocomplete="screen==='setup' ? 'new-password' : 'current-password'"
            :placeholder="screen==='setup' ? 'min 6 characters' : 'password'"
            :disabled="authBusy"/>
        </div>
        <div class="auth-error" x-show="authErr" x-text="authErr"></div>
        <button type="submit" class="auth-btn" :disabled="authBusy||!authUser||!authPass">
          <span x-show="!authBusy" x-text="screen==='setup' ? 'Create account' : 'Sign in'"></span>
          <span x-show="authBusy">...</span>
        </button>
      </form>
    </div>
  </div>

  <!-- App -->
  <div x-show="screen==='app'" class="app">

    <header class="app-header">
      <div class="conn-dot" :class="connected?'live':''"></div>
      <div class="brand">Kaptaan</div>
      <span class="state-pill" :class="(status.state||'').toLowerCase()" x-text="status.state||'ready'"></span>

      <div class="nav-tabs">
        <button class="nav-tab" :class="view==='chat'?'active':''" @click="view='chat'">Chat</button>
        <button class="nav-tab" :class="view==='settings'?'active':''" @click="view='settings'; loadSettings()">Settings</button>
      </div>

      <button class="pause-btn" :class="status.state==='paused'?'paused':''"
        @click="togglePause()"
        x-text="status.state==='paused'?'Resume':'Pause'"></button>
    </header>

    <!-- Chat view -->
    <main x-show="view==='chat'" class="chat-view">
      <div class="messages" x-ref="msgs">
        <template x-if="messages.length===0">
          <div class="empty" style="margin-top: 24px;">
            Send a message to start. Upload product docs in Settings so I can ground my plans in them.
          </div>
        </template>
        <template x-for="(m, i) in messages" :key="i">
          <div>
            <template x-if="m.type==='separator'">
              <div class="bubble system" x-text="m.text"></div>
            </template>
            <template x-if="m.type!=='separator'">
              <div :class="'msg-row ' + (isUser(m) ? 'from-user' : '')">
                <div :class="'bubble ' + bubbleClass(m)" x-html="renderMsg(m.text)"></div>
              </div>
            </template>
            <template x-if="m.type!=='separator' && m.ts">
              <div :class="'ts ' + (isUser(m) ? '' : '')" x-text="m.ts"></div>
            </template>
          </div>
        </template>
        <div id="feed-end"></div>
      </div>

      <div class="composer">
        <textarea x-model="input" x-ref="inputEl" rows="1" placeholder="Ask Kaptaan anything…"
          @input="autoResize($el)"
          @keydown.enter.prevent="if(!$event.shiftKey && input.trim()) send()"></textarea>
        <button class="send-btn" :disabled="!input.trim()" @click="send()" aria-label="Send">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"
            stroke-linecap="round" stroke-linejoin="round">
            <line x1="22" y1="2" x2="11" y2="13"/>
            <polygon points="22 2 15 22 11 13 2 9 22 2" fill="currentColor" stroke="none"/>
          </svg>
        </button>
      </div>
    </main>

    <!-- Settings view -->
    <main x-show="view==='settings'" class="settings-view">

      <section class="settings-section">
        <h2>Project documents</h2>
        <div class="desc">Upload Markdown specs the planner should ground its work in.</div>
        <div class="settings-card">
          <div style="display:flex; gap:8px; margin-bottom: 10px;">
            <button class="settings-action primary" @click="$refs.fileIn.click()" :disabled="uploading">
              <span x-show="!uploading">Upload .md</span>
              <span x-show="uploading">Uploading…</span>
            </button>
            <button class="settings-action" @click="loadDocs()">Refresh</button>
          </div>
          <input type="file" accept=".md,text/markdown" x-ref="fileIn" @change="upload($event)" hidden/>

          <template x-if="docs.length===0">
            <div class="empty">No documents uploaded yet.</div>
          </template>
          <template x-for="d in docs" :key="d.id">
            <div class="settings-row">
              <span x-text="d.filename"></span>
              <span class="meta" x-text="d.uploaded"></span>
            </div>
          </template>
        </div>
      </section>

      <section class="settings-section">
        <h2>Recent activity</h2>
        <div class="settings-card">
          <div style="display:flex; gap:8px; margin-bottom:10px;">
            <button class="settings-action" @click="loadLogs()">Refresh logs</button>
          </div>
          <pre class="console" x-text="logsText || 'No logs yet.'"></pre>
        </div>
      </section>

      <section class="settings-section">
        <h2>LLM usage</h2>
        <div class="settings-card">
          <div style="display:flex; gap:8px; margin-bottom:10px;">
            <button class="settings-action" @click="loadUsage()">Refresh usage</button>
          </div>
          <pre class="console" x-text="usageText || 'No usage recorded yet.'"></pre>
        </div>
      </section>

      <section class="settings-section">
        <h2>Session</h2>
        <div class="settings-card">
          <div style="display:flex; gap:8px;">
            <button class="settings-action" @click="clearChat()">Clear chat history</button>
            <button class="settings-action danger" @click="logout()">Logout</button>
          </div>
        </div>
      </section>
    </main>

  </div>

<script>
function kaptaan() {
  return {
    screen:   'loading',
    view:     'chat',

    authUser: '', authPass: '', authErr: '', authBusy: false,

    messages:  [],
    status:    { state: 'ready', project: '' },
    input:     '',
    connected: false,
    _es:       null,

    // Settings state
    docs:      [],
    logsText:  '',
    usageText: '',
    uploading: false,

    // ── Lifecycle ──────────────────────────────────────────────
    async init() {
      try {
        const r = await fetch('/api/auth/status');
        const d = await r.json();
        if (d.loggedIn) {
          this.screen = 'app';
          this.$nextTick(() => this.connectSSE());
        } else {
          this.screen = d.hasUser ? 'login' : 'setup';
        }
      } catch {
        this.screen = 'login';
      }
    },

    // ── Auth ───────────────────────────────────────────────────
    async submitAuth() {
      this.authErr = ''; this.authBusy = true;
      const url = this.screen === 'setup' ? '/api/auth/setup' : '/api/auth/login';
      try {
        const r = await fetch(url, {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username: this.authUser, password: this.authPass }),
        });
        const d = await r.json();
        if (!r.ok) { this.authErr = d.error || 'Something went wrong'; }
        else {
          this.authPass = ''; this.screen = 'app';
          this.$nextTick(() => this.connectSSE());
        }
      } catch { this.authErr = 'Network error — please try again'; }
      finally { this.authBusy = false; }
    },

    async logout() {
      await fetch('/api/auth/logout', { method: 'POST' });
      if (this._es) { this._es.close(); this._es = null; }
      this.messages = []; this.docs = []; this.connected = false;
      this.input = ''; this.authUser = ''; this.authPass = '';
      this.view = 'chat'; this.screen = 'login';
    },

    // ── SSE ────────────────────────────────────────────────────
    connectSSE() {
      if (this._es) this._es.close();
      const es = new EventSource('/events');
      this._es = es;

      es.addEventListener('status', e => {
        try { this.status = JSON.parse(e.data); } catch {}
      });

      es.addEventListener('msg', e => {
        try {
          const m = JSON.parse(e.data);
          if (m.type === 'builder_status') return;     // not shown in simplified UI
          if (m.type === 'pr_review') {
            const text = '**PR ready:** ' + (m.pr_url || '(local)') +
              '\n\n' + (m.manager_note || '') +
              '\n\nReply **yes** to merge or **no** to reject.';
            this.push({ type: 'ask', text, ts: m.ts });
            return;
          }
          this.push(m);
        } catch {}
      });

      es.addEventListener('history_end', () => { this.scrollBottom(); });

      es.onopen = () => { this.connected = true; };
      es.onerror = () => {
        this.connected = false;
        fetch('/api/auth/status').then(r => r.json()).then(d => {
          if (!d.loggedIn) { es.close(); this.screen = 'login'; }
        }).catch(() => {});
      };
    },

    // ── Helpers ───────────────────────────────────────────────
    push(m) {
      this.messages.push(m);
      this.$nextTick(() => this.scrollBottom());
    },

    scrollBottom() {
      const el = document.getElementById('feed-end');
      if (el) el.scrollIntoView({ behavior: 'smooth', block: 'end' });
    },

    isUser(m) { return m.type === 'reply'; },
    bubbleClass(m) {
      if (m.type === 'reply')   return 'user';
      if (m.type === 'ask')     return 'ask';
      if (m.type === 'message') return 'agent';
      return 'agent';
    },
    renderMsg(text) {
      if (!text) return '';
      return DOMPurify.sanitize(marked.parse(text));
    },

    autoResize(el) {
      el.style.height = 'auto';
      el.style.height = Math.min(el.scrollHeight, 120) + 'px';
    },

    // ── Send chat message ────────────────────────────────────
    async send() {
      const text = this.input.trim();
      if (!text) return;
      this.input = '';
      this.$nextTick(() => this.autoResize(this.$refs.inputEl));
      const r = await fetch('/api/chat', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
      });
      if (r.status === 401) { this.screen = 'login'; }
    },

    // ── Pause / Resume ────────────────────────────────────────
    async togglePause() {
      const url = this.status.state === 'paused' ? '/api/resume' : '/api/pause';
      await fetch(url, { method: 'POST' });
    },

    // ── Settings actions ──────────────────────────────────────
    async loadSettings() {
      this.loadDocs();
      this.loadLogs();
      this.loadUsage();
    },

    async loadDocs() {
      try {
        const r = await fetch('/api/docs');
        if (!r.ok) return;
        const d = await r.json();
        this.docs = d.docs || [];
      } catch {}
    },

    async loadLogs() {
      try {
        const r = await fetch('/api/log');
        if (!r.ok) return;
        const d = await r.json();
        if (!d.logs || d.logs.length === 0) { this.logsText = 'No logs yet.'; return; }
        this.logsText = d.logs.map(l => '[' + l.time + '] ' + l.event + ' — ' + l.text).join('\n');
      } catch {}
    },

    async loadUsage() {
      try {
        const r = await fetch('/api/usage');
        if (!r.ok) return;
        const d = await r.json();
        const fmt = (rows) => rows && rows.length
          ? rows.map(u => '  ' + u.provider + '/' + u.model + ': in=' + u.prompt_tokens + ' out=' + u.completion_tokens + ' calls=' + u.calls).join('\n')
          : '  (none)';
        this.usageText = 'TODAY:\n' + fmt(d.today) + '\n\nALL TIME:\n' + fmt(d.all);
      } catch {}
    },

    async upload(event) {
      const file = event.target.files[0];
      if (!file) return;
      event.target.value = '';
      this.uploading = true;
      try {
        const fd = new FormData();
        fd.append('file', file);
        const r = await fetch('/api/upload', { method: 'POST', body: fd });
        if (r.status === 401) { this.screen = 'login'; return; }
        await this.loadDocs();
      } finally { this.uploading = false; }
    },

    async clearChat() {
      if (!confirm('Clear all chat messages?')) return;
      const r = await fetch('/api/clear', { method: 'POST' });
      if (r.ok) { this.messages = []; }
    },
  };
}
</script>
</body>
</html>`
