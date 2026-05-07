package web

const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
<meta name="mobile-web-app-capable" content="yes" />
<meta name="apple-mobile-web-app-capable" content="yes" />
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
<title>Kaptaan</title>
<script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/dompurify@3.0.6/dist/purify.min.js"></script>
<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.14.1/dist/cdn.min.js"></script>
<style>
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

:root {
  --black:   #000000;
  --ink:     #0a0a0a;
  --surface: #111111;
  --card:    #1c1c1e;
  --border:  #2c2c2e;
  --border2: #3a3a3c;
  --dim:     #555555;
  --muted:   #8e8e93;
  --sub:     #aeaeb2;
  --text:    #f2f2f7;
  --white:   #ffffff;
  --font:    ui-monospace, 'Cascadia Code', 'SF Mono', Monaco, Menlo, Consolas, monospace;
  --radius:  12px;
  --safe-t:  env(safe-area-inset-top, 0px);
  --safe-b:  env(safe-area-inset-bottom, 0px);
  --safe-l:  env(safe-area-inset-left, 0px);
  --safe-r:  env(safe-area-inset-right, 0px);
  --header:  52px;
  --proj:    44px;
  --compose: 80px;
}

html, body {
  height: 100%;
  height: -webkit-fill-available;
  background: var(--black);
  color: var(--text);
  font-family: var(--font);
  font-size: 13px;
  line-height: 1.5;
  overscroll-behavior: none;
  -webkit-tap-highlight-color: transparent;
  -webkit-font-smoothing: antialiased;
}

[x-cloak] { display: none !important; }

a { color: inherit; text-decoration: none; }
button { font-family: var(--font); cursor: pointer; border: none; background: none; color: inherit; }
input, textarea { font-family: var(--font); font-size: 16px; color: var(--text); background: transparent; border: none; outline: none; }
input::placeholder, textarea::placeholder { color: var(--dim); }

/* ─── Auth ─────────────────────────────────────────────────────────────── */
.auth-wrap {
  min-height: 100%;
  min-height: -webkit-fill-available;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: calc(var(--safe-t) + 24px) calc(var(--safe-l) + 20px) calc(var(--safe-b) + 24px) calc(var(--safe-r) + 20px);
  background: var(--black);
}
.auth-card {
  width: 100%;
  max-width: 360px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 28px 24px;
}
.auth-logo {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 20px;
}
.auth-logo span {
  font-size: 18px;
  font-weight: 600;
  letter-spacing: -0.02em;
  color: var(--white);
}
.auth-sub {
  font-size: 12px;
  color: var(--muted);
  margin-bottom: 20px;
}
.auth-field {
  width: 100%;
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 12px 14px;
  font-size: 16px;
  color: var(--text);
  margin-bottom: 10px;
  display: block;
}
.auth-field:focus { border-color: var(--border2); }
.auth-err {
  font-size: 12px;
  color: #ff453a;
  margin-bottom: 10px;
}
.btn-primary {
  width: 100%;
  background: var(--white);
  color: var(--black);
  border-radius: 8px;
  padding: 13px 14px;
  font-size: 14px;
  font-weight: 600;
  font-family: var(--font);
  cursor: pointer;
  border: none;
  transition: opacity 0.1s;
  -webkit-tap-highlight-color: transparent;
}
.btn-primary:active { opacity: 0.8; }

/* ─── App shell ─────────────────────────────────────────────────────────── */
.app {
  display: flex;
  flex-direction: column;
  height: 100%;
  height: -webkit-fill-available;
  overflow: hidden;
  background: var(--black);
}

/* ─── Header ────────────────────────────────────────────────────────────── */
.header {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  padding-top: var(--safe-t);
  padding-left: calc(var(--safe-l) + 14px);
  padding-right: calc(var(--safe-r) + 14px);
  height: calc(var(--header) + var(--safe-t));
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  position: relative;
}
.header-icon {
  flex-shrink: 0;
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.header-title {
  flex: 1;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--white);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.status-pill {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: var(--muted);
  flex-shrink: 0;
}
.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--dim);
  flex-shrink: 0;
}
.status-dot.running {
  background: #ffd60a;
  box-shadow: 0 0 6px #ffd60a66;
  animation: blink 1.2s infinite;
}
.status-dot.idle { background: #30d158; }
@keyframes blink { 0%,100%{opacity:.5} 50%{opacity:1} }

.icon-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: 8px;
  flex-shrink: 0;
  -webkit-tap-highlight-color: transparent;
}
.icon-btn:active { background: var(--card); }

