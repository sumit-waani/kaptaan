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
  <link href="https://fonts.googleapis.com/css2?family=Geist+Mono:wght@300;400;500;600&family=Geist:wght@300;400;500;600;700&display=swap" rel="stylesheet"/>
  <style>
    :root {
      --bg:         #0a0a0a;
      --bg1:        #111111;
      --bg2:        #1a1a1a;
      --bg3:        #222222;
      --line:       #2a2a2a;
      --line2:      #333333;
      --text:       #f0f0f0;
      --text2:      #a0a0a0;
      --text3:      #606060;
      --white:      #ffffff;

      --safe-top:    env(safe-area-inset-top,    0px);
      --safe-bottom: env(safe-area-inset-bottom, 0px);
      --safe-left:   env(safe-area-inset-left,   0px);
      --safe-right:  env(safe-area-inset-right,  0px);

      --font:       'Geist', -apple-system, BlinkMacSystemFont, sans-serif;
      --mono:       'Geist Mono', 'SF Mono', monospace;
    }

    *, *::before, *::after {
      box-sizing: border-box;
      margin: 0; padding: 0;
      -webkit-tap-highlight-color: transparent;
    }

    html {
      height: 100%;
      height: -webkit-fill-available;
      overscroll-behavior: none;
    }

    body {
      font-family: var(--font);
      background: var(--bg);
      color: var(--text);
      font-size: 15px;
      line-height: 1.5;
      min-height: 100vh;
      min-height: -webkit-fill-available;
      min-height: 100dvh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }

    /* ── Spinner ──────────────────────────────────────────────── */
    .auth-loading-wrap {
      flex: 1;
      display: flex;
      align-items: center;
      justify-content: center;
      background: var(--bg);
    }

    .spinner {
      width: 28px; height: 28px;
      border: 2px solid var(--line2);
      border-top-color: var(--text2);
      border-radius: 50%;
      animation: spin 0.7s linear infinite;
    }

    @keyframes spin { to { transform: rotate(360deg); } }

    /* ── Auth ─────────────────────────────────────────────────── */
    .auth-wrap {
      flex: 1;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      padding: calc(var(--safe-top) + 40px) 24px calc(var(--safe-bottom) + 40px);
      background: var(--bg);
    }

    .auth-wordmark {
      font-family: var(--mono);
      font-size: 13px;
      font-weight: 500;
      letter-spacing: 0.2em;
      text-transform: uppercase;
      color: var(--text3);
      margin-bottom: 6px;
    }

    .auth-title-big {
      font-size: 32px;
      font-weight: 700;
      letter-spacing: -1px;
      color: var(--white);
      margin-bottom: 40px;
    }

    .auth-card {
      width: 100%;
      max-width: 360px;
      background: var(--bg1);
      border: 1px solid var(--line);
      border-radius: 16px;
      padding: 28px 24px;
    }

    .auth-card-title {
      font-size: 16px;
      font-weight: 600;
      color: var(--text2);
      margin-bottom: 24px;
      font-family: var(--mono);
      letter-spacing: 0.03em;
      text-transform: uppercase;
      font-size: 11px;
    }

    .field {
      display: flex;
      flex-direction: column;
      gap: 7px;
      margin-bottom: 14px;
    }

    .field label {
      font-size: 11px;
      font-weight: 600;
      color: var(--text3);
      text-transform: uppercase;
      letter-spacing: 0.08em;
      font-family: var(--mono);
    }

    .field input {
      background: var(--bg);
      border: 1px solid var(--line2);
      border-radius: 10px;
      padding: 13px 14px;
      color: var(--text);
      font-size: 15px;
      font-family: var(--font);
      outline: none;
      width: 100%;
      transition: border-color 0.15s;
      -webkit-appearance: none;
      appearance: none;
    }

    .field input:focus {
      border-color: var(--text2);
    }

    .field input::placeholder {
      color: var(--text3);
    }

    .auth-error {
      background: rgba(255,255,255,0.05);
      border: 1px solid var(--line2);
      color: var(--text2);
      border-radius: 8px;
      padding: 10px 14px;
      font-size: 13px;
      margin-bottom: 14px;
    }

    .auth-btn {
      width: 100%;
      padding: 14px;
      border-radius: 10px;
      border: none;
      background: var(--white);
      color: var(--bg);
      font-size: 14px;
      font-weight: 700;
      font-family: var(--font);
      cursor: pointer;
      transition: opacity 0.15s, transform 0.1s;
      margin-top: 6px;
      letter-spacing: 0.01em;
    }

    .auth-btn:active { transform: scale(0.98); opacity: 0.85; }
    .auth-btn:disabled { opacity: 0.25; cursor: not-allowed; transform: none; }

    /* ── App shell ────────────────────────────────────────────── */
    .app {
      flex: 1;
      display: flex;
      flex-direction: column;
      overflow: hidden;
      position: relative;
    }

    /* ── Header ───────────────────────────────────────────────── */
    .app-header {
      flex-shrink: 0;
      background: var(--bg);
      border-bottom: 1px solid var(--line);
      padding-top: calc(var(--safe-top) + 12px);
      padding-bottom: 12px;
      padding-left: calc(var(--safe-left) + 16px);
      padding-right: calc(var(--safe-right) + 8px);
      display: flex;
      align-items: center;
      gap: 10px;
      min-height: 0;
    }

    .header-left {
      display: flex;
      align-items: center;
      gap: 8px;
      flex-shrink: 0;
    }

    .conn-dot {
      width: 6px; height: 6px;
      border-radius: 50%;
      background: var(--text3);
      flex-shrink: 0;
      transition: background 0.4s;
    }

    .conn-dot.live { background: var(--text2); }

    .state-pill {
      font-family: var(--mono);
      font-size: 10px;
      font-weight: 500;
      letter-spacing: 0.1em;
      text-transform: uppercase;
      color: var(--text3);
      border: 1px solid var(--line2);
      border-radius: 4px;
      padding: 3px 7px;
      flex-shrink: 0;
      transition: color 0.2s, border-color 0.2s;
    }

    .state-pill.executing { color: var(--text); border-color: var(--text2); }
    .state-pill.clarifying { color: var(--text2); border-color: var(--text2); }
    .state-pill.planning { color: var(--text2); border-color: var(--line2); }
    .state-pill.error { color: var(--text2); border-color: var(--text3); }

    .trust-num {
      font-family: var(--mono);
      font-size: 11px;
      color: var(--text3);
      flex-shrink: 0;
    }

    .header-project {
      flex: 1;
      font-size: 13px;
      font-weight: 500;
      color: var(--text2);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      text-align: center;
    }

    .builder-pill {
      font-family: var(--mono);
      font-size: 10px;
      color: var(--text3);
      background: var(--bg1);
      border: 1px solid var(--line);
      border-radius: 4px;
      padding: 3px 8px;
      max-width: 160px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      flex-shrink: 1;
    }

    .icon-btn {
      background: none;
      border: none;
      cursor: pointer;
      color: var(--text3);
      width: 44px; height: 44px;
      border-radius: 8px;
      display: flex;
      align-items: center;
      justify-content: center;
      flex-shrink: 0;
      transition: background 0.12s, color 0.12s;
    }

    .icon-btn:active { background: var(--bg2); color: var(--text); }
    .icon-btn svg { width: 20px; height: 20px; }

    /* ── Feed ─────────────────────────────────────────────────── */
    .feed {
      flex: 1;
      overflow-y: auto;
      overflow-x: hidden;
      -webkit-overflow-scrolling: touch;
      padding: 12px 16px 8px;
      display: flex;
      flex-direction: column;
      gap: 2px;
    }

    /* Scrollbar */
    .feed::-webkit-scrollbar { width: 3px; }
    .feed::-webkit-scrollbar-track { background: transparent; }
    .feed::-webkit-scrollbar-thumb { background: var(--line2); border-radius: 2px; }

    .msg-row {
      display: flex;
      flex-direction: column;
      max-width: 82%;
      margin-bottom: 8px;
    }

    .msg-row.agent { align-self: flex-start; }
    .msg-row.user-row { align-self: flex-end; }

    .msg-bubble {
      padding: 10px 13px;
      font-size: 14px;
      line-height: 1.6;
      word-break: break-word;
      border-radius: 14px;
    }

    /* Agent message */
    .msg-bubble.message {
      background: var(--bg1);
      border: 1px solid var(--line);
      border-bottom-left-radius: 4px;
      color: var(--text);
    }

    /* Clarifying ask */
    .msg-bubble.ask {
      background: var(--bg1);
      border: 1px solid var(--line2);
      border-bottom-left-radius: 4px;
      color: var(--text);
    }

    /* Clarifying ask — subtle left accent */
    .msg-bubble.ask::before {
      content: '';
      display: block;
      width: 2px;
      height: 100%;
      background: var(--text3);
      position: absolute;
      left: 0; top: 0;
      border-radius: 2px 0 0 2px;
    }

    .msg-bubble.ask {
      position: relative;
      padding-left: 14px;
    }

    /* User reply */
    .msg-bubble.reply {
      background: var(--bg3);
      border: 1px solid var(--line2);
      border-bottom-right-radius: 4px;
      color: var(--text);
    }

    .msg-bubble.history { opacity: 0.45; }

    .msg-ts {
      font-family: var(--mono);
      font-size: 10px;
      color: var(--text3);
      margin-top: 3px;
      padding: 0 3px;
    }

    .msg-row.user-row .msg-ts { text-align: right; }

    /* Markdown inside bubbles */
    .msg-bubble p { margin: 0 0 6px; }
    .msg-bubble p:last-child { margin-bottom: 0; }

    .msg-bubble code {
      background: var(--bg3);
      padding: 2px 5px;
      border-radius: 4px;
      font-size: 12px;
      font-family: var(--mono);
    }

    .msg-bubble pre {
      background: var(--bg);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 10px 12px;
      overflow-x: auto;
      margin: 6px 0;
      font-size: 12px;
      font-family: var(--mono);
    }

    .msg-bubble pre code { background: none; padding: 0; }
    .msg-bubble ul, .msg-bubble ol { padding-left: 18px; margin: 4px 0; }
    .msg-bubble strong { color: var(--white); font-weight: 600; }

    /* ── PR Card ──────────────────────────────────────────────── */
    .pr-card {
      border: 1px solid var(--line2);
      border-radius: 12px;
      background: var(--bg1);
      padding: 14px 15px;
      display: flex;
      flex-direction: column;
      gap: 10px;
      max-width: 100%;
    }

    .pr-card-header {
      display: flex;
      align-items: flex-start;
      gap: 8px;
      flex-wrap: wrap;
    }

    .pr-badge {
      font-family: var(--mono);
      font-size: 10px;
      letter-spacing: 0.08em;
      text-transform: uppercase;
      background: var(--bg3);
      color: var(--text2);
      border-radius: 4px;
      padding: 3px 7px;
      flex-shrink: 0;
      margin-top: 1px;
    }

    .pr-title {
      font-size: 14px;
      font-weight: 600;
      flex: 1;
      color: var(--text);
      line-height: 1.4;
    }

    .pr-link {
      font-family: var(--mono);
      font-size: 11px;
      color: var(--text2);
      text-decoration: none;
      flex-shrink: 0;
      border: 1px solid var(--line2);
      border-radius: 4px;
      padding: 3px 8px;
    }

    .pr-note {
      font-size: 13px;
      color: var(--text2);
      line-height: 1.55;
    }

    .pr-diff {
      font-size: 12px;
      color: var(--text3);
    }

    .pr-diff summary {
      cursor: pointer;
      user-select: none;
      margin-bottom: 6px;
      font-family: var(--mono);
      font-size: 11px;
      letter-spacing: 0.04em;
    }

    .pr-diff pre {
      background: var(--bg);
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 10px;
      overflow-x: auto;
      font-family: var(--mono);
      font-size: 11px;
      max-height: 240px;
      overflow-y: auto;
    }

    .pr-ts {
      font-family: var(--mono);
      font-size: 10px;
      color: var(--text3);
    }

    /* ── Separator ────────────────────────────────────────────── */
    .history-sep {
      display: flex;
      align-items: center;
      gap: 10px;
      color: var(--text3);
      font-family: var(--mono);
      font-size: 10px;
      letter-spacing: 0.08em;
      padding: 8px 0;
    }

    .history-sep::before, .history-sep::after {
      content: '';
      flex: 1;
      height: 1px;
      background: var(--line);
    }

    /* ── Bottom bar ───────────────────────────────────────────── */
    .bottom-bar {
      flex-shrink: 0;
      background: var(--bg);
      border-top: 1px solid var(--line);
      padding: 8px calc(var(--safe-right) + 10px) calc(var(--safe-bottom) + 8px) calc(var(--safe-left) + 8px);
      display: flex;
      align-items: flex-end;
      gap: 8px;
    }

    .reply-input {
      flex: 1;
      background: var(--bg1);
      border: 1px solid var(--line2);
      border-radius: 12px;
      padding: 11px 14px;
      color: var(--text);
      font-size: 15px;
      font-family: var(--font);
      outline: none;
      resize: none;
      max-height: 120px;
      overflow-y: auto;
      line-height: 1.45;
      transition: border-color 0.15s;
      -webkit-overflow-scrolling: touch;
      display: block;
      width: 100%;
    }

    .reply-input:focus { border-color: var(--text3); }
    .reply-input:disabled { opacity: 0.3; cursor: not-allowed; }
    .reply-input::placeholder { color: var(--text3); }

    .send-btn {
      width: 42px; height: 42px;
      border-radius: 10px;
      border: none;
      background: var(--white);
      color: var(--bg);
      display: flex;
      align-items: center;
      justify-content: center;
      cursor: pointer;
      flex-shrink: 0;
      transition: opacity 0.15s, transform 0.1s;
    }

    .send-btn:active { transform: scale(0.93); }
    .send-btn:disabled { opacity: 0.15; cursor: not-allowed; transform: none; }
    .send-btn svg { width: 18px; height: 18px; }

    .upload-wrap { position: relative; flex-shrink: 0; }

    .upload-badge {
      position: absolute;
      top: 6px; right: 6px;
      width: 6px; height: 6px;
      background: var(--text2);
      border-radius: 50%;
      border: 1.5px solid var(--bg);
    }

    /* ── Sheet ────────────────────────────────────────────────── */
    .backdrop {
      position: fixed; inset: 0;
      background: rgba(0,0,0,0.7);
      z-index: 100;
      backdrop-filter: blur(4px);
      -webkit-backdrop-filter: blur(4px);
    }

    .sheet {
      position: fixed;
      left: 0; right: 0; bottom: 0;
      background: var(--bg1);
      border-radius: 18px 18px 0 0;
      border-top: 1px solid var(--line);
      z-index: 101;
      transform: translateY(100%);
      transition: transform 0.3s cubic-bezier(0.32, 0, 0.15, 1);
      max-height: 80vh;
      overflow-y: auto;
      -webkit-overflow-scrolling: touch;
      padding-bottom: calc(var(--safe-bottom) + 16px);
    }

    .sheet.open { transform: translateY(0); }

    .sheet-handle {
      width: 32px; height: 3px;
      background: var(--line2);
      border-radius: 2px;
      margin: 12px auto 8px;
    }

    .sheet-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 4px 16px 14px;
      border-bottom: 1px solid var(--line);
    }

    .sheet-title {
      font-family: var(--mono);
      font-size: 11px;
      font-weight: 500;
      letter-spacing: 0.1em;
      text-transform: uppercase;
      color: var(--text3);
    }

    .sheet-section {
      padding: 6px 0;
      border-bottom: 1px solid var(--line);
    }

    .sheet-section:last-child { border-bottom: none; }

    .sheet-label {
      font-family: var(--mono);
      font-size: 10px;
      font-weight: 600;
      letter-spacing: 0.1em;
      text-transform: uppercase;
      color: var(--text3);
      padding: 10px 16px 4px;
    }

    .sheet-cmd {
      display: flex;
      align-items: center;
      gap: 14px;
      width: 100%;
      padding: 13px 16px;
      background: none;
      border: none;
      color: var(--text);
      font-size: 15px;
      font-family: var(--font);
      cursor: pointer;
      text-align: left;
      transition: background 0.1s;
    }

    .sheet-cmd:active { background: var(--bg2); }

    .cmd-icon {
      font-size: 16px;
      width: 24px;
      text-align: center;
      flex-shrink: 0;
    }

    .cmd-label { flex: 1; font-weight: 400; }

    .cmd-desc {
      font-size: 12px;
      color: var(--text3);
      font-family: var(--mono);
    }

    .sheet-cmd.danger { color: var(--text2); }

    [x-cloak] { display: none !important; }
  </style>
