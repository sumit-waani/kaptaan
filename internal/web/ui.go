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
  <meta name="theme-color" content="#0d1117"/>
  <title>Kaptaan</title>
  <script src="https://cdn.jsdelivr.net/npm/marked@12.0.0/marked.min.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/dompurify@3.1.6/dist/purify.min.js"></script>
  <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.14.1/dist/cdn.min.js"></script>
  <style>
    :root {
      --bg:        #0d1117;
      --surface:   #161b22;
      --surface2:  #1c2128;
      --border:    #30363d;
      --text:      #e6edf3;
      --muted:     #8b949e;
      --accent:    #58a6ff;
      --green:     #3fb950;
      --yellow:    #d29922;
      --red:       #f85149;
      --purple:    #bc8cff;
      --safe-top:    env(safe-area-inset-top,    0px);
      --safe-bottom: env(safe-area-inset-bottom, 0px);
      --safe-left:   env(safe-area-inset-left,   0px);
      --safe-right:  env(safe-area-inset-right,  0px);
    }

    *, *::before, *::after {
      box-sizing: border-box; margin: 0; padding: 0;
      -webkit-tap-highlight-color: transparent;
    }

    html {
      height: 100%; height: -webkit-fill-available;
      overscroll-behavior: none;
    }

    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      background: var(--bg); color: var(--text);
      font-size: 16px; line-height: 1.5;
      min-height: 100vh; min-height: -webkit-fill-available;
      min-height: 100dvh;
      display: flex; flex-direction: column;
      overflow: hidden;
    }

    /* ── Auth screens ─────────────────────────────────────────── */
    .auth-wrap {
      flex: 1; display: flex; flex-direction: column;
      align-items: center; justify-content: center;
      padding: calc(var(--safe-top) + 32px) 24px calc(var(--safe-bottom) + 32px);
      background: var(--bg);
    }

    .auth-logo {
      font-size: 48px; margin-bottom: 8px; line-height: 1;
    }

    .auth-name {
      font-size: 26px; font-weight: 700; letter-spacing: -0.5px;
      color: var(--text); margin-bottom: 4px;
    }

    .auth-sub {
      font-size: 14px; color: var(--muted); margin-bottom: 40px; text-align: center;
    }

    .auth-card {
      width: 100%; max-width: 360px;
      background: var(--surface); border: 1px solid var(--border);
      border-radius: 16px; padding: 28px 24px;
    }

    .auth-title {
      font-size: 20px; font-weight: 700; margin-bottom: 20px; color: var(--text);
    }

    .field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 16px; }

    .field label {
      font-size: 13px; font-weight: 600; color: var(--muted);
      text-transform: uppercase; letter-spacing: 0.04em;
    }

    .field input {
      background: var(--bg); border: 1px solid var(--border);
      border-radius: 10px; padding: 14px 16px;
      color: var(--text); font-size: 16px; font-family: inherit;
      outline: none; width: 100%;
      transition: border-color 0.15s;
      -webkit-appearance: none; appearance: none;
    }

    .field input:focus { border-color: var(--accent); }

    .auth-error {
      background: rgba(248,81,73,0.12); border: 1px solid rgba(248,81,73,0.3);
      color: var(--red); border-radius: 8px; padding: 10px 14px;
      font-size: 14px; margin-bottom: 16px;
    }

    .auth-btn {
      width: 100%; padding: 15px; border-radius: 12px; border: none;
      background: var(--accent); color: #fff; font-size: 16px;
      font-weight: 700; font-family: inherit; cursor: pointer;
      transition: opacity 0.15s, transform 0.1s;
      margin-top: 4px;
    }
    .auth-btn:active { transform: scale(0.98); opacity: 0.85; }
    .auth-btn:disabled { opacity: 0.4; cursor: not-allowed; transform: none; }

    .auth-loading-wrap {
      flex: 1; display: flex; align-items: center; justify-content: center;
    }
    .spinner {
      width: 36px; height: 36px; border: 3px solid var(--border);
      border-top-color: var(--accent); border-radius: 50%;
      animation: spin 0.8s linear infinite;
    }
    @keyframes spin { to { transform: rotate(360deg); } }

    /* ── App layout ───────────────────────────────────────────── */
    .app {
      flex: 1; display: flex; flex-direction: column;
      overflow: hidden; position: relative;
    }

    /* ── Header ───────────────────────────────────────────────── */
    .app-header {
      flex-shrink: 0;
      background: var(--surface);
      border-bottom: 1px solid var(--border);
      padding-top: calc(var(--safe-top) + 10px);
      padding-bottom: 10px;
      padding-left: calc(var(--safe-left) + 16px);
      padding-right: calc(var(--safe-right) + 16px);
      display: flex; align-items: center; gap: 10px;
    }

    .header-state {
      display: flex; align-items: center; gap: 8px; flex: 1; min-width: 0;
    }

    .state-chip {
      display: inline-flex; align-items: center;
      padding: 4px 10px; border-radius: 20px;
      font-size: 11px; font-weight: 700; letter-spacing: 0.05em;
      text-transform: uppercase; flex-shrink: 0;
      background: rgba(88,166,255,0.15); color: var(--accent);
    }
    .state-chip.executing { background: rgba(63,185,80,0.15); color: var(--green); }
    .state-chip.clarifying { background: rgba(188,140,255,0.15); color: var(--purple); }
    .state-chip.planning { background: rgba(210,153,34,0.15); color: var(--yellow); }
    .state-chip.error { background: rgba(248,81,73,0.15); color: var(--red); }

    .header-trust {
      font-size: 13px; font-weight: 600; color: var(--muted);
      flex-shrink: 0;
    }

    .header-project {
      font-size: 14px; color: var(--muted); flex: 1;
      white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
      text-align: center;
    }

    .builder-pill {
      font-size: 12px;
      color: var(--muted);
      background: var(--surface2);
      border-radius: 20px;
      padding: 3px 10px;
      max-width: 200px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      flex-shrink: 1;
    }

    .icon-btn {
      background: none; border: none; cursor: pointer; color: var(--muted);
      width: 44px; height: 44px; border-radius: 22px;
      display: flex; align-items: center; justify-content: center;
      flex-shrink: 0; transition: background 0.15s, color 0.15s;
    }
    .icon-btn:active { background: var(--surface2); color: var(--text); }
    .icon-btn svg { width: 22px; height: 22px; }

    /* ── Message feed ─────────────────────────────────────────── */
    .feed {
      flex: 1; overflow-y: auto; overflow-x: hidden;
      -webkit-overflow-scrolling: touch;
      padding: 16px 16px 8px;
      display: flex; flex-direction: column; gap: 10px;
    }

    .msg-row { display: flex; flex-direction: column; max-width: 88%; }
    .msg-row.agent { align-self: flex-start; }
    .msg-row.user-row { align-self: flex-end; }

    .msg-bubble {
      padding: 11px 15px; border-radius: 18px;
      font-size: 15px; line-height: 1.55; word-break: break-word;
    }
    .msg-bubble.message {
      background: var(--surface); border: 1px solid var(--border);
      border-bottom-left-radius: 4px; color: var(--text);
    }
    .msg-bubble.ask {
      background: rgba(188,140,255,0.12); border: 1px solid rgba(188,140,255,0.25);
      border-bottom-left-radius: 4px; color: var(--text);
    }
    .msg-bubble.reply {
      background: rgba(63,185,80,0.15); border: 1px solid rgba(63,185,80,0.2);
      border-bottom-right-radius: 4px; color: var(--text);
    }
    .msg-bubble.history {
      opacity: 0.65;
    }

    .msg-ts {
      font-size: 11px; color: var(--muted); margin-top: 4px;
      padding: 0 4px;
    }
    .msg-row.user-row .msg-ts { text-align: right; }

    /* Markdown inside bubbles */
    .msg-bubble p { margin: 0 0 8px; }
    .msg-bubble p:last-child { margin-bottom: 0; }
    .msg-bubble code {
      background: rgba(255,255,255,0.08); padding: 2px 5px;
      border-radius: 4px; font-size: 13px; font-family: 'SF Mono', monospace;
    }
    .msg-bubble pre {
      background: rgba(0,0,0,0.3); border-radius: 8px; padding: 10px 12px;
      overflow-x: auto; margin: 6px 0; font-size: 13px;
    }
    .msg-bubble pre code { background: none; padding: 0; }
    .msg-bubble ul, .msg-bubble ol { padding-left: 20px; margin: 4px 0; }
    .msg-bubble strong { color: #fff; }

    .pr-card {
      border: 1px solid rgba(88,166,255,0.3);
      border-radius: 14px;
      background: rgba(88,166,255,0.06);
      padding: 14px 16px;
      display: flex;
      flex-direction: column;
      gap: 10px;
      max-width: 100%;
    }
    .pr-card-header {
      display: flex;
      align-items: center;
      gap: 10px;
      flex-wrap: wrap;
    }
    .pr-badge {
      background: rgba(63,185,80,0.2);
      color: var(--green);
      border-radius: 20px;
      padding: 3px 10px;
      font-size: 12px;
      font-weight: 700;
      flex-shrink: 0;
    }
    .pr-title {
      font-size: 15px;
      font-weight: 600;
      flex: 1;
    }
    .pr-link {
      font-size: 13px;
      color: var(--accent);
      text-decoration: none;
      flex-shrink: 0;
    }
    .pr-note {
      font-size: 14px;
      color: var(--text);
      line-height: 1.5;
    }
    .pr-diff {
      font-size: 13px;
      color: var(--muted);
    }
    .pr-diff summary {
      cursor: pointer;
      user-select: none;
      margin-bottom: 6px;
    }
    .pr-diff pre {
      background: rgba(0,0,0,0.3);
      border-radius: 8px;
      padding: 10px;
      overflow-x: auto;
      font-size: 12px;
      max-height: 300px;
      overflow-y: auto;
    }
    .pr-ts {
      font-size: 11px;
      color: var(--muted);
    }

    .history-sep {
      display: flex; align-items: center; gap: 8px;
      color: var(--muted); font-size: 12px; padding: 4px 0;
    }
    .history-sep::before, .history-sep::after {
      content: ''; flex: 1; height: 1px; background: var(--border);
    }

    /* ── Bottom bar ───────────────────────────────────────────── */
    .bottom-bar {
      flex-shrink: 0;
      background: var(--surface); border-top: 1px solid var(--border);
      padding: 8px calc(var(--safe-right) + 8px) calc(var(--safe-bottom) + 8px) calc(var(--safe-left) + 8px);
      display: flex; align-items: flex-end; gap: 8px;
    }

    .reply-input {
      flex: 1; background: var(--bg); border: 1px solid var(--border);
      border-radius: 22px; padding: 11px 16px;
      color: var(--text); font-size: 16px; font-family: inherit;
      outline: none; resize: none; max-height: 120px; overflow-y: auto;
      line-height: 1.4; transition: border-color 0.15s;
      -webkit-overflow-scrolling: touch;
      display: block; width: 100%;
    }
    .reply-input:focus { border-color: var(--accent); }
    .reply-input:disabled { opacity: 0.4; cursor: not-allowed; }
    .reply-input::placeholder { color: var(--muted); }

    .send-btn {
      width: 44px; height: 44px; border-radius: 22px; border: none;
      background: var(--accent); color: #fff;
      display: flex; align-items: center; justify-content: center;
      cursor: pointer; flex-shrink: 0;
      transition: opacity 0.15s, transform 0.1s;
    }
    .send-btn:active { transform: scale(0.92); }
    .send-btn:disabled { opacity: 0.3; cursor: not-allowed; transform: none; }
    .send-btn svg { width: 20px; height: 20px; }

    /* ── Command sheet ────────────────────────────────────────── */
    .backdrop {
      position: fixed; inset: 0; background: rgba(0,0,0,0.55);
      z-index: 100; backdrop-filter: blur(2px);
      -webkit-backdrop-filter: blur(2px);
    }

    .sheet {
      position: fixed; left: 0; right: 0; bottom: 0;
      background: var(--surface); border-radius: 20px 20px 0 0;
      border-top: 1px solid var(--border);
      z-index: 101;
      transform: translateY(100%);
      transition: transform 0.32s cubic-bezier(0.4, 0, 0.2, 1);
      max-height: 85vh; overflow-y: auto;
      -webkit-overflow-scrolling: touch;
      padding-bottom: calc(var(--safe-bottom) + 8px);
    }
    .sheet.open { transform: translateY(0); }

    .sheet-handle {
      width: 36px; height: 4px; background: var(--border);
      border-radius: 2px; margin: 12px auto 8px;
    }

    .sheet-header {
      display: flex; align-items: center; justify-content: space-between;
      padding: 0 16px 12px; border-bottom: 1px solid var(--border);
    }
    .sheet-title { font-size: 17px; font-weight: 700; }

    .sheet-section { padding: 8px 0; border-bottom: 1px solid var(--border); }
    .sheet-section:last-child { border-bottom: none; }

    .sheet-label {
      font-size: 11px; font-weight: 700; letter-spacing: 0.06em;
      text-transform: uppercase; color: var(--muted);
      padding: 8px 16px 4px;
    }

    .sheet-cmd {
      display: flex; align-items: center; gap: 14px;
      width: 100%; padding: 14px 16px; background: none; border: none;
      color: var(--text); font-size: 16px; font-family: inherit;
      cursor: pointer; text-align: left;
      transition: background 0.12s;
      min-height: 54px;
    }
    .sheet-cmd:active { background: var(--surface2); }
    .sheet-cmd .cmd-icon { font-size: 20px; width: 28px; text-align: center; flex-shrink: 0; }
    .sheet-cmd .cmd-label { flex: 1; }
    .sheet-cmd .cmd-desc { font-size: 13px; color: var(--muted); }

    .sheet-cmd.danger { color: var(--red); }

    /* ── Upload indicator ─────────────────────────────────────── */
    .upload-badge {
      position: absolute; top: -4px; right: -4px;
      width: 10px; height: 10px; background: var(--accent);
      border-radius: 50%; border: 2px solid var(--surface);
    }
    .upload-wrap { position: relative; flex-shrink: 0; }

    /* ── Connected dot ────────────────────────────────────────── */
    .conn-dot {
      width: 7px; height: 7px; border-radius: 50%;
      background: var(--muted); flex-shrink: 0;
      transition: background 0.3s;
    }
    .conn-dot.live { background: var(--green); }

    [x-cloak] { display: none !important; }
  </style>
</head>
<body x-data="kaptaan()" x-init="init()" x-cloak>

  <!-- ── Loading ─────────────────────────────────────────────── -->
  <div x-show="screen==='loading'" class="auth-loading-wrap">
    <div class="spinner"></div>
  </div>

  <!-- ── Setup / Login ───────────────────────────────────────── -->
  <div x-show="screen==='setup'||screen==='login'" class="auth-wrap">
    <div class="auth-logo">🤖</div>
    <div class="auth-name">Kaptaan</div>
    <div class="auth-sub" x-text="screen==='setup' ? 'Your autonomous CTO agent' : 'Welcome back'"></div>

    <div class="auth-card">
      <div class="auth-title" x-text="screen==='setup' ? 'Create account' : 'Sign in'"></div>
      <form @submit.prevent="submitAuth()">
        <div class="field">
          <label>Username</label>
          <input type="text" x-model="authUser"
            autocomplete="username" autocapitalize="off"
            autocorrect="off" spellcheck="false"
            placeholder="choose a username"
            :disabled="authBusy"/>
        </div>
        <div class="field">
          <label>Password</label>
          <input type="password" x-model="authPass"
            :autocomplete="screen==='setup' ? 'new-password' : 'current-password'"
            :placeholder="screen==='setup' ? 'min 6 characters' : 'your password'"
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

  <!-- ── App ─────────────────────────────────────────────────── -->
  <div x-show="screen==='app'" class="app">

    <!-- Header -->
    <header class="app-header">
      <div class="header-state">
        <div class="conn-dot" :class="connected?'live':''"></div>
        <span class="state-chip"
          :class="(status.state||'').toLowerCase()"
          x-text="status.state||'idle'"></span>
        <span class="header-trust"
          x-show="status.trust>0"
          x-text="Math.round(status.trust)+'%'"></span>
      </div>
      <div class="header-project" x-text="status.project||'Kaptaan'"></div>
      <div class="builder-pill" x-show="builderStatus.milestone" x-text="builderStatusLabel()"></div>
      <button class="icon-btn" @click="showMenu=true" aria-label="Commands">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"
          stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <line x1="3" y1="8" x2="21" y2="8"/>
          <line x1="3" y1="16" x2="21" y2="16"/>
        </svg>
      </button>
    </header>

    <!-- Feed -->
    <div class="feed" id="feed" x-ref="feed">
      <template x-for="(m, i) in messages" :key="i">
        <div>
          <div x-show="m.type==='separator'" class="history-sep" x-text="m.text"></div>

          <template x-if="m.isPRReview">
            <div class="msg-row agent">
              <div class="pr-card">
                <div class="pr-card-header">
                  <span class="pr-badge">PR ready</span>
                  <span class="pr-title" x-text="m.task_title"></span>
                  <a class="pr-link" :href="m.pr_url" target="_blank" rel="noopener noreferrer">View on GitHub ↗</a>
                </div>
                <div class="pr-note" x-html="renderMsg(m.manager_note)"></div>
                <details class="pr-diff">
                  <summary>Show diff</summary>
                  <pre x-text="m.diff_summary"></pre>
                </details>
                <div class="pr-ts" x-text="m.ts"></div>
              </div>
            </div>
          </template>

          <div x-show="m.type!=='separator' && !m.isPRReview"
            class="msg-row"
            :class="m.type==='reply' ? 'user-row' : 'agent'">
            <div class="msg-bubble"
              :class="[m.type, m.history?'history':'']"
              x-html="renderMsg(m.text)"></div>
            <div class="msg-ts" x-text="m.ts"></div>
          </div>
        </div>
      </template>
      <div id="feed-end"></div>
    </div>

    <!-- Bottom bar -->
    <div class="bottom-bar">
      <div class="upload-wrap">
        <button class="icon-btn" @click="$refs.fileIn.click()" :disabled="uploading" aria-label="Upload doc">
          <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"
            stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M21.44 11.05l-9.19 9.19a6 6 0 01-8.49-8.49l9.19-9.19a4 4 0 015.66 5.66l-9.2 9.19a2 2 0 01-2.83-2.83l8.49-8.48"/>
          </svg>
        </button>
        <div class="upload-badge" x-show="uploading"></div>
      </div>
      <input type="file" accept=".md" x-ref="fileIn" @change="uploadDoc($event)" style="display:none"/>

      <textarea
        class="reply-input"
        x-model="replyText"
        x-ref="replyInput"
        :placeholder="askActive ? 'Type your reply…' : 'Waiting for agent…'"
        :disabled="!askActive"
        rows="1"
        @input="autoResize($el)"
        @keydown.enter.prevent="if(askActive && replyText.trim()) sendReply()"
      ></textarea>

      <button class="send-btn"
        :disabled="!askActive||!replyText.trim()"
        @click="sendReply()"
        aria-label="Send">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"
          stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <line x1="22" y1="2" x2="11" y2="13"/>
          <polygon points="22 2 15 22 11 13 2 9 22 2" fill="currentColor" stroke="none"/>
        </svg>
      </button>
    </div>
  </div>

  <!-- ── Command sheet backdrop ──────────────────────────────── -->
  <div x-show="showMenu" class="backdrop" @click="showMenu=false"
    x-transition:enter="transition ease-out duration-200"
    x-transition:enter-start="opacity-0" x-transition:enter-end="opacity-100"
    x-transition:leave="transition ease-in duration-150"
    x-transition:leave-start="opacity-100" x-transition:leave-end="opacity-0"></div>

  <!-- ── Command sheet ───────────────────────────────────────── -->
  <div class="sheet" :class="showMenu ? 'open' : ''">
    <div class="sheet-handle"></div>
    <div class="sheet-header">
      <span class="sheet-title">Commands</span>
      <button class="icon-btn" @click="showMenu=false" aria-label="Close">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"
          stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <line x1="18" y1="6" x2="6" y2="18"/>
          <line x1="6" y1="6" x2="18" y2="18"/>
        </svg>
      </button>
    </div>

    <div class="sheet-section">
      <div class="sheet-label">Info</div>
      <button class="sheet-cmd" @click="cmd('/api/status','status')">
        <span class="cmd-icon">📊</span>
        <span class="cmd-label">Status</span>
        <span class="cmd-desc">Agent state</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/score','score')">
        <span class="cmd-icon">🎯</span>
        <span class="cmd-label">Trust score</span>
        <span class="cmd-desc">Clarification progress</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/tasks','tasks')">
        <span class="cmd-icon">📋</span>
        <span class="cmd-label">Tasks</span>
        <span class="cmd-desc">Current plan</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/log','log')">
        <span class="cmd-icon">📜</span>
        <span class="cmd-label">Log</span>
        <span class="cmd-desc">Recent events</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/usage','usage')">
        <span class="cmd-icon">📈</span>
        <span class="cmd-label">Usage</span>
        <span class="cmd-desc">LLM token usage</span>
      </button>
    </div>

    <div class="sheet-section">
      <div class="sheet-label">Actions</div>
      <button class="sheet-cmd" @click="cmd('/api/scan','scan')">
        <span class="cmd-icon">🔍</span>
        <span class="cmd-label">Scan repo</span>
        <span class="cmd-desc">Find gaps &amp; bugs</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/pause','pause')">
        <span class="cmd-icon">⏸</span>
        <span class="cmd-label">Pause</span>
        <span class="cmd-desc">Pause the agent</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/resume','resume')">
        <span class="cmd-icon">▶</span>
        <span class="cmd-label">Resume</span>
        <span class="cmd-desc">Resume the agent</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/replan','replan')">
        <span class="cmd-icon">🔄</span>
        <span class="cmd-label">Replan</span>
        <span class="cmd-desc">Regenerate the plan</span>
      </button>
    </div>

    <div class="sheet-section">
      <div class="sheet-label">Danger</div>
      <button class="sheet-cmd" @click="cmd('/api/clear','clear')">
        <span class="cmd-icon">🧹</span>
        <span class="cmd-label">Clear history</span>
        <span class="cmd-desc">Wipe message feed</span>
      </button>
      <button class="sheet-cmd danger" @click="logout()">
        <span class="cmd-icon">🚪</span>
        <span class="cmd-label">Logout</span>
      </button>
    </div>
  </div>

<script>
function kaptaan() {
  return {
    // ── Auth state ───────────────────────────────────────────────
    screen:   'loading',   // loading | setup | login | app
    authUser: '',
    authPass: '',
    authErr:  '',
    authBusy: false,

    // ── App state ────────────────────────────────────────────────
    messages:  [],
    status:    { state: 'new', trust: 0, project: '', plan: 'none' },
    builderStatus: { taskTitle: '', milestone: '', detail: '' },
    askActive: false,
    replyText: '',
    showMenu:  false,
    uploading: false,
    connected: false,
    _es:       null,          // EventSource reference

    // ── Lifecycle ────────────────────────────────────────────────
    async init() {
      try {
        const r  = await fetch('/api/auth/status');
        const d  = await r.json();
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

    // ── Auth ─────────────────────────────────────────────────────
    async submitAuth() {
      this.authErr  = '';
      this.authBusy = true;
      const url = this.screen === 'setup' ? '/api/auth/setup' : '/api/auth/login';
      try {
        const r = await fetch(url, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username: this.authUser, password: this.authPass }),
        });
        const d = await r.json();
        if (!r.ok) {
          this.authErr = d.error || 'Something went wrong';
        } else {
          this.authPass = '';
          this.screen   = 'app';
          this.$nextTick(() => this.connectSSE());
        }
      } catch {
        this.authErr = 'Network error — please try again';
      } finally {
        this.authBusy = false;
      }
    },

    async logout() {
      this.showMenu = false;
      await fetch('/api/auth/logout', { method: 'POST' });
      if (this._es) { this._es.close(); this._es = null; }
      this.messages   = [];
      this.connected  = false;
      this.builderStatus = { taskTitle: '', milestone: '', detail: '' };
      this.askActive  = false;
      this.replyText  = '';
      this.authUser   = '';
      this.authPass   = '';
      this.screen     = 'login';
    },

    // ── SSE ──────────────────────────────────────────────────────
    connectSSE() {
      if (this._es) this._es.close();
      const es = new EventSource('/events');
      this._es = es;

      es.addEventListener('status', e => {
        try { this.status = JSON.parse(e.data); } catch {}
      });

      es.addEventListener('ask_active', () => { this.askActive = true; });
      es.addEventListener('ask_done',   () => { this.askActive = false; });

      es.addEventListener('msg', e => {
        try {
          const m = JSON.parse(e.data);
          if (m.type === 'builder_status') {
            this.builderStatus = {
              taskTitle: m.task_title || '',
              milestone: m.milestone || '',
              detail: m.detail || '',
            };
            if (m.milestone === 'pr_opened') {
              setTimeout(() => {
                this.builderStatus = { taskTitle: '', milestone: '', detail: '' };
              }, 5000);
            }
            return;
          }
          if (m.type === 'pr_review') {
            this.push({ ...m, isPRReview: true });
            this.askActive = true;
            return;
          }
          this.push(m);
          if (m.type === 'ask') this.askActive = true;
        } catch {}
      });

      es.addEventListener('history_end', () => {
        if (this.messages.length > 0) {
          this.messages.push({ type: 'separator', text: '— earlier —' });
        }
        this.scrollBottom();
      });

      es.onopen = () => { this.connected = true; };

      es.onerror = () => {
        this.connected = false;
        // If the server returns 401 the EventSource will error out.
        // Check auth and redirect to login if the session is gone.
        fetch('/api/auth/status').then(r => r.json()).then(d => {
          if (!d.loggedIn) {
            es.close();
            this.screen = 'login';
          }
        }).catch(() => {});
      };
    },

    // ── Message helpers ──────────────────────────────────────────
    push(m) {
      this.messages.push(m);
      this.$nextTick(() => this.scrollBottom());
    },

    scrollBottom() {
      const el = document.getElementById('feed-end');
      if (el) el.scrollIntoView({ behavior: 'smooth', block: 'end' });
    },

    renderMsg(text) {
      if (!text) return '';
      return DOMPurify.sanitize(marked.parse(text));
    },

    builderStatusLabel() {
      const icons = {
        started: '🔨', coding: '✍️', building: '⚙️', testing: '🧪', pr_opened: '✅'
      };
      const icon = icons[this.builderStatus.milestone] || '⏳';
      return icon + ' ' + this.builderStatus.taskTitle + ': ' + this.builderStatus.detail;
    },

    // ── Reply ────────────────────────────────────────────────────
    async sendReply() {
      const text = this.replyText.trim();
      if (!text || !this.askActive) return;
      this.replyText = '';
      this.$nextTick(() => this.autoResize(this.$refs.replyInput));
      const r = await fetch('/api/reply', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text }),
      });
      if (r.status === 401) { this.screen = 'login'; }
    },

    // ── Commands ─────────────────────────────────────────────────
    async cmd(url, label) {
      this.showMenu = false;
      const r = await fetch(url);
      if (r.status === 401) { this.screen = 'login'; return; }
      const d = await r.json();
      const keys = Object.keys(d);
      let text;
      if (keys.length === 1 && typeof d[keys[0]] === 'string') {
        text = '**' + label.toUpperCase() + '**\n' + d[keys[0]];
      } else {
        text = '**' + label.toUpperCase() + '**\nBTICKBTICKBTICKjson\n' + JSON.stringify(d, null, 2) + '\nBTICKBTICKBTICK';
      }
      this.push({ type: 'message', text: text, ts: new Date().toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'}) });
    },

    // ── Upload ───────────────────────────────────────────────────
    async uploadDoc(event) {
      const file = event.target.files[0];
      if (!file) return;
      event.target.value = '';
      this.uploading = true;
      const fd = new FormData();
      fd.append('file', file);
      try {
        const r = await fetch('/api/upload', { method: 'POST', body: fd });
        if (r.status === 401) { this.screen = 'login'; return; }
      } finally {
        this.uploading = false;
      }
    },

    // ── Auto-resize textarea ─────────────────────────────────────
    autoResize(el) {
      el.style.height = 'auto';
      el.style.height = Math.min(el.scrollHeight, 120) + 'px';
    },
  };
}
</script>
</body>
</html>`