/* ─── Project bar ───────────────────────────────────────────────────────── */
.proj-bar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  height: var(--proj);
  padding: 0 calc(var(--safe-l) + 14px) 0 calc(var(--safe-r) + 14px);
  background: var(--ink);
  border-bottom: 1px solid var(--border);
  overflow-x: auto;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;
}
.proj-bar::-webkit-scrollbar { display: none; }

.proj-select-wrap {
  flex: 1;
  min-width: 0;
  position: relative;
}
.proj-select {
  width: 100%;
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 7px 30px 7px 10px;
  font-size: 13px;
  font-family: var(--font);
  color: var(--text);
  appearance: none;
  -webkit-appearance: none;
}
.proj-select-caret {
  position: absolute;
  right: 10px;
  top: 50%;
  transform: translateY(-50%);
  pointer-events: none;
  color: var(--muted);
}
.proj-new-btn {
  flex-shrink: 0;
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 7px 12px;
  font-size: 13px;
  font-family: var(--font);
  color: var(--sub);
  white-space: nowrap;
  -webkit-tap-highlight-color: transparent;
}
.proj-new-btn:active { background: var(--border); }

/* ─── Feed ──────────────────────────────────────────────────────────────── */
.feed {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior: contain;
  padding: 16px calc(var(--safe-l) + 14px) 10px calc(var(--safe-r) + 14px);
  display: flex;
  flex-direction: column;
  gap: 10px;
  scrollbar-width: none;
}
.feed::-webkit-scrollbar { display: none; }

.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--dim);
  padding: 40px 0;
}
.empty-state svg { opacity: 0.3; }
.empty-state p { font-size: 12px; text-align: center; }

.bubble-row {
  display: flex;
}
.bubble-row.user { justify-content: flex-end; }
.bubble-row.agent { justify-content: flex-start; }

.bubble {
  max-width: 88%;
  border-radius: var(--radius);
  padding: 10px 12px;
  line-height: 1.55;
}
.bubble.user-bubble {
  background: var(--white);
  color: var(--black);
  border-bottom-right-radius: 3px;
}
.bubble.agent-bubble {
  background: var(--card);
  border: 1px solid var(--border);
  color: var(--text);
  border-bottom-left-radius: 3px;
}
.bubble.ask-bubble {
  background: var(--surface);
  border: 1px solid var(--border2);
  color: var(--sub);
  border-bottom-left-radius: 3px;
}
.bubble.reply-bubble {
  background: var(--card);
  border: 1px solid var(--border);
  color: var(--sub);
  border-bottom-right-radius: 3px;
}
.bubble-meta {
  font-size: 10px;
  color: var(--dim);
  margin-bottom: 4px;
  letter-spacing: 0.02em;
  text-transform: uppercase;
}
.bubble-meta.user-meta { color: rgba(0,0,0,0.4); }
.bubble-content { font-size: 13px; }
.bubble-content pre {
  background: rgba(0,0,0,0.4);
  border: 1px solid var(--border);
  padding: 8px 10px;
  border-radius: 6px;
  overflow-x: auto;
  font-size: 11px;
  margin: 6px 0;
  white-space: pre;
}
.bubble.user-bubble .bubble-content pre {
  background: rgba(0,0,0,0.08);
  border-color: rgba(0,0,0,0.1);
}
.bubble-content code {
  background: rgba(255,255,255,0.07);
  border-radius: 3px;
  padding: 1px 4px;
  font-size: 0.9em;
}
.bubble.user-bubble .bubble-content code {
  background: rgba(0,0,0,0.07);
}
.bubble-content pre code { background: transparent; padding: 0; }
.bubble-content h1,.bubble-content h2,.bubble-content h3 { font-weight: 600; margin: 6px 0 2px; }
.bubble-content ul,.bubble-content ol { padding-left: 1.2em; margin: 4px 0; }
.bubble-content p { margin: 2px 0; }
.bubble-content a { text-decoration: underline; opacity: 0.8; }

/* ─── Ask banner ─────────────────────────────────────────────────────────── */
.ask-banner {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px calc(var(--safe-l) + 14px);
  background: var(--ink);
  border-top: 1px solid var(--border);
  font-size: 11px;
  color: var(--muted);
}