</head>
<body x-data="kaptaan()" x-init="init()" x-cloak>

  <!-- Loading -->
  <div x-show="screen==='loading'" class="auth-loading-wrap">
    <div class="spinner"></div>
  </div>

  <!-- Auth -->
  <div x-show="screen==='setup'||screen==='login'" class="auth-wrap">
    <div class="auth-wordmark">Kaptaan</div>
    <div class="auth-title-big" x-text="screen==='setup' ? 'Create account' : 'Welcome back'"></div>

    <div class="auth-card">
      <div class="auth-card-title" x-text="screen==='setup' ? 'Setup' : 'Sign in'"></div>
      <form @submit.prevent="submitAuth()">
        <div class="field">
          <label>Username</label>
          <input type="text" x-model="authUser"
            autocomplete="username" autocapitalize="off"
            autocorrect="off" spellcheck="false"
            placeholder="username"
            :disabled="authBusy"/>
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

    <!-- Header -->
    <header class="app-header">
      <div class="header-left">
        <div class="conn-dot" :class="connected?'live':''"></div>
        <span class="state-pill"
          :class="(status.state||'').toLowerCase()"
          x-text="status.state||'idle'"></span>
        <span class="trust-num"
          x-show="status.trust>0"
          x-text="Math.round(status.trust)+'%'"></span>
      </div>

      <div class="header-project" x-text="status.project||'Kaptaan'"></div>

      <div class="builder-pill" x-show="builderStatus.milestone" x-text="builderStatusLabel()"></div>

      <button class="icon-btn" @click="showMenu=true" aria-label="Commands">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"
          stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
          <line x1="4" y1="8" x2="20" y2="8"/>
          <line x1="4" y1="16" x2="20" y2="16"/>
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
                  <a class="pr-link" :href="m.pr_url" target="_blank" rel="noopener noreferrer">View ↗</a>
                </div>
                <div class="pr-note" x-html="renderMsg(m.manager_note)"></div>
                <details class="pr-diff">
                  <summary>show diff</summary>
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
            stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
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
        :placeholder="askActive ? 'Reply…' : 'Waiting for agent…'"
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

  <!-- Sheet backdrop -->
  <div x-show="showMenu" class="backdrop" @click="showMenu=false"
    x-transition:enter="transition ease-out duration-200"
    x-transition:enter-start="opacity-0" x-transition:enter-end="opacity-100"
    x-transition:leave="transition ease-in duration-150"
    x-transition:leave-start="opacity-100" x-transition:leave-end="opacity-0"></div>

  <!-- Command sheet -->
  <div class="sheet" :class="showMenu ? 'open' : ''">
    <div class="sheet-handle"></div>
    <div class="sheet-header">
      <span class="sheet-title">Commands</span>
      <button class="icon-btn" @click="showMenu=false" aria-label="Close">
        <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24"
          stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
          <line x1="18" y1="6" x2="6" y2="18"/>
          <line x1="6" y1="6" x2="18" y2="18"/>
        </svg>
      </button>
    </div>

    <div class="sheet-section">
      <div class="sheet-label">Info</div>
      <button class="sheet-cmd" @click="cmd('/api/status','status')">
        <span class="cmd-icon">◈</span>
        <span class="cmd-label">Status</span>
        <span class="cmd-desc">Agent state</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/score','score')">
        <span class="cmd-icon">◎</span>
        <span class="cmd-label">Trust score</span>
        <span class="cmd-desc">Clarification progress</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/tasks','tasks')">
        <span class="cmd-icon">≡</span>
        <span class="cmd-label">Tasks</span>
        <span class="cmd-desc">Current plan</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/log','log')">
        <span class="cmd-icon">⌇</span>
        <span class="cmd-label">Log</span>
        <span class="cmd-desc">Recent events</span>
      </button>
      <button class="sheet-cmd" @click="cmd('/api/usage','usage')">
        <span class="cmd-icon">↗</span>
        <span class="cmd-label">Usage</span>
        <span class="cmd-desc">LLM token usage</span>
      </button>
    </div>

    <div class="sheet-section">
      <div class="sheet-label">Actions</div>
      <button class="sheet-cmd" @click="cmd('/api/scan','scan')">
        <span class="cmd-icon">⊙</span>
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
        <span class="cmd-icon">↺</span>
        <span class="cmd-label">Replan</span>
        <span class="cmd-desc">Regenerate the plan</span>
      </button>
    </div>

    <div class="sheet-section">
      <div class="sheet-label">Danger</div>
      <button class="sheet-cmd" @click="cmd('/api/clear','clear')">
        <span class="cmd-icon">⌫</span>
        <span class="cmd-label">Clear history</span>
        <span class="cmd-desc">Wipe message feed</span>
      </button>
      <button class="sheet-cmd danger" @click="logout()">
        <span class="cmd-icon">→</span>
        <span class="cmd-label">Logout</span>
      </button>
    </div>
  </div>

