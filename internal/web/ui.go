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
  --compose: 80px;
}

html { height: 100%; }

body {
  height: 100%;
  min-height: -webkit-fill-available;
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
  height: 100vh;
  height: 100svh;
  height: 100dvh;
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

/* ─── Feed ──────────────────────────────────────────────────────────────── */
.feed {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior: contain;
  padding: 0 calc(var(--safe-l) + 14px) 10px calc(var(--safe-r) + 14px);
  display: flex;
  flex-direction: column;
  gap: 10px;
  scrollbar-width: none;
}
.feed::-webkit-scrollbar { display: none; }

.feed-spacer { flex: 1 1 0; min-height: 16px; }

.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--dim);
}
.empty-state svg { opacity: 0.3; }
.empty-state p { font-size: 12px; text-align: center; }

.bubble-row { display: flex; }
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
.bubble.user-bubble .bubble-content code { background: rgba(0,0,0,0.07); }
.bubble-content pre code { background: transparent; padding: 0; }
.bubble-content h1,.bubble-content h2,.bubble-content h3 { font-weight: 600; margin: 6px 0 2px; }
.bubble-content ul,.bubble-content ol { padding-left: 1.2em; margin: 4px 0; }
.bubble-content p { margin: 2px 0; }
.bubble-content a { text-decoration: underline; opacity: 0.8; }

/* ─── Thinking indicator ────────────────────────────────────────────────── */
.thinking-dots {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 10px 14px;
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  border-bottom-left-radius: 3px;
}
.thinking-dots span {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--dim);
  animation: thinking-bounce 1.4s infinite ease-in-out;
}
.thinking-dots span:nth-child(1) { animation-delay: 0s; }
.thinking-dots span:nth-child(2) { animation-delay: 0.2s; }
.thinking-dots span:nth-child(3) { animation-delay: 0.4s; }
@keyframes thinking-bounce {
  0%, 60%, 100% { transform: translateY(0); opacity: 0.35; }
  30% { transform: translateY(-4px); opacity: 1; }
}