/* ─── Composer ──────────────────────────────────────────────────────────── */
.composer {
  flex-shrink: 0;
  display: flex;
  align-items: flex-end;
  gap: 8px;
  padding: 10px calc(var(--safe-l) + 14px) calc(var(--safe-b) + 10px) calc(var(--safe-r) + 14px);
  background: var(--surface);
  border-top: 1px solid var(--border);
}
.composer-input {
  flex: 1;
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 16px;
  font-family: var(--font);
  color: var(--text);
  resize: none;
  line-height: 1.45;
  max-height: 120px;
  min-height: 40px;
}
.composer-input:focus { border-color: var(--border2); }
.send-btn {
  flex-shrink: 0;
  width: 40px;
  height: 40px;
  border-radius: 10px;
  background: var(--white);
  color: var(--black);
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
  transition: opacity 0.1s;
}
.send-btn:disabled { opacity: 0.3; }
.send-btn:not(:disabled):active { opacity: 0.75; }

/* ─── Settings overlay ──────────────────────────────────────────────────── */
.settings-overlay {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  flex-direction: column;
  background: var(--black);
  overflow: hidden;
}
.settings-header {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  padding-top: var(--safe-t);
  padding-left: calc(var(--safe-l) + 14px);
  padding-right: calc(var(--safe-r) + 14px);
  height: calc(var(--header) + var(--safe-t));
  background: var(--surface);
  border-bottom: 1px solid var(--border);
}
.settings-title {
  flex: 1;
  font-size: 14px;
  font-weight: 600;
  color: var(--white);
}
.back-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 13px;
  color: var(--sub);
  -webkit-tap-highlight-color: transparent;
  padding: 8px 0;
}
.back-btn:active { opacity: 0.6; }

.settings-body {
  flex: 1;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding: 0 0 calc(var(--safe-b) + 20px) 0;
  scrollbar-width: none;
}
.settings-body::-webkit-scrollbar { display: none; }

.settings-section {
  margin-top: 28px;
  padding: 0 calc(var(--safe-l) + 14px);
}
.settings-section-label {
  font-size: 11px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted);
  margin-bottom: 8px;
  padding-left: 2px;
}
.settings-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  overflow: hidden;
}
.settings-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 13px 14px;
  border-bottom: 1px solid var(--border);
  min-height: 48px;
}
.settings-row:last-child { border-bottom: none; }
.settings-row-label {
  flex: 1;
  font-size: 13px;
  color: var(--text);
  min-width: 0;
}
.settings-row-value {
  font-size: 12px;
  color: var(--muted);
  max-width: 150px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: right;
}
.settings-input {
  width: 100%;
  background: transparent;
  font-size: 16px;
  font-family: var(--font);
  color: var(--text);
  padding: 12px 14px;
  display: block;
}
.settings-input-row {
  padding: 0;
  border-bottom: 1px solid var(--border);
}
.settings-input-row:last-child { border-bottom: none; }
.settings-input-label {
  font-size: 11px;
  color: var(--muted);
  padding: 10px 14px 0;
  letter-spacing: 0.03em;
}

