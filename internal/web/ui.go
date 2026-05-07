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
      font-family: var(--sans); background: var(--bg); color: var(--text);
      font-size: 15px; line-height: 1.5;
      -webkit-font-smoothing: antialiased; overscroll-behavior: none;
    }
    button { font-family: inherit; font-size: inherit; cursor: pointer; border: none; background: none; color: inherit; }
    input, textarea { font-family: inherit; font-size: inherit; color: inherit; background: none; border: none; outline: none; }
    a { color: var(--accent); text-decoration: none; }
    a:hover { text-decoration: underline; }

    [x-cloak] { display: none !important; }

    /* ─── Auth ─────────────────────────────────────────────────────── */
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
      background: var(--bg); border-bottom: 1px solid var(--line);
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
    .agent-pill {
      font-family: var(--mono); font-size: 10px; letter-spacing: 1px;
      padding: 3px 7px; border-radius: 4px; text-transform: uppercase;
      background: var(--bg2); color: var(--text2); margin-left: 4px;
    }
    .agent-pill.manager { background: rgba(139,92,246,0.15); color: #a78bfa; }
    .agent-pill.builder { background: rgba(59,130,246,0.15); color: var(--accent); }
    .agent-pill.ready   { background: rgba(34,197,94,0.12);  color: var(--ok); }
    .agent-pill.paused  { background: rgba(245,158,11,0.15); color: var(--warn); }
    .agent-pill.idle    { display: none; }

    /* Repo-not-connected banner */
    .repo-banner {
      flex-shrink: 0; padding: 8px 14px; background: rgba(245,158,11,0.12);
      border-bottom: 1px solid rgba(245,158,11,0.3); color: var(--warn);
      font-size: 13px; display: flex; align-items: center; gap: 8px;
    }
    .repo-banner button {
      margin-left: auto; padding: 4px 10px; border-radius: 6px;
      background: var(--warn); color: #000; font-size: 12px; font-weight: 600;
    }

    /* PR Approval card */
    .pr-card {
      background: var(--bg1); border: 1px solid var(--accent); border-radius: 12px;
      padding: 14px; margin: 6px 0;
    }
    .pr-card-head {
      display: flex; align-items: center; gap: 8px; margin-bottom: 8px;
      font-family: var(--mono); font-size: 11px; letter-spacing: 1.5px;
      color: var(--accent); text-transform: uppercase;
    }
    .pr-card-title { font-weight: 600; font-size: 15px; margin-bottom: 6px; color: var(--text); }
    .pr-card-meta { font-family: var(--mono); font-size: 11px; color: var(--text3); margin-bottom: 10px; }
    .pr-card-note { font-size: 13.5px; color: var(--text2); margin-bottom: 12px; line-height: 1.45; }
    .pr-card-actions { display: flex; gap: 8px; }
    .pr-btn { padding: 9px 18px; border-radius: 8px; font-size: 13px; font-weight: 600; }
    .pr-btn.approve { background: var(--ok); color: #000; }
    .pr-btn.reject  { background: transparent; color: var(--err); border: 1px solid var(--err); }
    .pr-btn:disabled { opacity: 0.5; cursor: not-allowed; }

    /* Plan tree (Phase → Task → Job) */
    .phase-block { margin-bottom: 14px; }
    .phase-head {
      font-family: var(--mono); font-size: 11px; letter-spacing: 1.5px;
      color: var(--text3); text-transform: uppercase; margin-bottom: 6px;
      display: flex; align-items: center; gap: 8px;
    }
    .phase-head .ln { flex: 1; height: 1px; background: var(--line); }
    .task-row {
      padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px;
      margin-bottom: 6px; background: var(--bg1);
    }
    .task-row.active   { border-color: var(--accent); background: rgba(59,130,246,0.06); }
    .task-row.done     { opacity: 0.7; }
    .task-row .top { display: flex; gap: 8px; align-items: center; }
    .task-row .title { font-weight: 600; flex: 1; }
    .task-row .desc { color: var(--text2); font-size: 13px; margin-top: 4px; }
    .task-row .links { font-family: var(--mono); font-size: 11px; color: var(--text3); margin-top: 6px; }
    .subtask-row { padding: 5px 10px; border-left: 2px solid var(--line2); margin: 4px 0 4px 12px; font-size: 13px; color: var(--text2); display: flex; gap: 8px; align-items: center; }

    /* Milestone timeline */
    .timeline { display: flex; flex-direction: column; gap: 4px; margin-top: 4px; }
    .timeline-row {
      display: flex; gap: 10px; align-items: flex-start; padding: 4px 0;
      font-size: 13px;
    }
    .timeline-row .t-time {
      font-family: var(--mono); font-size: 10px; color: var(--text3);
      flex-shrink: 0; padding-top: 2px; min-width: 56px;
    }
    .timeline-row .t-evt {
      font-family: var(--mono); font-size: 10.5px; padding: 1px 6px; border-radius: 4px;
      background: var(--bg2); color: var(--text2); flex-shrink: 0; text-transform: uppercase;
      letter-spacing: 0.5px;
    }
    .timeline-row .t-msg { color: var(--text); word-break: break-word; }
    .timeline-row .t-evt.builder_start, .timeline-row .t-evt.task_complete { background: rgba(34,197,94,0.18); color: var(--ok); }
    .timeline-row .t-evt.sandbox_error, .timeline-row .t-evt.build_failed, .timeline-row .t-evt.test_failed { background: rgba(239,68,68,0.18); color: var(--err); }

    /* Failure / retry */
    .retry-pill {
      font-family: var(--mono); font-size: 10px; padding: 2px 7px; border-radius: 4px;
      background: rgba(245,158,11,0.18); color: var(--warn); letter-spacing: 0.5px;
    }
    .failure-card {
      border: 1px solid rgba(239,68,68,0.4); border-radius: 10px; padding: 12px;
      background: rgba(239,68,68,0.04); margin-top: 6px;
    }
    .failure-head { display: flex; gap: 8px; align-items: center; cursor: pointer; }
    .failure-head .chev { color: var(--text3); font-family: var(--mono); }

    .composer.locked textarea { opacity: 0.55; }
    .composer .lock-msg {
      flex: 1; font-family: var(--mono); font-size: 11.5px; color: var(--text3);
      padding: 10px 12px; border: 1px dashed var(--line2); border-radius: 18px; text-align: center;
    }

    .nav-tabs { display: flex; gap: 2px; margin-left: auto; background: var(--bg1); border: 1px solid var(--line); border-radius: 8px; padding: 2px; }
    .nav-tab { padding: 6px 12px; border-radius: 6px; font-size: 13px; font-weight: 500; color: var(--text2); }
    .nav-tab.active { background: var(--bg2); color: var(--text); }

    .hdr-btn {
      padding: 6px 10px; border-radius: 8px; font-size: 12px; font-weight: 500;
      background: var(--bg1); border: 1px solid var(--line2); color: var(--text);
    }
    .hdr-btn.paused { background: var(--warn); border-color: var(--warn); color: #000; }
    .hdr-btn.stop { background: var(--err); border-color: var(--err); color: #fff; }

    /* ─── Chat ───────────────────────────────────────────────────── */
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

    /* ─── Settings ───────────────────────────────────────────────── */
    .settings-view { flex: 1; overflow-y: auto; padding: 16px 14px calc(var(--safe-bottom) + 24px); }
    .settings-section { margin-bottom: 22px; }
    .settings-section h2 { font-size: 13px; font-family: var(--mono); letter-spacing: 1.5px; color: var(--text3); text-transform: uppercase; margin-bottom: 8px; }
    .settings-card { background: var(--bg1); border: 1px solid var(--line); border-radius: 12px; padding: 14px; }
    .settings-row { display: flex; justify-content: space-between; align-items: center; gap: 10px; padding: 8px 0; border-bottom: 1px solid var(--line); font-size: 14px; }
    .settings-row:last-child { border-bottom: none; }
    .settings-row .meta { font-family: var(--mono); font-size: 11px; color: var(--text3); }
    .settings-row .row-main { flex: 1; min-width: 0; }
    .settings-row .row-main .name { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .settings-action {
      padding: 9px 14px; border-radius: 8px; background: var(--bg2); border: 1px solid var(--line2);
      font-size: 13px; font-weight: 500;
    }
    .settings-action.primary { background: var(--text); color: var(--bg); border-color: var(--text); }
    .settings-action.danger  { color: var(--err); border-color: rgba(239,68,68,0.3); }
    .settings-action:disabled { opacity: 0.5; cursor: not-allowed; }
    .icon-btn { padding: 4px 8px; border-radius: 6px; color: var(--text3); font-size: 16px; line-height: 1; }
    .icon-btn:hover { color: var(--err); background: var(--bg2); }
    .empty { color: var(--text3); font-size: 13px; text-align: center; padding: 16px; }
    pre.console {
      background: var(--bg); padding: 12px; border-radius: 8px; font-family: var(--mono); font-size: 11.5px;
      max-height: 240px; overflow: auto; white-space: pre-wrap; word-break: break-word; color: var(--text2);
      border: 1px solid var(--line);
    }
    .desc { color: var(--text2); font-size: 13px; margin-bottom: 10px; }

    .job-status {
      font-family: var(--mono); font-size: 10px; padding: 2px 6px; border-radius: 4px;
      background: var(--bg2); color: var(--text2); letter-spacing: 0.5px; text-transform: uppercase;
    }
    .job-status.queued          { background: rgba(160,160,160,0.15); color: var(--text2); }
    .job-status.running         { background: rgba(59,130,246,0.18); color: var(--accent); }
    .job-status.awaiting_review { background: rgba(245,158,11,0.18); color: var(--warn); }
    .job-status.merged          { background: rgba(34,197,94,0.18);  color: var(--ok); }
    .job-status.failed          { background: rgba(239,68,68,0.18);  color: var(--err); }
    .job-status.rejected        { background: rgba(239,68,68,0.12);  color: var(--err); }

    /* ─── Mobile ─────────────────────────────────────────────────── */
    @media (max-width: 600px) {
      .composer textarea { max-height: 80px; }
      .nav-tab { padding: 5px 10px; font-size: 12px; }
      .brand { font-size: 14px; }
      .state-pill { display: none; }
      .messages { padding: 10px 8px 4px; }
      .bubble { max-width: 88%; font-size: 14px; }
      .settings-view { padding: 12px 10px calc(var(--safe-bottom) + 18px); }
    }
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
      <span class="agent-pill" :class="(status.agent||'idle').toLowerCase()" x-text="status.agent||''"></span>

      <div class="nav-tabs">
        <button class="nav-tab" :class="view==='chat'?'active':''" @click="view='chat'">Chat</button>
        <button class="nav-tab" :class="view==='builder'?'active':''" @click="view='builder'; loadBuilderState(); loadPlanTree()">Builder</button>
        <button class="nav-tab" :class="view==='settings'?'active':''" @click="view='settings'; loadSettings()">Settings</button>
      </div>

      <button x-show="canStop()" class="hdr-btn stop" @click="cancel()">Stop</button>
      <button x-show="!canStop()" class="hdr-btn"
        :class="status.state==='paused'?'paused':''"
        @click="togglePause()"
        x-text="status.state==='paused'?'Resume':'Pause'"></button>
    </header>

    <!-- Repo-not-connected banner -->
    <div x-show="status.project && status.repo_connected===false" class="repo-banner">
      <span>⚠️ No GitHub repo connected — planning is disabled. Connect a repo + token in Settings to begin.</span>
      <button @click="view='settings'; loadSettings()">Open Settings</button>
    </div>

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
            <template x-if="m.type==='pr_review'">
              <div class="pr-card">
                <div class="pr-card-head">
                  <span>📋 Pull Request — Review needed</span>
                </div>
                <div class="pr-card-title" x-text="m.task_title || 'Builder finished a task'"></div>
                <div class="pr-card-meta">
                  <template x-if="m.pr_url">
                    <span><a :href="m.pr_url" target="_blank" rel="noopener" x-text="m.pr_url"></a></span>
                  </template>
                  <template x-if="!m.pr_url"><span>(local diff)</span></template>
                  <template x-if="m.branch"><span> · <span x-text="m.branch"></span></span></template>
                </div>
                <template x-if="m.diff_summary">
                  <pre class="console" style="max-height:200px; margin-bottom:10px;" x-text="m.diff_summary"></pre>
                </template>
                <template x-if="m.manager_note">
                  <div class="pr-card-note" x-html="renderMsg(m.manager_note)"></div>
                </template>
                <div class="pr-card-actions" x-show="!m.resolved">
                  <button class="pr-btn approve" :disabled="askBusy" @click="answerPR(m, 'yes')">✓ Approve & Merge</button>
                  <button class="pr-btn reject"  :disabled="askBusy" @click="answerPR(m, 'no')">✕ Reject</button>
                </div>
                <div x-show="m.resolved" style="font-family: var(--mono); font-size:11px; color: var(--text3); margin-top: 8px;"
                  x-text="'➜ Replied: ' + (m.resolved||'')"></div>
              </div>
            </template>
            <template x-if="m.type!=='separator' && m.type!=='pr_review'">
              <div :class="'msg-row ' + (isUser(m) ? 'from-user' : '')">
                <div :class="'bubble ' + bubbleClass(m)" x-html="renderMsg(m.text)"></div>
              </div>
            </template>
            <template x-if="m.type!=='separator' && m.type!=='pr_review' && m.ts">
              <div class="ts" x-text="m.ts"></div>
            </template>
          </div>
        </template>
        <div id="feed-end"></div>
      </div>

      <div class="composer" :class="composerLocked() ? 'locked' : ''">
        <template x-if="!composerLocked()">
          <textarea x-model="input" x-ref="inputEl" rows="1" :placeholder="composerPlaceholder()"
            @input="autoResize($el)"
            @keydown.enter.prevent="if(!$event.shiftKey && input.trim()) send()"></textarea>
        </template>
        <template x-if="composerLocked()">
          <div class="lock-msg" x-text="lockMessage()"></div>
        </template>
        <button class="send-btn" :disabled="!input.trim() || composerLocked()" @click="send()" aria-label="Send">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"
            stroke-linecap="round" stroke-linejoin="round">
            <line x1="22" y1="2" x2="11" y2="13"/>
            <polygon points="22 2 15 22 11 13 2 9 22 2" fill="currentColor" stroke="none"/>
          </svg>
        </button>
      </div>
    </main>

    <!-- Builder view -->
    <main x-show="view==='builder'" class="settings-view">

      <section class="settings-section">
        <h2>Plan
          <span style="font-family: var(--sans); font-weight: 400; color: var(--text3); text-transform: none; letter-spacing: 0; font-size: 11px; margin-left: 6px;">
            (Phase → Task → Job — sequential, one PR at a time)
          </span>
        </h2>
        <div class="settings-card">
          <template x-if="planTree.empty">
            <div class="empty">No plan yet. Send a message in Chat to have the Manager draft one.</div>
          </template>
          <template x-if="!planTree.empty">
            <div>
              <div class="meta" style="font-family: var(--mono); font-size:11px; color: var(--text3); margin-bottom: 10px;"
                   x-show="planTree.plan">
                <span x-text="'Plan v' + (planTree.plan ? planTree.plan.version : '?')"></span>
                <span> · </span>
                <span x-text="(planTree.plan && planTree.plan.status) || ''"></span>
                <button class="settings-action" style="margin-left:10px; padding:3px 9px;" @click="loadPlanTree()">Refresh</button>
              </div>
              <template x-for="ph in (planTree.phases || [])" :key="ph.number">
                <div class="phase-block">
                  <div class="phase-head">
                    <span x-text="'Phase ' + ph.number"></span>
                    <span class="ln"></span>
                  </div>
                  <template x-for="t in ph.tasks" :key="t.id">
                    <div :class="'task-row ' + (t.status==='in_progress'?'active':'') + ' ' + (t.status==='done'?'done':'')">
                      <div class="top">
                        <div class="title" x-text="t.title"></div>
                        <span :class="'job-status ' + t.status" x-text="(t.status||'').replace('_',' ')"></span>
                        <template x-if="t.job && t.job.retry_count > 0">
                          <span class="retry-pill" x-text="'Attempt ' + (t.job.retry_count + 1) + '/3'"></span>
                        </template>
                      </div>
                      <template x-if="t.description">
                        <div class="desc" x-text="t.description"></div>
                      </template>
                      <div class="links" x-show="t.pr_url || (t.job && (t.job.branch || t.job.pr_url))">
                        <template x-if="t.job && t.job.branch">
                          <span x-text="'branch ' + t.job.branch"></span>
                        </template>
                        <template x-if="(t.job && t.job.pr_url) || t.pr_url">
                          <span> · <a :href="(t.job && t.job.pr_url) || t.pr_url" target="_blank" rel="noopener">PR</a></span>
                        </template>
                      </div>
                      <template x-for="st in (t.subtasks || [])" :key="st.id">
                        <div class="subtask-row">
                          <span style="flex:1;" x-text="st.title"></span>
                          <span :class="'job-status ' + st.status" x-text="(st.status||'').replace('_',' ')"></span>
                        </div>
                      </template>
                    </div>
                  </template>
                </div>
              </template>
            </div>
          </template>
        </div>
      </section>


      <section class="settings-section">
        <h2>Running task
          <span style="font-family: var(--sans); font-weight: 400; color: var(--text3); text-transform: none; letter-spacing: 0; font-size: 11px; margin-left: 6px;">
            (live — auto-updates as the Builder works)
          </span>
        </h2>
        <div class="settings-card">
          <template x-if="!builderState.running">
            <div class="empty">No task is currently running. Queued and recent jobs are below.</div>
          </template>
          <template x-if="builderState.running">
            <div>
              <div style="display:flex; justify-content:space-between; align-items:center; gap:10px; margin-bottom:6px; flex-wrap:wrap;">
                <div style="font-weight:600; font-size:15px;" x-text="builderState.running.task_title"></div>
                <div style="display:flex; gap:6px; align-items:center;">
                  <template x-if="builderState.running.retry_count > 0">
                    <span class="retry-pill" x-text="'Attempt ' + (builderState.running.retry_count + 1) + '/' + (builderState.running.max_retries || 3)"></span>
                  </template>
                  <span class="job-status running">running</span>
                </div>
              </div>
              <div class="meta" style="font-family: var(--mono); font-size:11px; color: var(--text3); margin-bottom: 10px;">
                <span x-text="'job #' + builderState.running.job_id"></span>
                <span> · </span>
                <span x-text="builderState.running.branch || '(no branch yet)'"></span>
                <template x-if="builderState.running.started_at">
                  <span> · started <span x-text="builderState.running.started_at"></span></span>
                </template>
              </div>
              <template x-if="builderState.running.task_desc">
                <div class="desc" x-text="builderState.running.task_desc"></div>
              </template>

              <template x-if="builderState.running.subtasks && builderState.running.subtasks.length">
                <div style="margin-top:8px;">
                  <div style="font-family: var(--mono); font-size:11px; color: var(--text3); letter-spacing: 1.5px; text-transform: uppercase; margin-bottom:6px;">Subtasks</div>
                  <template x-for="st in builderState.running.subtasks" :key="st.id">
                    <div class="settings-row" style="padding: 6px 0;">
                      <div class="row-main"><div class="name" x-text="st.title"></div></div>
                      <span :class="'job-status ' + st.status" x-text="(st.status||'').replace('_',' ')"></span>
                    </div>
                  </template>
                </div>
              </template>

              <div style="margin-top:12px;">
                <div style="font-family: var(--mono); font-size:11px; color: var(--text3); letter-spacing: 1.5px; text-transform: uppercase; margin-bottom:6px;">Milestones</div>
                <template x-if="!builderState.running.milestones || !builderState.running.milestones.length">
                  <div class="empty">No milestones yet — sandbox warming up.</div>
                </template>
                <div class="timeline" x-show="builderState.running.milestones && builderState.running.milestones.length">
                  <template x-for="m in (builderState.running.milestones || [])" :key="m.time + m.event">
                    <div class="timeline-row">
                      <div class="t-time" x-text="m.time"></div>
                      <div :class="'t-evt ' + m.event" x-text="(m.event || '').replace(/_/g,' ')"></div>
                      <div class="t-msg" x-text="m.payload"></div>
                    </div>
                  </template>
                </div>
              </div>
            </div>
          </template>
        </div>
      </section>

      <!-- Most recent failure (expandable) -->
      <section class="settings-section" x-show="builderState.failed">
        <h2>Last failure</h2>
        <template x-if="builderState.failed">
          <div class="failure-card">
            <div class="failure-head" @click="failureOpen = !failureOpen">
              <span class="chev" x-text="failureOpen ? '▼' : '▶'"></span>
              <div style="flex:1;">
                <div style="font-weight:600;" x-text="builderState.failed.task_title"></div>
                <div class="meta" style="font-family: var(--mono); font-size:11px; color: var(--text3);">
                  job #<span x-text="builderState.failed.job_id"></span>
                  <span> · attempt </span><span x-text="(builderState.failed.retry_count || 0) + 1"></span>
                  <template x-if="builderState.failed.failed_at">
                    <span> · failed <span x-text="builderState.failed.failed_at"></span></span>
                  </template>
                </div>
              </div>
              <span class="job-status failed">failed</span>
            </div>
            <div x-show="failureOpen" style="margin-top:10px;">
              <template x-if="builderState.failed.build_output">
                <div style="margin-top:8px;">
                  <div style="font-family: var(--mono); font-size:10px; color: var(--text3); letter-spacing: 1.5px; text-transform: uppercase; margin-bottom:4px;">Build output</div>
                  <pre class="console" x-text="builderState.failed.build_output"></pre>
                </div>
              </template>
              <template x-if="builderState.failed.test_output">
                <div style="margin-top:8px;">
                  <div style="font-family: var(--mono); font-size:10px; color: var(--text3); letter-spacing: 1.5px; text-transform: uppercase; margin-bottom:4px;">Test output</div>
                  <pre class="console" x-text="builderState.failed.test_output"></pre>
                </div>
              </template>
            </div>
          </div>
        </template>
      </section>

      <section class="settings-section">
        <h2>Queue</h2>
        <div class="settings-card">
          <template x-if="!builderState.queue || !builderState.queue.length">
            <div class="empty">Queue is empty.</div>
          </template>
          <template x-for="q in (builderState.queue || [])" :key="q.job_id">
            <div class="settings-row">
              <div class="row-main">
                <div class="name" x-text="q.task_title"></div>
                <div class="meta">job #<span x-text="q.job_id"></span> · <span x-text="q.branch || '(no branch)'"></span></div>
              </div>
              <span class="job-status queued">queued</span>
            </div>
          </template>
        </div>
      </section>

      <section class="settings-section">
        <h2>Recent</h2>
        <div class="settings-card">
          <div style="display:flex; gap:8px; margin-bottom:10px;">
            <button class="settings-action" @click="loadBuilderState()">Refresh</button>
            <span style="font-family: var(--mono); font-size:11px; color: var(--text3); align-self:center;"
              x-text="builderState.updated ? ('updated ' + builderState.updated) : ''"></span>
          </div>
          <template x-if="!builderState.recent || !builderState.recent.length">
            <div class="empty">No recent jobs.</div>
          </template>
          <template x-for="r in (builderState.recent || [])" :key="r.job_id">
            <div class="settings-row">
              <div class="row-main">
                <div class="name" x-text="r.task_title"></div>
                <div class="meta">job #<span x-text="r.job_id"></span> · <span x-text="r.branch || '(no branch)'"></span></div>
              </div>
            </div>
          </template>
        </div>
      </section>

    </main>

    <!-- Settings view -->
    <main x-show="view==='settings'" class="settings-view">

      <section class="settings-section">
        <h2>Projects
          <span style="font-family: var(--sans); font-weight: 400; color: var(--text3); text-transform: none; letter-spacing: 0; font-size: 11px; margin-left: 6px;">
            (each project has its own repo, token, chat history and tasks)
          </span>
        </h2>
        <div class="settings-card">
          <template x-if="projects.length===0">
            <div class="empty">Loading projects…</div>
          </template>
          <template x-for="p in projects" :key="p.id">
            <div style="border-bottom: 1px solid var(--line); padding: 10px 0;">
              <div style="display:flex; justify-content:space-between; align-items:center; gap:10px;">
                <div style="font-weight:600;" x-text="p.name + (p.active ? '  ★ active' : '')"></div>
                <div style="display:flex; gap:6px;">
                  <button class="settings-action" x-show="!p.active" @click="activateProject(p)">Switch</button>
                  <button class="settings-action" @click="editProject(p)">Edit</button>
                  <button class="settings-action danger" @click="clearProject(p)">Clear data</button>
                  <button class="settings-action danger" @click="deleteProject(p)">Delete</button>
                </div>
              </div>
              <div class="meta" style="font-family: var(--mono); font-size:11px; color: var(--text3); margin-top:4px;">
                <span x-text="'#' + p.id"></span>
                <span> · </span>
                <span x-text="p.repo_url || '(no repo set)'"></span>
                <span> · </span>
                <span x-text="p.has_token ? ('token ' + p.github_token) : 'no token'"></span>
              </div>
            </div>
          </template>

          <div style="margin-top: 14px;">
            <div style="font-family: var(--mono); font-size:11px; color: var(--text3); letter-spacing: 1.5px; text-transform: uppercase; margin-bottom:6px;">
              Add new project
            </div>
            <div style="display:flex; flex-direction:column; gap:6px;">
              <input class="hdr-btn" style="padding:9px 12px;" placeholder="Name (e.g. acme-api)" x-model="newProj.name"/>
              <input class="hdr-btn" style="padding:9px 12px;" placeholder="GitHub repo (owner/name)" x-model="newProj.repo_url"/>
              <input class="hdr-btn" style="padding:9px 12px;" type="password" placeholder="GitHub token (optional)" x-model="newProj.github_token"/>
              <label style="font-size:12px; color: var(--text2); display:flex; gap:6px; align-items:center; cursor:pointer;">
                <input type="checkbox" x-model="newProj.activate"/> Switch to it after creating
              </label>
              <button class="settings-action primary" :disabled="!newProj.name.trim()" @click="addProject()">Create project</button>
            </div>
          </div>
        </div>
      </section>

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
              <div class="row-main">
                <div class="name" x-text="d.filename"></div>
                <div class="meta" x-text="d.uploaded"></div>
              </div>
              <button class="icon-btn" :title="'Delete ' + d.filename" @click="deleteDoc(d)">×</button>
            </div>
          </template>
        </div>
      </section>

      <!-- Diagnostics — de-emphasized, collapsed by default. The Builder
           tab is the primary surface for live activity. -->
      <section class="settings-section">
        <h2 @click="showLogs = !showLogs" style="cursor:pointer; user-select:none;">
          <span x-text="showLogs ? '▼' : '▶'" style="display:inline-block; width:14px; color: var(--text3);"></span>
          Diagnostics
          <span style="font-family: var(--sans); font-weight: 400; color: var(--text3); text-transform: none; letter-spacing: 0; font-size: 11px; margin-left: 6px;">
            (raw logs, traces, LLM usage — the Builder tab is the main view)
          </span>
        </h2>
        <div x-show="showLogs">
          <div class="settings-card" style="margin-bottom: 10px;">
            <div style="font-family: var(--mono); font-size:10px; color: var(--text3); letter-spacing: 1.5px; text-transform: uppercase; margin-bottom:8px;">Builder activity (raw)</div>
            <div style="display:flex; gap:8px; margin-bottom:10px;">
              <button class="settings-action" @click="loadBuilder()">Refresh</button>
            </div>
            <template x-if="jobs.length===0">
              <div class="empty">No builder jobs yet.</div>
            </template>
            <template x-for="j in jobs" :key="j.id">
              <div class="settings-row">
                <div class="row-main">
                  <div class="name" x-text="j.task_title || ('Job #' + j.id)"></div>
                  <div class="meta">
                    <span x-text="j.branch || '(no branch)'"></span>
                    <span> · </span>
                    <span x-text="j.updated"></span>
                    <template x-if="j.pr_url">
                      <span> · <a :href="j.pr_url" target="_blank" rel="noopener">PR</a></span>
                    </template>
                  </div>
                </div>
                <span :class="'job-status ' + j.status" x-text="(j.status||'').replace('_',' ')"></span>
              </div>
            </template>
          </div>

          <div class="settings-card" style="margin-bottom: 10px;">
            <div style="font-family: var(--mono); font-size:10px; color: var(--text3); letter-spacing: 1.5px; text-transform: uppercase; margin-bottom:8px;">Agent trace</div>
            <div style="display:flex; gap:8px; margin-bottom:10px; align-items: center;"
                 x-effect="traceAutoEffect">
              <button class="settings-action" @click="loadTrace()">Refresh</button>
              <label style="font-size:12px; color: var(--text2); display:flex; gap:6px; align-items:center; cursor:pointer;">
                <input type="checkbox" x-model="traceAuto" style="margin:0;"/> Auto-refresh
              </label>
            </div>
            <pre class="console" style="max-height: 280px;" x-text="traceText || 'No trace entries yet.'"></pre>
          </div>

          <div class="settings-card" style="margin-bottom: 10px;">
            <div style="font-family: var(--mono); font-size:10px; color: var(--text3); letter-spacing: 1.5px; text-transform: uppercase; margin-bottom:8px;">Task log</div>
            <div style="display:flex; gap:8px; margin-bottom:10px;">
              <button class="settings-action" @click="loadLogs()">Refresh</button>
            </div>
            <pre class="console" x-text="logsText || 'No task events yet.'"></pre>
          </div>

          <div class="settings-card">
            <div style="font-family: var(--mono); font-size:10px; color: var(--text3); letter-spacing: 1.5px; text-transform: uppercase; margin-bottom:8px;">LLM usage</div>
            <div style="display:flex; gap:8px; margin-bottom:10px;">
              <button class="settings-action" @click="loadUsage()">Refresh usage</button>
            </div>
            <pre class="console" x-text="usageText || 'No usage recorded yet.'"></pre>
          </div>
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

    get traceAutoEffect() {
      if (this.traceAuto && !this._traceTimer) {
        this._traceTimer = setInterval(() => this.loadTrace(), 2000);
      } else if (!this.traceAuto && this._traceTimer) {
        clearInterval(this._traceTimer); this._traceTimer = null;
      }
      return this.traceAuto;
    },

    authUser: '', authPass: '', authErr: '', authBusy: false,

    messages:  [],
    status:    { state: 'ready', project: '' },
    input:     '',
    connected: false,
    _es:       null,

    docs:       [],
    jobs:       [],
    logsText:   '',
    usageText:  '',
    traceText:  '',
    traceAuto:  false,
    _traceTimer: null,
    uploading:  false,

    projects:     [],
    newProj:      { name: '', repo_url: '', github_token: '', activate: true },
    builderState: { running: null, queue: [], recent: [], updated: '' },
    planTree:     { empty: true, plan: null, phases: [], repo_connected: false },
    pendingPRs:   {}, // job_id -> message ref, so older cards stay actionable
    askBusy:      false,
    failureOpen:  false,
    showLogs:     false,

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
      } catch { this.screen = 'login'; }
    },

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
      this.messages = []; this.docs = []; this.jobs = []; this.connected = false;
      this.input = ''; this.authUser = ''; this.authPass = '';
      this.view = 'chat'; this.screen = 'login';
    },

    connectSSE() {
      if (this._es) this._es.close();
      const es = new EventSource('/events');
      this._es = es;

      es.addEventListener('status', e => {
        try {
          this.status = JSON.parse(e.data);
          if (this.view === 'settings') this.loadTrace();
        } catch {}
      });

      es.addEventListener('msg', e => {
        try {
          const m = JSON.parse(e.data);
          if (m.type === 'builder_status') return;
          if (m.type === 'pr_review') {
            // Track every pending PR card by job_id so older cards remain
            // actionable when reviews stack up.
            m.resolved = '';
            if (m.job_id) this.pendingPRs[m.job_id] = m;
            this.push(m);
            return;
          }
          this.push(m);
        } catch {}
      });

      es.addEventListener('history_end', () => { this.scrollBottom(); });

      es.addEventListener('builder_state', e => {
        try {
          this.builderState = JSON.parse(e.data);
        } catch {}
      });

      es.addEventListener('ask_done', () => {
        if (this.view === 'settings') this.loadTrace();
      });

      es.onopen = () => { this.connected = true; };
      es.onerror = () => {
        this.connected = false;
        fetch('/api/auth/status').then(r => r.json()).then(d => {
          if (!d.loggedIn) { es.close(); this.screen = 'login'; }
        }).catch(() => {});
      };
    },

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
      return 'agent';
    },
    renderMsg(text) {
      if (!text) return '';
      return DOMPurify.sanitize(marked.parse(text));
    },
    autoResize(el) {
      el.style.height = 'auto';
      const cap = window.innerWidth <= 600 ? 80 : 120;
      el.style.height = Math.min(el.scrollHeight, cap) + 'px';
    },

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

    async togglePause() {
      const url = this.status.state === 'paused' ? '/api/resume' : '/api/pause';
      await fetch(url, { method: 'POST' });
    },

    async cancel() {
      await fetch('/api/cancel', { method: 'POST' });
    },

    async loadSettings() {
      this.loadDocs(); this.loadBuilder(); this.loadLogs(); this.loadUsage(); this.loadTrace(); this.loadProjects();
    },

    async loadBuilderState() {
      try {
        const r = await fetch('/api/builder/state');
        if (!r.ok) return;
        this.builderState = await r.json();
      } catch {}
    },

    async loadPlanTree() {
      try {
        const r = await fetch('/api/plan/tree');
        if (!r.ok) return;
        this.planTree = await r.json();
      } catch {}
    },

    canStop() {
      const s = (this.status.state || '').toLowerCase();
      const a = (this.status.agent || '').toLowerCase();
      return s === 'thinking' || a === 'manager' || a === 'builder';
    },

    hasPendingPR() {
      for (const k in this.pendingPRs) {
        if (this.pendingPRs[k] && !this.pendingPRs[k].resolved) return true;
      }
      return false;
    },

    composerLocked() {
      // Lock chat input when the agent is mid-thought, paused, or waiting
      // for a yes/no on a PR (use the PR card buttons instead).
      // EXCEPTION: if the manager is asking a free-form question (ask_active),
      // the input must stay open so the user can type their reply — even
      // though state is technically still "thinking".
      if (this.hasPendingPR()) return true;
      if (this.status.ask_active) return false;
      const s = (this.status.state || '').toLowerCase();
      return s === 'thinking' || s === 'paused';
    },

    composerPlaceholder() {
      if (this.status.ask_active) return 'Type your reply (e.g. yes / no)…';
      return 'Ask Kaptaan anything…';
    },

    lockMessage() {
      if (this.hasPendingPR()) return 'Use the Approve / Reject buttons above to continue.';
      const s = (this.status.state || '').toLowerCase();
      if (s === 'paused') return 'Paused — press Resume to continue.';
      if (s === 'thinking') {
        const a = (this.status.agent || '').toLowerCase();
        if (a === 'builder') return 'Builder is working — press Stop to interrupt.';
        return 'Manager is thinking — press Stop to interrupt.';
      }
      return 'Waiting…';
    },

    async answerPR(m, choice) {
      if (this.askBusy || m.resolved) return;
      this.askBusy = true;
      try {
        const r = await fetch('/api/reply', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ text: choice }),
        });
        if (r.ok) {
          m.resolved = choice;
          if (m.job_id && this.pendingPRs[m.job_id]) delete this.pendingPRs[m.job_id];
          // Echo the user's choice into the chat as a normal reply bubble.
          this.push({ type: 'reply', text: choice, ts: new Date().toLocaleTimeString() });
        }
      } catch {}
      finally { this.askBusy = false; }
    },

    async loadProjects() {
      try {
        const r = await fetch('/api/projects');
        if (!r.ok) return;
        const d = await r.json();
        this.projects = d.projects || [];
      } catch {}
    },

    async addProject() {
      const name = (this.newProj.name || '').trim();
      if (!name) return;
      const r = await fetch('/api/projects', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name,
          repo_url: (this.newProj.repo_url || '').trim(),
          github_token: (this.newProj.github_token || '').trim(),
          activate: !!this.newProj.activate,
        }),
      });
      if (r.ok) {
        this.newProj = { name: '', repo_url: '', github_token: '', activate: true };
        await this.loadProjects();
      } else {
        const d = await r.json().catch(() => ({}));
        alert(d.error || 'Could not create project');
      }
    },

    async activateProject(p) {
      const r = await fetch('/api/projects/' + p.id + '/activate', { method: 'POST' });
      if (r.ok) {
        this.messages = [];
        await this.loadProjects();
        await this.loadBuilderState();
      }
    },

    async editProject(p) {
      const repo = prompt('GitHub repo (owner/name) for "' + p.name + '":', p.repo_url || '');
      if (repo === null) return;
      const tokenIn = prompt('GitHub token (leave blank to keep existing, "-" to clear):', '');
      if (tokenIn === null) return;
      const body = { name: p.name, repo_url: repo.trim() };
      if (tokenIn.trim() !== '') {
        body.github_token = tokenIn.trim() === '-' ? '' : tokenIn.trim();
      }
      const r = await fetch('/api/projects/' + p.id, {
        method: 'PATCH', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (r.ok) await this.loadProjects();
      else alert('Update failed');
    },

    async clearProject(p) {
      if (!confirm('Wipe ALL data (chats, plans, jobs, docs) for "' + p.name + '"? Project itself stays.')) return;
      const r = await fetch('/api/projects/' + p.id + '/clear', { method: 'POST' });
      if (r.ok) {
        if (p.active) this.messages = [];
        await this.loadProjects();
        await this.loadBuilderState();
      }
    },

    async deleteProject(p) {
      if (!confirm('Permanently delete project "' + p.name + '" and all its data?')) return;
      const r = await fetch('/api/projects/' + p.id, { method: 'DELETE' });
      if (r.ok) {
        if (p.active) this.messages = [];
        await this.loadProjects();
        await this.loadBuilderState();
      } else {
        const d = await r.json().catch(() => ({}));
        alert(d.error || 'Could not delete project');
      }
    },

    async loadTrace() {
      try {
        const r = await fetch('/api/trace');
        if (!r.ok) return;
        const d = await r.json();
        if (!d.traces || d.traces.length === 0) { this.traceText = ''; return; }
        this.traceText = d.traces
          .map(t => '[' + t.time + '] ' + t.scope + '/' + t.event + (t.detail ? ' — ' + t.detail : ''))
          .join('\n');
      } catch {}
    },

    async loadDocs() {
      try {
        const r = await fetch('/api/docs');
        if (!r.ok) return;
        const d = await r.json();
        this.docs = d.docs || [];
      } catch {}
    },

    async loadBuilder() {
      try {
        const r = await fetch('/api/builder');
        if (!r.ok) return;
        const d = await r.json();
        this.jobs = d.jobs || [];
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
          ? rows.map(u =>
              '  ' + u.provider + '/' + u.model +
              ': in=' + (u.prompt_tokens||0) +
              ' out=' + (u.completion_tokens||0) +
              ' total=' + (u.total_tokens||0) +
              ' calls=' + (u.calls||0)
            ).join('\n')
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

    async deleteDoc(doc) {
      if (!confirm('Delete ' + doc.filename + '?')) return;
      const r = await fetch('/api/docs/' + doc.id, { method: 'DELETE' });
      if (r.ok) { await this.loadDocs(); }
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