/* ─── Tool call group ───────────────────────────────────────────────────── */
.tool-group {
  max-width: 88%;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--ink);
  overflow: hidden;
}
.tool-group-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  cursor: pointer;
  user-select: none;
  -webkit-tap-highlight-color: transparent;
}
.tool-group-header:active { background: rgba(255,255,255,0.03); }
.tool-status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
  background: #30d158;
}
.tool-status-dot.running {
  background: #ffd60a;
  box-shadow: 0 0 5px #ffd60a66;
  animation: blink 1.2s infinite;
}
.tool-group-label {
  flex: 1;
  font-size: 11px;
  color: var(--muted);
  letter-spacing: 0.02em;
}
.tool-group-toggle {
  font-size: 9px;
  color: var(--dim);
  transition: transform 0.15s;
}
.tool-group-toggle.open { transform: rotate(180deg); }
.tool-rows { border-top: 1px solid var(--border); }
.tool-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 5px 10px;
  border-bottom: 1px solid var(--border);
}
.tool-row:last-child { border-bottom: none; }
.tool-row-icon { font-size: 10px; flex-shrink: 0; opacity: 0.6; }
.tool-name { font-size: 11px; font-weight: 600; color: var(--sub); flex-shrink: 0; }
.tool-args {
  font-size: 11px;
  color: var(--dim);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

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

.scratchpad-block {
  background: #111;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 12px 14px;
  font-family: 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 11px;
  line-height: 1.6;
  color: #c9d1d9;
  overflow-y: auto;
  max-height: 300px;
  white-space: pre-wrap;
  word-break: break-word;
  -webkit-overflow-scrolling: touch;
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
.action-btn:disabled { opacity: 0.4; cursor: default; }

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

.empty-list {
  padding: 20px 14px;
  font-size: 12px;
  color: var(--dim);
  text-align: center;
}

.err-msg { font-size: 12px; color: #ff453a; padding: 8px 14px; }
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
        <div class="header-title">kaptaan</div>
        <div class="status-pill">
          <div class="status-dot" :class="agentRunning ? 'running' : 'idle'"></div>
          <span x-text="agentRunning ? ('running' + (lastToolName ? ' · ' + lastToolName : '')) : 'idle'"></span>
        </div>
        <button class="icon-btn" @click="openSettings()">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#8e8e93" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="3"/>
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>
          </svg>
        </button>
      </header>

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
        <template x-if="messages.length > 0">
          <div class="feed-spacer"></div>
        </template>
        <template x-for="group in groupedMessages()" :key="group.gid">
          <div>
            <!-- Tool call group -->
            <template x-if="group.kind === 'toolgroup'">
              <div class="bubble-row agent">
                <div class="tool-group">
                  <div class="tool-group-header" @click="toggleToolGroup(group.gid)">
                    <div class="tool-status-dot" :class="isLastToolGroup(group.gid) && agentRunning ? 'running' : ''"></div>
                    <span class="tool-group-label" x-text="group.tools.length + (group.tools.length === 1 ? ' tool call' : ' tool calls')"></span>
                    <span class="tool-group-toggle" :class="isToolGroupOpen(group.gid) ? 'open' : ''">▼</span>
                  </div>
                  <template x-if="isToolGroupOpen(group.gid)">
                    <div class="tool-rows">
                      <template x-for="(t, ti) in group.tools" :key="ti">
                        <div class="tool-row">
                          <span class="tool-row-icon">⚡</span>
                          <span class="tool-name" x-text="parseToolCall(t.text).name"></span>
                          <span class="tool-args" x-text="parseToolCall(t.text).args"></span>
                        </div>
                      </template>
                    </div>
                  </template>
                </div>
              </div>
            </template>
            <!-- Regular message -->
            <template x-if="group.kind === 'message'">
              <div class="bubble-row" :class="group.msg.type === 'user' || group.msg.type === 'reply' ? 'user' : 'agent'">
                <div class="bubble" :class="bubbleClass(group.msg)">
                  <div class="bubble-meta" :class="group.msg.type === 'user' ? 'user-meta' : ''"
                       x-text="bubbleLabel(group.msg) + ' · ' + group.msg.ts"></div>
                  <div class="bubble-content" x-html="render(group.msg.text)"></div>
                </div>
              </div>
            </template>
          </div>
        </template>
        <!-- Thinking indicator -->
        <template x-if="agentRunning">
          <div class="bubble-row agent">
            <div class="thinking-dots">
              <span></span><span></span><span></span>
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

        <!-- Conversation -->
        <div class="settings-section">
          <div class="settings-section-label">conversation</div>
          <button class="action-btn" @click="clearConvo()">clear conversation</button>
        </div>

        <!-- DeepSeek Credit -->
        <div class="settings-section">
          <div class="settings-section-label">deepseek credit</div>
          <div class="settings-card">
            <template x-if="credits.data && credits.data.balance_infos">
              <template x-for="b in credits.data.balance_infos" :key="b.currency">
                <div class="settings-row">
                  <span class="settings-row-label" x-text="b.currency + ' balance'"></span>
                  <span class="settings-row-value" x-text="b.total_balance"></span>
                </div>
              </template>
            </template>
            <template x-if="!credits.data">
              <div class="empty-list">press check to load balance</div>
            </template>
            <template x-if="credits.error">
              <div class="err-msg" x-text="credits.error"></div>
            </template>
          </div>
          <button class="action-btn" @click="checkCredits()" :disabled="credits.loading"
            x-text="credits.loading ? 'checking…' : 'check credits'"></button>
        </div>

        <!-- Scratchpad -->
        <div class="settings-section">
          <div class="settings-section-label">scratchpad</div>
          <template x-if="scratchpad.error">
            <div class="err-msg" x-text="scratchpad.error"></div>
          </template>
          <template x-if="scratchpad.content !== null && !scratchpad.loading && !scratchpad.error">
            <pre class="scratchpad-block" x-text="scratchpad.content"></pre>
          </template>
          <template x-if="scratchpad.content === null && !scratchpad.loading && !scratchpad.error">
            <div class="empty-list">press refresh to load</div>
          </template>
          <button class="action-btn" @click="loadScratchpad()" :disabled="scratchpad.loading"
            x-text="scratchpad.loading ? 'loading…' : 'refresh'"></button>
        </div>

        <!-- Memories -->
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

      </div>
    </div>
  </template>

</div>

<script>
function kaptaan() {
  return {
    auth: { loggedIn:false, hasUser:false, username:'', password:'', error:'' },
    messages: [],
    composer: '',
    agentRunning: false,
    askActive: false,
    sse: null,
    showSettings: false,
    memories: [],
    credits: { loading: false, error: '', data: null },
    scratchpad: { loading: false, error: '', content: null },
    toolGroupsOpen: {},
    lastToolName: '',

    async init() {
      const s = await fetch('/api/auth/status').then(r=>r.json());
      this.auth.hasUser  = s.hasUser;
      this.auth.loggedIn = s.loggedIn;
      if (this.auth.loggedIn) this.bootApp();
    },

    async signup() {
      this.auth.error = '';
      const r = await fetch('/api/auth/setup', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({username:this.auth.username, password:this.auth.password})});
      if (!r.ok) { this.auth.error = (await r.json()).error||'failed'; return; }
      this.auth.loggedIn = true; this.auth.hasUser = true; this.bootApp();
    },

    async login() {
      this.auth.error = '';
      const r = await fetch('/api/auth/login', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({username:this.auth.username, password:this.auth.password})});
      if (!r.ok) { this.auth.error = (await r.json()).error||'invalid credentials'; return; }
      this.auth.loggedIn = true; this.bootApp();
    },

    async logout() {
      await fetch('/api/auth/logout', {method:'POST'});
      this.auth.loggedIn = false; this.showSettings = false;
      if (this.sse) this.sse.close();
    },

    bootApp() {
      this.connect();
    },

    connect() {
      if (this.sse) this.sse.close();
      this.sse = new EventSource('/events');
      this.sse.addEventListener('msg', e => this.onMsg(JSON.parse(e.data)));
      this.sse.addEventListener('state', e => { const s = JSON.parse(e.data); this.agentRunning = s.running; if (!s.running) this.lastToolName = ''; });
      this.sse.addEventListener('ask_state', e => { this.askActive = JSON.parse(e.data).active; });
    },

    onMsg(m) {
      this.messages.push(m);
      if (this.isToolCall(m)) {
        const parsed = this.parseToolCall(m.text);
        if (parsed.name) this.lastToolName = parsed.name;
      }
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

    isToolCall(m) {
      if (m.type !== 'message' || typeof m.text !== 'string') return false;
      const tick = String.fromCharCode(96);
      return m.text.startsWith(tick + 'tool' + tick);
    },

    groupedMessages() {
      const groups = [];
      let i = 0;
      while (i < this.messages.length) {
        const m = this.messages[i];
        if (this.isToolCall(m)) {
          const startIdx = i;
          const tools = [];
          while (i < this.messages.length && this.isToolCall(this.messages[i])) {
            tools.push(this.messages[i]);
            i++;
          }
          groups.push({ kind: 'toolgroup', tools, gid: 'tg' + startIdx });
        } else {
          groups.push({ kind: 'message', msg: m, gid: 'msg' + i });
          i++;
        }
      }
      return groups;
    },

    parseToolCall(text) {
      const m = text.match(/^\x60tool\x60\s+\*\*([^*]+)\*\*\s*([\s\S]*)/);
      if (m) return { name: m[1].trim(), args: m[2].trim() };
      return { name: text, args: '' };
    },

    isToolGroupOpen(gid) {
      return this.toolGroupsOpen[gid] !== false;
    },

    isLastToolGroup(gid) {
      const groups = this.groupedMessages();
      const toolGroups = groups.filter(g => g.kind === 'toolgroup');
      return toolGroups.length > 0 && toolGroups[toolGroups.length - 1].gid === gid;
    },

    toggleToolGroup(gid) {
      this.toolGroupsOpen[gid] = !this.isToolGroupOpen(gid);
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
      if (r.error) this.onMsg({type:'message', text:'error: '+r.error, ts: new Date().toTimeString().slice(0,8)});
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
      this.showSettings = true;
      this.refreshSettings();
      this.loadScratchpad();
    },

    closeSettings() {
      this.showSettings = false;
    },

    async refreshSettings() {
      const j = await this.api('/api/memories');
      this.memories = j.memories||[];
    },

    async deleteMemory(key) {
      await this.api('/api/memories?key='+encodeURIComponent(key), {method:'DELETE'});
      this.memories = this.memories.filter(m=>m.key!==key);
    },

    async loadScratchpad() {
      this.scratchpad = { loading: true, error: '', content: null };
      const j = await this.api('/api/scratchpad');
      if (j.error) {
        this.scratchpad = { loading: false, error: j.error, content: null };
        return;
      }
      this.scratchpad = { loading: false, error: '', content: j.content };
    },

    async checkCredits() {
      this.credits = { loading: true, error: '', data: null };
      const j = await fetch('/api/credits').then(r=>r.json()).catch(()=>({error:'network error'}));
      if (j.error) {
        this.credits = { loading: false, error: j.error, data: null };
        return;
      }
      this.credits = { loading: false, error: '', data: j };
    },

    api(path, opts={}) {
      const headers = Object.assign({'Content-Type':'application/json'}, opts.headers||{});
      return fetch(path, Object.assign({}, opts, {headers})).then(r=>r.json()).catch(()=>({}));
    },
  };
}
</script>
</body>
</html>
`