.action-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  padding: 13px 14px;
  font-size: 13px;
  font-family: var(--font);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  color: var(--text);
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
  margin-top: 8px;
}
.action-btn:active { background: var(--card); }
.action-btn.danger { color: #ff453a; }
.action-btn.primary { background: var(--white); color: var(--black); border-color: var(--white); font-weight: 600; }
.action-btn.primary:active { opacity: 0.8; }

.usage-row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.usage-row:last-child { border-bottom: none; }
.usage-model { font-size: 12px; color: var(--sub); }
.usage-tokens { font-size: 13px; color: var(--text); }

.memory-item {
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
}
.memory-item:last-child { border-bottom: none; }
.memory-key {
  font-size: 11px;
  color: var(--muted);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  margin-bottom: 4px;
}
.memory-content { font-size: 12px; color: var(--sub); white-space: pre-wrap; }
.memory-footer { display: flex; justify-content: space-between; align-items: center; margin-top: 6px; }
.memory-time { font-size: 10px; color: var(--dim); }
.del-btn { font-size: 11px; color: #ff453a; padding: 4px 0; }
.del-btn:active { opacity: 0.6; }

.plan-item {
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
}
.plan-item:last-child { border-bottom: none; }
.plan-filename { font-size: 12px; color: var(--sub); margin-bottom: 2px; }
.plan-meta { font-size: 10px; color: var(--dim); }
.plan-content {
  margin-top: 8px;
  background: var(--ink);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 8px 10px;
  font-size: 11px;
  color: var(--sub);
  white-space: pre-wrap;
  overflow-x: auto;
}

.doc-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
}
.doc-item:last-child { border-bottom: none; }
.doc-info { flex: 1; min-width: 0; }
.doc-name { font-size: 13px; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.doc-meta { font-size: 10px; color: var(--dim); }

.empty-list {
  padding: 20px 14px;
  font-size: 12px;
  color: var(--dim);
  text-align: center;
}

.err-msg { font-size: 12px; color: #ff453a; padding: 8px 14px; }

/* modal */
.modal-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0,0,0,0.7);
  display: flex;
  align-items: flex-end;
}
.modal-sheet {
  width: 100%;
  background: var(--surface);
  border-top: 1px solid var(--border);
  border-radius: 16px 16px 0 0;
  padding: 16px 16px calc(var(--safe-b) + 16px);
  max-height: 80vh;
  overflow-y: auto;
}
.modal-handle {
  width: 36px;
  height: 4px;
  background: var(--border2);
  border-radius: 2px;
  margin: 0 auto 16px;
}
.modal-title { font-size: 14px; font-weight: 600; color: var(--white); margin-bottom: 14px; }
.modal-input {
  width: 100%;
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 12px;
  font-size: 16px;
  font-family: var(--font);
  color: var(--text);
  margin-bottom: 10px;
  display: block;
}
.modal-input:focus { border-color: var(--border2); }
</style>
</head>
<body>
<div x-data="kaptaan()" x-init="init()" x-cloak>

  <!-- ── Auth ─────────────────────────────────────────────────────────────── -->
  <template x-if="!auth.loggedIn">
    <div class="auth-wrap">
      <div class="auth-card">
        <div class="auth-logo">
          <svg width="28" height="28" viewBox="0 0 28 28" fill="none">
            <circle cx="14" cy="8" r="3" stroke="#f2f2f7" stroke-width="1.5"/>
            <line x1="14" y1="11" x2="14" y2="22" stroke="#f2f2f7" stroke-width="1.5" stroke-linecap="round"/>
            <line x1="7" y1="16" x2="21" y2="16" stroke="#f2f2f7" stroke-width="1.5" stroke-linecap="round"/>
            <path d="M7 22 Q14 26 21 22" stroke="#f2f2f7" stroke-width="1.5" fill="none" stroke-linecap="round"/>
          </svg>
          <span>kaptaan</span>
        </div>
        <p class="auth-sub" x-text="auth.hasUser ? 'sign in to continue.' : 'create your account.'"></p>
        <input class="auth-field" type="text" placeholder="username" x-model="auth.username" autocomplete="username" autocapitalize="none" spellcheck="false" />
        <input class="auth-field" type="password" placeholder="password" x-model="auth.password" autocomplete="current-password"
          @keyup.enter="auth.hasUser ? login() : signup()" />
        <template x-if="auth.error">
          <p class="auth-err" x-text="auth.error"></p>
        </template>
        <button class="btn-primary" @click="auth.hasUser ? login() : signup()"
          x-text="auth.hasUser ? 'sign in' : 'create account'"></button>
      </div>
    </div>
  </template>

  <!-- ── Main app ──────────────────────────────────────────────────────────── -->
  <template x-if="auth.loggedIn && !showSettings">
    <div class="app">

      <!-- Header -->
      <header class="header">
        <div class="header-icon">
          <svg width="22" height="22" viewBox="0 0 28 28" fill="none">
            <circle cx="14" cy="8" r="3" stroke="#f2f2f7" stroke-width="1.5"/>
            <line x1="14" y1="11" x2="14" y2="22" stroke="#f2f2f7" stroke-width="1.5" stroke-linecap="round"/>
            <line x1="7" y1="16" x2="21" y2="16" stroke="#f2f2f7" stroke-width="1.5" stroke-linecap="round"/>
            <path d="M7 22 Q14 26 21 22" stroke="#f2f2f7" stroke-width="1.5" fill="none" stroke-linecap="round"/>
          </svg>
        </div>
        <div class="header-title" x-text="activeName()"></div>
        <div class="status-pill">
          <div class="status-dot" :class="agentRunning ? 'running' : 'idle'"></div>
          <span x-text="agentRunning ? 'running' : 'idle'"></span>
        </div>
        <button class="icon-btn" @click="openSettings()">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#8e8e93" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="3"/>
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>
          </svg>
        </button>
      </header>

      <!-- Project bar -->
      <div class="proj-bar">
        <div class="proj-select-wrap">
          <select class="proj-select" x-model.number="activeProjectID" @change="onProjectChange()">
            <template x-for="p in projects" :key="p.id">
              <option :value="p.id" x-text="p.name"></option>
            </template>
          </select>
          <svg class="proj-select-caret" width="10" height="6" viewBox="0 0 10 6" fill="#8e8e93">
            <path d="M1 1l4 4 4-4"/>
          </svg>
        </div>
        <button class="proj-new-btn" @click="openNewProject()">+ new</button>
      </div>

      <!-- Feed -->
      <div class="feed" x-ref="feed">
        <template x-if="messages.length === 0">
          <div class="empty-state">
            <svg width="40" height="40" viewBox="0 0 28 28" fill="none">
              <circle cx="14" cy="8" r="3" stroke="#555" stroke-width="1.5"/>
              <line x1="14" y1="11" x2="14" y2="22" stroke="#555" stroke-width="1.5" stroke-linecap="round"/>
              <line x1="7" y1="16" x2="21" y2="16" stroke="#555" stroke-width="1.5" stroke-linecap="round"/>
              <path d="M7 22 Q14 26 21 22" stroke="#555" stroke-width="1.5" fill="none" stroke-linecap="round"/>
            </svg>
            <p>send a message to start.</p>
          </div>
        </template>
        <template x-for="(m, i) in messages" :key="i">
          <div class="bubble-row" :class="m.type === 'user' || m.type === 'reply' ? 'user' : 'agent'">
            <div class="bubble" :class="bubbleClass(m)">
              <div class="bubble-meta" :class="m.type === 'user' ? 'user-meta' : ''"
                   x-text="bubbleLabel(m) + ' · ' + m.ts"></div>
              <div class="bubble-content" x-html="render(m.text)"></div>
            </div>
          </div>
        </template>
      </div>

      <!-- Ask banner -->
      <template x-if="askActive">
        <div class="ask-banner">
          <svg width="12" height="12" viewBox="0 0 12 12" fill="#8e8e93">
            <circle cx="6" cy="6" r="5" stroke="#8e8e93" stroke-width="1.2" fill="none"/>
            <line x1="6" y1="5" x2="6" y2="8" stroke="#8e8e93" stroke-width="1.2" stroke-linecap="round"/>
            <circle cx="6" cy="3.5" r="0.6" fill="#8e8e93"/>
          </svg>
          <span>waiting for your reply</span>
        </div>
      </template>

      <!-- Composer -->
      <div class="composer">
        <textarea class="composer-input" x-model="composer" rows="1"
          :placeholder="askActive ? 'type your reply…' : 'message kaptaan…'"
          @keydown.meta.enter="send()"
          @keydown.ctrl.enter="send()"
          @input="autoResize($event)"></textarea>
        <button class="send-btn" :disabled="!composer.trim()" @click="send()">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <line x1="22" y1="2" x2="11" y2="13"/>
            <polygon points="22 2 15 22 11 13 2 9 22 2"/>
          </svg>
        </button>
      </div>

    </div>
  </template>

  <!-- ── Settings ──────────────────────────────────────────────────────────── -->
  <template x-if="auth.loggedIn && showSettings">
    <div class="settings-overlay">
      <div class="settings-header">
        <button class="back-btn" @click="closeSettings()">
          <svg width="8" height="13" viewBox="0 0 8 14" fill="none">
            <path d="M7 1L1 7L7 13" stroke="#aeaeb2" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
          back
        </button>
        <span class="settings-title">settings</span>
        <button class="back-btn" style="color:#ff453a" @click="logout()">sign out</button>
      </div>

      <div class="settings-body">

        <!-- Project section -->
        <div class="settings-section">
          <div class="settings-section-label">project</div>
          <div class="settings-card">
            <div class="settings-input-row">
              <div class="settings-input-label">name</div>
              <input class="settings-input" type="text" x-model="edit.name" placeholder="project name" autocapitalize="none" spellcheck="false" />
            </div>
            <div class="settings-input-row">
              <div class="settings-input-label">github repo url</div>
              <input class="settings-input" type="url" x-model="edit.repo_url" placeholder="https://github.com/owner/repo" autocapitalize="none" spellcheck="false" />
            </div>
            <div class="settings-input-row">
              <div class="settings-input-label">github token <span style="color:var(--dim)">(leave blank to keep)</span></div>
              <input class="settings-input" type="password" x-model="edit.github_token" placeholder="ghp_…" autocomplete="off" />
            </div>
          </div>
          <template x-if="edit.error">
            <p class="err-msg" x-text="edit.error"></p>
          </template>
          <button class="action-btn primary" @click="saveProject()" style="margin-top:10px">save project</button>
          <button class="action-btn" @click="clearConvo()" style="margin-top:8px">clear conversation</button>
          <button class="action-btn danger" @click="deleteProject()" style="margin-top:8px">delete project</button>
        </div>

        <!-- Usage section -->
        <div class="settings-section">
          <div class="settings-section-label">usage</div>
          <div class="settings-card">
            <div class="settings-row" style="padding: 8px 14px; border-bottom: 1px solid var(--border);">
              <span class="settings-row-label" style="font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:0.04em">today</span>
            </div>
            <template x-for="r in (usage.today||[])" :key="'t'+r.model">
              <div class="usage-row">
                <span class="usage-model" x-text="r.provider + ' / ' + r.model"></span>
                <span class="usage-tokens" x-text="r.total_tokens.toLocaleString() + ' tok'"></span>
              </div>
            </template>
            <template x-if="!usage.today || usage.today.length === 0">
              <div class="empty-list">no calls today</div>
            </template>
            <div class="settings-row" style="padding: 8px 14px; border-bottom: 1px solid var(--border); border-top: 1px solid var(--border);">
              <span class="settings-row-label" style="font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:0.04em">all-time</span>
            </div>
            <template x-for="r in (usage.all||[])" :key="'a'+r.model">
              <div class="usage-row">
                <span class="usage-model" x-text="r.provider + ' / ' + r.model"></span>
                <span class="usage-tokens" x-text="r.total_tokens.toLocaleString() + ' tok'"></span>
              </div>
            </template>
            <template x-if="!usage.all || usage.all.length === 0">
              <div class="empty-list">no calls yet</div>
            </template>
          </div>
        </div>

        <!-- Memories section -->
        <div class="settings-section">
          <div class="settings-section-label">memories</div>
          <div class="settings-card">
            <template x-for="m in memories" :key="m.key">
              <div class="memory-item">
                <div class="memory-key" x-text="m.key"></div>
                <div class="memory-content" x-text="m.content"></div>
                <div class="memory-footer">
                  <span class="memory-time" x-text="m.updated_at"></span>
                  <button class="del-btn" @click="deleteMemory(m.key)">delete</button>
                </div>
              </div>
            </template>
            <template x-if="memories.length === 0">
              <div class="empty-list">no memories saved</div>
            </template>
          </div>
        </div>

        <!-- Plans section -->
        <div class="settings-section">
          <div class="settings-section-label">plans</div>
          <div class="settings-card">
            <template x-for="p in plans" :key="p.filename">
              <div class="plan-item">
                <div class="plan-filename" x-text="p.filename"></div>
                <div class="plan-meta" x-text="p.created + ' · ' + p.bytes + ' B'"></div>
                <template x-if="planView.filename === p.filename">
                  <pre class="plan-content" x-text="planView.content"></pre>
                </template>
                <button style="font-size:11px;color:var(--muted);margin-top:6px;padding:4px 0;" @click="loadPlan(p.filename)">
                  <span x-text="planView.filename === p.filename ? 'hide' : 'view'"></span>
                </button>
              </div>
            </template>
            <template x-if="plans.length === 0">
              <div class="empty-list">no plans</div>
            </template>
          </div>
        </div>

        <!-- Docs section -->
        <div class="settings-section">
          <div class="settings-section-label">reference docs</div>
          <div class="settings-card">
            <template x-for="d in docs" :key="d.id">
              <div class="doc-item">
                <div class="doc-info">
                  <div class="doc-name" x-text="d.filename"></div>
                  <div class="doc-meta" x-text="d.created + ' · ' + d.bytes + ' B'"></div>
                </div>
                <button class="del-btn" @click="deleteDoc(d.id)">delete</button>
              </div>
            </template>
            <template x-if="docs.length === 0">
              <div class="empty-list">no docs uploaded</div>
            </template>
          </div>
          <label style="display:block;margin-top:8px;">
            <div class="action-btn" style="cursor:pointer">upload doc</div>
            <input type="file" style="display:none" @change="uploadDoc($event)" />
          </label>
        </div>

      </div>
    </div>
  </template>

  <!-- ── New project modal ─────────────────────────────────────────────────── -->
  <template x-if="newProjectOpen">
    <div class="modal-overlay" @click.self="newProjectOpen=false">
      <div class="modal-sheet">
        <div class="modal-handle"></div>
        <div class="modal-title">new project</div>
        <input class="modal-input" type="text" placeholder="project name" x-model="newProject.name" autocapitalize="none" spellcheck="false" />
        <input class="modal-input" type="url" placeholder="github repo url (optional)" x-model="newProject.repo_url" autocapitalize="none" spellcheck="false" />
        <input class="modal-input" type="password" placeholder="github token (optional)" x-model="newProject.github_token" autocomplete="off" />
        <template x-if="newProject.error">
          <p class="err-msg" x-text="newProject.error"></p>
        </template>
        <button class="action-btn primary" @click="createProject()">create</button>
        <button class="action-btn" @click="newProjectOpen=false" style="margin-top:8px">cancel</button>
      </div>
    </div>
  </template>

</div>

<script>
function kaptaan() {
  return {
    auth: { loggedIn:false, hasUser:false, username:'', password:'', error:'' },
    projects: [],
    activeProjectID: null,
    messages: [],
    composer: '',
    agentRunning: false,
    askActive: false,
    sse: null,
    showSettings: false,
    usage: { all:[], today:[] },
    memories: [], plans: [], docs: [],
    planView: { filename:'', content:'' },
    edit: { name:'', repo_url:'', github_token:'', error:'' },
    newProjectOpen: false,
    newProject: { name:'', repo_url:'', github_token:'', error:'' },

    async init() {
      const s = await fetch('/api/auth/status').then(r=>r.json());
      this.auth.hasUser  = s.hasUser;
      this.auth.loggedIn = s.loggedIn;
      if (this.auth.loggedIn) await this.bootApp();
    },

    async signup() {
      this.auth.error = '';
      const r = await fetch('/api/auth/setup', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({username:this.auth.username, password:this.auth.password})});
      if (!r.ok) { this.auth.error = (await r.json()).error||'failed'; return; }
      this.auth.loggedIn = true; this.auth.hasUser = true; await this.bootApp();
    },

    async login() {
      this.auth.error = '';
      const r = await fetch('/api/auth/login', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({username:this.auth.username, password:this.auth.password})});
      if (!r.ok) { this.auth.error = (await r.json()).error||'invalid credentials'; return; }
      this.auth.loggedIn = true; await this.bootApp();
    },

    async logout() {
      await fetch('/api/auth/logout', {method:'POST'});
      this.auth.loggedIn = false; this.showSettings = false;
      if (this.sse) this.sse.close();
    },

    async bootApp() {
      await this.loadProjects();
      const stored = parseInt(localStorage.getItem('kaptaan_pid')||'0');
      if (stored && this.projects.some(p=>p.id===stored)) this.activeProjectID = stored;
      else if (this.projects.length) this.activeProjectID = this.projects[0].id;
      else { this.newProjectOpen = true; return; }
      this.connect();
    },

    async loadProjects() {
      const j = await fetch('/api/projects').then(r=>r.json());
      this.projects = j.projects || [];
    },

    activeName() {
      const p = this.projects.find(p=>p.id===this.activeProjectID);
      return p ? p.name : 'kaptaan';
    },

    activeProject() {
      return this.projects.find(p=>p.id===this.activeProjectID);
    },

    onProjectChange() {
      localStorage.setItem('kaptaan_pid', String(this.activeProjectID));
      this.messages = []; this.askActive = false;
      this.connect();
    },

    connect() {
      if (this.sse) this.sse.close();
      if (!this.activeProjectID) return;
      this.sse = new EventSource('/events?project='+this.activeProjectID);
      this.sse.addEventListener('msg', e => this.onMsg(JSON.parse(e.data)));
      this.sse.addEventListener('state', e => { this.agentRunning = JSON.parse(e.data).running; });
      this.sse.addEventListener('ask_state', e => { this.askActive = JSON.parse(e.data).active; });
    },

    onMsg(m) {
      this.messages.push(m);
      this.$nextTick(() => {
        const f = this.$refs.feed;
        if (f) f.scrollTop = f.scrollHeight;
      });
    },

    bubbleClass(m) {
      if (m.type === 'user')  return 'bubble user-bubble';
      if (m.type === 'reply') return 'bubble reply-bubble';
      if (m.type === 'ask')   return 'bubble ask-bubble';
      return 'bubble agent-bubble';
    },

    bubbleLabel(m) {
      return ({user:'you', reply:'you', ask:'kaptaan asks', message:'kaptaan'}[m.type])||'kaptaan';
    },

    render(text) {
      try { return DOMPurify.sanitize(marked.parse(text||'', {breaks:true})); }
      catch(e) { return DOMPurify.sanitize(text||''); }
    },

    async send() {
      const text = this.composer.trim();
      if (!text) return;
      this.composer = '';
      this.$nextTick(() => {
        const ta = document.querySelector('.composer-input');
        if (ta) { ta.style.height = ''; }
      });
      const path = this.askActive ? '/api/reply' : '/api/chat';
      const r = await this.api(path, {method:'POST', body: JSON.stringify({text})});
      if (r.error) this.onMsg({type:'message', text:'error: '+r.error, ts: new Date().toLocaleTimeString()});
    },

    autoResize(ev) {
      const el = ev.target;
      el.style.height = '';
      el.style.height = Math.min(el.scrollHeight, 120) + 'px';
    },

    async clearConvo() {
      await this.api('/api/conversation/clear', {method:'POST'});
      this.messages = [];
    },

    openSettings() {
      const p = this.activeProject();
      this.edit = { name: p?.name||'', repo_url: p?.repo_url||'', github_token:'', error:'' };
      this.showSettings = true;
      this.refreshSettings();
    },

    closeSettings() {
      this.showSettings = false;
    },

    async refreshSettings() {
      const [u, m, pl, d] = await Promise.all([
        fetch('/api/usage').then(r=>r.json()),
        this.api('/api/memories'),
        this.api('/api/plans'),
        this.api('/api/docs'),
      ]);
      this.usage = u;
      this.memories = m.memories||[];
      this.plans = pl.plans||[];
      this.docs = d.docs||[];
    },

    async saveProject() {
      this.edit.error = '';
      const p = this.activeProject();
      if (!p) return;
      const body = { name: this.edit.name, repo_url: this.edit.repo_url };
      if (this.edit.github_token) body.github_token = this.edit.github_token;
      const r = await fetch('/api/projects/'+p.id, {method:'PATCH', headers:{'Content-Type':'application/json'}, body: JSON.stringify(body)});
      const j = await r.json();
      if (!r.ok) { this.edit.error = j.error||'failed'; return; }
      await this.loadProjects();
    },

    async deleteProject() {
      if (!confirm('delete this project? all data will be lost.')) return;
      const p = this.activeProject();
      if (!p) return;
      const r = await fetch('/api/projects/'+p.id, {method:'DELETE'});
      const j = await r.json();
      if (!r.ok) { this.edit.error = j.error||'failed'; return; }
      this.showSettings = false;
      this.activeProjectID = null;
      await this.loadProjects();
      if (this.projects.length) {
        this.activeProjectID = this.projects[0].id;
        localStorage.setItem('kaptaan_pid', String(this.activeProjectID));
        this.connect();
      } else {
        this.newProjectOpen = true;
      }
    },

    openNewProject() {
      this.newProject = { name:'', repo_url:'', github_token:'', error:'' };
      this.newProjectOpen = true;
    },

    async createProject() {
      this.newProject.error = '';
      const r = await fetch('/api/projects', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({name:this.newProject.name, repo_url:this.newProject.repo_url, github_token:this.newProject.github_token})});
      const j = await r.json();
      if (!r.ok) { this.newProject.error = j.error||'failed'; return; }
      this.newProjectOpen = false;
      await this.loadProjects();
      if (j.project) {
        this.activeProjectID = j.project.id;
        localStorage.setItem('kaptaan_pid', String(this.activeProjectID));
        this.connect();
      }
    },

    async deleteMemory(key) {
      await this.api('/api/memories?key='+encodeURIComponent(key), {method:'DELETE'});
      this.memories = this.memories.filter(m=>m.key!==key);
    },

    async loadPlan(file) {
      if (this.planView.filename === file) { this.planView = {filename:'', content:''}; return; }
      const j = await this.api('/api/plans?file='+encodeURIComponent(file));
      this.planView = { filename: file, content: j.content||'' };
    },

    async uploadDoc(ev) {
      const f = ev.target.files[0]; if (!f) return;
      const fd = new FormData(); fd.append('file', f);
      await fetch('/api/docs', {method:'POST', headers:{'X-Project-ID': String(this.activeProjectID)}, body:fd});
      const j = await this.api('/api/docs');
      this.docs = j.docs||[];
      ev.target.value = '';
    },

    async deleteDoc(id) {
      await this.api('/api/docs/'+id, {method:'DELETE'});
      this.docs = this.docs.filter(d=>d.id!==id);
    },

    api(path, opts={}) {
      const headers = Object.assign({'Content-Type':'application/json','X-Project-ID': String(this.activeProjectID||'')}, opts.headers||{});
      return fetch(path, Object.assign({}, opts, {headers})).then(r=>r.json()).catch(()=>({}));
    },
  };
}
</script>
</body>
</html>
`