<script>
function kaptaan() {
  return {
    // ── Auth state
    screen:   'loading',
    authUser: '',
    authPass: '',
    authErr:  '',
    authBusy: false,

    // ── App state
    messages:  [],
    status:    { state: 'new', trust: 0, project: '', plan: 'none' },
    builderStatus: { taskTitle: '', milestone: '', detail: '' },
    askActive: false,
    replyText: '',
    showMenu:  false,
    uploading: false,
    connected: false,
    _es:       null,

    // ── Lifecycle
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

    // ── Auth
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

    // ── SSE
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
          this.messages.push({ type: 'separator', text: 'earlier' });
        }
        this.scrollBottom();
      });

      es.onopen = () => { this.connected = true; };

      es.onerror = () => {
        this.connected = false;
        fetch('/api/auth/status').then(r => r.json()).then(d => {
          if (!d.loggedIn) { es.close(); this.screen = 'login'; }
        }).catch(() => {});
      };
    },

    // ── Helpers
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
      const icons = { started: '○', coding: '◎', building: '◈', testing: '◉', pr_opened: '●' };
      const icon = icons[this.builderStatus.milestone] || '○';
      return icon + ' ' + this.builderStatus.taskTitle + ': ' + this.builderStatus.detail;
    },

    // ── Reply
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

    // ── Commands
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

    // ── Upload
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

    // ── Auto-resize textarea
    autoResize(el) {
      el.style.height = 'auto';
      el.style.height = Math.min(el.scrollHeight, 120) + 'px';
    },
  };
}
</script>
</body>
</html>`
