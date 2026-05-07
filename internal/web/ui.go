package web

import "strings"

// indexHTML is the single-page UI. We build it at init time so we can embed
// the backtick character (which cannot appear inside a Go raw string literal).
var indexHTML string

func init() {
        indexHTML = strings.ReplaceAll(rawHTML, "BTICK", "`")
}

// rawHTML uses the placeholder BTICK wherever a literal backtick is needed.
const rawHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
  <title>Kaptaan — CTO Agent</title>
  <script src="https://cdn.jsdelivr.net/npm/marked@12.0.0/marked.min.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/dompurify@3.1.6/dist/purify.min.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/htmx.org@1.9.12/dist/htmx.min.js"></script>
  <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.14.1/dist/cdn.min.js"></script>
  <style>
    :root {
      --bg:      #0d1117;
      --surface: #161b22;
      --border:  #30363d;
      --text:    #e6edf3;
      --muted:   #8b949e;
      --accent:  #58a6ff;
      --green:   #3fb950;
      --yellow:  #d29922;
      --red:     #f85149;
      --purple:  #bc8cff;
    }
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    html, body { height: 100%; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
      background: var(--bg); color: var(--text);
      display: flex; flex-direction: column; overflow: hidden;
    }

    /* Header */
    header {
      flex-shrink: 0; background: var(--surface);
      border-bottom: 1px solid var(--border);
      padding: 10px 20px; display: flex; align-items: center; gap: 10px;
    }
    header h1 { font-size: 1rem; font-weight: 800; letter-spacing: .05em; color: var(--accent); }
    .chip {
      padding: 2px 10px; border-radius: 20px; font-size: .7rem;
      font-weight: 700; text-transform: uppercase; letter-spacing: .06em;
    }
    .chip-state { border: 1px solid var(--yellow); color: var(--yellow); }
    .chip-trust { border: 1px solid var(--green);  color: var(--green);  }
    .spacer { flex: 1; }
    .dot { font-size: .7rem; }
    .dot-live { color: var(--green); }
    .dot-off  { color: var(--red); }

    /* Layout */
    .layout { flex: 1; display: flex; overflow: hidden; }

    /* Feed */
    .feed {
      flex: 1; overflow-y: auto; padding: 16px;
      display: flex; flex-direction: column; gap: 8px; scroll-behavior: smooth;
    }
    .feed::-webkit-scrollbar { width: 6px; }
    .feed::-webkit-scrollbar-thumb { background: var(--border); border-radius: 3px; }
    .msg {
      padding: 10px 14px; border-radius: 8px; line-height: 1.65;
      font-size: .875rem; word-break: break-word; max-width: 100%;
    }
    .msg-agent { background: var(--surface); border: 1px solid var(--border); border-left: 3px solid var(--accent); }
    .msg-ask   { background: var(--surface); border: 1px solid var(--border); border-left: 3px solid var(--purple); }
    .msg-reply { background: #1a2a1a; border: 1px solid #2a3f2a; border-left: 3px solid var(--green); }
    .msg-ts { font-size: .68rem; color: var(--muted); margin-bottom: 4px; }
    .history-sep {
      display: flex; align-items: center; gap: 10px;
      font-size: .7rem; color: var(--muted); padding: 4px 0;
    }
    .history-sep::before, .history-sep::after {
      content: ''; flex: 1; height: 1px; background: var(--border);
    }

    /* Markdown inside messages */
    .msg code { background: #1c2128; padding: 1px 5px; border-radius: 3px; font-family: monospace; font-size: .82em; }
    .msg pre  { background: #1c2128; padding: 10px; border-radius: 6px; overflow-x: auto; margin: 6px 0; }
    .msg pre code { background: none; padding: 0; }
    .msg p    { margin: 3px 0; }
    .msg ul, .msg ol { margin: 4px 0 4px 20px; }
    .msg strong { color: var(--text); }
    .msg a      { color: var(--accent); }

    /* Sidebar */
    .sidebar {
      width: 256px; flex-shrink: 0; border-left: 1px solid var(--border);
      background: var(--surface); overflow-y: auto; padding: 14px;
      display: flex; flex-direction: column; gap: 18px;
    }
    .sidebar h3 {
      font-size: .68rem; text-transform: uppercase;
      letter-spacing: .1em; color: var(--muted); margin-bottom: 8px;
    }
    .stat { display: flex; justify-content: space-between; font-size: .82rem; margin-bottom: 5px; }
    .stat-val { color: var(--accent); font-weight: 600; }
    .trust-bar  { height: 4px; background: var(--border); border-radius: 2px; margin: 5px 0 8px; }
    .trust-fill { height: 100%; background: var(--green); border-radius: 2px; transition: width .5s; }
    .cmd-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; }
    .cmd-btn {
      padding: 7px 6px; border-radius: 6px; border: 1px solid var(--border);
      background: var(--bg); color: var(--text); font-size: .78rem; cursor: pointer;
      text-align: center; transition: background .15s, border-color .15s;
      white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    }
    .cmd-btn:hover  { background: var(--border); }
    .cmd-btn:active { background: #3a3f4a; }
    .cmd-btn.danger { border-color: var(--red); color: var(--red); }
    .cmd-btn.danger:hover { background: #1e0d0d; }
    .upload-zone {
      border: 2px dashed var(--border); border-radius: 8px; padding: 14px 10px;
      text-align: center; cursor: pointer; font-size: .78rem; color: var(--muted);
      transition: all .15s; user-select: none;
    }
    .upload-zone.drag { border-color: var(--accent); color: var(--accent); background: rgba(88,166,255,.07); }
    .upload-zone input[type=file] { display: none; }

    /* Bottom bar */
    .bottom-bar {
      flex-shrink: 0; border-top: 1px solid var(--border); background: var(--surface);
      padding: 10px 14px; display: flex; gap: 10px; align-items: flex-end;
    }
    .ask-hint { font-size: .72rem; color: var(--purple); margin-bottom: 4px; }
    .reply-wrap { flex: 1; display: flex; flex-direction: column; }
    .reply-input {
      flex: 1; background: var(--bg); border: 1px solid var(--border);
      border-radius: 6px; padding: 9px 13px; color: var(--text); font-size: .875rem;
      font-family: inherit; outline: none; resize: none; transition: border-color .15s;
    }
    .reply-input:focus    { border-color: var(--accent); }
    .reply-input:disabled { opacity: .35; cursor: not-allowed; }
    .reply-btn {
      padding: 9px 20px; background: var(--accent); color: #0d1117;
      border: none; border-radius: 6px; font-weight: 700; font-size: .875rem;
      cursor: pointer; transition: opacity .15s; align-self: flex-end;
    }
    .reply-btn:disabled { opacity: .35; cursor: not-allowed; }
    .reply-btn:hover:not(:disabled) { opacity: .85; }
  </style>
</head>
<body x-data="app()" x-init="init()">

  <!-- Header -->
  <header>
    <h1>🤖 KAPTAAN</h1>
    <span class="chip chip-state" x-text="status.state || 'connecting'"></span>
    <span class="chip chip-trust" x-text="(status.trust||0).toFixed(1) + '%'"></span>
    <span style="font-size:.82rem;color:var(--muted)" x-text="status.project"></span>
    <span class="spacer"></span>
    <span class="dot" :class="connected ? 'dot-live' : 'dot-off'" x-text="connected ? '● live' : '● offline'"></span>
  </header>

  <div class="layout">

    <!-- Message Feed -->
    <div class="feed" id="feed">
      <template x-for="(m, i) in messages" :key="i">
        <template x-if="m.type === 'separator'">
          <div class="history-sep" x-text="m.text"></div>
        </template>
        <template x-if="m.type !== 'separator'">
          <div class="msg" :class="msgClass(m.type)">
            <div class="msg-ts" x-text="m.ts"></div>
            <div x-html="renderMd(m.text)"></div>
          </div>
        </template>
      </template>
      <div id="feed-end" style="height:1px"></div>
    </div>

    <!-- Sidebar -->
    <aside class="sidebar">

      <!-- Status -->
      <div>
        <h3>Status</h3>
        <div class="stat"><span>State</span><span class="stat-val" x-text="status.state || '—'"></span></div>
        <div class="stat"><span>Trust</span><span class="stat-val" x-text="(status.trust||0).toFixed(1)+'%'"></span></div>
        <div class="trust-bar"><div class="trust-fill" :style="'width:'+Math.min(status.trust||0,100)+'%'"></div></div>
        <div class="stat" style="align-items:flex-start">
          <span>Plan</span>
          <span class="stat-val" style="font-size:.72rem;text-align:right;max-width:140px" x-text="status.plan || '—'"></span>
        </div>
      </div>

      <!-- Commands -->
      <div>
        <h3>Commands</h3>
        <div class="cmd-grid">
          <button class="cmd-btn" @click="cmd('status')">📊 Status</button>
          <button class="cmd-btn" @click="cmd('score')">🎯 Score</button>
          <button class="cmd-btn" @click="cmd('tasks')">📋 Tasks</button>
          <button class="cmd-btn" @click="cmd('log')">📜 Log</button>
          <button class="cmd-btn" @click="cmd('usage')">📈 Usage</button>
          <button class="cmd-btn" @click="post('scan')">🔍 Scan</button>
          <button class="cmd-btn" @click="post('pause')">⏸ Pause</button>
          <button class="cmd-btn" @click="post('resume')">▶ Resume</button>
          <button class="cmd-btn" @click="post('replan')">🔄 Replan</button>
          <button class="cmd-btn danger" @click="post('clear')">🧹 Clear</button>
        </div>
      </div>

      <!-- Upload -->
      <div>
        <h3>Upload Docs</h3>
        <div
          class="upload-zone"
          :class="{ drag: dragging }"
          @dragover.prevent="dragging = true"
          @dragleave.prevent="dragging = false"
          @drop.prevent="handleDrop($event)"
          @click="$refs.fileInput.click()"
        >
          <input type="file" accept=".md" x-ref="fileInput" @change="handleFile($event)"/>
          <div x-show="!uploading">📄 Drop a <strong>.md</strong> file<br><small style="color:var(--muted)">or click to browse</small></div>
          <div x-show="uploading" style="color:var(--yellow)">⏳ Uploading…</div>
        </div>
      </div>

    </aside>
  </div>

  <!-- Bottom bar -->
  <div class="bottom-bar">
    <div class="reply-wrap">
      <div class="ask-hint" x-show="askActive" style="display:none">💬 Agent is waiting for your reply</div>
      <textarea
        class="reply-input"
        rows="1"
        :placeholder="askActive ? 'Type your reply and press Enter or Send…' : 'Waiting for agent question…'"
        :disabled="!askActive"
        x-model="replyText"
        @keydown.enter.prevent="if(askActive && replyText.trim()) sendReply()"
      ></textarea>
    </div>
    <button class="reply-btn" :disabled="!askActive || !replyText.trim()" @click="sendReply()">Send</button>
  </div>

<script>
function app() {
  return {
    messages:  [],
    status:    { state: 'connecting', trust: 0, project: '', plan: 'none' },
    askActive: false,
    replyText: '',
    connected: false,
    dragging:  false,
    uploading: false,

    init() {
      this.connectSSE();
    },

    connectSSE() {
      const es = new EventSource('/events');

      es.addEventListener('msg', (e) => {
        const d = JSON.parse(e.data);
        this.messages.push(d);
        if (d.type === 'ask') this.askActive = true;
        this.$nextTick(() => {
          document.getElementById('feed-end')?.scrollIntoView({ behavior: 'smooth' });
        });
      });

      es.addEventListener('status', (e) => {
        this.status = JSON.parse(e.data);
      });

      es.addEventListener('history_end', () => {
        if (this.messages.length > 0) {
          this.messages.push({ type: 'separator', text: '— earlier —', ts: '' });
          this.$nextTick(() => {
            document.getElementById('feed-end')?.scrollIntoView({ behavior: 'instant' });
          });
        }
      });

      es.addEventListener('ask_active', () => { this.askActive = true; });
      es.addEventListener('ask_done',   () => { this.askActive = false; });

      es.onopen  = () => { this.connected = true; };
      es.onerror = () => { this.connected = false; };
    },

    msgClass(type) {
      if (type === 'ask')   return 'msg-ask';
      if (type === 'reply') return 'msg-reply';
      return 'msg-agent';
    },

    renderMd(text) {
      if (!text) return '';
      try {
        const raw = marked.parse(String(text), { breaks: true, gfm: true });
        return DOMPurify.sanitize(raw, { USE_PROFILES: { html: true } });
      } catch (_) { return DOMPurify.sanitize(String(text)); }
    },

    push(text, type) {
      this.messages.push({ type: type || 'message', text, ts: new Date().toLocaleTimeString() });
      this.$nextTick(() => {
        document.getElementById('feed-end')?.scrollIntoView({ behavior: 'smooth' });
      });
    },

    async cmd(name) {
      try {
        const r = await fetch('/api/' + name);
        const d = await r.json();
        let text = '';

        if (name === 'status') {
          this.status = d;
          text = '**📊 Status**\n\n' +
            'Project: ' + d.project + '\n' +
            'State:   ' + d.state   + '\n' +
            'Trust:   ' + (d.trust||0).toFixed(1) + '%\n' +
            'Plan:    ' + (d.plan || 'none');

        } else if (name === 'score') {
          const bar = (v) => {
            const f = Math.round((v / 100) * 10);
            return '█'.repeat(Math.max(0, f)) + '░'.repeat(Math.max(0, 10 - f));
          };
          text = '**🎯 Trust Score: ' + (d.total||0).toFixed(1) + '%**\n\n' +
            'Doc Coverage   [' + bar(d.doc_coverage)   + '] ' + (d.doc_coverage||0).toFixed(0)   + '%\n' +
            'Clarifications [' + bar(d.clarifications) + '] ' + (d.clarifications||0).toFixed(0) + '%\n' +
            'Repo Scan      [' + bar(d.repo_scan)      + '] ' + (d.repo_scan||0).toFixed(0)      + '%\n' +
            'Low Ambiguity  [' + bar(d.ambiguity)      + '] ' + (d.ambiguity||0).toFixed(0)      + '%\n' +
            'Chunks indexed: ' + (d.chunks||0);

        } else if (name === 'tasks') {
          if (!d.tasks || d.tasks.length === 0) {
            text = '📋 No active plan yet. Upload your docs to get started.';
          } else {
            const icon = { done:'✅', in_progress:'🔄', failed:'❌', skipped:'⏭', approved:'👍' };
            const rows = d.tasks.map(t =>
              (icon[t.status] || '⏳') + ' Phase ' + t.phase + ' — **' + t.title + '** [' + t.status + ']'
            ).join('\n');
            text = '**📋 Plan v' + d.version + '**\n\n' + rows;
          }

        } else if (name === 'log') {
          if (!d.logs || d.logs.length === 0) {
            text = '📜 No log entries yet.';
          } else {
            const rows = d.logs.map(l => l.time + ' **' + l.event + '**: ' + l.text).join('\n');
            text = '**📜 Last events**\n\n' + rows;
          }

        } else if (name === 'usage') {
          if (!d.all || d.all.length === 0) {
            text = '📈 No usage recorded yet.';
          } else {
            const rows = d.all.map(u =>
              '**' + (u.Provider||u.provider) + '/' + (u.Model||u.model) + '** — ' +
              (u.TotalTokens || u.total_tokens || 0) + ' tokens'
            ).join('\n');
            text = '**📈 LLM Usage (all time)**\n\n' + rows;
          }
        }

        if (text) this.push(text, 'message');
      } catch (err) {
        this.push('❌ Request failed: ' + err.message, 'message');
      }
    },

    async post(name) {
      try { await fetch('/api/' + name, { method: 'POST' }); }
      catch (err) { this.push('❌ ' + name + ' failed: ' + err.message, 'message'); }
    },

    async sendReply() {
      const text = this.replyText.trim();
      if (!text || !this.askActive) return;
      this.replyText = '';
      this.askActive = false;
      try {
        await fetch('/api/reply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ text }),
        });
      } catch (err) {
        this.push('❌ Send failed: ' + err.message, 'message');
      }
    },

    async handleDrop(e) {
      this.dragging = false;
      const f = e.dataTransfer.files[0];
      if (f) await this.uploadFile(f);
    },

    async handleFile(e) {
      const f = e.target.files[0];
      if (f) await this.uploadFile(f);
      e.target.value = '';
    },

    async uploadFile(file) {
      if (!file.name.toLowerCase().endsWith('.md')) {
        alert('Only .md (Markdown) files are accepted.');
        return;
      }
      this.uploading = true;
      const fd = new FormData();
      fd.append('file', file);
      try {
        const r = await fetch('/api/upload', { method: 'POST', body: fd });
        if (!r.ok) {
          const e = await r.json().catch(() => ({}));
          this.push('❌ Upload failed: ' + (e.error || r.statusText), 'message');
        }
      } catch (err) {
        this.push('❌ Upload error: ' + err.message, 'message');
      } finally {
        this.uploading = false;
      }
    },
  };
}
</script>
</body>
</html>`
